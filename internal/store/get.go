package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Latest returns the newly indexed torrents, newest first. It stops after the
// newest shard has provided enough rows, because the shards are ordered by
// time.
func (s *Store) Latest(ctx context.Context, limit int) ([]Torrent, error) {
	if limit <= 0 {
		limit = 20
	}

	query := NewQuery()
	query.Sort = SortNewest
	query.Limit = limit

	result, err := s.Search(ctx, query)
	if err != nil {
		return nil, err
	}

	return result.Torrents, nil
}

// Get returns the torrent with the given infohash.
func (s *Store) Get(ctx context.Context, hash [20]byte) (Torrent, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, sh := range s.shardsNewestFirstLocked() {
		row := sh.db.QueryRowContext(ctx,
			"SELECT "+torrentColumns+" FROM torrents t WHERE t.info_hash = ? LIMIT 1",
			hash[:],
		)

		torrent, err := scanTorrent(row)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}

		if err != nil {
			return Torrent{}, false, fmt.Errorf("store: get %x: %w", hash, err)
		}

		return torrent, true, nil
	}

	return Torrent{}, false, nil
}
