package metainforequester

import (
	"context"
	"log/slog"
	"net/netip"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
)

type requestLogger struct {
	logger    *slog.Logger
	requester Requester
}

func (r requestLogger) Request(ctx context.Context, infoHash protocol.ID, addr netip.AddrPort) (Response, error) {
	start := time.Now()
	resp, err := r.requester.Request(ctx, infoHash, addr)

	attrs := []any{"infoHash", infoHash, "addr", addr, "duration", time.Since(start)}
	message := "metadata request"

	if err != nil {
		message += " failed"
		attrs = append(attrs, "error", err)
	} else {
		attrs = append(attrs, "peerId", resp.PeerID, "torrentName", resp.Info.BestName())
	}

	r.logger.Debug(message, attrs...)

	return resp, err
}
