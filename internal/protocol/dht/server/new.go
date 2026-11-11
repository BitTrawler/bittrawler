package server

import (
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/BitTrawler/bittrawler/internal/concurrency"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/responder"
	"golang.org/x/time/rate"
)

// New binds the UDP socket and starts the read loop.
func New(cfg Config, r responder.Responder) (Server, error) {
	if r == nil {
		return nil, fmt.Errorf("dht server: a responder is required")
	}

	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}

	if !cfg.ListenAddr.IsValid() {
		cfg.ListenAddr = netip.AddrPortFrom(netip.IPv4Unspecified(), 3334)
	}

	if cfg.QueryTimeout <= 0 {
		cfg.QueryTimeout = 4 * time.Second
	}

	if cfg.ResponderTimeout <= 0 {
		cfg.ResponderTimeout = 5 * time.Second
	}

	if cfg.OutboundRate == 0 {
		cfg.OutboundRate = rate.Every(time.Second)
	}

	if cfg.OutboundBurst == 0 {
		cfg.OutboundBurst = 4
	}

	if cfg.OutboundSize == 0 {
		cfg.OutboundSize = 1000
	}

	if cfg.OutboundTTL == 0 {
		cfg.OutboundTTL = 20 * time.Second
	}

	sock, err := NewSocket(cfg.ListenAddr)
	if err != nil {
		return nil, err
	}

	if err := sock.Open(cfg.ListenAddr); err != nil {
		_ = sock.Close()

		return nil, fmt.Errorf("bind %s: %w", cfg.ListenAddr, err)
	}

	localAddr, err := sock.LocalAddr()
	if err != nil {
		_ = sock.Close()

		return nil, fmt.Errorf("local address: %w", err)
	}

	s := &server{
		cfg:       cfg,
		logger:    cfg.Logger,
		responder: r,
		socket:    sock,
		localAddr: localAddr,
		queries:   make(map[queryKey]chan dht.RecvMsg),
		queryRate: concurrency.NewKeyedLimiter(
			cfg.OutboundRate, cfg.OutboundBurst, cfg.OutboundSize, cfg.OutboundTTL,
		),
		stopped: make(chan struct{}),
	}

	s.wg.Add(1)

	go s.read()

	return s, nil
}
