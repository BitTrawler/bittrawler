package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const shardPrefix = "torrents-"

// Config configures a store directory. One directory holds one monthly shard
// file per calendar month of created_at.
type Config struct {
	// Dir is the directory holding the shard files.
	Dir string
	// RetentionMonths is how many calendar months of shards to keep. Zero or
	// negative keeps everything.
	RetentionMonths int
	// BusyTimeout bounds how long SQLite waits for another connection's lock.
	BusyTimeout time.Duration
	// Now is the clock, replaceable in tests.
	Now func() time.Time
}

// Store is a directory of monthly SQLite shard files. One writer process is
// assumed; readers may be concurrent with it (WAL mode).
type Store struct {
	cfg     Config
	mu      sync.RWMutex
	shards  map[string]*shard
	writeMu sync.Mutex
}

type shard struct {
	key  string
	path string
	db   *sql.DB
}

// Open opens (or creates) the store directory, brings every existing shard up
// to the current schema version, and ensures the current month's shard exists.
func Open(cfg Config) (*Store, error) {
	if cfg.Dir == "" {
		return nil, errors.New("store: Config.Dir is required")
	}

	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	if cfg.BusyTimeout <= 0 {
		cfg.BusyTimeout = 5 * time.Second
	}

	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, err
	}

	s := &Store{cfg: cfg, shards: make(map[string]*shard)}

	paths, err := filepath.Glob(filepath.Join(cfg.Dir, shardPrefix+"*.db"))
	if err != nil {
		_ = s.Close()

		return nil, err
	}

	sort.Strings(paths)

	for _, path := range paths {
		key, err := shardKeyFromPath(path)
		if err != nil {
			_ = s.Close()

			return nil, err
		}

		sh, err := openShard(cfg, key, path)
		if err != nil {
			_ = s.Close()

			return nil, fmt.Errorf("open shard %s: %w", key, err)
		}

		s.shards[key] = sh
	}

	if _, err := s.shardFor(cfg.Now().UTC()); err != nil {
		_ = s.Close()

		return nil, err
	}

	return s, nil
}

// Close closes every shard.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error

	for key, sh := range s.shards {
		if err := sh.db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close shard %s: %w", key, err))
		}

		delete(s.shards, key)
	}

	return errors.Join(errs...)
}

// shardFor returns the shard for the month of t, creating it if needed.
func (s *Store) shardFor(t time.Time) (*shard, error) {
	key := monthKey(t)

	s.mu.RLock()
	sh, ok := s.shards[key]
	s.mu.RUnlock()

	if ok {
		return sh, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if sh, ok := s.shards[key]; ok {
		return sh, nil
	}

	sh, err := openShard(s.cfg, key, s.shardPath(key))
	if err != nil {
		return nil, err
	}

	s.shards[key] = sh

	return sh, nil
}

func (s *Store) shardPath(key string) string {
	return filepath.Join(s.cfg.Dir, shardPrefix+key+".db")
}

// shardsNewestFirst returns a snapshot of the open shards, newest month first.
func (s *Store) shardsNewestFirst() []*shard {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.shardsNewestFirstLocked()
}

// shardsNewestFirstLocked is the same snapshot for callers that already hold
// s.mu, either for reading or writing.
func (s *Store) shardsNewestFirstLocked() []*shard {
	keys := make([]string, 0, len(s.shards))
	for key := range s.shards {
		keys = append(keys, key)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(keys)))

	out := make([]*shard, 0, len(keys))
	for _, key := range keys {
		out = append(out, s.shards[key])
	}

	return out
}

func openShard(cfg Config, key, path string) (*shard, error) {
	// Connection-scoped pragmas go in the DSN so every pooled connection
	// gets them, not just the first one.
	dsn := path + "?_pragma=busy_timeout(" + strconv.FormatInt(cfg.BusyTimeout.Milliseconds(), 10) + ")" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=temp_store(memory)" +
		"&_pragma=cache_size(-32768)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := migrate(db); err != nil {
		_ = db.Close()

		return nil, err
	}

	return &shard{key: key, path: path, db: db}, nil
}

func shardKeyFromPath(path string) (string, error) {
	base := filepath.Base(path)
	trimmed := strings.TrimPrefix(base, shardPrefix)
	key := strings.TrimSuffix(trimmed, ".db")

	if _, err := monthStart(key); err != nil {
		return "", fmt.Errorf("shard %s: %w", base, err)
	}

	return key, nil
}
