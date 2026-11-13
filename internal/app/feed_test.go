package app_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// TestFeedCarriesTheStorageAndClassificationTables checks that the poll behind
// the dashboard carries the two tables it redraws, not just the counters.
func TestFeedCarriesTheStorageAndClassificationTables(t *testing.T) {
	now := time.Now().UTC()
	dir := t.TempDir()

	seedStore(t, dir, func() time.Time { return now }, store.Row{
		InfoHash:    lifecycleHash(9),
		Name:        "Some.Show.S01E01.1080p.WEB-DL.x264-GRP",
		Size:        1 << 30,
		CreatedAt:   now,
		Category:    classify.CategoryMoviesHD,
		FilesCount:  2,
		PrimaryFile: "Some.Show.S01E01.1080p.WEB-DL.x264-GRP.mkv",
	})

	_, baseURL := startOnDataDirectory(t, dir)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	type shard struct {
		Month    string `json:"month"`
		Rows     string `json:"rows"`
		Database string `json:"database"`
		WAL      string `json:"wal"`
	}

	type category struct {
		Name  string `json:"name"`
		Rows  string `json:"rows"`
		Share string `json:"share"`
	}

	var payload struct {
		Shards     []shard    `json:"shards"`
		Categories []category `json:"categories"`
	}

	// The statistics snapshot is taken by a goroutine just after start, so poll
	// until the feed carries the seeded shard rather than assuming a fixed wait.
	require.Eventually(t, func() bool {
		response := get(t, noRedirect(), baseURL+"/api/feed?limit=1", cookieHeader(session))
		defer response.Body.Close()

		require.Equal(t, http.StatusOK, response.StatusCode)
		require.NoError(t, json.NewDecoder(response.Body).Decode(&payload))

		return len(payload.Shards) > 0 && len(payload.Categories) > 0
	}, 5*time.Second, 50*time.Millisecond)

	require.Len(t, payload.Shards, 1)
	assert.Equal(t, now.Format("2006-01"), payload.Shards[0].Month)
	assert.NotEmpty(t, payload.Shards[0].Rows)
	assert.NotEmpty(t, payload.Shards[0].Database)

	require.NotEmpty(t, payload.Categories)
	assert.Equal(t, classify.CategoryName(classify.CategoryMoviesHD), payload.Categories[0].Name)
	assert.Equal(t, "1", payload.Categories[0].Rows)
	assert.Equal(t, "100.0%", payload.Categories[0].Share)
}
