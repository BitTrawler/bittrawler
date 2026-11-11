package dhtcrawler

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/blocking"
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/client"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/banning"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/metainforequester"
	"github.com/BitTrawler/bittrawler/internal/store"
	mi "github.com/anacrolix/torrent/metainfo"
	"github.com/bits-and-blooms/bloom/v3"
)

// errUnexpectedCall marks a DHT query the test did not expect.
var errUnexpectedCall = errors.New("unexpected DHT call")

func testID(n byte) protocol.ID {
	var id protocol.ID

	id[0] = n
	id[19] = 0xff

	return id
}

func testHash(n byte) protocol.ID {
	var hash protocol.ID

	hash[0] = 0xaa
	hash[19] = n

	return hash
}

func testAddr(n byte) netip.AddrPort {
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 0, 2, n}), 6881)
}

// fakeDHT is a DHT client whose queries are supplied per test. A nil function
// reports an unexpected call rather than returning a zero result.
type fakeDHT struct {
	pingFn             func(context.Context, netip.AddrPort) (client.PingResult, error)
	findNodeFn         func(context.Context, netip.AddrPort, protocol.ID) (client.FindNodeResult, error)
	getPeersFn         func(context.Context, netip.AddrPort, protocol.ID) (client.GetPeersResult, error)
	getPeersScrapeFn   func(context.Context, netip.AddrPort, protocol.ID) (client.GetPeersScrapeResult, error)
	sampleInfoHashesFn func(context.Context, netip.AddrPort, protocol.ID) (client.SampleInfoHashesResult, error)
}

func (f *fakeDHT) Ping(ctx context.Context, addr netip.AddrPort) (client.PingResult, error) {
	if f.pingFn == nil {
		return client.PingResult{}, errUnexpectedCall
	}

	return f.pingFn(ctx, addr)
}

func (f *fakeDHT) FindNode(
	ctx context.Context,
	addr netip.AddrPort,
	target protocol.ID,
) (client.FindNodeResult, error) {
	if f.findNodeFn == nil {
		return client.FindNodeResult{}, errUnexpectedCall
	}

	return f.findNodeFn(ctx, addr, target)
}

func (f *fakeDHT) GetPeers(
	ctx context.Context,
	addr netip.AddrPort,
	hash protocol.ID,
) (client.GetPeersResult, error) {
	if f.getPeersFn == nil {
		return client.GetPeersResult{}, errUnexpectedCall
	}

	return f.getPeersFn(ctx, addr, hash)
}

func (f *fakeDHT) GetPeersScrape(
	ctx context.Context,
	addr netip.AddrPort,
	hash protocol.ID,
) (client.GetPeersScrapeResult, error) {
	if f.getPeersScrapeFn == nil {
		return client.GetPeersScrapeResult{}, errUnexpectedCall
	}

	return f.getPeersScrapeFn(ctx, addr, hash)
}

func (f *fakeDHT) SampleInfoHashes(
	ctx context.Context,
	addr netip.AddrPort,
	target protocol.ID,
) (client.SampleInfoHashesResult, error) {
	if f.sampleInfoHashesFn == nil {
		return client.SampleInfoHashesResult{}, errUnexpectedCall
	}

	return f.sampleInfoHashesFn(ctx, addr, target)
}

// fakeRequester returns one fixed metadata result and counts the calls.
type fakeRequester struct {
	mutex    sync.Mutex
	requests int
	info     metainfo.Info
	err      error
}

func (f *fakeRequester) Request(
	_ context.Context,
	_ protocol.ID,
	_ netip.AddrPort,
) (metainforequester.Response, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.requests++

	if f.err != nil {
		return metainforequester.Response{}, f.err
	}

	return metainforequester.Response{Info: f.info}, nil
}

// testCrawler bundles a crawler with the dependencies a test needs to inspect.
type testCrawler struct {
	crawler   *Crawler
	kTable    ktable.Table
	store     *store.Store
	blocklist *blocking.Blocklist
}

func newTestCrawler(
	t *testing.T,
	dht client.Client,
	requester metainforequester.Requester,
	cfg Config,
) *testCrawler {
	t.Helper()

	dir := t.TempDir()

	st, err := store.Open(store.Config{Dir: filepath.Join(dir, "store")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	t.Cleanup(func() { _ = st.Close() })

	blocklist, err := blocking.Open(blocking.Config{Dir: filepath.Join(dir, "blocklist")})
	if err != nil {
		t.Fatalf("open blocklist: %v", err)
	}

	t.Cleanup(func() { _ = blocklist.Close() })

	kTable := ktable.New(testID(0))

	cfg.KTable = kTable
	cfg.DHT = dht
	cfg.Requester = requester
	cfg.Banning = banning.New()
	cfg.Blocklist = blocklist
	cfg.Store = st
	cfg.Logger = slog.New(slog.DiscardHandler)
	cfg.BootstrapNodes = []string{}

	if cfg.TriageBatchWait == 0 {
		cfg.TriageBatchWait = 20 * time.Millisecond
	}

	if cfg.PersistBatchWait == 0 {
		cfg.PersistBatchWait = 20 * time.Millisecond
	}

	if cfg.MetadataTimeout == 0 {
		cfg.MetadataTimeout = 2 * time.Second
	}

	if cfg.Workers == (WorkerCounts{}) {
		cfg.Workers = WorkerCounts{Ping: 1, FindNode: 1, SampleInfoHashes: 1, GetPeers: 1, Metadata: 1}
	}

	crawler, err := New(cfg)
	if err != nil {
		t.Fatalf("new crawler: %v", err)
	}

	return &testCrawler{crawler: crawler, kTable: kTable, store: st, blocklist: blocklist}
}

// waitForTorrent polls the store until it holds at least one torrent.
func waitForTorrent(t *testing.T, st *store.Store, timeout time.Duration) store.Torrent {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		query := store.NewQuery()
		query.Sort = store.SortNewest
		query.Limit = 10

		res, err := st.Search(context.Background(), query)
		if err != nil {
			t.Fatalf("search: %v", err)
		}

		if len(res.Torrents) > 0 {
			return res.Torrents[0]
		}

		if time.Now().After(deadline) {
			t.Fatalf("no torrent was stored within %s", timeout)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func TestCrawlerCrawlsDiscoveredTorrent(t *testing.T) {
	nodeID := testID(1)
	nodeAddr := testAddr(1)
	hash := testHash(1)
	peerAddr := testAddr(2)

	info := metainfo.Info{
		Name:        "Example.Torrent.1080p",
		PieceLength: 262144,
		Files: []mi.FileInfo{
			{Length: 1024, Path: []string{"small.mkv"}},
			{Length: 4096, Path: []string{"large.mkv"}},
		},
	}

	scrapeBloom := *bloom.NewWithEstimates(100, 0.01)
	for _, item := range []string{"seeder-one", "seeder-two", "seeder-three"} {
		scrapeBloom.AddString(item)
	}

	dht := &fakeDHT{
		pingFn: func(context.Context, netip.AddrPort) (client.PingResult, error) {
			return client.PingResult{ID: nodeID}, nil
		},
		findNodeFn: func(context.Context, netip.AddrPort, protocol.ID) (client.FindNodeResult, error) {
			return client.FindNodeResult{ID: nodeID}, nil
		},
		sampleInfoHashesFn: func(
			_ context.Context,
			addr netip.AddrPort,
			_ protocol.ID,
		) (client.SampleInfoHashesResult, error) {
			if addr != nodeAddr {
				return client.SampleInfoHashesResult{}, errUnexpectedCall
			}

			return client.SampleInfoHashesResult{
				ID:       nodeID,
				Samples:  []protocol.ID{hash},
				Interval: 3600,
			}, nil
		},
		getPeersFn: func(
			_ context.Context,
			addr netip.AddrPort,
			gotHash protocol.ID,
		) (client.GetPeersResult, error) {
			if addr != nodeAddr || gotHash != hash {
				return client.GetPeersResult{}, errUnexpectedCall
			}

			return client.GetPeersResult{ID: nodeID, Values: []netip.AddrPort{peerAddr}}, nil
		},
		getPeersScrapeFn: func(
			_ context.Context,
			_ netip.AddrPort,
			gotHash protocol.ID,
		) (client.GetPeersScrapeResult, error) {
			if gotHash != hash {
				return client.GetPeersScrapeResult{}, errUnexpectedCall
			}

			return client.GetPeersScrapeResult{ID: nodeID, BfSeeders: scrapeBloom, BfPeers: scrapeBloom}, nil
		},
	}

	requester := &fakeRequester{info: info}
	tc := newTestCrawler(t, dht, requester, Config{})
	tc.kTable.BatchCommand(ktable.PutNode{ID: nodeID, Addr: nodeAddr})

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)

	go func() { runDone <- tc.crawler.Run(ctx) }()

	row := waitForTorrent(t, tc.store, 15*time.Second)

	if row.InfoHash != hash {
		t.Errorf("infohash = %x, want %x", row.InfoHash, hash)
	}

	if row.Name != info.Name {
		t.Errorf("name = %q, want %q", row.Name, info.Name)
	}

	if row.Size != 5120 {
		t.Errorf("size = %d, want 5120", row.Size)
	}

	if row.FilesCount != 2 {
		t.Errorf("files count = %d, want 2", row.FilesCount)
	}

	if row.PrimaryFile != "large.mkv" {
		t.Errorf("primary file = %q, want %q", row.PrimaryFile, "large.mkv")
	}

	if row.Seeders != 3 {
		t.Errorf("seeders = %d, want 3", row.Seeders)
	}

	if row.Leechers != 3 {
		t.Errorf("leechers = %d, want 3", row.Leechers)
	}

	if row.CreatedAt.IsZero() {
		t.Error("created_at is zero")
	}

	cancel()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("crawler did not stop after cancellation")
	}
}

func TestCrawlerStopsWhenContextIsCancelled(t *testing.T) {
	block := func(ctx context.Context) error {
		<-ctx.Done()

		return ctx.Err()
	}

	dht := &fakeDHT{
		pingFn: func(ctx context.Context, _ netip.AddrPort) (client.PingResult, error) {
			return client.PingResult{}, block(ctx)
		},
		findNodeFn: func(ctx context.Context, _ netip.AddrPort, _ protocol.ID) (client.FindNodeResult, error) {
			return client.FindNodeResult{}, block(ctx)
		},
		getPeersFn: func(ctx context.Context, _ netip.AddrPort, _ protocol.ID) (client.GetPeersResult, error) {
			return client.GetPeersResult{}, block(ctx)
		},
		getPeersScrapeFn: func(
			ctx context.Context,
			_ netip.AddrPort,
			_ protocol.ID,
		) (client.GetPeersScrapeResult, error) {
			return client.GetPeersScrapeResult{}, block(ctx)
		},
		sampleInfoHashesFn: func(
			ctx context.Context,
			_ netip.AddrPort,
			_ protocol.ID,
		) (client.SampleInfoHashesResult, error) {
			return client.SampleInfoHashesResult{}, block(ctx)
		},
	}

	tc := newTestCrawler(t, dht, &fakeRequester{}, Config{})
	tc.kTable.BatchCommand(ktable.PutNode{ID: testID(1), Addr: testAddr(1)})

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)

	go func() { runDone <- tc.crawler.Run(ctx) }()

	// Let the schedulers queue a query so the cancellation interrupts an
	// in-flight request rather than an idle loop.
	time.Sleep(1200 * time.Millisecond)
	cancel()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("crawler did not stop after cancellation")
	}
}

func TestNewRequiresDependencies(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected an error for a config without dependencies")
	}
}
