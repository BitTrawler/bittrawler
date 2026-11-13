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

// torrentsAPIFixture seeds an index with two unidentified rows and one film.
func torrentsAPIFixture(t *testing.T) (baseURL, key string) {
	t.Helper()

	dir := t.TempDir()
	now := time.Now().UTC()

	seedStore(t, dir, func() time.Time { return now },
		store.Row{
			InfoHash:  lifecycleHash(20),
			Name:      "Mystery.Bundle.2024.1080p.WEB-DL",
			Size:      1 << 30,
			CreatedAt: now.Add(-2 * time.Hour),
			Category:  classify.CategoryOther,
		},
		store.Row{
			InfoHash:  lifecycleHash(21),
			Name:      "Unknown.Archive.Vol.3",
			Size:      4 << 20,
			CreatedAt: now.Add(-time.Hour),
			Category:  classify.CategoryOther,
		},
		store.Row{
			InfoHash:  lifecycleHash(22),
			Name:      "Sunset.Drive.2019.1080p.BluRay.x264-GRP",
			Size:      1 << 30,
			CreatedAt: now,
			Category:  classify.CategoryMoviesHD,
		},
	)

	application, baseURL := startOnDataDirectory(t, dir)
	completeSetup(t, baseURL)

	return baseURL, application.Status().TorznabAPIKey
}

func TestTorrentsAPIAuthentication(t *testing.T) {
	// Before setup the endpoint says so, whatever key is sent.
	_, unconfiguredURL := start(t)

	response := get(t, noRedirect(), unconfiguredURL+"/api/torrents", map[string]string{"X-Api-Key": "anything"})
	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	response.Body.Close()

	baseURL, key := torrentsAPIFixture(t)

	for _, headers := range []map[string]string{nil, {"X-Api-Key": "wrong"}} {
		response := get(t, noRedirect(), baseURL+"/api/torrents", headers)
		assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		response.Body.Close()
	}

	response = get(t, noRedirect(), baseURL+"/api/torrents?apikey="+key, nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	response.Body.Close()
}

func TestTorrentsAPIRowsAndFilters(t *testing.T) {
	baseURL, key := torrentsAPIFixture(t)

	type row struct {
		Hash        string `json:"hash"`
		Name        string `json:"name"`
		CategoryID  int    `json:"category_id"`
		Category    string `json:"category"`
		Size        int64  `json:"size"`
		Files       int    `json:"files"`
		PrimaryFile string `json:"primary_file"`
		CreatedAt   int64  `json:"created_at"`
	}

	type payload struct {
		Total  int   `json:"total"`
		Limit  int   `json:"limit"`
		Offset int   `json:"offset"`
		Rows   []row `json:"rows"`
	}

	fetch := func(query string) payload {
		t.Helper()

		response := get(t, noRedirect(), baseURL+"/api/torrents"+query, map[string]string{"X-Api-Key": key})
		require.Equal(t, http.StatusOK, response.StatusCode)

		var body payload
		require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
		response.Body.Close()

		return body
	}

	all := fetch("")
	assert.Equal(t, 3, all.Total)
	assert.Equal(t, 100, all.Limit)
	require.Len(t, all.Rows, 3)
	// Newest first.
	assert.Equal(t, "Sunset.Drive.2019.1080p.BluRay.x264-GRP", all.Rows[0].Name)
	assert.Equal(t, classify.CategoryMoviesHD, all.Rows[0].CategoryID)
	assert.NotEmpty(t, all.Rows[0].Hash)
	assert.Positive(t, all.Rows[0].Size)
	assert.Positive(t, all.Rows[0].CreatedAt)

	other := fetch("?cat=8000")
	assert.Equal(t, 2, other.Total)
	require.Len(t, other.Rows, 2)

	for _, row := range other.Rows {
		assert.Equal(t, classify.CategoryOther, row.CategoryID)
		assert.Equal(t, "Other", row.Category)
	}

	assert.Equal(t, "Unknown.Archive.Vol.3", other.Rows[0].Name)

	page := fetch("?limit=1&offset=1")
	assert.Equal(t, 3, page.Total)
	assert.Equal(t, 1, page.Limit)
	assert.Equal(t, 1, page.Offset)
	require.Len(t, page.Rows, 1)
	assert.Equal(t, "Unknown.Archive.Vol.3", page.Rows[0].Name)

	search := fetch("?q=mystery")
	assert.Equal(t, 1, search.Total)
	require.Len(t, search.Rows, 1)
	assert.Equal(t, "Mystery.Bundle.2024.1080p.WEB-DL", search.Rows[0].Name)

	clamped := fetch("?limit=1000")
	assert.Equal(t, 500, clamped.Limit)

	for _, query := range []string{"?cat=abc", "?limit=0", "?offset=-1"} {
		response := get(t, noRedirect(), baseURL+"/api/torrents"+query, map[string]string{"X-Api-Key": key})
		assert.Equal(t, http.StatusBadRequest, response.StatusCode, query)
		response.Body.Close()
	}

	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/torrents", nil)
	require.NoError(t, err)
	request.Header.Set("X-Api-Key", key)

	response, err := noRedirect().Do(request)
	require.NoError(t, err)
	assert.Equal(t, http.StatusMethodNotAllowed, response.StatusCode)
	response.Body.Close()
}
