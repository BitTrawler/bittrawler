// Package blocking records infohashes that must never be indexed. The banning
// rules reject bad metadata at intake; the blocklist remembers the decision so
// the same hash is not crawled again after a restart.
//
// The blocklist does not delete torrents that are already in the store: it only
// remembers the rejection, and a purge of indexed rows belongs to a later admin
// surface.
package blocking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/sqlite"
)

// dbFileName is the name of the blocklist database inside Config.Dir.
const dbFileName = "blocklist.db"

// hashChunkSize is the maximum number of infohashes bound into one statement,
// which keeps the insert and the lookup below SQLite's parameter limit.
const hashChunkSize = 500

// createTableSQL creates the blocklist table. WITHOUT ROWID makes the primary
// key the table itself, so a lookup and an insert conflict check are one B-tree
// probe.
const createTableSQL = `CREATE TABLE IF NOT EXISTS blocked_infohash (
	info_hash BLOB NOT NULL PRIMARY KEY,
	blocked_at INTEGER NOT NULL
) WITHOUT ROWID`

// Config configures the blocklist database.
type Config struct {
	// Dir is the directory holding blocklist.db.
	Dir string
	// BusyTimeout bounds how long SQLite waits for a lock. Defaults to 5s.
	BusyTimeout time.Duration
}

// Blocklist is a persistent set of infohashes that must not be indexed.
type Blocklist struct {
	db *sql.DB
	mu sync.Mutex // serialises writes
}

// Open opens (or creates) the blocklist in the given directory.
func Open(cfg Config) (*Blocklist, error) {
	if cfg.Dir == "" {
		return nil, errors.New("blocking: Config.Dir is required")
	}

	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("blocking: create directory: %w", err)
	}

	db, err := sqlite.Open(filepath.Join(cfg.Dir, dbFileName), cfg.BusyTimeout)
	if err != nil {
		return nil, fmt.Errorf("blocking: open database: %w", err)
	}

	if _, err := db.Exec(createTableSQL); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("blocking: create table: %w", err)
	}

	return &Blocklist{db: db}, nil
}

// Filter returns the hashes that are not blocked, in their original order, with
// duplicates removed.
func (b *Blocklist) Filter(ctx context.Context, hashes []protocol.ID) ([]protocol.ID, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	unique := dedupe(hashes)

	blocked := make(map[protocol.ID]struct{}, len(unique))

	for start := 0; start < len(unique); start += hashChunkSize {
		end := min(start+hashChunkSize, len(unique))

		if err := b.loadBlocked(ctx, unique[start:end], blocked); err != nil {
			return nil, err
		}
	}

	out := make([]protocol.ID, 0, len(unique))

	for _, hash := range unique {
		if _, ok := blocked[hash]; ok {
			continue
		}

		out = append(out, hash)
	}

	return out, nil
}

// Block records hashes as blocked and returns how many were not already
// recorded. Blocking is idempotent.
func (b *Blocklist) Block(ctx context.Context, hashes []protocol.ID) (int, error) {
	if len(hashes) == 0 {
		return 0, nil
	}

	blockedAt := time.Now().Unix()

	b.mu.Lock()
	defer b.mu.Unlock()

	inserted := 0

	for start := 0; start < len(hashes); start += hashChunkSize {
		end := min(start+hashChunkSize, len(hashes))

		n, err := b.insertChunk(ctx, hashes[start:end], blockedAt)
		inserted += n

		if err != nil {
			return inserted, err
		}
	}

	return inserted, nil
}

// Close closes the database.
func (b *Blocklist) Close() error {
	return b.db.Close()
}

// dedupe removes repeated hashes while preserving the order of first
// occurrence.
func dedupe(hashes []protocol.ID) []protocol.ID {
	seen := make(map[protocol.ID]struct{}, len(hashes))
	out := make([]protocol.ID, 0, len(hashes))

	for _, hash := range hashes {
		if _, ok := seen[hash]; ok {
			continue
		}

		seen[hash] = struct{}{}
		out = append(out, hash)
	}

	return out
}

// loadBlocked adds the blocked hashes in one chunk to blocked.
func (b *Blocklist) loadBlocked(ctx context.Context, hashes []protocol.ID, blocked map[protocol.ID]struct{}) error {
	args := make([]any, len(hashes))

	var placeholders strings.Builder

	for i, hash := range hashes {
		if i > 0 {
			placeholders.WriteString(",")
		}

		placeholders.WriteString("?")

		args[i] = hash[:]
	}

	rows, err := b.db.QueryContext(ctx,
		"SELECT info_hash FROM blocked_infohash WHERE info_hash IN ("+placeholders.String()+")", args...)
	if err != nil {
		return fmt.Errorf("blocking: lookup: %w", err)
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var value []byte
		if err := rows.Scan(&value); err != nil {
			return fmt.Errorf("blocking: lookup: %w", err)
		}

		var hash protocol.ID

		copy(hash[:], value)
		blocked[hash] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("blocking: lookup: %w", err)
	}

	return nil
}

// insertChunk inserts one chunk of hashes, ignoring those already recorded, and
// returns how many rows were new.
func (b *Blocklist) insertChunk(ctx context.Context, hashes []protocol.ID, blockedAt int64) (int, error) {
	args := make([]any, 0, len(hashes)*2)

	var placeholders strings.Builder

	for i, hash := range hashes {
		if i > 0 {
			placeholders.WriteString(",")
		}

		placeholders.WriteString("(?,?)")

		args = append(args, hash[:], blockedAt)
	}

	res, err := b.db.ExecContext(ctx,
		"INSERT INTO blocked_infohash (info_hash, blocked_at) VALUES "+
			placeholders.String()+" ON CONFLICT DO NOTHING", args...)
	if err != nil {
		return 0, fmt.Errorf("blocking: insert: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("blocking: insert: %w", err)
	}

	return int(n), nil
}
