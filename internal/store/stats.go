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

// Stats is the store's on-disk footprint and contents.
type Stats struct {
	// Shards is ordered newest month first.
	Shards []ShardStats
	Rows   int
	// DBBytes and WALBytes are the totals across shards.
	DBBytes  int64
	WALBytes int64
	// Categories counts rows per stored Newznab category across all shards.
	Categories map[int]int
}

// Stats reports the row count, the category breakdown and the file sizes of
// every retained shard. The counts come from memory, so this is cheap enough
// for a timer; only the file sizes are read from disk.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	s.mu.RLock()
	shards := s.shardsNewestFirstLocked()
	s.mu.RUnlock()

	s.countsMu.Lock()
	shardRows := make(map[string]int, len(s.shardRows))
	for key, rows := range s.shardRows {
		shardRows[key] = rows
	}
	categories := make(map[int]int, len(s.categories))
	for category, rows := range s.categories {
		categories[category] = rows
	}
	s.countsMu.Unlock()

	stats := Stats{Categories: categories}

	for _, sh := range shards {
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
			Rows:     shardRows[sh.key],
			DBBytes:  dbBytes,
			WALBytes: walBytes,
		})
		stats.Rows += shardRows[sh.key]
		stats.DBBytes += dbBytes
		stats.WALBytes += walBytes
	}

	return stats, nil
}

// shardCounts counts the shard's rows and groups them by category. One grouped
// query per shard returns both, so the totals cannot disagree with the
// per-category counts.
func shardCounts(ctx context.Context, sh *shard) (int, map[int]int, error) {
	rows, err := sh.db.QueryContext(ctx, "SELECT category, count(*) FROM torrents GROUP BY category")
	if err != nil {
		return 0, nil, fmt.Errorf("count shard %s: %w", sh.key, err)
	}

	defer rows.Close()

	var (
		total      int
		categories = make(map[int]int)
	)

	for rows.Next() {
		var (
			category int
			count    int
		)

		if err := rows.Scan(&category, &count); err != nil {
			return 0, nil, fmt.Errorf("count shard %s: %w", sh.key, err)
		}

		categories[category] += count
		total += count
	}

	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("count shard %s: %w", sh.key, err)
	}

	return total, categories, nil
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
