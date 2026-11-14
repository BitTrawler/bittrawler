package app_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statsSample is one history sample as the endpoint encodes it.
type statsSample struct {
	At                string `json:"at"`
	TorrentsPersisted uint64 `json:"torrents_persisted"`
	HashesTriaged     uint64 `json:"hashes_triaged"`
	MetadataFetched   uint64 `json:"metadata_fetched"`
}

func TestStatsAPIBeforeSetup(t *testing.T) {
	_, baseURL := start(t)

	// Before there is an administrator account the endpoint reports that it
	// cannot be used yet, whatever key is sent.
	response := get(t, noRedirect(), baseURL+"/api/stats", map[string]string{"X-Api-Key": "anything"})
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)

	var payload map[string]any
	require.NoError(t, json.NewDecoder(response.Body).Decode(&payload))
	response.Body.Close()

	assert.Equal(t, "not set up", payload["error"])
}

func TestStatsAPIKeyAndContract(t *testing.T) {
	application, baseURL := start(t)
	completeSetup(t, baseURL)

	apiKey := application.Status().TorznabAPIKey
	require.NotEmpty(t, apiKey)

	// Missing and wrong keys are refused, with the same body.
	for _, headers := range []map[string]string{nil, {"X-Api-Key": "wrong"}} {
		response := get(t, noRedirect(), baseURL+"/api/stats", headers)
		require.Equal(t, http.StatusUnauthorized, response.StatusCode)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(response.Body).Decode(&payload))
		response.Body.Close()

		assert.Equal(t, "invalid API key", payload["error"])
	}

	// A signed-in browser still has to send the key.
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	response := get(t, noRedirect(), baseURL+"/api/stats", cookieHeader(session))
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	response.Body.Close()

	// The key may arrive in the header.
	response = get(t, noRedirect(), baseURL+"/api/stats", map[string]string{"X-Api-Key": apiKey})
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "no-store", response.Header.Get("Cache-Control"))
	assert.Equal(t, "application/json; charset=utf-8", response.Header.Get("Content-Type"))

	raw := body(t, response)

	var document map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &document))

	// Twelve top-level fields: the optional history is absent.
	assert.Len(t, document, 12)

	for _, key := range []string{
		"version", "uptime_seconds", "configured", "crawling", "crawl_enabled",
		"ingest_per_minute", "dht", "store", "categories", "counters", "pipeline", "runtime",
	} {
		assert.Contains(t, document, key)
	}

	var dht map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(document["dht"], &dht))
	assert.Len(t, dht, 2)
	assert.Contains(t, dht, "nodes")
	assert.Contains(t, dht, "hashes")

	var store map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(document["store"], &store))
	assert.Len(t, store, 4)

	for _, key := range []string{"rows", "bytes", "bytes_per_row", "shards"} {
		assert.Contains(t, store, key)
	}

	var stats struct {
		Version         string  `json:"version"`
		UptimeSeconds   int64   `json:"uptime_seconds"`
		Configured      bool    `json:"configured"`
		Crawling        bool    `json:"crawling"`
		CrawlEnabled    bool    `json:"crawl_enabled"`
		IngestPerMinute float64 `json:"ingest_per_minute"`
		DHT             struct {
			Nodes  int `json:"nodes"`
			Hashes int `json:"hashes"`
		} `json:"dht"`
		Store struct {
			Rows        int               `json:"rows"`
			Bytes       int64             `json:"bytes"`
			BytesPerRow float64           `json:"bytes_per_row"`
			Shards      []json.RawMessage `json:"shards"`
		} `json:"store"`
		Categories []json.RawMessage `json:"categories"`
		Counters   map[string]any    `json:"counters"`
		Runtime    map[string]any    `json:"runtime"`
	}

	require.NoError(t, json.Unmarshal([]byte(raw), &stats))

	assert.Equal(t, "test", stats.Version)
	assert.GreaterOrEqual(t, stats.UptimeSeconds, int64(0))
	assert.True(t, stats.Configured)
	assert.False(t, stats.Crawling)
	assert.False(t, stats.CrawlEnabled)
	assert.GreaterOrEqual(t, stats.DHT.Nodes, 0)
	assert.GreaterOrEqual(t, stats.DHT.Hashes, 0)
	assert.GreaterOrEqual(t, stats.Store.Rows, 0)
	assert.GreaterOrEqual(t, stats.Store.Bytes, int64(0))
	assert.GreaterOrEqual(t, stats.Store.BytesPerRow, 0.0)
	assert.NotNil(t, stats.Store.Shards, "shards is an array even when empty")
	assert.NotNil(t, stats.Categories, "categories is an array even when empty")
	assert.Len(t, stats.Counters, 22)

	for _, key := range []string{"torrents_persisted", "hashes_triaged", "metadata_fetched"} {
		assert.Contains(t, stats.Counters, key)
	}

	for _, key := range []string{"heap_bytes", "rss_bytes", "goroutines", "cpu_percent"} {
		assert.Contains(t, stats.Runtime, key)
	}

	for _, shard := range stats.Store.Shards {
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(shard, &fields))
		assert.Len(t, fields, 4)

		for _, key := range []string{"month", "rows", "bytes", "wal_bytes"} {
			assert.Contains(t, fields, key)
		}
	}

	// The key may also arrive in the query.
	response = get(t, noRedirect(), baseURL+"/api/stats?apikey="+url.QueryEscape(apiKey), nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	response.Body.Close()
}

func TestStatsAPIHistoryAndMethod(t *testing.T) {
	application, baseURL := start(t)
	completeSetup(t, baseURL)

	apiKey := url.QueryEscape(application.Status().TorznabAPIKey)

	// The history is opt-in.
	response := get(t, noRedirect(), baseURL+"/api/stats?apikey="+apiKey, nil)
	require.Equal(t, http.StatusOK, response.StatusCode)

	var plain map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(response.Body).Decode(&plain))
	response.Body.Close()

	_, present := plain["history"]
	assert.False(t, present, "history is only returned when asked for")

	// The first stats pass fills a sample quickly; wait for one so its fields
	// are checked too.
	var (
		samples   []statsSample
		sampleRaw []map[string]json.RawMessage
	)

	for attempt := 0; ; attempt++ {
		response = get(t, noRedirect(), baseURL+"/api/stats?apikey="+apiKey+"&history=1", nil)

		var raw map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(response.Body).Decode(&raw))
		response.Body.Close()

		history, ok := raw["history"]
		require.True(t, ok, "history is returned when asked for")

		samples = nil
		sampleRaw = nil

		require.NoError(t, json.Unmarshal(history, &samples))
		require.NoError(t, json.Unmarshal(history, &sampleRaw))

		if len(samples) > 0 {
			break
		}

		require.Less(t, attempt, 250, "no history sample was recorded within five seconds")

		time.Sleep(20 * time.Millisecond)
	}

	for _, fields := range sampleRaw {
		assert.Len(t, fields, 4)

		for _, key := range []string{"at", "torrents_persisted", "hashes_triaged", "metadata_fetched"} {
			assert.Contains(t, fields, key)
		}
	}

	for _, sample := range samples {
		parsed, err := time.Parse(time.RFC3339, sample.At)
		require.NoError(t, err)
		assert.Equal(t, time.UTC, parsed.Location())
	}

	// A method-scoped route answers 405 before the handler runs.
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/stats", nil)
	require.NoError(t, err)

	postResponse, err := noRedirect().Do(request)
	require.NoError(t, err)
	defer postResponse.Body.Close()

	assert.Equal(t, http.StatusMethodNotAllowed, postResponse.StatusCode)
}
