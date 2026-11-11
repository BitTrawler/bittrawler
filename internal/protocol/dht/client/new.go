package client

import (
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/server"
)

// New returns a DHT client that sends its queries through the given server.
func New(nodeID protocol.ID, srv server.Server) Client {
	return serverAdapter{nodeID: nodeID, server: srv}
}
