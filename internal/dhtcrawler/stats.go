package dhtcrawler

import (
	"sync/atomic"
)

// Stats is a snapshot of the crawler's process-lifetime counters. Rates are
// derived by differencing two snapshots rather than being computed here.
type Stats struct {
	// NodesDiscovered counts nodes reported by crawl responses.
	NodesDiscovered uint64
	// SamplesReceived counts BEP 51 replies.
	SamplesReceived uint64
	// SampleFailures counts sample_infohashes queries that failed for a reason
	// other than the node not supporting them, such as a lost packet.
	SampleFailures uint64
	// SampleUnsupported counts nodes that answered sample_infohashes with a
	// method unknown error. They are not asked again.
	SampleUnsupported uint64
	// HashesDiscovered counts infohashes in those replies, including ones this
	// process had already seen.
	HashesDiscovered uint64
	// HashesTriaged counts unique hashes put through the blocklist and store
	// checks.
	HashesTriaged uint64
	// PeerLookups counts get_peers replies that contained at least one peer.
	PeerLookups uint64
	// PeerLookupsEmpty counts get_peers replies that contained no peers. The
	// node answered, so it is reachable, but it could not help with the hash.
	PeerLookupsEmpty uint64
	// PeerLookupsFailed counts get_peers queries that failed or timed out, which
	// says the node is unreachable rather than unhelpful.
	PeerLookupsFailed uint64
	// MetadataFetched counts torrents whose metadata was fetched and accepted.
	MetadataFetched uint64
	// MetadataFailed counts torrents for which no peer supplied acceptable
	// metadata.
	MetadataFailed uint64
	// InfohashesBlocked counts hashes rejected by the banning rules.
	InfohashesBlocked uint64
	// InfohashesDeceptive counts hashes whose files were executables under a
	// media name, which are blocked as malware.
	InfohashesDeceptive uint64
	// TorrentsPersisted counts rows written to the store.
	TorrentsPersisted uint64
	// TorrentsDropped counts adult torrents the classifier dropped. Adult
	// content is dropped only when configured to be; banned and deceptive
	// content is counted by InfohashesBlocked and InfohashesDeceptive instead.
	TorrentsDropped uint64
	// TorrentsDroppedScript counts torrents dropped by the writing-system filter.
	TorrentsDroppedScript uint64
	// PersistFailures counts batches that could not be written.
	PersistFailures uint64
	// TMDBCalls counts classifications sent to the TMDB confirmation stage.
	TMDBCalls uint64
	// TMDBMatched counts lookups that found a confident match.
	TMDBMatched uint64
	// TMDBCorrected counts classifications TMDB changed.
	TMDBCorrected uint64
	// TMDBAdult counts torrents TMDB flagged as adult.
	TMDBAdult uint64
	// TMDBFailures counts lookups that could not be completed.
	TMDBFailures uint64
	// Pipeline is the state of every crawl stage at the moment of the snapshot.
	Pipeline PipelineStats
}

// StageStats describes one crawl stage: how many workers it has, how many items
// are waiting for one and how many are being worked on.
type StageStats struct {
	Workers  int
	Queued   int
	InFlight int
}

// PipelineStats is a snapshot of the whole crawl pipeline. A stage whose queue
// keeps filling and whose workers are all busy is the stage holding the crawl
// back.
type PipelineStats struct {
	Ping     StageStats
	FindNode StageStats
	Sample   StageStats
	GetPeers StageStats
	Metadata StageStats
	TMDB     StageStats
	Triage   StageStats
	Persist  StageStats
}

// crawlerStats holds the live counters behind Stats. Every field is incremented
// from a crawl worker, so they are atomics rather than plain integers and the
// crawl path pays for a single add per event.
type crawlerStats struct {
	nodesDiscovered       atomic.Uint64
	samplesReceived       atomic.Uint64
	sampleFailures        atomic.Uint64
	sampleUnsupported     atomic.Uint64
	hashesDiscovered      atomic.Uint64
	hashesTriaged         atomic.Uint64
	peerLookups           atomic.Uint64
	peerLookupsEmpty      atomic.Uint64
	peerLookupsFailed     atomic.Uint64
	metadataFetched       atomic.Uint64
	metadataFailed        atomic.Uint64
	infohashesBlocked     atomic.Uint64
	infohashesDeceptive   atomic.Uint64
	torrentsPersisted     atomic.Uint64
	torrentsDropped       atomic.Uint64
	torrentsDroppedScript atomic.Uint64
	persistFailures       atomic.Uint64
	tmdbCalls             atomic.Uint64
	tmdbMatched           atomic.Uint64
	tmdbCorrected         atomic.Uint64
	tmdbAdult             atomic.Uint64
	tmdbFailures          atomic.Uint64
}

// Stats returns a snapshot of the crawler's counters. It is safe to call while
// the crawl is running; the calls may not all observe the same instant.
func (c *Crawler) Stats() Stats {
	return Stats{
		NodesDiscovered:       c.stats.nodesDiscovered.Load(),
		SamplesReceived:       c.stats.samplesReceived.Load(),
		SampleFailures:        c.stats.sampleFailures.Load(),
		SampleUnsupported:     c.stats.sampleUnsupported.Load(),
		HashesDiscovered:      c.stats.hashesDiscovered.Load(),
		HashesTriaged:         c.stats.hashesTriaged.Load(),
		PeerLookups:           c.stats.peerLookups.Load(),
		PeerLookupsEmpty:      c.stats.peerLookupsEmpty.Load(),
		PeerLookupsFailed:     c.stats.peerLookupsFailed.Load(),
		MetadataFetched:       c.stats.metadataFetched.Load(),
		MetadataFailed:        c.stats.metadataFailed.Load(),
		InfohashesBlocked:     c.stats.infohashesBlocked.Load(),
		InfohashesDeceptive:   c.stats.infohashesDeceptive.Load(),
		TorrentsPersisted:     c.stats.torrentsPersisted.Load(),
		TorrentsDropped:       c.stats.torrentsDropped.Load(),
		TorrentsDroppedScript: c.stats.torrentsDroppedScript.Load(),
		PersistFailures:       c.stats.persistFailures.Load(),
		TMDBCalls:             c.stats.tmdbCalls.Load(),
		TMDBMatched:           c.stats.tmdbMatched.Load(),
		TMDBCorrected:         c.stats.tmdbCorrected.Load(),
		TMDBAdult:             c.stats.tmdbAdult.Load(),
		TMDBFailures:          c.stats.tmdbFailures.Load(),
		Pipeline:              c.pipelineStats(),
	}
}

// pipelineStats reads the current queue depths and worker occupancy.
func (c *Crawler) pipelineStats() PipelineStats {
	return PipelineStats{
		Ping: StageStats{
			Workers:  c.cfg.Workers.Ping,
			Queued:   len(c.forPing),
			InFlight: int(c.inFlight.ping.Load()),
		},
		FindNode: StageStats{
			Workers:  c.cfg.Workers.FindNode,
			Queued:   len(c.forFind),
			InFlight: int(c.inFlight.findNode.Load()),
		},
		Sample: StageStats{
			Workers:  c.cfg.Workers.SampleInfoHashes,
			Queued:   len(c.forSample),
			InFlight: int(c.inFlight.sample.Load()),
		},
		GetPeers: StageStats{
			Workers:  c.cfg.Workers.GetPeers,
			Queued:   len(c.forPeers),
			InFlight: int(c.inFlight.getPeers.Load()),
		},
		Metadata: StageStats{
			Workers:  c.cfg.Workers.Metadata,
			Queued:   len(c.forFetch),
			InFlight: int(c.inFlight.metadata.Load()),
		},
		TMDB: StageStats{
			Workers:  c.cfg.TMDBWorkers,
			Queued:   len(c.forTMDB),
			InFlight: int(c.inFlight.tmdb.Load()),
		},
		Triage: StageStats{
			Workers:  1,
			Queued:   len(c.forTriage),
			InFlight: int(c.inFlight.triage.Load()),
		},
		Persist: StageStats{
			Workers:  1,
			Queued:   len(c.forPersist),
			InFlight: int(c.inFlight.persist.Load()),
		},
	}
}
