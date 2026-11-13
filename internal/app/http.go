package app

import (
	"encoding/json"
	"net/http"

	"github.com/BitTrawler/bittrawler/internal/store"
	"github.com/BitTrawler/bittrawler/internal/web"
)

// routes builds the HTTP surface. The Torznab endpoint authenticates with its
// own API key; the pages authenticate with the session cookie, except for the
// setup page, the sign-in page and the health check.
func (a *App) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/torznab", a.serveTorznab)
	mux.HandleFunc("/torznab/api", a.serveTorznab)
	mux.HandleFunc("GET /healthz", a.handleHealth)
	mux.HandleFunc("GET /version", a.handleVersion)
	mux.Handle("GET /static/", staticHandler())
	mux.HandleFunc("GET /setup", a.handleSetupForm)
	mux.HandleFunc("POST /setup", a.handleSetupSubmit)
	mux.HandleFunc("GET /login", a.handleLoginForm)
	mux.HandleFunc("POST /login", a.handleLoginSubmit)
	mux.HandleFunc("POST /logout", a.handleLogout)
	mux.HandleFunc("GET /settings", a.handleSettings)
	mux.HandleFunc("POST /settings", a.handleSettingsSave)
	mux.HandleFunc("POST /settings/torznab/rotate", a.handleRotateAPIKey)
	mux.HandleFunc("POST /settings/admin", a.handleAdminSave)
	mux.HandleFunc("POST /crawl", a.handleCrawl)
	mux.HandleFunc("GET /{$}", a.handleHome)

	return mux
}

// serveTorznab builds the endpoint from the current settings for every request,
// so rotating the API key in the settings page applies immediately.
func (a *App) serveTorznab(w http.ResponseWriter, r *http.Request) {
	endpoint, err := a.torznabHandler()
	if err != nil {
		a.logger.Error("Torznab endpoint", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	endpoint.ServeHTTP(w, r)
}

// staticHandler serves the embedded stylesheet and icons. The file server needs
// the mount prefix removed from the path.
func staticHandler() http.Handler {
	files := http.StripPrefix("/static/", http.FileServerFS(web.Static()))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}

// handleHome renders the dashboard.
func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	status := a.Status()
	cfg := a.currentConfig()

	data := web.DashboardData{
		Shell:           a.shell("dashboard", "Overview"),
		Commit:          status.Commit,
		BuildDate:       status.BuildDate,
		APIKey:          status.TorznabAPIKey,
		TorznabURL:      a.torznabURL(r),
		DHTAddress:      status.DHTAddress,
		DataDirectory:   a.dataDir,
		RetentionMonths: cfg.Store.RetentionMonths,
		CrawlEnabled:    cfg.Crawler.Enabled,
	}

	a.renderPage(w, "dashboard", data)
}

// handleHealth is the liveness and readiness endpoint. It does no database
// work, so a container health check can call it often.
func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := a.Status()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"version":        status.Version,
		"uptime_seconds": int64(status.Uptime.Seconds()),
		"configured":     status.Configured,
		"crawling":       status.Crawling,
	})
}

// handleVersion reports the build and the store schema version.
func (a *App) handleVersion(w http.ResponseWriter, r *http.Request) {
	schema, err := store.SchemaVersion()
	if err != nil {
		schema = 0
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"version":      a.info.Version,
		"commit":       a.info.Commit,
		"build_date":   a.info.Date,
		"store_schema": schema,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}

// renderPage renders a page and turns a template error into a 500.
func (a *App) renderPage(w http.ResponseWriter, name string, data any) {
	if err := a.pages.Render(w, http.StatusOK, name, data); err != nil {
		a.logger.Error("render page", "page", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
