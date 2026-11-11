package responder

import (
	"context"
	"log/slog"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol/dht"
)

type responderLogger struct {
	responder Responder
	logger    *slog.Logger
}

func (r responderLogger) Respond(ctx context.Context, msg dht.RecvMsg) (dht.Return, error) {
	start := time.Now()
	ret, err := r.responder.Respond(ctx, msg)

	message := msg.Msg.Q
	attrs := []any{"from", msg.From, "duration", time.Since(start)}

	if err == nil {
		switch msg.Msg.Q {
		case dht.QFindNode:
			if msg.Msg.A != nil {
				attrs = append(attrs, "target", msg.Msg.A.Target)
			}

			attrs = append(attrs, "nodes", len(ret.Nodes))
		case dht.QGetPeers:
			attrs = append(attrs, "values", len(ret.Values), "nodes", len(ret.Nodes), "token", ret.Token)
		case dht.QAnnouncePeer:
			if msg.Msg.A != nil {
				attrs = append(attrs, "infoHash", msg.Msg.A.InfoHash, "token", msg.Msg.A.Token)
			}

			attrs = append(attrs, "port", msg.AnnouncePort())
		case dht.QSampleInfohashes:
			if ret.Samples != nil {
				attrs = append(attrs, "samples", len(*ret.Samples))
			}

			if ret.Num != nil {
				attrs = append(attrs, "num", *ret.Num)
			}

			if ret.Interval != nil {
				attrs = append(attrs, "interval", *ret.Interval)
			}

			attrs = append(attrs, "nodes", len(ret.Nodes))
		}
	} else {
		message += " error"
		attrs = append(attrs, "error", err)
	}

	r.logger.Debug(message, attrs...)

	return ret, err
}
