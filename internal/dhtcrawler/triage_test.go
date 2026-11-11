package dhtcrawler

import (
	"context"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/store"
)

func TestTriageBatchQueuesOnlyUnseenUnblockedHashes(t *testing.T) {
	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{})

	stored := testHash(1)
	blocked := testHash(2)
	fresh := testHash(3)

	ctx := context.Background()

	if _, err := tc.store.Put(ctx, store.Row{
		InfoHash:  stored,
		Name:      "already indexed",
		Size:      1,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	if _, err := tc.blocklist.Block(ctx, []protocol.ID{blocked}); err != nil {
		t.Fatalf("block: %v", err)
	}

	tc.crawler.triageBatch(ctx, []hashAndNode{
		{infoHash: stored, node: testAddr(1)},
		{infoHash: blocked, node: testAddr(2)},
		{infoHash: fresh, node: testAddr(3)},
		{infoHash: fresh, node: testAddr(4)},
	})

	if got := len(tc.crawler.forPeers); got != 1 {
		t.Fatalf("queued %d hashes for a peer lookup, want 1", got)
	}

	item := <-tc.crawler.forPeers

	if item.infoHash != fresh {
		t.Errorf("queued hash %x, want %x", item.infoHash, fresh)
	}

	// The first node that reported the hash is the one used for the lookup.
	if item.node != testAddr(3) {
		t.Errorf("queued node %s, want %s", item.node, testAddr(3))
	}
}

func TestTriageBatchWithNothingNewQueuesNothing(t *testing.T) {
	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{})

	stored := testHash(1)

	ctx := context.Background()

	if _, err := tc.store.Put(ctx, store.Row{
		InfoHash:  stored,
		Name:      "already indexed",
		Size:      1,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	tc.crawler.triageBatch(ctx, []hashAndNode{{infoHash: stored, node: testAddr(1)}})

	if got := len(tc.crawler.forPeers); got != 0 {
		t.Fatalf("queued %d hashes for a peer lookup, want 0", got)
	}
}
