package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// waitForReclassify polls the settings page until a pass has finished and
// returns the page.
func waitForReclassify(t *testing.T, baseURL string, session *http.Cookie) string {
	t.Helper()

	var page string

	require.Eventually(t, func() bool {
		response := get(t, noRedirect(), baseURL+"/settings", cookieHeader(session))
		require.Equal(t, http.StatusOK, response.StatusCode)

		page = body(t, response)

		return !strings.Contains(page, "data-reclassify-running") && strings.Contains(page, "Last pass")
	}, 15*time.Second, 100*time.Millisecond)

	return page
}

// torrentCount asks the API how many rows are in a category.
func torrentCount(t *testing.T, baseURL, apiKey string, category int) int {
	t.Helper()

	response := get(t, noRedirect(), fmt.Sprintf("%s/api/torrents?cat=%d&limit=1", baseURL, category),
		map[string]string{"X-Api-Key": apiKey})
	require.Equal(t, http.StatusOK, response.StatusCode)

	var payload struct {
		Total int `json:"total"`
	}

	require.NoError(t, json.NewDecoder(response.Body).Decode(&payload))
	response.Body.Close()

	return payload.Total
}

// TestReclassifyFromTheSettingsPage checks the maintenance card: a preview
// reports the change without writing it, and apply writes it, category and
// search index together.
func TestReclassifyFromTheSettingsPage(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	seedStore(t, dir, func() time.Time { return now },
		store.Row{
			InfoHash:    lifecycleHash(40),
			Name:        "Cory.Chase.And.Friend.2024.1080p",
			Size:        1 << 30,
			CreatedAt:   now,
			Category:    classify.CategoryOther,
			FilesCount:  1,
			PrimaryFile: "clip.mp4",
		},
		store.Row{
			InfoHash:  lifecycleHash(41),
			Name:      "Sunset.Drive.2019.1080p.BluRay.x264-GRP",
			Size:      1 << 30,
			CreatedAt: now,
			Category:  classify.CategoryMoviesHD,
		},
	)

	application, baseURL := startOnDataDirectory(t, dir)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")
	apiKey := application.Status().TorznabAPIKey

	post := func(action string) {
		t.Helper()

		response := postForm(t, noRedirect(), baseURL, "/settings/reclassify",
			url.Values{"action": {action}}, session)
		require.Equal(t, http.StatusSeeOther, response.StatusCode)
		response.Body.Close()
	}

	// A preview reports the change but leaves the stored row alone.
	post("preview")

	page := waitForReclassify(t, baseURL, session)
	assert.Contains(t, page, "1 changed")
	assert.Contains(t, page, "0 written")
	assert.Contains(t, page, "Other -&gt; XXX")
	assert.Contains(t, page, "Cory.Chase.And.Friend.2024.1080p")
	assert.Contains(t, page, `href="/torrent/`, "the changed row links to its detail page")
	assert.NotContains(t, page, "Rejected torrents")
	assert.NotContains(t, page, "Remove ", "nothing can be removed before a pass reports rejected rows")
	assert.Equal(t, 0, torrentCount(t, baseURL, apiKey, classify.CategoryXXX))

	// Apply writes it, and the row moves to XXX.
	post("apply")

	page = waitForReclassify(t, baseURL, session)
	assert.Contains(t, page, "1 changed")
	assert.Contains(t, page, "1 written")
	assert.Equal(t, 1, torrentCount(t, baseURL, apiKey, classify.CategoryXXX))
	assert.Equal(t, 0, torrentCount(t, baseURL, apiKey, classify.CategoryOther))
}

// TestCleanupRemovesRejectedRows checks the maintenance card's destructive
// action: rows the blocking rules now reject are deleted from the index, and
// the page lists exactly which ones.
func TestCleanupRemovesRejectedRows(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	seedStore(t, dir, func() time.Time { return now },
		store.Row{
			InfoHash:    lifecycleHash(50),
			Name:        "(lolsonly) child cjhildporn hairless",
			Size:        25 << 20,
			CreatedAt:   now,
			Category:    classify.CategoryXXX,
			FilesCount:  1,
			PrimaryFile: "(lolsonly) child cjhildporn hairless.rar",
		},
		store.Row{
			InfoHash:  lifecycleHash(51),
			Name:      "Sunset.Drive.2019.1080p.BluRay.x264-GRP",
			Size:      1 << 30,
			CreatedAt: now,
			Category:  classify.CategoryMoviesHD,
		},
	)

	application, baseURL := startOnDataDirectory(t, dir)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")
	apiKey := application.Status().TorznabAPIKey

	// A preview reports the row and offers the removal only once it knows there
	// is something to remove.
	response := postForm(t, noRedirect(), baseURL, "/settings/reclassify",
		url.Values{"action": {"preview"}}, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	page := waitForReclassify(t, baseURL, session)
	assert.Contains(t, page, "Rejected torrents")
	assert.Contains(t, page, "cjhildporn")
	assert.Contains(t, page, "banned")
	assert.Contains(t, page, "Remove 1 Rejected Torrents")

	// Removing deletes it and clears the list, so the action cannot be repeated
	// against rows that are already gone.
	response = postForm(t, noRedirect(), baseURL, "/settings/reclassify",
		url.Values{"action": {"cleanup"}}, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	page = waitForReclassify(t, baseURL, session)
	assert.Contains(t, page, "1 deleted")
	assert.NotContains(t, page, "cjhildporn")
	assert.NotContains(t, page, "Remove 1 Rejected Torrents")

	assert.Equal(t, 0, torrentCount(t, baseURL, apiKey, classify.CategoryXXX))
	assert.Equal(t, 1, torrentCount(t, baseURL, apiKey, classify.CategoryMoviesHD))
}
