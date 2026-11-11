package responder

import (
	"context"
	"log/slog"
	"math"
	"net/netip"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
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

func newTestResponder(t *testing.T) (Responder, ktable.Table) {
	t.Helper()

	table := ktable.New(testID(0))
	unlimited := rate.Inf

	return New(Config{
		KTable:                   table,
		Logger:                   slog.New(slog.DiscardHandler),
		OverallRate:              unlimited,
		OverallBurst:             math.MaxInt32,
		PerIPRate:                unlimited,
		PerIPBurst:               math.MaxInt32,
		PerIPSize:                1000,
		PerIPTTL:                 time.Minute,
		TokenSecret:              []byte("test-token-secret"),
		SampleInfoHashesInterval: 10,
	}), table
}

func recv(from netip.AddrPort, q string, args *dht.MsgArgs) dht.RecvMsg {
	return dht.RecvMsg{
		From: from,
		Msg: dht.Msg{
			Q: q,
			A: args,
			T: "aa",
			Y: dht.YQuery,
		},
	}
}

func TestPing(t *testing.T) {
	r, _ := newTestResponder(t)

	ret, err := r.Respond(context.Background(), recv(testAddr(1), dht.QPing, &dht.MsgArgs{ID: testID(1)}))
	if err != nil {
		t.Fatalf("ping: %v", err)
	}

	if ret.ID != testID(0) {
		t.Fatalf("id = %x, want the local node id", ret.ID)
	}
}

func TestFindNode(t *testing.T) {
	r, table := newTestResponder(t)

	table.PutNode(testID(1), testAddr(1))
	table.PutNode(testID(2), testAddr(2))

	args := &dht.MsgArgs{ID: testID(3), Target: testID(99)}

	ret, err := r.Respond(context.Background(), recv(testAddr(3), dht.QFindNode, args))
	if err != nil {
		t.Fatalf("find_node: %v", err)
	}

	if len(ret.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(ret.Nodes))
	}
}

func TestGetPeersUnknownHashReturnsTokenAndNodes(t *testing.T) {
	r, table := newTestResponder(t)

	table.PutNode(testID(1), testAddr(1))

	args := &dht.MsgArgs{ID: testID(2), InfoHash: testID(9)}

	ret, err := r.Respond(context.Background(), recv(testAddr(2), dht.QGetPeers, args))
	if err != nil {
		t.Fatalf("get_peers: %v", err)
	}

	if ret.Token == nil || *ret.Token == "" {
		t.Fatal("no token returned")
	}

	if len(ret.Values) != 0 {
		t.Fatalf("values = %d, want none for an unknown hash", len(ret.Values))
	}

	if len(ret.Nodes) != 1 {
		t.Fatalf("nodes = %d, want the one known node", len(ret.Nodes))
	}
}

func TestAnnouncePeerRoundTrip(t *testing.T) {
	r, table := newTestResponder(t)

	from := testAddr(2)
	nodeID := testID(2)
	hash := testID(9)
	port := 51413

	getRet, err := r.Respond(context.Background(), recv(from, dht.QGetPeers, &dht.MsgArgs{ID: nodeID, InfoHash: hash}))
	if err != nil {
		t.Fatalf("get_peers: %v", err)
	}

	args := &dht.MsgArgs{
		ID:       nodeID,
		InfoHash: hash,
		Token:    *getRet.Token,
		Port:     &port,
	}

	if _, err := r.Respond(context.Background(), recv(from, dht.QAnnouncePeer, args)); err != nil {
		t.Fatalf("announce_peer: %v", err)
	}

	stored := table.GetHashOrClosestNodes(hash)
	if !stored.Found {
		t.Fatal("announced hash not stored")
	}

	peers := stored.Hash.Peers()
	if len(peers) != 1 {
		t.Fatalf("peers = %d, want 1", len(peers))
	}

	want := netip.AddrPortFrom(from.Addr(), uint16(port))
	if peers[0].Addr != want {
		t.Fatalf("peer = %v, want %v", peers[0].Addr, want)
	}
}

func TestAnnouncePeerRejectsInvalidToken(t *testing.T) {
	r, _ := newTestResponder(t)

	args := &dht.MsgArgs{ID: testID(2), InfoHash: testID(9), Token: "not-a-token"}

	if _, err := r.Respond(context.Background(), recv(testAddr(2), dht.QAnnouncePeer, args)); err != ErrInvalidToken {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestAnnounceTokenIsBoundToSourceAddress(t *testing.T) {
	r, _ := newTestResponder(t)

	nodeID := testID(2)
	hash := testID(9)

	getRet, err := r.Respond(context.Background(), recv(testAddr(2), dht.QGetPeers, &dht.MsgArgs{ID: nodeID, InfoHash: hash}))
	if err != nil {
		t.Fatalf("get_peers: %v", err)
	}

	args := &dht.MsgArgs{ID: nodeID, InfoHash: hash, Token: *getRet.Token}

	if _, err := r.Respond(context.Background(), recv(testAddr(3), dht.QAnnouncePeer, args)); err != ErrInvalidToken {
		t.Fatalf("err = %v, want ErrInvalidToken for a different source address", err)
	}
}

func TestSampleInfohashes(t *testing.T) {
	r, table := newTestResponder(t)

	for i := 0; i < 5; i++ {
		table.PutHash(testID(byte(i+20)), []ktable.HashPeer{{Addr: testAddr(byte(i + 20))}})
	}

	ret, err := r.Respond(context.Background(), recv(testAddr(1), dht.QSampleInfohashes, &dht.MsgArgs{ID: testID(1), Target: testID(5)}))
	if err != nil {
		t.Fatalf("sample_infohashes: %v", err)
	}

	if ret.Samples == nil {
		t.Fatal("samples field missing")
	}

	if len(*ret.Samples) != 5 {
		t.Fatalf("samples = %d, want 5", len(*ret.Samples))
	}

	if ret.Num == nil || *ret.Num != 5 {
		t.Fatalf("num = %v, want 5", ret.Num)
	}

	if ret.Interval == nil || *ret.Interval != 10 {
		t.Fatalf("interval = %v, want 10", ret.Interval)
	}
}

func TestUnknownMethodAndMissingArguments(t *testing.T) {
	r, _ := newTestResponder(t)

	if _, err := r.Respond(context.Background(), recv(testAddr(1), "no_such_query", &dht.MsgArgs{ID: testID(1)})); err != ErrMethodUnknown {
		t.Fatalf("err = %v, want ErrMethodUnknown", err)
	}

	if _, err := r.Respond(context.Background(), recv(testAddr(1), dht.QPing, nil)); err != ErrMissingArguments {
		t.Fatalf("err = %v, want ErrMissingArguments", err)
	}

	if _, err := r.Respond(context.Background(), recv(testAddr(1), dht.QGetPeers, &dht.MsgArgs{ID: testID(1)})); err != ErrMissingArguments {
		t.Fatalf("err = %v, want ErrMissingArguments for a missing infohash", err)
	}
}

func TestLimiterLayers(t *testing.T) {
	// Per-IP limiter: one request per hour, burst 1.
	l := NewLimiter(rate.Inf, 1000, rate.Every(time.Hour), 1, 10, time.Minute)

	if !l.Allow(testAddr(1).Addr()) {
		t.Fatal("first request denied")
	}

	if l.Allow(testAddr(1).Addr()) {
		t.Fatal("second request from the same address allowed")
	}

	if !l.Allow(testAddr(2).Addr()) {
		t.Fatal("another address denied by the overall limiter")
	}

	// Overall limiter takes precedence.
	o := NewLimiter(rate.Every(time.Hour), 1, rate.Inf, 1000, 10, time.Minute)

	if !o.Allow(testAddr(1).Addr()) {
		t.Fatal("first request denied")
	}

	if o.Allow(testAddr(2).Addr()) {
		t.Fatal("second address allowed despite the exhausted overall limit")
	}
}

func TestResponderRateLimitsRequests(t *testing.T) {
	table := ktable.New(testID(0))

	r := New(Config{
		KTable:       table,
		Logger:       slog.New(slog.DiscardHandler),
		OverallRate:  rate.Inf,
		OverallBurst: 1000,
		PerIPRate:    rate.Every(time.Hour),
		PerIPBurst:   1,
		PerIPSize:    10,
		PerIPTTL:     time.Minute,
	})

	if _, err := r.Respond(context.Background(), recv(testAddr(1), dht.QPing, &dht.MsgArgs{ID: testID(1)})); err != nil {
		t.Fatalf("first request: %v", err)
	}

	if _, err := r.Respond(context.Background(), recv(testAddr(1), dht.QPing, &dht.MsgArgs{ID: testID(1)})); err != ErrTooManyRequests {
		t.Fatalf("err = %v, want ErrTooManyRequests", err)
	}
}

func TestDiscoveredNodesSink(t *testing.T) {
	table := ktable.New(testID(0))
	sink := make(chan ktable.Node, 1)

	r := New(Config{
		KTable:          table,
		Logger:          slog.New(slog.DiscardHandler),
		DiscoveredNodes: sink,
		OverallRate:     rate.Inf,
		OverallBurst:    math.MaxInt32,
		PerIPRate:       rate.Inf,
		PerIPBurst:      math.MaxInt32,
	})

	from := testAddr(4)

	if _, err := r.Respond(context.Background(), recv(from, dht.QPing, &dht.MsgArgs{ID: testID(4)})); err != nil {
		t.Fatalf("ping: %v", err)
	}

	select {
	case node := <-sink:
		if node.ID() != testID(4) || node.Addr() != from {
			t.Fatalf("discovered node = %v at %v", node.ID(), node.Addr())
		}
	case <-time.After(time.Second):
		t.Fatal("discovered node not sent")
	}
}

func TestNodeDiscoveriesAreDroppedWhenSinkIsFull(t *testing.T) {
	table := ktable.New(testID(0))
	sink := make(chan ktable.Node) // unbuffered and never read

	r := New(Config{
		KTable:          table,
		Logger:          slog.New(slog.DiscardHandler),
		DiscoveredNodes: sink,
		OverallRate:     rate.Inf,
		OverallBurst:    math.MaxInt32,
		PerIPRate:       rate.Inf,
		PerIPBurst:      math.MaxInt32,
	})

	done := make(chan struct{})

	go func() {
		_, _ = r.Respond(context.Background(), recv(testAddr(5), dht.QPing, &dht.MsgArgs{ID: testID(5)}))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("response blocked on a full discovery sink")
	}
}

func TestTokenSecretIsUsed(t *testing.T) {
	r, _ := newTestResponder(t)

	hash := testID(9)
	nodeID := testID(2)

	first, err := r.Respond(context.Background(), recv(testAddr(2), dht.QGetPeers, &dht.MsgArgs{ID: nodeID, InfoHash: hash}))
	if err != nil {
		t.Fatalf("get_peers: %v", err)
	}

	second, err := r.Respond(context.Background(), recv(testAddr(2), dht.QGetPeers, &dht.MsgArgs{ID: nodeID, InfoHash: hash}))
	if err != nil {
		t.Fatalf("get_peers: %v", err)
	}

	if *first.Token != *second.Token {
		t.Fatal("token for the same query, address and secret is not stable")
	}

}
