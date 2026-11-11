package ktable

import (
	"net/netip"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable/btree"
)

func testID(n byte) protocol.ID {
	var id protocol.ID

	id[0] = n
	id[19] = 0xff

	return id
}

func testAddr(n byte) netip.AddrPort {
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 0, 2, n}), 6881)
}

func TestPutNodeAndGetClosest(t *testing.T) {
	tbl := New(testID(0))

	id := testID(1)
	addr := testAddr(1)

	if res := tbl.PutNode(id, addr); res != btree.PutAccepted {
		t.Fatalf("put node = %v, want accepted", res)
	}

	nodes := tbl.GetClosestNodes(id)
	if len(nodes) != 1 {
		t.Fatalf("closest nodes = %d, want 1", len(nodes))
	}

	if nodes[0].ID() != id || nodes[0].Addr() != addr {
		t.Fatalf("closest node = %v at %v", nodes[0].ID(), nodes[0].Addr())
	}
}

func TestPutNodeRejectsInvalidAddr(t *testing.T) {
	tbl := New(testID(0))

	if res := tbl.PutNode(testID(1), netip.AddrPort{}); res != btree.PutRejected {
		t.Fatalf("invalid addr accepted: %v", res)
	}
}

func TestDropNodeForgetsAddr(t *testing.T) {
	tbl := New(testID(0))

	id := testID(1)
	addr := testAddr(1)

	tbl.PutNode(id, addr)

	if unknown := tbl.FilterKnownAddrs([]netip.Addr{addr.Addr()}); len(unknown) != 0 {
		t.Fatalf("addr after put reported unknown")
	}

	if !tbl.DropNode(id, nil) {
		t.Fatal("drop node returned false")
	}

	if unknown := tbl.FilterKnownAddrs([]netip.Addr{addr.Addr()}); len(unknown) != 1 {
		t.Fatalf("addr after drop not forgotten")
	}
}

// The hash keyspace is a sample of seen infohashes, not a routing bucket: when
// it is full, the oldest hash makes room so sample_infohashes keeps rotating
// instead of freezing on the first 80 infohashes ever seen.
func TestHashKeyspaceRotates(t *testing.T) {
	tbl := New(testID(0))

	for i := 0; i < hashesK; i++ {
		id := testID(byte(i + 1))

		if res := tbl.PutHash(id, []HashPeer{{Addr: testAddr(byte(i + 1))}}); res == btree.PutRejected {
			t.Fatalf("hash %d rejected before the keyspace was full", i)
		}
	}

	table := tbl.(*table)
	if got := table.hashes.count(); got != hashesK {
		t.Fatalf("hashes = %d, want %d", got, hashesK)
	}

	newest := testID(byte(hashesK + 1))

	if res := tbl.PutHash(newest, []HashPeer{{Addr: testAddr(byte(hashesK + 1))}}); res == btree.PutRejected {
		t.Fatal("new hash rejected when the keyspace was full")
	}

	if got := table.hashes.count(); got != hashesK {
		t.Fatalf("hashes after eviction = %d, want %d", got, hashesK)
	}

	if !tbl.GetHashOrClosestNodes(newest).Found {
		t.Fatal("new hash not stored")
	}

	survivors := 0

	for i := 0; i < hashesK; i++ {
		if tbl.GetHashOrClosestNodes(testID(byte(i + 1))).Found {
			survivors++
		}
	}

	if survivors != hashesK-1 {
		t.Fatalf("%d old hashes survived eviction, want %d", survivors, hashesK-1)
	}

	// The evicted hash's peer must be forgotten too, or FilterKnownAddrs will
	// keep suppressing addresses we no longer have hashes for.
	addrs := make([]netip.Addr, 0, hashesK+1)
	for i := 0; i <= hashesK; i++ {
		addrs = append(addrs, testAddr(byte(i+1)).Addr())
	}

	if unknown := tbl.FilterKnownAddrs(addrs); len(unknown) != 1 {
		t.Fatalf("unknown addrs = %d, want exactly the evicted hash's peer", len(unknown))
	}
}

func TestSampleHashesAndNodes(t *testing.T) {
	tbl := New(testID(0))

	for i := 0; i < 10; i++ {
		tbl.PutHash(testID(byte(i+1)), []HashPeer{{Addr: testAddr(byte(i + 1))}})
	}

	sample := tbl.SampleHashesAndNodes()
	if sample.TotalHashes != 10 {
		t.Fatalf("total hashes = %d, want 10", sample.TotalHashes)
	}

	if len(sample.Hashes) != 10 {
		t.Fatalf("sampled %d hashes, want 10", len(sample.Hashes))
	}
}

func TestBatchCommand(t *testing.T) {
	tbl := New(testID(0))

	tbl.BatchCommand(
		PutNode{ID: testID(1), Addr: testAddr(1)},
		PutHash{ID: testID(2), Peers: []HashPeer{{Addr: testAddr(2)}}},
	)

	if len(tbl.GetClosestNodes(testID(1))) != 1 {
		t.Fatal("batch put node not applied")
	}

	if !tbl.GetHashOrClosestNodes(testID(2)).Found {
		t.Fatal("batch put hash not applied")
	}
}

// A node returned by a query is an immutable snapshot of the table at that
// moment: a later update must not change what the caller is holding, and it
// must not race with the caller reading it.
func TestNodeQueriesReturnSnapshots(t *testing.T) {
	tbl := New(testID(0))

	id := testID(1)
	firstAddr := testAddr(1)
	secondAddr := testAddr(2)

	tbl.PutNode(id, firstAddr)

	nodes := tbl.GetOldestNodes(time.Now(), 0)
	if len(nodes) != 1 {
		t.Fatalf("oldest nodes = %d, want 1", len(nodes))
	}

	tbl.PutNode(id, secondAddr, NodeResponded())

	if got := nodes[0].Addr(); got != firstAddr {
		t.Fatalf("snapshot addr changed to %v, want %v", got, firstAddr)
	}

	if got := nodes[0].Time(); !got.IsZero() {
		t.Fatalf("snapshot time changed to %v, want zero", got)
	}
}

// The same applies to hashes: the peers a caller holds must not change when the
// table is updated.
func TestHashQueriesReturnSnapshots(t *testing.T) {
	tbl := New(testID(0))

	id := testID(2)
	peer := testAddr(3)

	tbl.PutHash(id, []HashPeer{{Addr: peer}})

	res := tbl.GetHashOrClosestNodes(id)
	if !res.Found {
		t.Fatal("hash not found")
	}

	peers := res.Hash.Peers()
	if len(peers) != 1 {
		t.Fatalf("peers = %d, want 1", len(peers))
	}

	tbl.PutHash(id, []HashPeer{{Addr: testAddr(4)}})

	if got := res.Hash.Peers(); len(got) != 1 {
		t.Fatalf("snapshot peers = %d, want 1", len(got))
	}

	if got := res.Hash.Peers()[0].Addr; got != peer {
		t.Fatalf("snapshot peer changed to %v, want %v", got, peer)
	}
}
