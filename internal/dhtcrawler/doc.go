// Package dhtcrawler discovers infohashes on the BitTorrent DHT and fetches
// their metadata.
//
// Hashes come from sample_infohashes replies and from nodes that query this
// instance's DHT server (the latter supplies nodes only). A hash is triaged
// against the blocklist and the store before any work is spent on it; unseen
// hashes are looked up with get_peers, their metadata is requested from the
// peers that answer, and the resulting torrents are written to the store in
// batches.
//
// There is no queue and no reprocessing. A torrent is crawled once: a hash the
// store already holds is skipped, which is also why the seeders and leechers
// recorded with it are the values seen when it was first discovered.
//
// The crawler does not own its dependencies. The k-table, DHT client, metadata
// requester, banning checker, blocklist and store are constructed and closed by
// the caller; Run only starts and stops the crawl workers.
package dhtcrawler
