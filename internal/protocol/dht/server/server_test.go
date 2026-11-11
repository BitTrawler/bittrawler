package server

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/responder"
	"golang.org/x/time/rate"
)

func testID(n byte) protocol.ID {
	var id protocol.ID

	id[0] = n
	id[19] = 0xff

	return id
}

func newTestServer(t *testing.T) (Server, ktable.Table) {
	t.Helper()

	table := ktable.New(testID(0))

	unlimited := responder.Config{
		KTable:       table,
		Logger:       slog.New(slog.DiscardHandler),
		OverallRate:  rate.Inf,
		OverallBurst: math.MaxInt32,
		PerIPRate:    rate.Inf,
		PerIPBurst:   math.MaxInt32,
		TokenSecret:  []byte("test-secret"),
	}

	cfg := NewDefaultConfig()
	cfg.ListenAddr = netip.MustParseAddrPort("127.0.0.1:0")
	cfg.Logger = slog.New(slog.DiscardHandler)
	cfg.QueryTimeout = 200 * time.Millisecond
	cfg.OutboundRate = rate.Inf
	cfg.OutboundBurst = math.MaxInt32

	srv, err := New(cfg, responder.New(unlimited))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	t.Cleanup(func() { _ = srv.Close() })

	return srv, table
}

func TestTransactionIDsAreRandom(t *testing.T) {
	seen := make(map[string]struct{}, 1000)

	for i := 0; i < 1000; i++ {
		id := newTransactionID()

		if len(id) < 4 {
			t.Fatalf("transaction id %q is shorter than 4 bytes", id)
		}

		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate transaction id %q", id)
		}

		seen[id] = struct{}{}
	}
}

func TestHandleResponseRequiresAddressAndID(t *testing.T) {
	addr := netip.MustParseAddrPort("192.0.2.1:6881")
	other := netip.MustParseAddrPort("192.0.2.2:6881")

	s := &server{
		queries: make(map[queryKey]chan dht.RecvMsg),
		logger:  slog.New(slog.DiscardHandler),
		stopped: make(chan struct{}),
	}

	ch := make(chan dht.RecvMsg, 1)
	s.queries[queryKey{addr: addr, id: "abcd"}] = ch

	// Right ID, wrong address: ignored.
	s.handleResponse(dht.RecvMsg{From: other, Msg: dht.Msg{T: "abcd", Y: dht.YResponse}})

	select {
	case <-ch:
		t.Fatal("response from the wrong address was accepted")
	default:
	}

	// Right address, wrong ID: ignored.
	s.handleResponse(dht.RecvMsg{From: addr, Msg: dht.Msg{T: "zzzz", Y: dht.YResponse}})

	select {
	case <-ch:
		t.Fatal("response with the wrong transaction id was accepted")
	default:
	}

	// Matching both: delivered once, and a duplicate must not block.
	s.handleResponse(dht.RecvMsg{From: addr, Msg: dht.Msg{T: "abcd", Y: dht.YResponse}})

	select {
	case <-ch:
	default:
		t.Fatal("matching response was not delivered")
	}

	s.handleResponse(dht.RecvMsg{From: addr, Msg: dht.Msg{T: "abcd", Y: dht.YResponse}})
}

func TestQueryTimesOut(t *testing.T) {
	srv, _ := newTestServer(t)

	// A bound UDP socket that never answers.
	dead, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = dead.Close() })

	addr := dead.LocalAddr().(*net.UDPAddr)
	target := netip.AddrPortFrom(netip.MustParseAddr(addr.IP.String()), uint16(addr.Port))

	start := time.Now()

	_, err = srv.Query(context.Background(), target, dht.QPing, dht.MsgArgs{ID: testID(1)})
	if err == nil {
		t.Fatal("query to a dead node returned no error")
	}

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	srv, _ := newTestServer(t)

	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case <-srv.Done():
	case <-time.After(time.Second):
		t.Fatal("Done not closed")
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestLocalAddrHasEphemeralPort(t *testing.T) {
	srv, _ := newTestServer(t)

	if srv.LocalAddr().Port() == 0 {
		t.Fatal("local address has no port")
	}

	if !srv.LocalAddr().Addr().IsLoopback() {
		t.Fatalf("local address = %s, want loopback", srv.LocalAddr())
	}
}

func TestQueryOnClosedServerFails(t *testing.T) {
	srv, _ := newTestServer(t)

	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := srv.Query(context.Background(), netip.MustParseAddrPort("127.0.0.1:1"), dht.QPing, dht.MsgArgs{ID: testID(1)})
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want a closed-server error", err)
	}
}
