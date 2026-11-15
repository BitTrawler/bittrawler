// Package sqlite opens the application's SQLite databases with one set of
// connection settings.
package sqlite

import (
	"database/sql"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

// defaultBusyTimeout is used when the caller passes a non-positive timeout.
const defaultBusyTimeout = 5 * time.Second

// Open opens the database at path, creating the file if necessary, with WAL
// journalling, NORMAL synchronous writes, a memory temp store, an 8 MB page
// cache, and busyTimeout as the lock wait. It does not create the containing
// directory and it does not apply a schema.
//
// The page cache is small on purpose. A store keeps one database open per
// month, each connection holds its own cache, and the pure-Go SQLite allocator
// costs about twice the setting in resident memory. Measured on a 200k-row
// shard, 8 MB keeps the seen check, search and a full scan within 2% of 32 MB,
// while a twelve-month store costs 230 MB rather than 760 MB.
//
// Two connections cover the one batched writer and the occasional read; the
// second is not kept open while idle, so it does not fill a cache of its own.
func Open(path string, busyTimeout time.Duration) (*sql.DB, error) {
	if busyTimeout <= 0 {
		busyTimeout = defaultBusyTimeout
	}

	// Connection-scoped pragmas go in the DSN so every pooled connection gets
	// them, not just the first one.
	dsn := path + "?_pragma=busy_timeout(" + strconv.FormatInt(busyTimeout.Milliseconds(), 10) + ")" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=temp_store(memory)" +
		"&_pragma=cache_size(-8192)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)

	return db, nil
}
