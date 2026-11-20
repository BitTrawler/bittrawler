package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofrs/flock"
	"golang.org/x/time/rate"

	"github.com/BitTrawler/bittrawler/internal/blocking"
	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/crawlstate"
	"github.com/BitTrawler/bittrawler/internal/dhtcrawler"
	"github.com/BitTrawler/bittrawler/internal/drops"
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/responder"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/server"
	"github.com/BitTrawler/bittrawler/internal/settings"
	"github.com/BitTrawler/bittrawler/internal/store"
	"github.com/BitTrawler/bittrawler/internal/torznab"
	"github.com/BitTrawler/bittrawler/internal/version"
	"github.com/BitTrawler/bittrawler/internal/web"
)

// App is a BitTrawler instance: its databases, its DHT node, its crawler, its
// HTTP server and the retention timer, with one lifecycle.
type App struct {
	logger *slog.Logger
	info   version.Info

	configMu sync.RWMutex
	loaded   config.Loaded

	dataDir string

	lock          *flock.Flock
	settingsStore *settings.Store
	torrentStore  *store.Store
	blocklist     *blocking.Blocklist
	crawlState    *crawlstate.Store
	classifier    *classify.Live
	rejected      *drops.Recorder
	dht           server.Server
	table         ktable.Table
	nodeID        protocol.ID
	discovered    chan ktable.Node

	pages      *web.Pages
	listener   net.Listener
	httpServer *http.Server

	ctx     context.Context
	cancel  context.CancelFunc
	started time.Time
	wg      sync.WaitGroup
	errCh   chan error
	errOnce sync.Once

	lifecycleMu sync.Mutex
	running     bool
	stopped     bool

	auth    atomic.Pointer[authState]
	limiter *loginLimiter

	crawler atomic.Pointer[dhtcrawler.Crawler]
	stats   statsCache
	tmdb    tmdbState

	reclassify reclassifyState

	crawlMu      sync.Mutex
	crawlCancel  context.CancelFunc
	crawlDone    chan struct{}
	crawlRunning bool
	crawlErr     error
}

// New prepares an instance: it opens the settings database, computes the
// effective settings, generates the secrets a new installation needs, opens the
// store and the blocklist, binds the DHT socket and the HTTP listener, and
// builds the routing table. It starts no goroutines; call Start.
//
// A new installation has no administrator account. In that state the instance
// serves the setup page and does not crawl until setup is finished.
func New(ctx context.Context, overrides config.Overrides, info version.Info, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	dataDir, err := filepath.Abs(config.DataDirectory(overrides))
	if err != nil {
		return nil, fmt.Errorf("app: resolve data directory: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("app: create data directory: %w", err)
	}

	a := &App{
		logger:     logger,
		info:       info,
		dataDir:    dataDir,
		discovered: make(chan ktable.Node, 1024),
		errCh:      make(chan error, 1),
		limiter:    newLoginLimiter(),
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = a.closeResources()
		}
	}()

	lock, err := lockDataDirectory(dataDir)
	if err != nil {
		return nil, err
	}

	a.lock = lock

	a.settingsStore, err = settings.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}

	values, err := a.settingsStore.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}

	a.loaded, err = config.Load(values, overrides)
	if err != nil {
		return nil, err
	}

	if err := a.ensureSecrets(ctx); err != nil {
		return nil, err
	}

	a.refreshAuth()

	// The classifier and the rejection record outlive any one crawl: the drop
	// rules can change while the crawler runs, and the overview keeps its feed
	// when the crawl is paused and resumed.
	a.classifier = classify.NewLive(classifyConfig(a.loaded.Config))
	a.rejected = drops.NewRecorder(drops.DefaultCapacity)

	a.pages, err = web.New()
	if err != nil {
		return nil, err
	}

	a.torrentStore, err = store.Open(store.Config{
		Dir:             dataDir,
		RetentionMonths: a.loaded.Config.Store.RetentionMonths,
	})
	if err != nil {
		return nil, fmt.Errorf("app: open store: %w", err)
	}

	a.blocklist, err = blocking.Open(blocking.Config{Dir: dataDir})
	if err != nil {
		return nil, fmt.Errorf("app: open blocklist: %w", err)
	}

	a.crawlState, err = crawlstate.Open(dataDir)
	if err != nil {
		return nil, fmt.Errorf("app: open crawl state: %w", err)
	}

	a.nodeID = protocol.RandomNodeID()
	a.table = ktable.New(a.nodeID)

	if err := a.openDHT(); err != nil {
		return nil, err
	}

	a.listener, err = net.Listen("tcp", a.loaded.Config.Web.Address)
	if err != nil {
		return nil, fmt.Errorf("app: listen on %s: %w", a.loaded.Config.Web.Address, err)
	}

	a.httpServer = &http.Server{
		Handler:           a.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	cleanup = false

	return a, nil
}

// openDHT binds the DHT socket and starts its read loop.
func (a *App) openDHT() error {
	address, err := netip.ParseAddrPort(a.loaded.Config.DHT.ListenAddress)
	if err != nil {
		return fmt.Errorf("app: DHT listen address: %w", err)
	}

	dht := a.loaded.Config.DHT

	a.dht, err = server.New(server.Config{
		ListenAddr:       address,
		QueryTimeout:     dht.QueryTimeout,
		ResponderTimeout: dht.ResponderTimeout,
		Logger:           a.logger.With("component", "dht-server"),
		OutboundRate:     rate.Limit(dht.OutboundRate),
		OutboundBurst:    dht.OutboundBurst,
	}, responder.New(responder.Config{
		KTable:                   a.table,
		Logger:                   a.logger.With("component", "dht-responder"),
		DiscoveredNodes:          a.discovered,
		SampleInfoHashesInterval: int64(dht.SampleInfoHashesInterval),
		OverallRate:              rate.Limit(dht.OverallQueryRate),
		OverallBurst:             dht.OverallQueryBurst,
		PerIPRate:                rate.Limit(dht.PerIPQueryRate),
		PerIPBurst:               dht.PerIPQueryBurst,
	}))
	if err != nil {
		return fmt.Errorf("app: start DHT server: %w", err)
	}

	return nil
}

// Start begins serving HTTP, runs the retention timer and starts crawling when
// the installation is configured and crawling is enabled.
func (a *App) Start(ctx context.Context) error {
	a.lifecycleMu.Lock()

	if a.running {
		a.lifecycleMu.Unlock()

		return errors.New("app: already started")
	}

	a.running = true
	a.ctx, a.cancel = context.WithCancel(ctx)
	a.started = time.Now()
	a.lifecycleMu.Unlock()

	if a.Configured() && a.currentConfig().Crawler.Enabled {
		if err := a.startCrawl(); err != nil {
			a.logger.Error("crawl did not start", "error", err)
		}
	}

	a.wg.Add(3)

	go func() {
		defer a.wg.Done()

		a.serveHTTP()
	}()

	go func() {
		defer a.wg.Done()

		a.runStats(a.ctx)
	}()

	go func() {
		defer a.wg.Done()

		a.runRetention(a.ctx)
	}()

	logger := a.logger.With(
		"version", a.info.Version,
		"web", a.WebAddress(),
		"dht", a.DHTAddress(),
		"data", a.dataDir,
	)

	if a.Configured() {
		logger.Info("started")
	} else {
		logger.Warn("started without an administrator account; open the web interface to finish setup")
	}

	return nil
}

// Stop shuts the instance down: it stops serving, stops the crawler and waits
// for its last batches to be written, then closes the databases.
func (a *App) Stop(ctx context.Context) error {
	a.lifecycleMu.Lock()

	if a.stopped {
		a.lifecycleMu.Unlock()

		return nil
	}

	a.stopped = true
	cancel := a.cancel

	a.lifecycleMu.Unlock()

	var errs []error

	if a.httpServer != nil {
		if err := a.httpServer.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			errs = append(errs, fmt.Errorf("app: stop HTTP: %w", err))
		}
	}

	if err := a.stopCrawl(ctx); err != nil {
		errs = append(errs, err)
	}

	if cancel != nil {
		cancel()
	}

	a.wg.Wait()

	errs = append(errs, a.closeResources())

	return errors.Join(errs...)
}

// Errors returns the channel that reports a fatal runtime error.
func (a *App) Errors() <-chan error {
	return a.errCh
}

func (a *App) fail(err error) {
	a.errOnce.Do(func() {
		select {
		case a.errCh <- err:
		default:
		}
	})
}

// Configured reports whether an administrator account has been set up.
func (a *App) Configured() bool {
	return a.auth.Load() != nil
}

// Status is a snapshot of the instance for the pages and the health check.
type Status struct {
	Version       string
	Commit        string
	BuildDate     string
	Started       time.Time
	Uptime        time.Duration
	DataDirectory string
	WebAddress    string
	DHTAddress    string
	Crawling      bool
	CrawlEnabled  bool
	Configured    bool
	TorznabAPIKey string
	CrawlError    string
}

// Status returns a snapshot of the instance.
func (a *App) Status() Status {
	cfg := a.currentConfig()

	a.crawlMu.Lock()
	crawling := a.crawlRunning
	crawlErr := a.crawlErr
	a.crawlMu.Unlock()

	return Status{
		Version:       a.info.Version,
		Commit:        a.info.Commit,
		BuildDate:     a.info.Date,
		Started:       a.started,
		Uptime:        time.Since(a.started).Round(time.Second),
		DataDirectory: a.dataDir,
		WebAddress:    a.WebAddress(),
		DHTAddress:    a.DHTAddress(),
		Crawling:      crawling,
		CrawlEnabled:  cfg.Crawler.Enabled,
		Configured:    a.Configured(),
		TorznabAPIKey: cfg.Torznab.APIKey,
		CrawlError:    errorString(crawlErr),
	}
}

// assetVersion is the build stamp the static asset URLs carry.
func (a *App) assetVersion() string {
	return assetStamp(a.info, a.started)
}

// WebAddress returns the address the HTTP server is bound to.
func (a *App) WebAddress() string {
	if a.listener == nil {
		return ""
	}

	return a.listener.Addr().String()
}

// DHTAddress returns the address the DHT socket is bound to.
func (a *App) DHTAddress() string {
	if a.dht == nil {
		return ""
	}

	return a.dht.LocalAddr().String()
}

// SettingValue returns a setting's current value. It exists for the interface
// and for tests that need to observe a saved change.
func (a *App) SettingValue(key string) (any, bool) {
	return config.Get(a.currentConfig(), key)
}

// LogLevel returns the configured log level.
func (a *App) LogLevel() string {
	return a.currentConfig().Log.Level
}

func (a *App) currentConfig() config.Config {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	return a.loaded.Config
}

// setConfig applies a change to the in-memory settings. It does not write the
// settings database; callers do that first, so a failed write leaves memory
// untouched.
func (a *App) setConfig(change func(*config.Config)) {
	a.configMu.Lock()
	defer a.configMu.Unlock()

	cfg := a.loaded.Config
	change(&cfg)

	a.loaded.Config = cfg
}

// replaceConfig installs a validated configuration and records the settings
// that were written to the database.
func (a *App) replaceConfig(cfg config.Config, written []string) {
	a.configMu.Lock()
	defer a.configMu.Unlock()

	a.loaded.Config = cfg

	for _, key := range written {
		a.loaded.Sources[key] = config.SourceDatabase
	}
}

// sourceSnapshot returns where each setting's current value came from.
func (a *App) sourceSnapshot() map[string]config.Source {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	sources := make(map[string]config.Source, len(a.loaded.Sources))
	for key, source := range a.loaded.Sources {
		sources[key] = source
	}

	return sources
}

// ensureSecrets generates the values a new installation needs: the Torznab API
// key and the key that signs login sessions. Values supplied by the environment
// or the database are kept.
func (a *App) ensureSecrets(ctx context.Context) error {
	a.configMu.Lock()
	defer a.configMu.Unlock()

	values := make(map[string]string)

	if a.loaded.Config.Torznab.APIKey == "" {
		key, err := randomHex(16)
		if err != nil {
			return fmt.Errorf("app: generate the Torznab API key: %w", err)
		}

		a.loaded.Config.Torznab.APIKey = key
		a.loaded.Sources["torznab.api_key"] = config.SourceDatabase
		values["torznab.api_key"] = key
	}

	if a.loaded.Config.Web.SessionSecret == "" {
		secret, err := randomHex(32)
		if err != nil {
			return fmt.Errorf("app: generate the session key: %w", err)
		}

		a.loaded.Config.Web.SessionSecret = secret
		a.loaded.Sources["web.session_secret"] = config.SourceDatabase
		values["web.session_secret"] = secret
	}

	if len(values) == 0 {
		return nil
	}

	if err := a.settingsStore.SetMany(ctx, values); err != nil {
		return fmt.Errorf("app: store generated secrets: %w", err)
	}

	return nil
}

// refreshAuth reloads the credentials the HTTP handlers check.
func (a *App) refreshAuth() {
	cfg := a.currentConfig()

	if cfg.Web.AdminUsername == "" || cfg.Web.AdminPasswordHash == "" {
		a.auth.Store(nil)

		return
	}

	a.auth.Store(&authState{
		username: cfg.Web.AdminUsername,
		hash:     cfg.Web.AdminPasswordHash,
		secret:   []byte(cfg.Web.SessionSecret),
	})
}

func (a *App) serveHTTP() {
	if err := a.httpServer.Serve(a.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		a.fail(fmt.Errorf("app: serve: %w", err))
	}
}

// closeResources closes everything New opened, in reverse order. It is safe to
// call more than once.
func (a *App) closeResources() error {
	var errs []error

	if a.listener != nil {
		if err := a.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, fmt.Errorf("app: close HTTP listener: %w", err))
		}

		a.listener = nil
	}

	if a.dht != nil {
		if err := a.dht.Close(); err != nil {
			errs = append(errs, fmt.Errorf("app: close DHT server: %w", err))
		}

		a.dht = nil
	}

	if a.blocklist != nil {
		if err := a.blocklist.Close(); err != nil {
			errs = append(errs, fmt.Errorf("app: close blocklist: %w", err))
		}

		a.blocklist = nil
	}

	if a.crawlState != nil {
		if err := a.crawlState.Close(); err != nil {
			errs = append(errs, fmt.Errorf("app: close crawl state: %w", err))
		}

		a.crawlState = nil
	}

	if a.torrentStore != nil {
		if err := a.torrentStore.Close(); err != nil {
			errs = append(errs, fmt.Errorf("app: close store: %w", err))
		}

		a.torrentStore = nil
	}

	if a.settingsStore != nil {
		if err := a.settingsStore.Close(); err != nil {
			errs = append(errs, fmt.Errorf("app: close settings: %w", err))
		}

		a.settingsStore = nil
	}

	if a.lock != nil {
		if err := a.lock.Unlock(); err != nil {
			errs = append(errs, fmt.Errorf("app: release data directory lock: %w", err))
		}

		a.lock = nil
	}

	return errors.Join(errs...)
}

// randomHex returns n random bytes as hex text.
func randomHex(n int) (string, error) {
	buffer := make([]byte, n)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}

	return hex.EncodeToString(buffer), nil
}

// errorString renders an error for a status page, empty when there is none.
func errorString(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

// torznabHandler builds the endpoint from the current settings.
func (a *App) torznabHandler(r *http.Request) (http.Handler, error) {
	cfg := a.currentConfig()

	return torznab.NewHandler(torznab.Config{
		Title:        cfg.Torznab.Title,
		APIKey:       cfg.Torznab.APIKey,
		DefaultLimit: cfg.Torznab.DefaultLimit,
		MaxLimit:     cfg.Torznab.MaxLimit,
		SiteURL:      a.baseURL(r),
		Searcher:     a.torrentStore,
	})
}

// torznabURL returns the endpoint URL as seen by the browser.
func (a *App) torznabURL(r *http.Request) string {
	return a.baseURL(r) + "/torznab/api"
}

// baseURL reconstructs the URL the browser used, honouring the headers a
// reverse proxy sets.
func (a *App) baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}

	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = forwarded
	}

	return scheme + "://" + host
}
