package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const insertTorrentSQL = `INSERT INTO torrents
	(info_hash, name, size, created_at, category, content_id, seasons, episodes,
	 kind, air_date, files_count, primary_file, seeders, leechers)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

// Seen reports whether the infohash is present in any retained shard.
func (s *Store) Seen(ctx context.Context, hash [20]byte) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, sh := range s.shardsNewestFirstLocked() {
		found, err := exists(ctx, sh.db, hash)
		if err != nil {
			return false, err
		}

		if found {
			return true, nil
		}
	}

	return false, nil
}

// Put writes one torrent. It returns false when the infohash has already been
// seen, which is a rediscovery rather than a new torrent.
func (s *Store) Put(ctx context.Context, row Row) (bool, error) {
	n, err := s.PutBatch(ctx, []Row{row})

	return n > 0, err
}

// PutBatch writes torrents, skipping any whose infohash is already stored. Rows
// are routed to the shard for their created_at month, so an import or backfill
// creates historical shards while live ingest always lands in the current one.
func (s *Store) PutBatch(ctx context.Context, rows []Row) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}

	for _, row := range rows {
		if row.CreatedAt.IsZero() {
			return 0, fmt.Errorf("store: torrent %x has a zero created_at", row.InfoHash)
		}
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	groups := make(map[string][]Row, 4)
	order := make([]string, 0, 4)

	for _, row := range rows {
		key := monthKey(row.CreatedAt)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}

		groups[key] = append(groups[key], row)
	}

	inserted := 0

	for _, key := range order {
		month, err := monthStart(key)
		if err != nil {
			return inserted, err
		}

		sh, err := s.shardFor(month)
		if err != nil {
			return inserted, err
		}

		n, err := s.putGroup(ctx, key, sh, groups[key])
		inserted += n

		if err != nil {
			return inserted, err
		}
	}

	return inserted, nil
}

func (s *Store) putGroup(ctx context.Context, key string, sh *shard, rows []Row) (int, error) {
	tx, err := sh.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}

	defer func() { _ = tx.Rollback() }()

	inserted := 0

	for _, row := range rows {
		dup, err := exists(ctx, tx, row.InfoHash)
		if err != nil {
			return inserted, err
		}

		if !dup {
			dup, err = s.existsInOtherShards(ctx, key, row.InfoHash)
			if err != nil {
				return inserted, err
			}
		}

		if dup {
			continue
		}

		if _, err := tx.ExecContext(ctx, insertTorrentSQL,
			row.InfoHash[:], row.Name, row.Size, row.CreatedAt.UTC().Unix(),
			row.Category, nullableInt(row.ContentID), seasonsToken(row.Seasons),
			episodesToken(row.Episodes), kindToken(row.Seasons, row.Episodes),
			nullIfEmpty(row.AirDate), nullableIntOr(row.FilesCount),
			nullIfEmpty(row.PrimaryFile), row.Seeders, row.Leechers,
		); err != nil {
			return inserted, err
		}

		inserted++
	}

	if err := tx.Commit(); err != nil {
		return inserted, err
	}

	return inserted, nil
}

func (s *Store) existsInOtherShards(ctx context.Context, key string, hash [20]byte) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, sh := range s.shardsNewestFirstLocked() {
		if sh.key == key {
			continue
		}

		found, err := exists(ctx, sh.db, hash)
		if err != nil {
			return false, err
		}

		if found {
			return true, nil
		}
	}

	return false, nil
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func exists(ctx context.Context, q rowQuerier, hash [20]byte) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, "SELECT 1 FROM torrents WHERE info_hash = ? LIMIT 1", hash[:]).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("dedupe query: %w", err)
	}

	return true, nil
}

// nullableInt returns nil for zero, so an unknown content reference is NULL.
func nullableInt(v int64) any {
	if v == 0 {
		return nil
	}

	return v
}

// nullableIntOr returns nil for negative values, so an unknown file count is
// NULL.
func nullableIntOr(v int) any {
	if v < 0 {
		return nil
	}

	return v
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}

	return s
}
