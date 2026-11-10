// Package store is BitTrawler's storage layer: a directory of SQLite files,
// one per calendar month of created_at, holding one self-contained row per
// torrent.
//
// Writes go to the shard for the row's month and are deduplicated by infohash
// across the retained shards. Reads fan out over the shards, newest first, and
// stop early for date-ordered queries because shard time ranges are disjoint.
// Long-term retention unlinks the oldest shard files.
//
// The package assumes a single writer process. Readers may run concurrently in
// WAL mode, on a local filesystem, against the same directory.
package store
