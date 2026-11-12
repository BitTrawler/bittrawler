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
}

// crawlerStats holds the live counters behind Stats. Every field is incremented
// from a crawl worker, so they are atomics rather than plain integers and the
// crawl path pays for a single add per event.
type crawlerStats struct {
	nodesDiscovered       atomic.Uint64
	samplesReceived       atomic.Uint64
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
	}
}
