package store

import (
	"context"
	"fmt"
	"time"
)

// Removal identifies a stored row to delete: where it lives and the category it
// counts under. The row itself is not needed; the caller already has it from a
// scan.
type Removal struct {
	InfoHash  [20]byte
	CreatedAt time.Time
	Category  int
}

// Delete removes stored torrents from their shards. It is one transaction per
// shard, so a pass over every month costs one commit per month. The search
// index follows through the delete trigger, and the in-memory totals are
// adjusted after each commit.
//
// Deletion is for maintenance passes that reject rows after the fact; the crawl
// rejects them before writing instead.
func (s *Store) Delete(ctx context.Context, removals []Removal) (int, error) {
	if len(removals) == 0 {
		return 0, nil
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	byMonth := make(map[string][]Removal, 4)
	order := make([]string, 0, 4)

	for _, removal := range removals {
		key := monthKey(removal.CreatedAt)
		if _, ok := byMonth[key]; !ok {
			order = append(order, key)
		}

		byMonth[key] = append(byMonth[key], removal)
	}

	deleted := 0

	for _, key := range order {
		s.mu.RLock()
		sh, ok := s.shards[key]
		s.mu.RUnlock()

		if !ok {
			return deleted, fmt.Errorf("store: shard %s is no longer open", key)
		}

		n, removed, err := deleteShardRows(ctx, sh, byMonth[key])
		deleted += n

		if err != nil {
			return deleted, err
		}

		s.removeCounts(key, removed, n)
	}

	return deleted, nil
}

// deleteShardRows applies one shard's deletions in a transaction and reports
// how many rows went and under which categories.
func deleteShardRows(ctx context.Context, sh *shard, removals []Removal) (int, map[int]int, error) {
	tx, err := sh.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("delete from shard %s: %w", sh.key, err)
	}

	defer func() { _ = tx.Rollback() }()

	deleted := 0
	removed := make(map[int]int)

	for _, removal := range removals {
		result, err := tx.ExecContext(ctx, "DELETE FROM torrents WHERE info_hash = ?", removal.InfoHash[:])
		if err != nil {
			return deleted, nil, fmt.Errorf("delete from shard %s: %w", sh.key, err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return deleted, nil, fmt.Errorf("delete from shard %s: %w", sh.key, err)
		}

		if affected == 0 {
			continue
		}

		deleted++
		removed[removal.Category]++
	}

	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("delete from shard %s: %w", sh.key, err)
	}

	return deleted, removed, nil
}
