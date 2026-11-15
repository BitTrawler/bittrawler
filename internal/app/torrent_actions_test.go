package app_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/blocking"
	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/store"
)

func TestTorrentActionsRemoveAndBlock(t *testing.T) {
	// No token, so page rendering never calls TMDB.
	t.Setenv("BITTRAWLER_TMDB_TOKEN", "")

	const (
		removeHashHex = "3333333333333333333333333333333333333333"
		blockHashHex  = "4444444444444444444444444444444444444444"
	)

	dir := t.TempDir()

	seeded, err := store.Open(store.Config{Dir: dir})
	require.NoError(t, err)

	now := time.Now().UTC()

	inserted, err := seeded.PutBatch(context.Background(), []store.Row{
		{
			InfoHash:   mustHash(t, removeHashHex),
			Name:       "Remove.Me.2024.1080p.BluRay.x264-GRP",
			Size:       1 << 30,
			CreatedAt:  now.Add(-time.Hour),
			Category:   classify.CategoryMoviesHD,
			FilesCount: 1,
			Seeders:    3,
		},
		{
			InfoHash:   mustHash(t, blockHashHex),
			Name:       "Block.Me.2024.1080p.BluRay.x264-GRP",
			Size:       1 << 30,
			CreatedAt:  now,
			Category:   classify.CategoryMoviesHD,
			FilesCount: 1,
			Seeders:    4,
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, inserted)
	require.NoError(t, seeded.Close())

	_, baseURL := startOnDataDirectory(t, dir)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	// The page offers both actions, with a confirmation on each.
	response := get(t, noRedirect(), baseURL+"/torrent/"+removeHashHex, cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	page := body(t, response)
	assert.Contains(t, page, `action="/torrent/`+removeHashHex+`/remove"`)
	assert.Contains(t, page, `action="/torrent/`+removeHashHex+`/block"`)
	assert.Contains(t, page, "Block and Remove")
	assert.Contains(t, page, "data-confirm=")

	// A foreign origin is refused before anything is deleted.
	response = postWithOriginHeaders(t, baseURL,
		"/torrent/"+removeHashHex+"/remove", nil, session, "http://evil.example", "")
	require.Equal(t, http.StatusForbidden, response.StatusCode)
	response.Body.Close()

	response = get(t, noRedirect(), baseURL+"/torrent/"+removeHashHex, cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)
	response.Body.Close()

	// Removing deletes the row and says so on the page it lands on.
	response = postForm(t, noRedirect(), baseURL, "/torrent/"+removeHashHex+"/remove", nil, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()
	assert.Equal(t, "/?removed=removed", response.Header.Get("Location"))

	response = get(t, noRedirect(), baseURL+"/torrent/"+removeHashHex, cookieHeader(session))
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	response.Body.Close()

	// Blocking deletes the row too, and remembers the hash. The blocklist is a
	// separate database in the data directory, so a second handle can read it.
	response = postForm(t, noRedirect(), baseURL, "/torrent/"+blockHashHex+"/block", nil, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()
	assert.Equal(t, "/?removed=blocked", response.Header.Get("Location"))

	response = get(t, noRedirect(), baseURL+"/torrent/"+blockHashHex, cookieHeader(session))
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	response.Body.Close()

	blocklist, err := blocking.Open(blocking.Config{Dir: dir})
	require.NoError(t, err)

	allowed, err := blocklist.Filter(context.Background(), []protocol.ID{mustHash(t, blockHashHex)})
	require.NoError(t, err)
	assert.Empty(t, allowed, "the blocked hash must be remembered")
	require.NoError(t, blocklist.Close())
}
