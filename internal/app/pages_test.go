package app_test

import (
	"context"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/app"
	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/store"
	"github.com/BitTrawler/bittrawler/internal/version"
)

// startOnDataDirectory mirrors the shared start helper but starts the app on a
// directory the test has already prepared with stashed torrents.
func startOnDataDirectory(t *testing.T, dir string) (*app.App, string) {
	t.Helper()

	t.Setenv("BITTRAWLER_CRAWLER_ENABLED", "false")
	t.Setenv("BITTRAWLER_CRAWLER_BOOTSTRAP_NODES", "")

	application, err := app.New(
		context.Background(),
		config.Overrides{
			DataDirectory: dir,
			WebAddress:    "127.0.0.1:0",
			DHTAddress:    "127.0.0.1:0",
			LogLevel:      "error",
		},
		version.Info{Version: "test", Commit: "test", Date: "today"},
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)

	require.NoError(t, application.Start(context.Background()))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		assert.NoError(t, application.Stop(ctx))
	})

	// Registered after the stop cleanup so it runs before it (cleanups run
	// last-in, first-out) and clears the pooled connections Shutdown can wait
	// on.
	closeIdleConnections(t)

	return application, "http://" + application.WebAddress()
}

func TestTorrentPageShowsAStoredRow(t *testing.T) {
	const (
		seedInfoHashHex = "0123456789abcdef0123456789abcdef01234567"
		seedName        = "Sunset.Drive.2019.1080p.BluRay.x264-GRP"
		seedPrimaryFile = "Sunset.Drive.2019.1080p.BluRay.x264-GRP.mkv"
		seedSearchWord  = "sunset"
	)

	decoded, err := hex.DecodeString(seedInfoHashHex)
	require.NoError(t, err)

	var seedHash [20]byte
	copy(seedHash[:], decoded)

	dir := t.TempDir()

	// Seed one real row the way the crawler's writer would, then close the
	// store so the app can open the directory itself.
	seeded, err := store.Open(store.Config{Dir: dir})
	require.NoError(t, err)

	inserted, err := seeded.PutBatch(context.Background(), []store.Row{{
		InfoHash:    seedHash,
		Name:        seedName,
		Size:        1 << 30,
		CreatedAt:   time.Now().UTC(),
		Category:    classify.CategoryMoviesHD,
		FilesCount:  3,
		PrimaryFile: seedPrimaryFile,
		Seeders:     12,
		Leechers:    4,
	}})
	require.NoError(t, err)
	require.Equal(t, 1, inserted)
	require.NoError(t, seeded.Close())

	_, baseURL := startOnDataDirectory(t, dir)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	response := get(t, noRedirect(), baseURL+"/torrent/"+seedInfoHashHex, cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	page := body(t, response)

	// The header, the details card and the download card all come from the
	// stored row.
	assert.Contains(t, page, seedName)
	assert.Contains(t, page, "Movies/HD")
	assert.Contains(t, page, "1.0 GiB")
	assert.Contains(t, page, "<dt>Files</dt>")
	assert.Contains(t, page, "<dd>3</dd>")
	assert.Contains(t, page, seedPrimaryFile)
	assert.Contains(t, page, seedInfoHashHex)
	assert.Contains(t, page, "12 seeding, 4 leeching")

	// The magnet link carries the info hash, the URL-escaped name and the size.
	assert.Contains(t, page, "magnet:?xt=urn:btih:"+seedInfoHashHex)
	assert.Contains(t, page, "&amp;dn="+seedName)
	assert.Contains(t, page, "&amp;xl=1073741824")

	// A search for a word in the release name lists the row.
	response = get(t, noRedirect(), baseURL+"/search?q="+seedSearchWord, cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	results := body(t, response)
	assert.Contains(t, results, "1 torrents matched.")
	assert.Contains(t, results, seedName)
	assert.Contains(t, results, `href="/torrent/`+seedInfoHashHex+`"`)

	// A valid but unstored info hash gets the not-found page.
	missingHash := strings.Repeat("ab", 20)

	response = get(t, noRedirect(), baseURL+"/torrent/"+missingHash, cookieHeader(session))
	require.Equal(t, http.StatusNotFound, response.StatusCode)

	notFound := body(t, response)
	assert.Contains(t, notFound, "Torrent Not Found")
	assert.Contains(t, notFound, missingHash)
}
