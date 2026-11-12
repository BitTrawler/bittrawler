package store

import (
	"context"
	"fmt"
	"os"
)

// ShardStats describes one monthly shard on disk.
type ShardStats struct {
	// Month is the shard's month, formatted as YYYY-MM.
	Month string
	// Rows is the number of torrents in the shard.
	Rows int
	// DBBytes is the size of the database file. SQLite writes in pages, so this
	// is the file's allocated content, not the sum of the rows.
	DBBytes int64
	// WALBytes is the size of the write-ahead log file, zero when it has been
	// checkpointed away.
	WALBytes int64
}

// Stats is the store's on-disk footprint.
type Stats struct {
	// Shards is ordered newest month first.
	Shards []ShardStats
	Rows   int
	// DBBytes and WALBytes are the totals across shards.
	DBBytes  int64
	WALBytes int64
}

// Stats reports the row count and file sizes of every retained shard. Counting
// rows means a scan of each shard, so this is meant for a timer rather than a
// request path; use the crawler's counters for live rates.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	shards := s.shardsNewestFirstLocked()

	var stats Stats

	for _, sh := range shards {
		var rows int
		if err := sh.db.QueryRowContext(ctx, "SELECT count(*) FROM torrents").Scan(&rows); err != nil {
			return Stats{}, fmt.Errorf("count shard %s: %w", sh.key, err)
		}

		dbBytes, err := fileSize(sh.path)
		if err != nil {
			return Stats{}, err
		}

		walBytes, err := fileSize(sh.path + "-wal")
		if err != nil {
			return Stats{}, err
		}

		stats.Shards = append(stats.Shards, ShardStats{
			Month:    sh.key,
			Rows:     rows,
			DBBytes:  dbBytes,
			WALBytes: walBytes,
		})
		stats.Rows += rows
		stats.DBBytes += dbBytes
		stats.WALBytes += walBytes
	}

	return stats, nil
}

// fileSize returns the size of path, or zero when it does not exist.
func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}

	return info.Size(), nil
}
