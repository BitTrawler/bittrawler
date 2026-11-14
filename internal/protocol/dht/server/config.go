package server

import (
	"log/slog"
	"net/netip"
	"time"

	"golang.org/x/time/rate"
)

// Config configures the DHT UDP server.
type Config struct {
	// ListenAddr is the UDP address to bind. Port 0 selects an ephemeral port.
	ListenAddr netip.AddrPort
	// QueryTimeout bounds how long an outgoing query waits for a response.
	QueryTimeout time.Duration
	// ResponderTimeout bounds how long an incoming query may take to answer.
	ResponderTimeout time.Duration
	// Logger may be nil.
	Logger *slog.Logger
	// Outgoing queries are rate limited per remote IP so a single node is
	// not hammered.
	OutboundRate  rate.Limit
	OutboundBurst int
	OutboundSize  int
	OutboundTTL   time.Duration
}

// NewDefaultConfig returns the DHT server defaults. The per-address outgoing
// rate is bitmagnet's four times over: peer lookups are addressed at the node
// that reported the hash, and the crawl's rate is bounded by how fast those
// nodes answer, not by the socket.
func NewDefaultConfig() Config {
	return Config{
		ListenAddr:       netip.AddrPortFrom(netip.IPv4Unspecified(), 3334),
		QueryTimeout:     4 * time.Second,
		ResponderTimeout: 5 * time.Second,
		OutboundRate:     4,
		OutboundBurst:    8,
		OutboundSize:     1000,
		OutboundTTL:      20 * time.Second,
	}
}
