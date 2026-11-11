package responder

import (
	"context"

	"github.com/BitTrawler/bittrawler/internal/protocol/dht"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
)

// nodeDiscovery feeds nodes that successfully query us back to the crawler, so
// the routing table learns about the network from inbound traffic as well.
//
// The send is non-blocking: a full sink drops the node instead of stalling the
// response path or leaking a goroutine per request.
type nodeDiscovery struct {
	responder Responder
	sink      chan<- ktable.Node
}

func (r nodeDiscovery) Respond(ctx context.Context, msg dht.RecvMsg) (dht.Return, error) {
	ret, err := r.responder.Respond(ctx, msg)
	if err != nil || r.sink == nil || msg.Msg.A == nil {
		return ret, err
	}

	select {
	case r.sink <- ktable.NewNode(msg.Msg.A.ID, msg.From):
	default:
	}

	return ret, err
}
