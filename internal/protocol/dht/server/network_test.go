package server_test

import (
	"log/slog"
	"math"
	"net/netip"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/client"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/responder"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/server"
	"golang.org/x/time/rate"
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

type node struct {
	id    protocol.ID
	srv   server.Server
	table ktable.Table
}

func newNode(t *testing.T, seed byte) node {
	t.Helper()

	table := ktable.New(testID(seed))
	discard := slog.New(slog.DiscardHandler)

	r := responder.New(responder.Config{
		KTable:       table,
		Logger:       discard,
		OverallRate:  rate.Inf,
		OverallBurst: math.MaxInt32,
		PerIPRate:    rate.Inf,
		PerIPBurst:   math.MaxInt32,
		TokenSecret:  []byte("test-secret"),
	})

	cfg := server.NewDefaultConfig()
	cfg.ListenAddr = netip.MustParseAddrPort("127.0.0.1:0")
	cfg.Logger = discard
	cfg.QueryTimeout = 2 * time.Second
	cfg.OutboundRate = rate.Inf
	cfg.OutboundBurst = math.MaxInt32

	srv, err := server.New(cfg, r)
	if err != nil {
		t.Fatalf("new node: %v", err)
	}

	t.Cleanup(func() { _ = srv.Close() })

	return node{id: testID(seed), srv: srv, table: table}
}

func TestPingBetweenNodes(t *testing.T) {
	a := newNode(t, 1)
	b := newNode(t, 2)

	got, err := client.New(a.id, a.srv).Ping(t.Context(), b.srv.LocalAddr())
	if err != nil {
		t.Fatalf("ping: %v", err)
	}

	if got.ID != b.id {
		t.Fatalf("ping id = %x, want %x", got.ID, b.id)
	}
}

func TestFindNodeBetweenNodes(t *testing.T) {
	a := newNode(t, 1)
	b := newNode(t, 2)

	b.table.PutNode(testID(3), testAddr(3))

	res, err := client.New(a.id, a.srv).FindNode(t.Context(), b.srv.LocalAddr(), testID(99))
	if err != nil {
		t.Fatalf("find_node: %v", err)
	}

	if len(res.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(res.Nodes))
	}

	if res.Nodes[0].ID != testID(3) || res.Nodes[0].Addr != testAddr(3) {
		t.Fatalf("node = %v at %v", res.Nodes[0].ID, res.Nodes[0].Addr)
	}
}

func TestGetPeersBetweenNodes(t *testing.T) {
	a := newNode(t, 1)
	b := newNode(t, 2)

	hash := testID(9)
	b.table.PutHash(hash, []ktable.HashPeer{{Addr: testAddr(4)}})

	res, err := client.New(a.id, a.srv).GetPeers(t.Context(), b.srv.LocalAddr(), hash)
	if err != nil {
		t.Fatalf("get_peers: %v", err)
	}

	if len(res.Values) != 1 || res.Values[0] != testAddr(4) {
		t.Fatalf("values = %v, want the stored peer", res.Values)
	}
}

func TestSampleInfoHashesBetweenNodes(t *testing.T) {
	a := newNode(t, 1)
	b := newNode(t, 2)

	for i := 0; i < 3; i++ {
		b.table.PutHash(testID(byte(20+i)), []ktable.HashPeer{{Addr: testAddr(byte(20 + i))}})
	}

	res, err := client.New(a.id, a.srv).SampleInfoHashes(t.Context(), b.srv.LocalAddr(), testID(50))
	if err != nil {
		t.Fatalf("sample_infohashes: %v", err)
	}

	if res.Num != 3 {
		t.Fatalf("num = %d, want 3", res.Num)
	}

	if len(res.Samples) != 3 {
		t.Fatalf("samples = %d, want 3", len(res.Samples))
	}

	if res.Interval != 10 {
		t.Fatalf("interval = %d, want 10", res.Interval)
	}
}

func TestErrorResponseBecomesError(t *testing.T) {
	a := newNode(t, 1)
	b := newNode(t, 2)

	// announce_peer without the token from get_peers: the responder returns a
	// protocol error, which the client must surface.
	res := dht.MsgArgs{
		ID:       a.id,
		InfoHash: testID(9),
		Token:    "bogus",
	}

	_, err := a.srv.Query(t.Context(), b.srv.LocalAddr(), dht.QAnnouncePeer, res)
	if err == nil {
		t.Fatal("announce_peer with a bogus token returned no error")
	}
}

func TestQueryToStoppedNodeTimesOut(t *testing.T) {
	a := newNode(t, 1)
	b := newNode(t, 2)

	addr := b.srv.LocalAddr()

	if err := b.srv.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := a.srv.Query(t.Context(), addr, dht.QPing, dht.MsgArgs{ID: a.id}); err == nil {
		t.Fatal("query to a stopped node returned no error")
	}
}
