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
// journalling, NORMAL synchronous writes, a memory temp store, a 32 MB page
// cache, and busyTimeout as the lock wait. It does not create the containing
// directory and it does not apply a schema.
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
		"&_pragma=cache_size(-32768)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	return db, nil
}
