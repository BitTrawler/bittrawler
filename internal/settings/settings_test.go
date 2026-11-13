package settings_test

import (
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
