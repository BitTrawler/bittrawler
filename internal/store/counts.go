package store

import (
	"context"
	"fmt"
)

// seedCounts reads every open shard once and fills the in-memory totals. This
// is the only time the store scans its rows for statistics; writes and
// retention keep the totals current afterwards.
func (s *Store) seedCounts(ctx context.Context) error {
	s.mu.RLock()
	shards := s.shardsNewestFirstLocked()
	s.mu.RUnlock()

	shardRows := make(map[string]int, len(shards))
	categories := make(map[int]int)

	for _, sh := range shards {
		rows, counts, err := shardCounts(ctx, sh)
		if err != nil {
			return fmt.Errorf("seed counts: %w", err)
		}

		shardRows[sh.key] = rows

		for category, count := range counts {
			categories[category] += count
		}
	}

	s.countsMu.Lock()
	defer s.countsMu.Unlock()

	s.shardRows = shardRows
	s.categories = categories

	return nil
}

// addCounts records rows committed by a write.
func (s *Store) addCounts(key string, rows int, added map[int]int) {
	if rows == 0 {
		return
	}

	s.countsMu.Lock()
	defer s.countsMu.Unlock()

	s.shardRows[key] += rows

	for category, count := range added {
		s.categories[category] += count
	}
}

// subtractCounts forgets a shard removed by retention.
func (s *Store) subtractCounts(key string, removed map[int]int) {
	s.countsMu.Lock()
	defer s.countsMu.Unlock()

	delete(s.shardRows, key)

	for category, count := range removed {
		s.categories[category] -= count

		if s.categories[category] <= 0 {
			delete(s.categories, category)
		}
	}
}
