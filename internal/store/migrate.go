package store

import (
	"database/sql"
	"embed"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migration is one schema version, applied to every shard that predates it.
type migration struct {
	version    int
	name       string
	statements []string
}

// migrations returns the embedded migrations in version order. The version is
// the numeric filename prefix: 0001_init.sql is version 1.
func migrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}

	sort.Strings(names)

	out := make([]migration, 0, len(names))
	for _, name := range names {
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, fmt.Errorf("migration %s: expected NNNN_name.sql", name)
		}

		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", name, err)
		}

		body, err := migrationFS.ReadFile(path.Join("migrations", name))
		if err != nil {
			return nil, err
		}

		out = append(out, migration{
			version:    version,
			name:       name,
			statements: splitStatements(string(body)),
		})
	}

	return out, nil
}

// migrate brings one shard database up to the current schema version, in one
// transaction per migration so a failure leaves the shard at its previous
// version.
func migrate(db *sql.DB) error {
	want, err := migrations()
	if err != nil {
		return err
	}

	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	latest := 0
	for _, m := range want {
		if m.version > latest {
			latest = m.version
		}
	}

	// A shard written by a newer release carries migrations this build does not
	// have. Running the ones it does know would leave the schema half-understood,
	// so refuse the shard instead.
	if current > latest {
		return fmt.Errorf(
			"shard schema version %d is newer than this build's %d; use a newer release or restore the shard from a backup",
			current, latest,
		)
	}

	for _, m := range want {
		if m.version <= current {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		for _, statement := range m.statements {
			if _, err := tx.Exec(statement); err != nil {
				_ = tx.Rollback()

				return fmt.Errorf("migration %s: %w", m.name, err)
			}
		}

		// user_version is a plain pragma; the version is an embedded integer.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			_ = tx.Rollback()

			return fmt.Errorf("migration %s: set user_version: %w", m.name, err)
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// splitStatements splits a migration into executable statements. Statements
// inside -- +goose StatementBegin/StatementEnd blocks are passed through whole,
// because SQLite trigger bodies contain semicolons; everything else is split on
// top-level semicolons, ignoring quoted strings and comments.
func splitStatements(src string) []string {
	var (
		out   []string
		plain strings.Builder
	)

	flush := func() {
		out = append(out, splitSemicolons(plain.String())...)
		plain.Reset()
	}

	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "-- +goose StatementBegin"):
			flush()

			var block strings.Builder
			for i++; i < len(lines); i++ {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "-- +goose StatementEnd") {
					break
				}

				block.WriteString(lines[i])
				block.WriteString("\n")
			}

			if s := strings.TrimSpace(block.String()); s != "" {
				out = append(out, s)
			}
		case strings.HasPrefix(trimmed, "-- +goose"):
			// Up/Down markers are not used; every migration here is forward only.
		default:
			plain.WriteString(line)
			plain.WriteString("\n")
		}
	}

	flush()

	return out
}

// splitSemicolons splits plain SQL on top-level semicolons.
func splitSemicolons(src string) []string {
	var (
		out         []string
		current     strings.Builder
		inSingle    bool
		inDouble    bool
		inBracket   bool
		inLineCom   bool
		inBlockCom  bool
		lastWritten byte
	)

	for i := 0; i < len(src); i++ {
		c := src[i]

		switch {
		case inLineCom:
			current.WriteByte(c)
			if c == '\n' {
				inLineCom = false
			}
		case inBlockCom:
			current.WriteByte(c)
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				current.WriteByte('/')
				i++
				inBlockCom = false
			}
		case inSingle:
			current.WriteByte(c)
			if c == '\'' {
				if i+1 < len(src) && src[i+1] == '\'' {
					current.WriteByte(src[i+1])
					i++
				} else {
					inSingle = false
				}
			}
		case inDouble:
			current.WriteByte(c)
			if c == '"' {
				inDouble = false
			}
		case inBracket:
			current.WriteByte(c)
			if c == ']' {
				inBracket = false
			}
		default:
			switch {
			case c == '-' && i+1 < len(src) && src[i+1] == '-':
				inLineCom = true
				current.WriteByte(c)
			case c == '/' && i+1 < len(src) && src[i+1] == '*':
				inBlockCom = true
				current.WriteByte(c)
			case c == '\'':
				inSingle = true
				current.WriteByte(c)
			case c == '"':
				inDouble = true
				current.WriteByte(c)
			case c == '[':
				inBracket = true
				current.WriteByte(c)
			case c == ';':
				if s := strings.TrimSpace(current.String()); s != "" {
					out = append(out, s)
				}

				current.Reset()
			default:
				current.WriteByte(c)
			}

			lastWritten = c
		}
	}

	_ = lastWritten

	if s := strings.TrimSpace(current.String()); s != "" {
		out = append(out, s)
	}

	return out
}

// SchemaVersion returns the schema version new shards are created with.
func SchemaVersion() (int, error) {
	migrations, err := migrations()
	if err != nil {
		return 0, err
	}

	if len(migrations) == 0 {
		return 0, nil
	}

	return migrations[len(migrations)-1].version, nil
}
