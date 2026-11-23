package dhtcrawler

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/drops"
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/metainforequester"
)

// scriptedRequester fails the first n requests and then returns a fixed info
// dict, recording the order in which peers were asked.
type scriptedRequester struct {
	mutex    sync.Mutex
	failures int
	calls    []netip.AddrPort
	info     metainfo.Info
}

func (r *scriptedRequester) Request(
	_ context.Context,
	_ protocol.ID,
	peer netip.AddrPort,
) (metainforequester.Response, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.calls = append(r.calls, peer)

	if len(r.calls) <= r.failures {
		return metainforequester.Response{}, errors.New("peer is gone")
	}

	return metainforequester.Response{Info: r.info}, nil
}

func TestFetchMetainfoTriesPeersInOrder(t *testing.T) {
	requester := &scriptedRequester{
		failures: 1,
		info:     metainfo.Info{Name: "Valid.Torrent", Length: 4096},
	}

	tc := newTestCrawler(t, &fakeDHT{}, requester, Config{})

	job := infoHashWithPeers{
		infoHash: testHash(1),
		node:     testAddr(1),
		peers:    []netip.AddrPort{testAddr(2), testAddr(3)},
	}

	info, err := tc.crawler.fetchMetainfo(context.Background(), job)
	if err != nil {
		t.Fatalf("fetch metadata: %v", err)
	}

	if info.Name != requester.info.Name {
		t.Errorf("name = %q, want %q", info.Name, requester.info.Name)
	}

	want := []netip.AddrPort{testAddr(2), testAddr(3)}
	if len(requester.calls) != len(want) {
		t.Fatalf("asked %d peers, want %d", len(requester.calls), len(want))
	}

	for i, peer := range want {
		if requester.calls[i] != peer {
			t.Errorf("peer %d = %s, want %s", i, requester.calls[i], peer)
		}
	}
}

func TestFetchMetainfoRecordsBlockedTorrent(t *testing.T) {
	// The name is too short and the size is too small, so both banning checks
	// reject it.
	requester := &fakeRequester{info: metainfo.Info{Name: "bad", Length: 1}}

	rejected := drops.NewRecorder(10)

	tc := newTestCrawler(t, &fakeDHT{}, requester, Config{Rejected: rejected})

	hash := testHash(9)
	job := infoHashWithPeers{
		infoHash: hash,
		node:     testAddr(1),
		peers:    []netip.AddrPort{testAddr(2)},
	}

	if _, err := tc.crawler.fetchMetainfo(context.Background(), job); err == nil {
		t.Fatal("expected banned metadata to return an error")
	}

	allowed, err := tc.blocklist.Filter(context.Background(), []protocol.ID{hash})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}

	if len(allowed) != 0 {
		t.Fatal("banned infohash was not recorded in the blocklist")
	}

	events := rejected.Recent(10, nil)
	if len(events) != 1 {
		t.Fatalf("recorded %d rejections, want 1", len(events))
	}

	if events[0].Reason != classify.DropBlocked {
		t.Fatalf("recorded reason %v, want %v", events[0].Reason, classify.DropBlocked)
	}

	if events[0].Name != "bad" || events[0].Size != 1 {
		t.Fatalf("recorded %q of %d bytes, want %q of 1 byte", events[0].Name, events[0].Size, "bad")
	}

	if events[0].InfoHash != hash {
		t.Fatalf("recorded hash %v, want %v", events[0].InfoHash, hash)
	}
}

func TestFetchMetainfoReportsNoMetadata(t *testing.T) {
	requester := &fakeRequester{err: errors.New("peer is gone")}

	tc := newTestCrawler(t, &fakeDHT{}, requester, Config{})

	job := infoHashWithPeers{
		infoHash: testHash(1),
		node:     testAddr(1),
		peers:    []netip.AddrPort{testAddr(2)},
	}

	if _, err := tc.crawler.fetchMetainfo(context.Background(), job); !errors.Is(err, errNoMetadata) {
		t.Fatalf("error = %v, want %v", err, errNoMetadata)
	}
}

func TestUniquePeers(t *testing.T) {
	invalid := netip.AddrPort{}

	got := uniquePeers([]netip.AddrPort{
		testAddr(1),
		invalid,
		testAddr(1),
		testAddr(2),
		testAddr(3),
		testAddr(4),
	}, 3)

	want := []netip.AddrPort{testAddr(1), testAddr(2), testAddr(3)}

	if len(got) != len(want) {
		t.Fatalf("got %d peers, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("peer %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestUsablePeersSkipsPeersThatFailedRecently(t *testing.T) {
	tc := newTestCrawler(t, &fakeDHT{}, &scriptedRequester{}, Config{})

	dead := testAddr(2)
	alive := testAddr(3)

	tc.crawler.peerFailed(dead)

	peers := tc.crawler.usablePeers([]netip.AddrPort{dead, alive}, 10)
	if len(peers) != 1 || peers[0] != alive {
		t.Fatalf("usable peers = %v, want just %v", peers, alive)
	}
}

func TestFailedMetadataRequestRemembersThePeer(t *testing.T) {
	requester := &scriptedRequester{failures: 1, info: metainfo.Info{Name: "Valid.Torrent", Length: 4096}}
	tc := newTestCrawler(t, &fakeDHT{}, requester, Config{})

	dead := testAddr(2)

	_, _ = tc.crawler.fetchMetainfo(context.Background(), infoHashWithPeers{
		infoHash: testHash(1),
		node:     testAddr(1),
		peers:    []netip.AddrPort{dead, testAddr(3)},
	})

	if _, failed := tc.crawler.failedPeers.Get(dead); !failed {
		t.Fatal("the peer that failed was not remembered")
	}
}
