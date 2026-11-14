package app

import (
	"crypto/subtle"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/dhtcrawler"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// handleStatsAPI serves GET /api/stats. It reads the cached snapshot, so a poll
// costs no store work.
func (a *App) handleStatsAPI(w http.ResponseWriter, r *http.Request) {
	if !a.Configured() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not set up"})

		return
	}

	if !a.statsKeyValid(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid API key"})

		return
	}

	status := a.Status()
	snapshot := a.snapshot()

	response := statsAPIResponse{
		Version:         status.Version,
		UptimeSeconds:   int64(status.Uptime.Seconds()),
		Configured:      status.Configured,
		Crawling:        status.Crawling,
		CrawlEnabled:    status.CrawlEnabled,
		IngestPerMinute: math.Round(snapshot.RatePerMinute*10) / 10,
		DHT:             feedKTable{Nodes: snapshot.Nodes, Hashes: snapshot.Hashes},
		Store: statsAPIStore{
			Rows:        snapshot.StoreRows,
			Bytes:       snapshot.StoreBytes,
			BytesPerRow: math.Round(snapshot.BytesPerRow),
			Shards:      statsAPIShards(snapshot.Shards),
		},
		Categories: statsAPICategories(snapshot.Categories),
		Counters:   feedCountersFrom(snapshot.Counters),
		Pipeline:   statsAPIPipelineFrom(snapshot.Counters.Pipeline),
		Runtime: feedRuntime{
			HeapBytes:  snapshot.HeapAlloc,
			RSSBytes:   snapshot.RSSBytes,
			Goroutines: snapshot.Goroutines,
			CPUPercent: snapshot.CPUPercent,
		},
	}

	if r.URL.Query().Get("history") == "1" {
		history := statsAPIHistoryFrom(snapshot.History)
		response.History = &history
	}

	writeJSON(w, http.StatusOK, response)
}

// statsKeyValid reports whether the request carries the Torznab API key. A
// session cookie is never accepted: this endpoint is for machines.
func (a *App) statsKeyValid(r *http.Request) bool {
	expected := a.currentConfig().Torznab.APIKey
	if expected == "" {
		return false
	}

	key := r.URL.Query().Get("apikey")
	if key == "" {
		key = r.Header.Get("X-Api-Key")
	}

	return subtle.ConstantTimeCompare([]byte(key), []byte(expected)) == 1
}

// statsAPIShards converts the snapshot's shard rows into the API form.
func statsAPIShards(shards []store.ShardStats) []statsAPIShard {
	rows := make([]statsAPIShard, 0, len(shards))

	for _, shard := range shards {
		rows = append(rows, statsAPIShard{
			Month:    shard.Month,
			Rows:     shard.Rows,
			Bytes:    shard.DBBytes + shard.WALBytes,
			WALBytes: shard.WALBytes,
		})
	}

	return rows
}

// statsAPICategories orders the category counts the way the overview does:
// largest first, then by id so equal counts are stable.
func statsAPICategories(counts map[int]int) []statsAPICategory {
	categories := make([]statsAPICategory, 0, len(counts))

	for category, count := range counts {
		categories = append(categories, statsAPICategory{
			ID:    category,
			Name:  classify.CategoryName(category),
			Count: count,
		})
	}

	sort.Slice(categories, func(i, j int) bool {
		if categories[i].Count != categories[j].Count {
			return categories[i].Count > categories[j].Count
		}

		return categories[i].ID < categories[j].ID
	})

	return categories
}

// statsAPIHistoryFrom exposes the retained rate samples with RFC 3339 times.
func statsAPIHistoryFrom(history []rateSample) []statsAPIHistory {
	samples := make([]statsAPIHistory, 0, len(history))

	for _, sample := range history {
		samples = append(samples, statsAPIHistory{
			At:                sample.at.UTC().Format(time.RFC3339),
			TorrentsPersisted: sample.persisted,
			HashesTriaged:     sample.triaged,
			MetadataFetched:   sample.metadata,
		})
	}

	return samples
}

// statsAPIResponse is the body of GET /api/stats.
type statsAPIResponse struct {
	Version         string             `json:"version"`
	UptimeSeconds   int64              `json:"uptime_seconds"`
	Configured      bool               `json:"configured"`
	Crawling        bool               `json:"crawling"`
	CrawlEnabled    bool               `json:"crawl_enabled"`
	IngestPerMinute float64            `json:"ingest_per_minute"`
	DHT             feedKTable         `json:"dht"`
	Store           statsAPIStore      `json:"store"`
	Categories      []statsAPICategory `json:"categories"`
	Counters        feedCounters       `json:"counters"`
	Pipeline        statsAPIPipeline   `json:"pipeline"`
	Runtime         feedRuntime        `json:"runtime"`
	// History is a pointer so a requested but empty history still encodes as
	// an array instead of being left out.
	History *[]statsAPIHistory `json:"history,omitempty"`
}

// statsAPIPipeline is the crawl pipeline's queue and worker state.
type statsAPIPipeline struct {
	Ping     statsAPIStage `json:"ping"`
	FindNode statsAPIStage `json:"find_node"`
	Sample   statsAPIStage `json:"sample"`
	GetPeers statsAPIStage `json:"get_peers"`
	Metadata statsAPIStage `json:"metadata"`
	TMDB     statsAPIStage `json:"tmdb"`
	Triage   statsAPIStage `json:"triage"`
	Persist  statsAPIStage `json:"persist"`
}

// statsAPIStage is one stage of the pipeline.
type statsAPIStage struct {
	Workers  int `json:"workers"`
	Queued   int `json:"queued"`
	InFlight int `json:"in_flight"`
}

func statsAPIPipelineFrom(pipeline dhtcrawler.PipelineStats) statsAPIPipeline {
	stage := func(stage dhtcrawler.StageStats) statsAPIStage {
		return statsAPIStage{Workers: stage.Workers, Queued: stage.Queued, InFlight: stage.InFlight}
	}

	return statsAPIPipeline{
		Ping:     stage(pipeline.Ping),
		FindNode: stage(pipeline.FindNode),
		Sample:   stage(pipeline.Sample),
		GetPeers: stage(pipeline.GetPeers),
		Metadata: stage(pipeline.Metadata),
		TMDB:     stage(pipeline.TMDB),
		Triage:   stage(pipeline.Triage),
		Persist:  stage(pipeline.Persist),
	}
}

// statsAPIStore is the store's footprint.
type statsAPIStore struct {
	Rows        int             `json:"rows"`
	Bytes       int64           `json:"bytes"`
	BytesPerRow float64         `json:"bytes_per_row"`
	Shards      []statsAPIShard `json:"shards"`
}

// statsAPIShard is one monthly shard; Bytes includes the write-ahead log.
type statsAPIShard struct {
	Month    string `json:"month"`
	Rows     int    `json:"rows"`
	Bytes    int64  `json:"bytes"`
	WALBytes int64  `json:"wal_bytes"`
}

// statsAPICategory is one stored category and its row count.
type statsAPICategory struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// statsAPIHistory is one retained rate sample.
type statsAPIHistory struct {
	At                string `json:"at"`
	TorrentsPersisted uint64 `json:"torrents_persisted"`
	HashesTriaged     uint64 `json:"hashes_triaged"`
	MetadataFetched   uint64 `json:"metadata_fetched"`
}
