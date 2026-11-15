package sqlite

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOpenAppliesPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(path, 3*time.Second)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	cases := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"synchronous", "1"},
		{"temp_store", "2"},
		{"cache_size", "-8192"},
		{"busy_timeout", "3000"},
	}

	for _, c := range cases {
		var got string
		if err := db.QueryRow("PRAGMA " + c.pragma).Scan(&got); err != nil {
			t.Fatalf("pragma %s: %v", c.pragma, err)
		}

		if got != c.want {
			t.Fatalf("pragma %s = %q, want %q", c.pragma, got, c.want)
		}
	}
}

func TestOpenDefaultsBusyTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(path, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	var got string
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&got); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}

	if want := "5000"; got != want {
		t.Fatalf("busy_timeout = %q, want %q", got, want)
	}
}

func TestOpenPersistsDataAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(path, time.Second)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if _, err := db.Exec("CREATE TABLE t (v TEXT NOT NULL)"); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := db.Exec("INSERT INTO t (v) VALUES ('persisted')"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path, time.Second)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	t.Cleanup(func() { _ = reopened.Close() })

	var got string
	if err := reopened.QueryRow("SELECT v FROM t").Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}

	if got != "persisted" {
		t.Fatalf("got %q, want %q", got, "persisted")
	}
}

func TestOpenDoesNotCreateDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "test.db")

	db, err := Open(path, time.Second)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec("CREATE TABLE t (v TEXT)"); err == nil {
		t.Fatal("write to a database in a missing directory succeeded")
	}
}
