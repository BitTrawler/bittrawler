package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	_ "modernc.org/sqlite"

	"github.com/BitTrawler/bittrawler/internal/sqlite"
)

const (
	// fileName is the settings database inside the data directory.
	fileName = "settings.db"
	// currentVersion is the schema version this binary writes. A database with
	// a higher version was written by a newer binary and is refused.
	currentVersion = 1
	// busyTimeout bounds how long a statement waits for another connection's
	// lock. The settings database is written by one process at a time.
	busyTimeout = 5000
)

// migrations take the schema from version i to version i+1.
var migrations = []string{
	`CREATE TABLE IF NOT EXISTS settings (
		key   TEXT NOT NULL PRIMARY KEY,
		value TEXT NOT NULL
	) WITHOUT ROWID`,
}

// Store is the settings database.
type Store struct {
	db       *sql.DB
	path     string
	readOnly bool
}

// Open opens the settings database in dir, creating it and its schema when they
// do not exist. The directory is created if it is missing.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("settings: a directory is required")
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("settings: create %s: %w", dir, err)
	}

	path := filepath.Join(dir, fileName)

	// Create the file with its final permissions before SQLite opens it, so the
	// database and the journal files SQLite creates beside it cannot be read by
	// other users. The file holds the API key and the password hash.
	if err := ensureFile(path); err != nil {
		return nil, err
	}

	db, err := sqlite.Open(path, 0)
	if err != nil {
		return nil, fmt.Errorf("settings: open %s: %w", path, err)
	}

	store := &Store{db: db, path: path}

	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()

		return nil, err
	}

	return store, nil
}

// OpenReadOnly opens an existing settings database without permitting writes.
// It does not create the database, so a caller can tell an installation that
// has never run from one that has.
func OpenReadOnly(dir string) (*Store, error) {
	path := filepath.Join(dir, fileName)

	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}

	dsn := (&url.URL{Scheme: "file", Path: path}).String() +
		"?mode=ro&_pragma=busy_timeout(" + strconv.Itoa(busyTimeout) + ")"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("settings: open %s: %w", path, err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db, path: path, readOnly: true}

	if err := store.checkVersion(context.Background()); err != nil {
		_ = db.Close()

		return nil, err
	}

	return store, nil
}

// Path returns the database file.
func (s *Store) Path() string {
	return s.path
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// All returns every stored setting.
func (s *Store) All(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("settings: read: %w", err)
	}

	defer rows.Close()

	values := make(map[string]string)

	for rows.Next() {
		var key, value string

		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("settings: read: %w", err)
		}

		values[key] = value
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: read: %w", err)
	}

	return values, nil
}

// Get returns one stored setting.
func (s *Store) Get(ctx context.Context, key string) (string, bool, error) {
	var value string

	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("settings: read %s: %w", key, err)
	}

	return value, true, nil
}

// Set stores one setting.
func (s *Store) Set(ctx context.Context, key, value string) error {
	return s.SetMany(ctx, map[string]string{key: value})
}

// SetMany stores several settings in one transaction, so a setup form that sets
// an account and a token either applies both or neither.
func (s *Store) SetMany(ctx context.Context, values map[string]string) error {
	if s.readOnly {
		return errors.New("settings: database is read only")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("settings: write: %w", err)
	}

	defer tx.Rollback()

	for key, value := range values {
		if key == "" {
			return errors.New("settings: a key is required")
		}

		_, err := tx.ExecContext(ctx,
			"INSERT INTO settings (key, value) VALUES (?, ?) "+
				"ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			key, value,
		)
		if err != nil {
			return fmt.Errorf("settings: write %s: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("settings: write: %w", err)
	}

	return nil
}

// Delete removes one setting, returning it to its default.
func (s *Store) Delete(ctx context.Context, key string) error {
	if s.readOnly {
		return errors.New("settings: database is read only")
	}

	if _, err := s.db.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", key); err != nil {
		return fmt.Errorf("settings: delete %s: %w", key, err)
	}

	return nil
}

// migrate brings the schema up to currentVersion.
func (s *Store) migrate(ctx context.Context) error {
	version, err := s.version(ctx)
	if err != nil {
		return err
	}

	if version > currentVersion {
		return fmt.Errorf(
			"settings: %s has schema version %d, which is newer than this binary supports (%d)",
			s.path, version, currentVersion,
		)
	}

	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("settings: migrate: %w", err)
		}

		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()

			return fmt.Errorf("settings: migrate to version %d: %w", i+1, err)
		}

		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			_ = tx.Rollback()

			return fmt.Errorf("settings: migrate to version %d: %w", i+1, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("settings: migrate to version %d: %w", i+1, err)
		}
	}

	return nil
}

// checkVersion refuses a database this binary cannot read.
func (s *Store) checkVersion(ctx context.Context) error {
	version, err := s.version(ctx)
	if err != nil {
		return err
	}

	if version > currentVersion {
		return fmt.Errorf(
			"settings: %s has schema version %d, which is newer than this binary supports (%d)",
			s.path, version, currentVersion,
		)
	}

	return nil
}

func (s *Store) version(ctx context.Context) (int, error) {
	var version int

	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("settings: read schema version: %w", err)
	}

	return version, nil
}

// ensureFile creates the database file with owner-only permissions if it does
// not exist, and tightens its permissions if it does.
func ensureFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("settings: create %s: %w", path, err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("settings: create %s: %w", path, err)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("settings: set permissions on %s: %w", path, err)
	}

	return nil
}
