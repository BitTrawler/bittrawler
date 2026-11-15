package store

import (
	"context"
	"fmt"
	"time"
)

// Classified is the part of a row that reclassification can change: everything
// derived from the name and the file list. The infohash and the created time
// locate the row; content_id is left alone because a TMDB lookup cannot be
// repeated from stored data.
type Classified struct {
	InfoHash  [20]byte
	CreatedAt time.Time
	// From is the category the row is stored under now. It is required: the
	// store moves the row's place in the category breakdown from this category
	// to Category, so the overview follows a pass without a restart.
	From     int
	Category int
	Seasons  []int
	Episodes []int
	AirDate  string
}

// ForEach calls fn for every stored torrent, newest shard first. It streams the
// rows, so a large index does not have to fit in memory. It is meant for a
// maintenance command run while the writer is stopped: the caller should hold
// the data directory lock.
func (s *Store) ForEach(ctx context.Context, fn func(Torrent) error) error {
	s.mu.RLock()
	shards := s.shardsNewestFirstLocked()
	s.mu.RUnlock()

	for _, sh := range shards {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := forEachInShard(ctx, sh, fn); err != nil {
			return err
		}
	}

	return nil
}

// forEachInShard streams one shard's rows into fn.
func forEachInShard(ctx context.Context, sh *shard, fn func(Torrent) error) error {
	rows, err := sh.db.QueryContext(ctx, "SELECT "+torrentColumns+" FROM torrents t")
	if err != nil {
		return fmt.Errorf("read shard %s: %w", sh.key, err)
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		torrent, err := scanTorrent(rows)
		if err != nil {
			return fmt.Errorf("read shard %s: %w", sh.key, err)
		}

		if err := fn(torrent); err != nil {
			return err
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read shard %s: %w", sh.key, err)
	}

	return nil
}

// UpdateClassifications writes the derived fields of changed rows: category,
// the season and episode tokens search uses, and the kind token. Rows are
// grouped into one transaction per shard, because that is the unit SQLite
// commits. The in-memory category breakdown follows each commit, so the
// overview's classification table counts the rows under their new category
// straight away.
func (s *Store) UpdateClassifications(ctx context.Context, changes []Classified) (int, error) {
	if len(changes) == 0 {
		return 0, nil
	}

	for _, change := range changes {
		if change.From <= 0 {
			return 0, fmt.Errorf("store: update torrent %x has no category to move from", change.InfoHash)
		}
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	byMonth := make(map[string][]Classified, 4)
	order := make([]string, 0, 4)

	for _, change := range changes {
		key := monthKey(change.CreatedAt)
		if _, ok := byMonth[key]; !ok {
			order = append(order, key)
		}

		byMonth[key] = append(byMonth[key], change)
	}

	updated := 0

	for _, key := range order {
		s.mu.RLock()
		sh, ok := s.shards[key]
		s.mu.RUnlock()

		if !ok {
			return updated, fmt.Errorf("store: shard %s is no longer open", key)
		}

		n, removed, added, err := updateShardClassifications(ctx, sh, byMonth[key])
		updated += n

		if err != nil {
			return updated, err
		}

		s.moveCounts(removed, added)
	}

	return updated, nil
}

// updateShardClassifications applies one shard's changes in a transaction and
// reports which categories they left and joined.
func updateShardClassifications(ctx context.Context, sh *shard, changes []Classified) (int, map[int]int, map[int]int, error) {
	tx, err := sh.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, nil, err
	}

	defer func() { _ = tx.Rollback() }()

	statement, err := tx.PrepareContext(ctx, `UPDATE torrents
		SET category = ?, seasons = ?, episodes = ?, kind = ?, air_date = ?
		WHERE info_hash = ?`)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("prepare update for shard %s: %w", sh.key, err)
	}

	defer func() { _ = statement.Close() }()

	updated := 0
	removed := make(map[int]int)
	added := make(map[int]int)

	for _, change := range changes {
		result, err := statement.ExecContext(ctx,
			change.Category,
			seasonsToken(change.Seasons),
			episodesToken(change.Episodes),
			kindToken(change.Seasons, change.Episodes),
			nullIfEmpty(change.AirDate),
			change.InfoHash[:],
		)
		if err != nil {
			return updated, nil, nil, fmt.Errorf("update torrent %x in shard %s: %w", change.InfoHash, sh.key, err)
		}

		n, err := result.RowsAffected()
		if err != nil {
			return updated, nil, nil, err
		}

		if n == 0 {
			continue
		}

		updated += int(n)

		if change.From != change.Category {
			removed[change.From]++
			added[change.Category]++
		}
	}

	if err := tx.Commit(); err != nil {
		return updated, nil, nil, err
	}

	return updated, removed, added, nil
}
