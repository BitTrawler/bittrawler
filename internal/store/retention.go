package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
)

// DropExpired removes whole shards older than the retention window. Deleting a
// month is unlinking its files: no rows are deleted, no vacuum runs, and no
// pages are rewritten. Callers should quiesce queries while it runs.
func (s *Store) DropExpired(ctx context.Context) ([]string, error) {
	if s.cfg.RetentionMonths <= 0 {
		return nil, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cutoff := monthKey(s.cfg.Now().UTC().AddDate(0, -(s.cfg.RetentionMonths - 1), 0))

	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		dropped []string
		errs    []error
	)

	for key, sh := range s.shards {
		if key >= cutoff {
			continue
		}

		// Remove the shard from the open set first: a failed unlink must not
		// leave a closed handle that every later query would trip over.
		delete(s.shards, key)

		if err := sh.db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close shard %s: %w", key, err))
		}

		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(sh.path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove %s%s: %w", sh.path, suffix, err))
			}
		}

		dropped = append(dropped, key)
	}

	sort.Strings(dropped)

	return dropped, errors.Join(errs...)
}

// ShardMonths returns the months that the store currently has open, oldest
// first. It exists for status reporting and tests.
func (s *Store) ShardMonths() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]string, 0, len(s.shards))
	for key := range s.shards {
		out = append(out, key)
	}

	sort.Strings(out)

	return out
}
