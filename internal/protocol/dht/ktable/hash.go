package ktable

import (
	"errors"
	"net/netip"
	"time"
)

// ErrHashKeyspaceFull is the reason recorded when the oldest hash makes room
// for a newly discovered one.
var ErrHashKeyspaceFull = errors.New("hash keyspace full")

type hashKeyspace struct {
	keyspace[[]HashPeer, HashOption, Hash, *hash]
}

type Hash interface {
	keyspaceItem
	Peers() []HashPeer
	Dropped() bool
}

type hash struct {
	id           ID
	peers        map[string]HashPeer
	discoveredAt time.Time
	// lastRequestedAt time.Time
	droppedReason error
	reverseMap    *reverseMap
}

type HashPeer struct {
	Addr netip.AddrPort
}

type HashOption interface {
	apply(*hash)
}

var _ keyspaceItemPrivate[[]HashPeer, HashOption, Hash] = (*hash)(nil)

func (h *hash) update(peers []HashPeer) {
	for _, p := range peers {
		h.peers[p.Addr.Addr().String()] = p
		h.reverseMap.putAddrHashes(p.Addr.Addr(), h.id)
	}
}

func (h *hash) apply(option HashOption) {
	option.apply(h)
}

func (h *hash) drop(reason error) {
	h.droppedReason = reason
	for _, peer := range h.peers {
		h.reverseMap.dropHashForAddr(peer.Addr.Addr(), h.id)
	}
}

func (h *hash) public() Hash {
	return h.snapshot()
}

// hashSnapshot is an immutable view of a hash and its peers. Like nodeSnapshot
// it exists so readers cannot race with concurrent updates to the live item.
type hashSnapshot struct {
	id      ID
	peers   []HashPeer
	dropped bool
}

func (h hashSnapshot) ID() ID {
	return h.id
}

func (h hashSnapshot) Peers() []HashPeer {
	return h.peers
}

func (h hashSnapshot) Dropped() bool {
	return h.dropped
}

func (h *hash) snapshot() hashSnapshot {
	peers := make([]HashPeer, 0, len(h.peers))

	for _, peer := range h.peers {
		peers = append(peers, peer)
	}

	return hashSnapshot{id: h.id, peers: peers, dropped: h.droppedReason != nil}
}

// dropOldest evicts the least recently discovered hash, freeing one slot.
func (k *hashKeyspace) dropOldest(reason error) bool {
	var (
		oldest   ID
		oldestAt time.Time
		found    bool
	)

	for id, item := range k.items {
		if !found || item.discoveredAt.Before(oldestAt) {
			oldest, oldestAt, found = id, item.discoveredAt, true
		}
	}

	if !found {
		return false
	}

	return k.drop(oldest, reason)
}

func (h *hash) hasPeers() bool {
	return len(h.peers) > 0
}

func (h *hash) ID() ID {
	return h.id
}

func (h *hash) Peers() []HashPeer {
	peers := make([]HashPeer, 0, len(h.peers))
	for _, p := range h.peers {
		peers = append(peers, p)
	}

	return peers
}

func (h *hash) Dropped() bool {
	return h.droppedReason != nil
}
