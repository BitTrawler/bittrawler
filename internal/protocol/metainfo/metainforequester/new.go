package metainforequester

import (
	"log/slog"
	"net"
	"time"

	"github.com/BitTrawler/bittrawler/internal/concurrency"
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"golang.org/x/time/rate"
)

// New returns a requester that fetches metadata from peers over BEP 9/10.
// Requests to the same peer are rate limited.
func New(cfg Config, logger *slog.Logger) Requester {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 6 * time.Second
	}

	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 3 * time.Second
	}

	if cfg.PerNodeRate <= 0 {
		cfg.PerNodeRate = 2
	}

	if cfg.PerNodeBurst <= 0 {
		cfg.PerNodeBurst = 4
	}

	if cfg.LimiterSize <= 0 {
		cfg.LimiterSize = 1000
	}

	if cfg.LimiterTTL <= 0 {
		cfg.LimiterTTL = 20 * time.Second
	}

	var r Requester = requester{
		clientID: protocol.RandomPeerID(),
		timeout:  cfg.RequestTimeout,
		dialer: &net.Dialer{
			Timeout:   cfg.DialTimeout,
			KeepAlive: -1,
		},
	}

	r = requestLogger{requester: r, logger: logger}
	r = requestLimiter{
		requester: r,
		limiter: concurrency.NewKeyedLimiter(
			rate.Limit(cfg.PerNodeRate), cfg.PerNodeBurst, cfg.LimiterSize, cfg.LimiterTTL,
		),
	}

	return r
}
