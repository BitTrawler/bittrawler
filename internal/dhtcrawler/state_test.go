package dhtcrawler

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/crawlstate"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
)

// fakeState records what the crawler saves and returns what it is told to load.
type fakeState struct {
	loaded    []crawlstate.Node
	saved     []crawlstate.Node
	loadCalls int
	saveCalls int
}

func (f *fakeState) Load(context.Context, int) ([]crawlstate.Node, error) {
	f.loadCalls++

	return f.loaded, nil
}

func (f *fakeState) Save(_ context.Context, nodes []crawlstate.Node) error {
	f.saveCalls++
	f.saved = nodes

	return nil
}

func TestRestoreStateLoadsNodesAndLearning(t *testing.T) {
	lastResponded := time.Now().Add(-time.Hour).Round(time.Second)
	nextSample := time.Now().Add(15 * time.Minute).Round(time.Second)

	state := &fakeState{loaded: []crawlstate.Node{{
		ID:              testID(1),
		Addr:            testAddr(1),
		Support:         crawlstate.SupportYes,
		LastRespondedAt: lastResponded,
		NextSampleAt:    nextSample,
		Sampled:         900,
		Total:           12000,
	}}}

	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{State: state})
	tc.crawler.restoreState(context.Background())

	if state.loadCalls != 1 {
		t.Fatalf("load calls = %d, want 1", state.loadCalls)
	}

	states := tc.kTable.SnapshotNodes(0)
	if len(states) != 1 {
		t.Fatalf("routing table holds %d nodes, want 1", len(states))
	}

	restored := states[0]
	if restored.ID != testID(1) || restored.Addr != testAddr(1) {
		t.Fatalf("restored node = %v at %v", restored.ID, restored.Addr)
	}

	if restored.Support != ktable.NodeSupportYes || restored.Sampled != 900 || restored.Total != 12000 {
		t.Fatalf("restored learning = %+v", restored)
	}

	if !restored.NextSampleAt.Equal(nextSample) {
		t.Fatalf("next sample = %v, want %v", restored.NextSampleAt, nextSample)
	}
}

func TestSaveStateWritesTheRoutingTable(t *testing.T) {
	state := &fakeState{}
	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{State: state})

	tc.kTable.PutNode(testID(1), testAddr(1), ktable.NodeRestored(ktable.NodeState{
		Support:      ktable.NodeSupportYes,
		Sampled:      42,
		Total:        800,
		NextSampleAt: time.Now().Add(time.Minute),
	}))

	tc.crawler.saveState()

	if state.saveCalls != 1 {
		t.Fatalf("save calls = %d, want 1", state.saveCalls)
	}

	if len(state.saved) != 1 {
		t.Fatalf("saved %d nodes, want 1", len(state.saved))
	}

	saved := state.saved[0]
	if saved.ID != testID(1) || saved.Addr != testAddr(1) {
		t.Fatalf("saved node = %v at %v", saved.ID, saved.Addr)
	}

	if saved.Support != crawlstate.SupportYes || saved.Sampled != 42 || saved.Total != 800 {
		t.Fatalf("saved learning = %+v", saved)
	}
}

func TestSaveStateSkipsInvalidNodes(t *testing.T) {
	state := &fakeState{}
	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{State: state})

	tc.kTable.PutNode(testID(1), netip.AddrPort{})

	tc.crawler.saveState()

	if len(state.saved) != 0 {
		t.Fatalf("saved %d nodes with invalid addresses, want 0", len(state.saved))
	}
}
