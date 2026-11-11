package responder

import (
	"log/slog"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
	"golang.org/x/time/rate"
)

// Config configures the DHT query responder.
type Config struct {
	KTable ktable.Table
	Logger *slog.Logger
	// DiscoveredNodes receives nodes that query us successfully, for the
	// crawler to add to the routing table. It may be nil.
	DiscoveredNodes chan<- ktable.Node
	// SampleInfoHashesInterval is the BEP 51 interval, in seconds, that we
	// advertise to other nodes.
	SampleInfoHashesInterval int64
	// TokenSecret keys the announce_peer tokens. A random one is used when nil.
	TokenSecret []byte

	OverallRate  rate.Limit
	OverallBurst int
	PerIPRate    rate.Limit
	PerIPBurst   int
	PerIPSize    int
	PerIPTTL     time.Duration
}

// New returns a responder that answers KRPC queries from the routing table,
// rate limited overall and per source address.
func New(cfg Config) Responder {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}

	if cfg.SampleInfoHashesInterval <= 0 {
		cfg.SampleInfoHashesInterval = 10
	}

	if cfg.TokenSecret == nil {
		cfg.TokenSecret = protocol.RandomNodeID().Bytes()
	}

	if cfg.OverallRate == 0 {
		cfg.OverallRate = rate.Every(time.Second / 50)
	}

	if cfg.OverallBurst == 0 {
		cfg.OverallBurst = 20
	}

	if cfg.PerIPRate == 0 {
		cfg.PerIPRate = rate.Every(time.Second)
	}

	if cfg.PerIPBurst == 0 {
		cfg.PerIPBurst = 10
	}

	if cfg.PerIPSize == 0 {
		cfg.PerIPSize = 1000
	}

	if cfg.PerIPTTL == 0 {
		cfg.PerIPTTL = 20 * time.Second
	}

	var r Responder = responder{
		nodeID:                   cfg.KTable.Origin(),
		kTable:                   cfg.KTable,
		tokenSecret:              cfg.TokenSecret,
		sampleInfoHashesInterval: cfg.SampleInfoHashesInterval,
	}

	r = responderLimiter{
		responder: r,
		limiter: NewLimiter(
			cfg.OverallRate, cfg.OverallBurst,
			cfg.PerIPRate, cfg.PerIPBurst, cfg.PerIPSize, cfg.PerIPTTL,
		),
	}

	r = responderLogger{responder: r, logger: cfg.Logger}
	r = nodeDiscovery{responder: r, sink: cfg.DiscoveredNodes}

	return r
}
