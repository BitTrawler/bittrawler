package ktable

import (
	"net/netip"

	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable/btree"
)

type Command interface {
	exec(*table)
}

type CommandReturn[T any] interface {
	Command
	Query[T]
}

var _ CommandReturn[btree.PutResult] = PutNode{}

type PutNode struct {
	ID      ID
	Addr    netip.AddrPort
	Options []NodeOption
}

func (c PutNode) execReturn(t *table) btree.PutResult {
	if !c.Addr.IsValid() {
		return btree.PutRejected
	}

	return t.nodes.put(c.ID, c.Addr, c.Options...)
}

func (c PutNode) exec(t *table) {
	c.execReturn(t)
}

var _ CommandReturn[bool] = DropNode{}

type DropNode struct {
	ID     ID
	Reason error
}

func (c DropNode) execReturn(t *table) bool {
	return t.nodes.drop(c.ID, c.Reason)
}

func (c DropNode) exec(t *table) {
	c.execReturn(t)
}

var _ CommandReturn[bool] = DropAddr{}

type DropAddr struct {
	Addr   netip.Addr
	Reason error
}

func (c DropAddr) execReturn(t *table) bool {
	id, ok := t.addrs.getPeerIDForAddr(c.Addr)
	if !ok {
		return false
	}

	return t.nodes.drop(id, c.Reason)
}

func (c DropAddr) exec(t *table) {
	c.execReturn(t)
}

var _ CommandReturn[btree.PutResult] = PutHash{}

type PutHash struct {
	ID      ID
	Peers   []HashPeer
	Options []HashOption
}

func (c PutHash) execReturn(t *table) btree.PutResult {
	// The hash space is a bounded sample of recently seen infohashes, not a
	// Kademlia bucket. Keeping the total at hashesK also guarantees the btree
	// accepts the new hash, because no bucket can be fuller than the whole.
	if _, exists := t.hashes.get(c.ID); !exists {
		for t.hashes.count() >= t.hashesK {
			if !t.hashes.dropOldest(ErrHashKeyspaceFull) {
				break
			}
		}
	}

	return t.hashes.put(c.ID, c.Peers, c.Options...)
}

func (c PutHash) exec(t *table) {
	c.execReturn(t)
}
