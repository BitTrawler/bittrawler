package dhtcrawler

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/client"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
)

// sampleCrawler returns a crawler that knows one node, ready to be sampled.
func sampleCrawler(t *testing.T, err error) (*testCrawler, ktable.Node) {
	t.Helper()

	dht := &fakeDHT{
		sampleInfoHashesFn: func(context.Context, netip.AddrPort, protocol.ID) (client.SampleInfoHashesResult, error) {
			return client.SampleInfoHashesResult{}, err
		},
	}

	tc := newTestCrawler(t, dht, &fakeRequester{}, Config{})

	id := testID(1)
	addr := testAddr(1)

	tc.kTable.PutNode(id, addr,
		ktable.NodeRestored(ktable.NodeState{LastRespondedAt: time.Now().Add(-time.Minute)}),
	)

	return tc, ktable.NewNode(id, addr)
}

func TestSampleTimeoutLeavesNodeSamplable(t *testing.T) {
	tc, node := sampleCrawler(t, errors.New("dht query \"sample_infohashes\" timed out"))

	tc.crawler.sampleInfoHashes(context.Background(), node)

	if got := tc.crawler.stats.sampleFailures.Load(); got != 1 {
		t.Fatalf("sample failures = %d, want 1", got)
	}

	if got := tc.crawler.stats.sampleUnsupported.Load(); got != 0 {
		t.Fatalf("unsupported nodes = %d, want 0", got)
	}

	states := tc.kTable.SnapshotNodes(0)
	if len(states) != 1 {
		t.Fatalf("routing table holds %d nodes, want 1", len(states))
	}

	if states[0].Support == ktable.NodeSupportNo {
		t.Fatal("a timeout marked the node as unsupported")
	}

	if !states[0].NextSampleAt.After(time.Now().Add(50 * time.Second)) {
		t.Fatalf("node is due for another sample at %v, want a backoff", states[0].NextSampleAt)
	}
}

func TestSampleMethodUnknownMarksNodeUnsupported(t *testing.T) {
	tc, node := sampleCrawler(t, &dht.Error{Code: dht.ErrorCodeMethodUnknown, Msg: "method unknown"})

	tc.crawler.sampleInfoHashes(context.Background(), node)

	if got := tc.crawler.stats.sampleUnsupported.Load(); got != 1 {
		t.Fatalf("unsupported nodes = %d, want 1", got)
	}

	if got := tc.crawler.stats.sampleFailures.Load(); got != 0 {
		t.Fatalf("sample failures = %d, want 0", got)
	}

	states := tc.kTable.SnapshotNodes(0)
	if len(states) != 1 {
		t.Fatalf("routing table holds %d nodes, want 1", len(states))
	}

	if states[0].Support != ktable.NodeSupportNo {
		t.Fatalf("support = %v, want unsupported", states[0].Support)
	}
}

func TestMethodUnknownRecognisesOnlyKRPCMethodErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "method unknown", err: &dht.Error{Code: dht.ErrorCodeMethodUnknown}, want: true},
		{name: "other KRPC error", err: &dht.Error{Code: dht.ErrorCodeServerError}, want: false},
		{name: "timeout", err: errors.New("timed out"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := methodUnknown(test.err); got != test.want {
				t.Errorf("methodUnknown = %v, want %v", got, test.want)
			}
		})
	}
}
