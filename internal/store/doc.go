// Package store is BitTrawler's storage layer: a directory of SQLite files, one
// per calendar month of created_at, holding one self-contained row per torrent.
//
// Writes go to the shard for the row's month; deduplication checks the shards
// that are kept. Reads fan out over the shards newest first and stop early for
// date-ordered queries, because each shard covers a disjoint range of time.
// Retention unlinks the oldest shard files.
//
// One process writes. Readers may run concurrently in WAL mode against the same
// directory, as long as the filesystem is local.
package store
