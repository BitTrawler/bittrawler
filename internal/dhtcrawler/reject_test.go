package dhtcrawler

import (
	"context"
	"net/netip"
	"testing"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/drops"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo"
)

// cjkInfo is metadata whose name is written entirely in one script, which is
// what the writing-system filter drops.
var cjkInfo = metainfo.Info{Name: "寄生兽 完全版", Length: 4096}

func metadataJob() infoHashWithPeers {
	return infoHashWithPeers{
		infoHash: testHash(7),
		node:     testAddr(1),
		peers:    []netip.AddrPort{testAddr(2)},
	}
}

// TestMetadataRecordsADrop checks that a rejected torrent reaches the overview's
// feed with the name and reason it was rejected for.
func TestMetadataRecordsADrop(t *testing.T) {
	t.Parallel()

	rejected := drops.NewRecorder(10)

	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{info: cjkInfo}, Config{
		Classifier: classify.NewLive(classify.Config{DropScripts: []string{"cjk"}}),
		Rejected:   rejected,
	})

	tc.crawler.metadata(context.Background(), metadataJob())

	events := rejected.Recent(10, nil)
	if len(events) != 1 {
		t.Fatalf("recorded %d rejections, want 1", len(events))
	}

	if events[0].Name != cjkInfo.Name {
		t.Fatalf("recorded name %q, want %q", events[0].Name, cjkInfo.Name)
	}

	if events[0].Size != cjkInfo.TotalLength() {
		t.Fatalf("recorded size %d, want %d", events[0].Size, cjkInfo.TotalLength())
	}

	if events[0].Reason != classify.DropScript {
		t.Fatalf("recorded reason %v, want %v", events[0].Reason, classify.DropScript)
	}

	if events[0].InfoHash != testHash(7) {
		t.Fatalf("recorded hash %v, want %v", events[0].InfoHash, testHash(7))
	}
}

// TestChangedFilterAppliesToARunningCrawl is the regression the live classifier
// exists for: turning the writing-system filter on used to leave the running
// crawl dropping nothing until it was restarted.
func TestChangedFilterAppliesToARunningCrawl(t *testing.T) {
	t.Parallel()

	live := classify.NewLive(classify.Config{})
	rejected := drops.NewRecorder(10)

	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{info: cjkInfo}, Config{
		Classifier: live,
		Rejected:   rejected,
	})

	// Without the filter the torrent is indexed, so nothing is recorded.
	tc.crawler.metadata(context.Background(), metadataJob())

	if events := rejected.Recent(10, nil); len(events) != 0 {
		t.Fatalf("recorded %d rejections before the filter was set", len(events))
	}

	live.Set(classify.Config{DropScripts: []string{"cjk"}})

	tc.crawler.metadata(context.Background(), metadataJob())

	events := rejected.Recent(10, nil)
	if len(events) != 1 {
		t.Fatalf("recorded %d rejections after the filter was set, want 1", len(events))
	}

	if events[0].Reason != classify.DropScript {
		t.Fatalf("recorded reason %v, want %v", events[0].Reason, classify.DropScript)
	}

	// And turning it back off keeps the torrent again.
	live.Set(classify.Config{})

	tc.crawler.metadata(context.Background(), metadataJob())

	if events := rejected.Recent(10, nil); len(events) != 1 {
		t.Fatalf("recorded %d rejections after the filter was cleared, want 1", len(events))
	}
}
