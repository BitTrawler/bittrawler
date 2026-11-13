package settings_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/settings"
)

func open(t *testing.T) (*settings.Store, string) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "data")

	store, err := settings.Open(dir)
	require.NoError(t, err)

	t.Cleanup(func() { _ = store.Close() })

	return store, dir
}

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, dir := open(t)

	values, err := store.All(ctx)
	require.NoError(t, err)
	assert.Empty(t, values)

	require.NoError(t, store.Set(ctx, "web.address", "127.0.0.1:9999"))

	value, ok, err := store.Get(ctx, "web.address")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "127.0.0.1:9999", value)

	require.NoError(t, store.SetMany(ctx, map[string]string{
		"torznab.api_key": "abc",
		"tmdb.token":      "def",
	}))

	_, ok, err = store.Get(ctx, "missing")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, store.Delete(ctx, "torznab.api_key"))
	_, ok, err = store.Get(ctx, "torznab.api_key")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, store.Close())

	reopened, err := settings.Open(dir)
	require.NoError(t, err)

	defer reopened.Close()

	values, err = reopened.All(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"web.address": "127.0.0.1:9999",
		"tmdb.token":  "def",
	}, values)
}

func TestValuesAreStoredExactly(t *testing.T) {
	ctx := context.Background()
	store, _ := open(t)

	values := map[string]string{
		"empty":   "",
		"unicode": "Кириллица 蝙蝠侠",
		"quotes":  `"quoted" and 'single'`,
		"newline": "line one\nline two",
	}

	for key, value := range values {
		require.NoError(t, store.Set(ctx, key, value))
	}

	for key, want := range values {
		got, ok, err := store.Get(ctx, key)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, want, got, key)
	}
}

func TestFilePermissions(t *testing.T) {
	store, dir := open(t)

	info, err := os.Stat(filepath.Join(dir, "settings.db"))
	require.NoError(t, err)

	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.NotEmpty(t, store.Path())

	info, err = os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestReadOnly(t *testing.T) {
	ctx := context.Background()
	store, dir := open(t)

	require.NoError(t, store.Set(ctx, "web.address", "127.0.0.1:9999"))
	require.NoError(t, store.Close())

	readOnly, err := settings.OpenReadOnly(dir)
	require.NoError(t, err)

	defer readOnly.Close()

	value, ok, err := readOnly.Get(ctx, "web.address")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "127.0.0.1:9999", value)

	assert.Error(t, readOnly.Set(ctx, "web.address", "127.0.0.1:1"))
	assert.Error(t, readOnly.Delete(ctx, "web.address"))
}

func TestReadOnlyMissingDatabase(t *testing.T) {
	_, err := settings.OpenReadOnly(t.TempDir())

	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestRefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.db")

	// Create a database from a hypothetical newer release.
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	require.NoError(t, db.Ping())

	_, err = db.Exec("PRAGMA user_version = 99")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = settings.Open(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer")
}

// TestReadOnlyFromRelativeDirectory covers the recovery commands, which are
// given whatever -data held and therefore often a relative path.
func TestReadOnlyFromRelativeDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "data"), 0o700))

	t.Chdir(dir)

	store, err := settings.Open("data")
	require.NoError(t, err)

	require.NoError(t, store.Set(context.Background(), "web.address", "127.0.0.1:9999"))
	require.NoError(t, store.Close())

	readOnly, err := settings.OpenReadOnly("data")
	require.NoError(t, err)

	defer readOnly.Close()

	value, ok, err := readOnly.Get(context.Background(), "web.address")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "127.0.0.1:9999", value)
}

// TestOpenFailsWhenDirectoryIsAFile checks that the data directory path being a
// regular file is reported, not treated as an existing empty directory.
func TestOpenFailsWhenDirectoryIsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")

	require.NoError(t, os.WriteFile(path, []byte("not a directory"), 0o600))

	_, err := settings.Open(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path)

	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.True(t, info.Mode().IsRegular())
}

// TestOpenFailsOnCorruptDatabase checks that a settings.db holding something
// other than SQLite fails cleanly rather than being reported as usable.
func TestOpenFailsOnCorruptDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.db")
	garbage := bytes.Repeat([]byte("not a SQLite database;"), 256)

	require.NoError(t, os.WriteFile(path, garbage, 0o600))

	_, err := settings.Open(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read schema version")

	// The corrupt file is left in place so an operator can inspect it.
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, int64(len(garbage)), info.Size())
}

// TestOpenReadOnlyFailsWhenPathIsADirectory checks that OpenReadOnly refuses a
// settings.db that is a directory. os.Stat succeeds there, so the refusal must
// come from opening or reading it as a database.
func TestOpenReadOnlyFailsWhenPathIsADirectory(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.Mkdir(filepath.Join(dir, "settings.db"), 0o700))

	_, err := settings.OpenReadOnly(dir)
	require.Error(t, err)
}

// TestDeleteAbsentKeyAndEmptyKey records the empty-key behaviour: writes are
// rejected, reads treat it as absent, and deleting an absent key is harmless.
func TestDeleteAbsentKeyAndEmptyKey(t *testing.T) {
	ctx := context.Background()
	store, _ := open(t)

	require.NoError(t, store.Delete(ctx, "absent"))

	err := store.Set(ctx, "", "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a key is required")

	value, ok, err := store.Get(ctx, "")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, value)

	require.NoError(t, store.Delete(ctx, ""))

	values, err := store.All(ctx)
	require.NoError(t, err)
	assert.Empty(t, values)
}

// TestSetManyEmptyMapIsANoOp checks that an empty map commits nothing and
// leaves existing settings alone.
func TestSetManyEmptyMapIsANoOp(t *testing.T) {
	ctx := context.Background()
	store, _ := open(t)

	require.NoError(t, store.Set(ctx, "keep", "before"))

	require.NoError(t, store.SetMany(ctx, map[string]string{}))

	values, err := store.All(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"keep": "before"}, values)
}

// TestSetManyWritesEveryKeyInOneCall checks a mixed batch: all keys land and a
// later call updates without adding duplicates.
func TestSetManyWritesEveryKeyInOneCall(t *testing.T) {
	ctx := context.Background()
	store, _ := open(t)

	batch := map[string]string{
		"web.address":     "127.0.0.1:9999",
		"torznab.api_key": "abc",
		"log.level":       "debug",
		"empty":           "",
	}

	require.NoError(t, store.SetMany(ctx, batch))

	values, err := store.All(ctx)
	require.NoError(t, err)
	assert.Equal(t, batch, values)

	// A second batch overwrites in the same way, so setting is idempotent.
	require.NoError(t, store.SetMany(ctx, map[string]string{"web.address": "127.0.0.1:1234"}))

	value, ok, err := store.Get(ctx, "web.address")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "127.0.0.1:1234", value)

	values, err = store.All(ctx)
	require.NoError(t, err)
	assert.Len(t, values, len(batch))
}
