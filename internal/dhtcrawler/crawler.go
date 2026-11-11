package dhtcrawler

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"sync"

	"github.com/BitTrawler/bittrawler/internal/blocking"
	"github.com/BitTrawler/bittrawler/internal/concurrency"
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/client"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/banning"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/metainforequester"
	"github.com/BitTrawler/bittrawler/internal/store"
	"github.com/bits-and-blooms/bloom/v3"
)

// hashAndNode is a discovered infohash together with the node that reported it.
type hashAndNode struct {
	infoHash protocol.ID
	node     netip.AddrPort
}

// infoHashWithPeers is an infohash whose peers have been looked up.
type infoHashWithPeers struct {
	infoHash protocol.ID
	node     netip.AddrPort
	peers    []netip.AddrPort
}

// Crawler discovers infohashes and fetches their metadata.
type Crawler struct {
	cfg       Config
	kTable    ktable.Table
	client    client.Client
	requester metainforequester.Requester
	banning   banning.Checker
	blocklist *blocking.Blocklist
	store     *store.Store
	logger    *slog.Logger

	inbound    <-chan ktable.Node
	discovered chan ktable.Node
	forPing    chan ktable.Node
	forFind    chan ktable.Node
	forSample  chan ktable.Node
	forTriage  chan hashAndNode
	forPeers   chan hashAndNode
	forFetch   chan infoHashWithPeers
	forPersist chan store.Row

	seenHashes   *seenHashes
	soughtNodeID *concurrency.AtomicValue[protocol.ID]
}

// New validates the dependencies and returns a crawler. It starts no
// goroutines; call Run to crawl.
func New(cfg Config) (*Crawler, error) {
	if cfg.KTable == nil {
		return nil, errors.New("dhtcrawler: Config.KTable is required")
	}

	if cfg.DHT == nil {
		return nil, errors.New("dhtcrawler: Config.DHT is required")
	}

	if cfg.Requester == nil {
		return nil, errors.New("dhtcrawler: Config.Requester is required")
	}

	if cfg.Banning == nil {
		return nil, errors.New("dhtcrawler: Config.Banning is required")
	}

	if cfg.Blocklist == nil {
		return nil, errors.New("dhtcrawler: Config.Blocklist is required")
	}

	if cfg.Store == nil {
		return nil, errors.New("dhtcrawler: Config.Store is required")
	}

	cfg = cfg.withDefaults()

	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}

	w := cfg.Workers

	return &Crawler{
		cfg:          cfg,
		kTable:       cfg.KTable,
		client:       cfg.DHT,
		requester:    cfg.Requester,
		banning:      cfg.Banning,
		blocklist:    cfg.Blocklist,
		store:        cfg.Store,
		logger:       cfg.Logger,
		inbound:      cfg.DiscoveredNodes,
		discovered:   make(chan ktable.Node, 1024),
		forPing:      make(chan ktable.Node, w.Ping),
		forFind:      make(chan ktable.Node, 8*w.FindNode),
		forSample:    make(chan ktable.Node, 8*w.SampleInfoHashes),
		forTriage:    make(chan hashAndNode, 2048),
		forPeers:     make(chan hashAndNode, 8*w.GetPeers),
		forFetch:     make(chan infoHashWithPeers, 8*w.Metadata),
		forPersist:   make(chan store.Row, 1024),
		seenHashes:   newSeenHashes(cfg.SeenHashesSize, cfg.SeenHashesFalsePositiveRate),
		soughtNodeID: &concurrency.AtomicValue[protocol.ID]{},
	}, nil
}

// Run starts the crawl workers and blocks until ctx is cancelled, then waits
// for them to stop. A cancelled context is an orderly shutdown and returns nil;
// the completed batches are written before Run returns. Run may only be called
// once per crawler.
func (c *Crawler) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	c.startDiscovery(ctx, &wg)
	c.startTriage(ctx, &wg)
	c.startFetch(ctx, &wg)
	startWorker(ctx, &wg, c.runPersist)

	<-ctx.Done()
	wg.Wait()

	return nil
}

// startWorker starts one long-running goroutine and registers it with wg.
func startWorker(ctx context.Context, wg *sync.WaitGroup, fn func(context.Context)) {
	wg.Add(1)

	go func() {
		defer wg.Done()

		fn(ctx)
	}()
}

// startPool starts n workers that each apply fn to items read from in until ctx
// is cancelled.
func startPool[T any](
	ctx context.Context,
	wg *sync.WaitGroup,
	n int,
	in <-chan T,
	fn func(context.Context, T),
) {
	for range n {
		startWorker(ctx, wg, func(ctx context.Context) {
			for {
				item, ok := recv(ctx, in)
				if !ok {
					return
				}

				fn(ctx, item)
			}
		})
	}
}

// send delivers v on ch, giving up when ctx is cancelled. Every send in the
// package goes through it: a cancelled crawl must not block a worker on a
// channel whose readers have stopped, and a bare select with a done case is
// exactly where a break would silently stop the wrong loop.
func send[T any](ctx context.Context, ch chan<- T, v T) bool {
	select {
	case ch <- v:
		return true
	case <-ctx.Done():
		return false
	}
}

// recv reads one item from ch, giving up when ctx is cancelled. The second
// result is false only when ctx is cancelled; the channels owned by the crawler
// are never closed.
func recv[T any](ctx context.Context, ch <-chan T) (T, bool) {
	select {
	case v := <-ch:
		return v, true
	case <-ctx.Done():
		var zero T

		return zero, false
	}
}

// seenHashes is a bloom filter of the infohashes this process has already
// triaged. Several sample workers mark it concurrently, so access is
// serialised; the lock is cheap next to the store queries it saves.
type seenHashes struct {
	mutex sync.Mutex
	bloom *bloom.BloomFilter
}

func newSeenHashes(size uint, falsePositiveRate float64) *seenHashes {
	return &seenHashes{bloom: bloom.NewWithEstimates(size, falsePositiveRate)}
}

// testAndAdd reports whether the hash was already in the filter and records it.
func (s *seenHashes) testAndAdd(hash protocol.ID) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.bloom.TestAndAdd(hash[:])
}
