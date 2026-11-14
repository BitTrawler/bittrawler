// Package crawlstate persists what the DHT crawler has learnt about nodes
// between runs: which nodes answer BEP 51 sample_infohashes queries and which
// have supplied hashes. The state goes in a crawl.db SQLite file in the data
// directory, so a restart does not begin with a cold routing table.
package crawlstate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/sqlite"
)

const (
	// dbFileName is the crawl state database inside the data directory.
	dbFileName = "crawl.db"
	// pruneAfter is how long a node keeps its row without being saved again.
	pruneAfter = 7 * 24 * time.Hour
	// maxNodes caps the table. A long crawl can learn about far more nodes than
	// it can ever usefully revisit, and the least productive ones are the first
	// to go.
	maxNodes = 50000
)

// Support is what is known about a node's answer to a BEP 51 sample_infohashes
// query.
type Support int8

const (
	SupportUnknown Support = iota
	SupportYes
	SupportNo
)

// Node is one routing table entry's learning state.
type Node struct {
	ID              protocol.ID
	Addr            netip.AddrPort
	Support         Support
	LastRespondedAt time.Time
	NextSampleAt    time.Time
	Sampled         int
	Total           int
	UpdatedAt       time.Time
}

// createTableSQL creates the node table. Times are Unix nanoseconds so that
// sub-second values survive a round trip; a zero time is stored as NULL because
// the zero time is outside UnixNano's range.
const createTableSQL = `CREATE TABLE IF NOT EXISTS node (
	id                BLOB NOT NULL PRIMARY KEY,
	addr              TEXT NOT NULL,
	support           INTEGER NOT NULL,
	last_responded_at INTEGER,
	next_sample_at    INTEGER,
	sampled           INTEGER NOT NULL,
	total             INTEGER NOT NULL,
	updated_at        INTEGER NOT NULL
) WITHOUT ROWID`

// createIndexSQL creates the indexes Load, age pruning and the row cap rely on.
var createIndexSQL = []string{
	`CREATE INDEX IF NOT EXISTS node_sampled ON node (sampled DESC, last_responded_at DESC)`,
	`CREATE INDEX IF NOT EXISTS node_updated_at ON node (updated_at)`,
}

// upsertSQL replaces a node's saved state, keyed by its 20-byte ID.
const upsertSQL = `INSERT INTO node (
	id, addr, support, last_responded_at, next_sample_at, sampled, total, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	addr = excluded.addr,
	support = excluded.support,
	last_responded_at = excluded.last_responded_at,
	next_sample_at = excluded.next_sample_at,
	sampled = excluded.sampled,
	total = excluded.total,
	updated_at = excluded.updated_at`

// selectSQL returns nodes in the order the crawler should restore them.
const selectSQL = `SELECT id, addr, support, last_responded_at, next_sample_at, sampled, total, updated_at
FROM node
ORDER BY sampled DESC, last_responded_at DESC, id
LIMIT ?`

// deleteAgedSQL drops rows that Save has not refreshed for pruneAfter.
const deleteAgedSQL = `DELETE FROM node WHERE updated_at < ?`

// deleteExcessSQL keeps the maxNodes most productive rows and drops the rest.
const deleteExcessSQL = `DELETE FROM node WHERE id NOT IN (
	SELECT id FROM node ORDER BY sampled DESC, last_responded_at DESC, id LIMIT ?
)`

// Store is the crawl state database.
type Store struct {
	db *sql.DB
	// mu serialises Save so two snapshots cannot contend for SQLite's write
	// lock.
	mu sync.Mutex
}

// Open opens the crawl state database in dir, creating the directory, the
// database and its schema if they do not exist.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("crawlstate: a directory is required")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("crawlstate: create %s: %w", dir, err)
	}

	db, err := sqlite.Open(filepath.Join(dir, dbFileName), 0)
	if err != nil {
		return nil, fmt.Errorf("crawlstate: open: %w", err)
	}

	if err := createSchema(context.Background(), db); err != nil {
		_ = db.Close()

		return nil, err
	}

	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Load returns up to limit stored nodes, most productive first and then most
// recently responded. A non-positive limit returns every node. Rows whose
// address is missing or unusable are skipped.
func (s *Store) Load(ctx context.Context, limit int) ([]Node, error) {
	if limit <= 0 {
		limit = -1
	}

	rows, err := s.db.QueryContext(ctx, selectSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("crawlstate: load: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var nodes []Node

	for rows.Next() {
		node, ok, err := scanNode(rows)
		if err != nil {
			return nil, err
		}

		if !ok {
			continue
		}

		nodes = append(nodes, node)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("crawlstate: load: %w", err)
	}

	return nodes, nil
}

// Save writes a snapshot of the routing table's learning state in one
// transaction, replacing rows by node ID. Nodes missing from the snapshot keep
// their rows until those age out or the row cap pushes them out.
func (s *Store) Save(ctx context.Context, nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("crawlstate: save: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	statement, err := tx.PrepareContext(ctx, upsertSQL)
	if err != nil {
		return fmt.Errorf("crawlstate: save: %w", err)
	}

	defer func() { _ = statement.Close() }()

	for _, node := range nodes {
		// A node with no identity, or with no address that could be dialled,
		// has no learning worth keeping and would only take space in the cap.
		if node.ID.IsZero() || !usableAddr(node.Addr) {
			continue
		}

		updatedAt := node.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = now
		}

		if _, err := statement.ExecContext(ctx,
			node.ID, node.Addr.String(), int64(node.Support),
			storeTime(node.LastRespondedAt), storeTime(node.NextSampleAt),
			node.Sampled, node.Total, updatedAt.UnixNano(),
		); err != nil {
			return fmt.Errorf("crawlstate: save: %w", err)
		}
	}

	if err := prune(ctx, tx, now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("crawlstate: save: %w", err)
	}

	return nil
}

// createSchema creates the table and indexes in one transaction, so an
// interrupted Open never leaves a half-created database behind.
func createSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("crawlstate: create schema: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	statements := append([]string{createTableSQL}, createIndexSQL...)

	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("crawlstate: create schema: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("crawlstate: create schema: %w", err)
	}

	return nil
}

// scanNode reads one row, reporting false for a row whose address cannot be
// used. Such a row can only come from a database written by another version or
// edited by hand, and one bad row should not fail the whole load.
func scanNode(rows *sql.Rows) (Node, bool, error) {
	var (
		node          Node
		addr          string
		support       int64
		lastResponded sql.NullInt64
		nextSample    sql.NullInt64
		updated       sql.NullInt64
	)

	if err := rows.Scan(&node.ID, &addr, &support, &lastResponded, &nextSample,
		&node.Sampled, &node.Total, &updated); err != nil {
		return Node{}, false, fmt.Errorf("crawlstate: load: %w", err)
	}

	parsed, err := netip.ParseAddrPort(addr)
	if err != nil || !usableAddr(parsed) {
		return Node{}, false, nil
	}

	node.Addr = parsed
	node.Support = Support(support)
	node.LastRespondedAt = loadTime(lastResponded)
	node.NextSampleAt = loadTime(nextSample)
	node.UpdatedAt = loadTime(updated)

	return node, true, nil
}

// prune applies the two row limits in the order the table is sized for: first
// age, then productivity.
func prune(ctx context.Context, tx *sql.Tx, now time.Time) error {
	if _, err := tx.ExecContext(ctx, deleteAgedSQL, now.Add(-pruneAfter).UnixNano()); err != nil {
		return fmt.Errorf("crawlstate: prune aged rows: %w", err)
	}

	var count int

	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM node").Scan(&count); err != nil {
		return fmt.Errorf("crawlstate: count rows: %w", err)
	}

	if count <= maxNodes {
		return nil
	}

	if _, err := tx.ExecContext(ctx, deleteExcessSQL, maxNodes); err != nil {
		return fmt.Errorf("crawlstate: prune excess rows: %w", err)
	}

	return nil
}

// usableAddr reports whether a node address could be dialled. A zero AddrPort
// has no host, and port zero names no service.
func usableAddr(addr netip.AddrPort) bool {
	return addr.IsValid() && addr.Port() != 0
}

// storeTime returns the SQLite value for a saved timestamp, or NULL for the
// zero time.
func storeTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}

	return t.UnixNano()
}

// loadTime reverses storeTime.
func loadTime(value sql.NullInt64) time.Time {
	if !value.Valid {
		return time.Time{}
	}

	return time.Unix(0, value.Int64).UTC()
}
