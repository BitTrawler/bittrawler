package dhtcrawler

import (
	"log/slog"
	"time"

	"github.com/BitTrawler/bittrawler/internal/blocking"
	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/client"
	"github.com/BitTrawler/bittrawler/internal/protocol/dht/ktable"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/banning"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo/metainforequester"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// Config configures the crawler. The zero value is not usable on its own: New
// fills in the timing and worker defaults, but the dependencies must be set.
type Config struct {
	// KTable is the routing table shared with the DHT server and responder.
	KTable ktable.Table
	// DHT is the client used for every outgoing query.
	DHT client.Client
	// Requester fetches metadata from peers.
	Requester metainforequester.Requester
	// Banning rejects metadata that is not worth indexing.
	Banning banning.Checker
	// Blocklist filters hashes blocked earlier and records hashes blocked
	// during the crawl.
	Blocklist *blocking.Blocklist
	// Store receives the crawled torrents.
	Store *store.Store
	// Classification controls what happens to torrents the classifier rejects.
	Classification classify.Config
	// DiscoveredNodes receives nodes that query the DHT server. It is optional:
	// when nil the crawler still discovers nodes from its own queries.
	DiscoveredNodes <-chan ktable.Node
	// BootstrapNodes seeds the routing table. Entries are host:port pairs.
	BootstrapNodes []string
	// BootstrapReseedInterval is how often the bootstrap nodes are re-resolved
	// and queued. Their IDs are learnt by pinging them, so repeating this keeps
	// the table anchored if the bootstrap addresses change.
	BootstrapReseedInterval time.Duration
	// OldNodeAge is how long a node can go without a successful response before
	// it is pinged again.
	OldNodeAge time.Duration
	// OldNodeCheckInterval is how often the table is scanned for stale nodes.
	OldNodeCheckInterval time.Duration
	// SampleInfoHashesOverride replaces the interval a node asks for in its
	// sample_infohashes reply when the reply contained hashes this process had
	// not seen. Zero honours the node's requested interval.
	SampleInfoHashesOverride time.Duration
	// SeenHashesSize is how many infohashes the in-process bloom filter is sized
	// for, and SeenHashesFalsePositiveRate is its false positive rate. The filter
	// stops the same hash being triaged more than once per process; it is not
	// persisted.
	SeenHashesSize              uint
	SeenHashesFalsePositiveRate float64
	// TriageBatchSize and TriageBatchWait bound how long discovered hashes wait
	// to be checked against the blocklist and the store.
	TriageBatchSize int
	TriageBatchWait time.Duration
	// PersistBatchSize and PersistBatchWait bound how long crawled torrents wait
	// to be written.
	PersistBatchSize int
	PersistBatchWait time.Duration
	// PeersPerInfoHash is the maximum number of peers metadata is requested
	// from. A popular torrent can return hundreds of peers, most of them gone.
	PeersPerInfoHash int
	// MetadataTimeout bounds the whole metadata fetch for one torrent, across
	// every peer tried.
	MetadataTimeout time.Duration
	// Workers sets how many goroutines run each pipeline stage.
	Workers WorkerCounts
	// Logger receives crawl events. A discarding logger is used when nil.
	Logger *slog.Logger
}

// WorkerCounts sets the concurrency of each pipeline stage. All values are
// goroutine counts, and each stage is independent: raising the metadata count
// does nothing for discovery if the sample count stays at one.
type WorkerCounts struct {
	// Ping checks whether a stale node is still alive.
	Ping int
	// FindNode asks nodes for the nodes closest to a random target, which grows
	// the routing table.
	FindNode int
	// SampleInfoHashes asks BEP 51 capable nodes for infohashes.
	SampleInfoHashes int
	// GetPeers looks up peers for an unseen infohash.
	GetPeers int
	// Metadata connects to peers and fetches torrent metadata.
	Metadata int
}

// DefaultConfig returns the crawler settings with no dependencies set. Callers
// start from this and fill in the dependencies.
func DefaultConfig() Config {
	return Config{
		BootstrapNodes:              defaultBootstrapNodes,
		BootstrapReseedInterval:     10 * time.Minute,
		OldNodeAge:                  15 * time.Minute,
		OldNodeCheckInterval:        10 * time.Second,
		SampleInfoHashesOverride:    time.Minute,
		SeenHashesSize:              10_000_000,
		SeenHashesFalsePositiveRate: 0.001,
		TriageBatchSize:             500,
		TriageBatchWait:             5 * time.Second,
		PersistBatchSize:            100,
		PersistBatchWait:            5 * time.Second,
		PeersPerInfoHash:            4,
		MetadataTimeout:             45 * time.Second,
		Workers: WorkerCounts{
			Ping:             4,
			FindNode:         8,
			SampleInfoHashes: 8,
			GetPeers:         16,
			Metadata:         32,
		},
	}
}

// defaultBootstrapNodes is the set the reference DHT client uses. They are
// hostnames rather than addresses because the addresses move.
var defaultBootstrapNodes = []string{
	"router.utorrent.com:6881",
	"router.bittorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"dht.aelitis.com:6881",
	"router.silotis.us:6881",
	"dht.libtorrent.org:25401",
}

// minSampleInfoHashesInterval is the shortest gap the crawler will leave
// between sample_infohashes requests to one node, whatever the node asks for.
const minSampleInfoHashesInterval = time.Minute

// withDefaults returns a copy of c with every unset value replaced by its
// default. SampleInfoHashesOverride is the exception: zero is a meaningful
// value there (honour the node's interval), so it is left alone.
func (c Config) withDefaults() Config {
	def := DefaultConfig()

	if c.BootstrapNodes == nil {
		c.BootstrapNodes = def.BootstrapNodes
	}

	if c.BootstrapReseedInterval <= 0 {
		c.BootstrapReseedInterval = def.BootstrapReseedInterval
	}

	if c.OldNodeAge <= 0 {
		c.OldNodeAge = def.OldNodeAge
	}

	if c.OldNodeCheckInterval <= 0 {
		c.OldNodeCheckInterval = def.OldNodeCheckInterval
	}

	if c.SeenHashesSize == 0 {
		c.SeenHashesSize = def.SeenHashesSize
	}

	if c.SeenHashesFalsePositiveRate <= 0 || c.SeenHashesFalsePositiveRate >= 1 {
		c.SeenHashesFalsePositiveRate = def.SeenHashesFalsePositiveRate
	}

	if c.TriageBatchSize <= 0 {
		c.TriageBatchSize = def.TriageBatchSize
	}

	if c.TriageBatchWait <= 0 {
		c.TriageBatchWait = def.TriageBatchWait
	}

	if c.PersistBatchSize <= 0 {
		c.PersistBatchSize = def.PersistBatchSize
	}

	if c.PersistBatchWait <= 0 {
		c.PersistBatchWait = def.PersistBatchWait
	}

	if c.PeersPerInfoHash <= 0 {
		c.PeersPerInfoHash = def.PeersPerInfoHash
	}

	if c.MetadataTimeout <= 0 {
		c.MetadataTimeout = def.MetadataTimeout
	}

	c.Workers = c.Workers.withDefaults(def.Workers)

	return c
}

func (w WorkerCounts) withDefaults(def WorkerCounts) WorkerCounts {
	if w.Ping <= 0 {
		w.Ping = def.Ping
	}

	if w.FindNode <= 0 {
		w.FindNode = def.FindNode
	}

	if w.SampleInfoHashes <= 0 {
		w.SampleInfoHashes = def.SampleInfoHashes
	}

	if w.GetPeers <= 0 {
		w.GetPeers = def.GetPeers
	}

	if w.Metadata <= 0 {
		w.Metadata = def.Metadata
	}

	return w
}
