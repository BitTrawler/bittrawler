package app

import (
	"strings"
	"sync"

	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/tmdb"
)

// tmdbState holds the one TMDB client the process uses. The crawler and the
// torrent page share it, so they share its rate limit and its cache, and the
// cache survives a crawl restart. A settings change is picked up the next time
// either of them asks.
type tmdbState struct {
	mu     sync.Mutex
	config tmdb.Config
	client *tmdb.Client
	built  bool
}

// clientFor returns the client for cfg, rebuilding it only when cfg changes. It
// returns nil when no token is configured.
func (s *tmdbState) clientFor(cfg tmdb.Config) (*tmdb.Client, error) {
	cfg.Token = strings.TrimSpace(cfg.Token)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.built &&
		s.config.Token == cfg.Token &&
		s.config.Timeout == cfg.Timeout &&
		s.config.RateLimit == cfg.RateLimit {
		return s.client, nil
	}

	if cfg.Token == "" {
		s.config, s.client, s.built = cfg, nil, true

		return nil, nil
	}

	client, err := tmdb.New(cfg)
	if err != nil {
		return nil, err
	}

	s.config, s.client, s.built = cfg, client, true

	return client, nil
}

// tmdbClient returns the shared TMDB client, or nil when TMDB is not configured
// or its settings cannot be used.
func (a *App) tmdbClient() *tmdb.Client {
	client, err := a.tmdb.clientFor(tmdbConfig(a.currentConfig()))
	if err != nil {
		a.logger.Warn("TMDB confirmation is disabled", "error", err)

		return nil
	}

	return client
}

// tmdbConfig is the part of the application settings the TMDB client needs.
func tmdbConfig(cfg config.Config) tmdb.Config {
	return tmdb.Config{
		Token:     cfg.TMDB.Token,
		Timeout:   cfg.TMDB.Timeout,
		RateLimit: cfg.TMDB.RateLimit,
	}
}
