package app_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/store"
)

// seedStore writes rows into the data directory through a store with the given
// clock and closes it, so the application can open the directory afterwards.
func seedStore(t *testing.T, dir string, now func() time.Time, rows ...store.Row) {
	t.Helper()

	seeded, err := store.Open(store.Config{Dir: dir, Now: now})
	require.NoError(t, err)

	inserted, err := seeded.PutBatch(context.Background(), rows)
	require.NoError(t, err)
	require.Equal(t, len(rows), inserted)

	require.NoError(t, seeded.Close())
}

func lifecycleHash(seed byte) [20]byte {
	var hash [20]byte

	for i := range hash {
		hash[i] = seed
	}

	return hash
}

// TestStartupDropsExpiredShards checks the retention pass the app runs from
// Start. The app's store uses the real clock and config.Overrides has no clock
// to inject, so determinism comes from the data: the old shard is January 2020,
// outside any retention window a running test can plausibly have, and
// runRetention calls dropExpired once before it waits for the first hourly tick
// (retention.go), so no timer wait is needed.
func TestStartupDropsExpiredShards(t *testing.T) {
	oldCreated := time.Date(2020, time.January, 15, 12, 0, 0, 0, time.UTC)
	oldName := "Ancient.Release.2019.1080p.BluRay.x264-OLD"
	recentName := "Recent.Release.2026.1080p.WEB-DL.x264-NEW"

	t.Run("expired shard is dropped at startup", func(t *testing.T) {
		dir := t.TempDir()

		// The old row is written through a store whose fixed clock creates the
		// 2020 shard; the recent row goes into the current month's shard.
		oldRow := store.Row{InfoHash: lifecycleHash(1), Name: oldName, CreatedAt: oldCreated, Category: 2040}
		seedStore(t, dir, func() time.Time { return oldCreated }, oldRow)

		recentRow := store.Row{InfoHash: lifecycleHash(2), Name: recentName, CreatedAt: time.Now().UTC(), Category: 2040}
		seedStore(t, dir, time.Now, recentRow)

		oldShard := filepath.Join(dir, "torrents-2020-01.db")

		_, err := os.Stat(oldShard)
		require.NoError(t, err, "the test needs a stored 2020 shard to expire")

		t.Setenv("BITTRAWLER_STORE_RETENTION_MONTHS", "1")

		application, baseURL := startOnDataDirectory(t, dir)

		require.Eventually(t, func() bool {
			_, err := os.Stat(oldShard)

			return errors.Is(err, os.ErrNotExist)
		}, 2*time.Second, 5*time.Millisecond, "startup retention did not drop the expired shard")

		currentShard := filepath.Join(dir, "torrents-"+time.Now().UTC().Format("2006-01")+".db")

		_, err = os.Stat(currentShard)
		require.NoError(t, err, "the current month's shard must be kept")

		// The store still answers queries: the retained row is served and the
		// dropped one is gone.
		key := url.QueryEscape(application.Status().TorznabAPIKey)

		response := get(t, noRedirect(), baseURL+"/torznab/api?t=search&q=recent&apikey="+key, nil)
		require.Equal(t, http.StatusOK, response.StatusCode)
		assert.Contains(t, body(t, response), recentName)

		response = get(t, noRedirect(), baseURL+"/torznab/api?t=search&q=ancient&apikey="+key, nil)
		require.Equal(t, http.StatusOK, response.StatusCode)
		assert.NotContains(t, body(t, response), oldName)
	})

	t.Run("zero retention keeps every month", func(t *testing.T) {
		dir := t.TempDir()

		oldRow := store.Row{InfoHash: lifecycleHash(3), Name: oldName, CreatedAt: oldCreated, Category: 2040}
		seedStore(t, dir, func() time.Time { return oldCreated }, oldRow)

		oldShard := filepath.Join(dir, "torrents-2020-01.db")

		_, err := os.Stat(oldShard)
		require.NoError(t, err)

		t.Setenv("BITTRAWLER_STORE_RETENTION_MONTHS", "0")

		application, baseURL := startOnDataDirectory(t, dir)

		// The effective setting proves the environment reached the app; zero
		// is documented to keep everything.
		months, ok := application.SettingValue("store.retention_months")
		require.True(t, ok)
		require.Equal(t, 0, months)

		// Wait until the HTTP server is up, then give the immediate startup
		// pass a moment to run. With zero retention it returns without doing
		// anything, so the shard must still be there.
		response := get(t, noRedirect(), baseURL+"/healthz", nil)
		require.Equal(t, http.StatusOK, response.StatusCode)
		response.Body.Close()

		time.Sleep(50 * time.Millisecond)

		_, err = os.Stat(oldShard)
		require.NoError(t, err, "retention zero must keep the 2020 shard")

		currentShard := filepath.Join(dir, "torrents-"+time.Now().UTC().Format("2006-01")+".db")

		_, err = os.Stat(currentShard)
		require.NoError(t, err)

		// The retained row is still searchable through the store.
		key := url.QueryEscape(application.Status().TorznabAPIKey)

		response = get(t, noRedirect(), baseURL+"/torznab/api?t=search&q=ancient&apikey="+key, nil)
		require.Equal(t, http.StatusOK, response.StatusCode)
		assert.Contains(t, body(t, response), oldName)
	})
}
