package dhtcrawler

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BitTrawler/bittrawler/internal/blocking"
	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/concurrency"
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/client"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/banning"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/metainforequester"
	"github.com/BitTrawler/bittrawler/internal/store"
	"github.com/bits-and-blooms/bloom/v3"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
)

const (
	// failedPeerTTL is how long a peer that did not supply metadata is skipped.
	// It keeps a timeout on a blackholed address from being paid again for every
	// torrent that lists it, without writing off a peer that is restarting.
	failedPeerTTL = 15 * time.Minute
	// failedPeerCapacity bounds the memory spent on that cache: a few megabytes
	// at tens of bytes per address.
	failedPeerCapacity = 100_000
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
	cfg        Config
	kTable     ktable.Table
	client     client.Client
	requester  metainforequester.Requester
	banning    banning.Checker
	classifier *classify.Live
	tmdb       TMDBSearcher
	blocklist  *blocking.Blocklist
	store      *store.Store
	state      StateStore
	logger     *slog.Logger
	stats      crawlerStats

	inbound    <-chan ktable.Node
	discovered chan ktable.Node
	forPing    chan ktable.Node
	forFind    chan ktable.Node
	forSample  chan ktable.Node
	forTriage  chan hashAndNode
	forPeers   chan hashAndNode
	forFetch   chan infoHashWithPeers
	forTMDB    chan tmdbJob
	forPersist chan store.Row

	seenHashes   *seenHashes
	soughtNodeID *concurrency.AtomicValue[protocol.ID]
	failedPeers  *lru.LRU[netip.AddrPort, struct{}]

	inFlight inFlightCounters
}

// inFlightCounters is how many workers of each stage are busy at this instant.
type inFlightCounters struct {
	ping     atomic.Int64
	findNode atomic.Int64
	sample   atomic.Int64
	getPeers atomic.Int64
	metadata atomic.Int64
	tmdb     atomic.Int64
	triage   atomic.Int64
	persist  atomic.Int64
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

	if cfg.Classifier == nil {
		cfg.Classifier = classify.NewLive(classify.Config{})
	}

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
		classifier:   cfg.Classifier,
		tmdb:         cfg.TMDB,
		blocklist:    cfg.Blocklist,
		store:        cfg.Store,
		state:        cfg.State,
		logger:       cfg.Logger,
		inbound:      cfg.DiscoveredNodes,
		discovered:   make(chan ktable.Node, 1024),
		forPing:      make(chan ktable.Node, w.Ping),
		forFind:      make(chan ktable.Node, 8*w.FindNode),
		forSample:    make(chan ktable.Node, 8*w.SampleInfoHashes),
		forTriage:    make(chan hashAndNode, 2048),
		forPeers:     make(chan hashAndNode, 8*w.GetPeers),
		forFetch:     make(chan infoHashWithPeers, 8*w.Metadata),
		forTMDB:      make(chan tmdbJob, 8*cfg.TMDBWorkers),
		forPersist:   make(chan store.Row, 1024),
		seenHashes:   newSeenHashes(cfg.SeenHashesSize, cfg.SeenHashesFalsePositiveRate),
		soughtNodeID: &concurrency.AtomicValue[protocol.ID]{},
		failedPeers:  lru.NewLRU[netip.AddrPort, struct{}](failedPeerCapacity, nil, failedPeerTTL),
	}, nil
}

// Run starts the crawl workers and blocks until ctx is cancelled, then waits
// for them to stop. A cancelled context is an orderly shutdown and returns nil;
// the completed batches are written before Run returns. Run may only be called
// once per crawler.
func (c *Crawler) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.restoreState(ctx)

	var wg sync.WaitGroup

	startWorker(ctx, &wg, c.runStateSaver)
	c.startDiscovery(ctx, &wg)
	c.startTriage(ctx, &wg)
	c.startFetch(ctx, &wg)
	startWorker(ctx, &wg, c.runPersist)
	c.startTMDB(ctx, &wg)

	<-ctx.Done()
	wg.Wait()
	c.flushTMDBQueue()

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
	inFlight *atomic.Int64,
	fn func(context.Context, T),
) {
	for range n {
		startWorker(ctx, wg, func(ctx context.Context) {
			for {
				item, ok := recv(ctx, in)
				if !ok {
					return
				}

				inFlight.Add(1)
				fn(ctx, item)
				inFlight.Add(-1)
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

// seenHashes is a rotating pair of bloom filters holding the infohashes this
// process has already triaged. Several sample workers mark it concurrently, so
// access is serialised; the lock is cheap next to the store queries it saves.
//
// A single filter degrades as it fills. At a typical crawl rate the configured
// capacity is reached in about two days, and after that the false-positive rate
// climbs: a few per cent within another day and tens of per cent within a week,
// so genuinely new hashes are skipped. Rotating keeps two generations instead.
// Both are tested, adds go to the active one, and when it reaches its share of
// the capacity the oldest generation is cleared and becomes the active one.
//
// The filter is an optimisation for the store's seen check, never the
// authority, so hashes that fall out of the window are simply offered again and
// rejected by the store.
type seenHashes struct {
	mutex sync.Mutex
	// blooms[active] is the generation being filled; the other index holds the
	// previous generation. Both are tested.
	blooms [2]*bloom.BloomFilter
	active int
	added  uint
	// perGen is how many hashes one generation holds, which is half the
	// configured size.
	perGen uint
}

func newSeenHashes(size uint, falsePositiveRate float64) *seenHashes {
	// The two generations share the configured capacity, so rotating does not
	// change what the filter costs. A size of one still needs a usable filter.
	perGen := size / 2
	if perGen == 0 {
		perGen = 1
	}

	return &seenHashes{
		blooms: [2]*bloom.BloomFilter{
			bloom.NewWithEstimates(perGen, falsePositiveRate),
			bloom.NewWithEstimates(perGen, falsePositiveRate),
		},
		perGen: perGen,
	}
}

// seen reports whether the hash is in either generation.
func (s *seenHashes) seen(hash protocol.ID) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.blooms[0].Test(hash[:]) || s.blooms[1].Test(hash[:])
}

// add records the hash. It is called once the hash has been accepted for
// triage, not when it is first seen: a hash dropped under back-pressure is left
// out of the filter so another node's sample can offer it again later.
func (s *seenHashes) add(hash protocol.ID) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.blooms[s.active].Add(hash[:])
	s.added++

	if s.added >= s.perGen {
		s.rotate()
	}
}

// rotate retires the oldest generation. The index that becomes active is the
// one that held the generation from two windows ago, so clearing it keeps the
// previous window's hashes and starts a fresh one.
func (s *seenHashes) rotate() {
	s.active ^= 1
	s.blooms[s.active].ClearAll()
	s.added = 0
}
