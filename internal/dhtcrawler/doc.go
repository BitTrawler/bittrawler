// Package dhtcrawler discovers infohashes on the BitTorrent DHT and fetches
// their metadata.
//
// Hashes come from sample_infohashes replies. Nodes that query this instance's
// own DHT server are used to grow the routing table, not as a source of hashes.
//
// A hash is triaged against the blocklist and the store before any work is
// spent on it. A new hash is looked up with get_peers, its metadata is
// requested from the peers that answer, and the torrent is written to the store
// in a batch. A torrent is crawled once: there is no queue and no reprocessing,
// which is why the seeders and leechers stored with it are the values seen when
// it was first discovered.
//
// The crawler does not own its dependencies. The k-table, DHT client, metadata
// requester, banning checker, blocklist and store are opened and closed by the
// caller; Run starts and stops the workers.
package dhtcrawler
