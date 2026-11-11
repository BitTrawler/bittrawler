package ktable

import (
	"net/netip"
	"time"
)

const (
	nodesK  = 80
	hashesK = 80
)

// New returns an empty routing table for the given local node ID.
func New(origin ID) Table {
	rm := &reverseMap{addrs: make(map[string]*infoForAddr)}

	nodes := nodeKeyspace{
		keyspace: newKeyspace[netip.AddrPort, NodeOption, Node, *node](
			origin,
			nodesK,
			func(id ID, addr netip.AddrPort) *node {
				rm.putAddrPeerID(addr.Addr(), id)

				return &node{
					nodeBase:     nodeBase{id: id, addr: addr},
					discoveredAt: time.Now(),
					reverseMap:   rm,
				}
			},
		),
	}

	hashes := hashKeyspace{
		keyspace: newKeyspace[[]HashPeer, HashOption, Hash, *hash](
			origin,
			hashesK,
			func(id ID, peers []HashPeer) *hash {
				peersMap := make(map[string]HashPeer, len(peers))

				for _, p := range peers {
					peersMap[p.Addr.Addr().String()] = p
					rm.putAddrHashes(p.Addr.Addr(), id)
				}

				return &hash{
					id:           id,
					peers:        peersMap,
					discoveredAt: time.Now(),
					reverseMap:   rm,
				}
			},
		),
	}

	return &table{
		origin:  origin,
		nodesK:  nodesK,
		hashesK: hashesK,
		nodes:   nodes,
		hashes:  hashes,
		addrs:   rm,
	}
}
