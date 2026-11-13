package app

import (
	"context"
	"encoding/hex"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/dhtcrawler"
	"github.com/BitTrawler/bittrawler/internal/store"
	"github.com/BitTrawler/bittrawler/internal/torznab"
	"github.com/BitTrawler/bittrawler/internal/web"
)

const (
	// pageSize is the number of search results per page.
	pageSize = 50
	// feedSize is the number of torrents on the live feed.
	feedSize = 20
)

// categoryOptions are the parent Newznab categories the search page filters by.
// A parent category matches every leaf beneath it.
var categoryOptions = []web.CategoryOption{
	{Value: 0, Label: "All categories"},
	{Value: 2000, Label: "Movies"},
	{Value: 5000, Label: "TV"},
	{Value: 3000, Label: "Audio"},
	{Value: 7000, Label: "Books"},
	{Value: 4000, Label: "PC"},
	{Value: 6000, Label: "XXX"},
	{Value: 8000, Label: "Other"},
}

// handleOverview renders the dashboard: live counters, the pipeline, storage,
// the newest torrents and the Torznab endpoint.
func (a *App) handleOverview(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	status := a.Status()
	cfg := a.currentConfig()
	snapshot := a.snapshot()

	notice := ""

	switch r.URL.Query().Get("crawl") {
	case "started":
		notice = "Crawling started. The first torrents usually appear within a minute."
	case "paused":
		notice = "Crawling paused. The index keeps serving while it is paused."
	}

	data := web.OverviewData{
		Shell:  a.shell("dashboard", "Overview"),
		Notice: notice,
		// The DHT bootstrap takes a little while: say so instead of showing
		// zeroes with no explanation.
		Warming:    status.Crawling && snapshot.Counters.NodesDiscovered == 0,
		CrawlError: status.CrawlError,
		Stats: web.OverviewStats{
			Stored:      humanCount(snapshot.StoreRows),
			Rate:        humanRate(snapshot.RatePerMinute),
			Database:    humanBytes(snapshot.StoreBytes),
			BytesPerRow: humanBytes(int64(math.Round(snapshot.BytesPerRow))),
			Nodes:       humanCount(snapshot.Nodes),
			Crawled:     humanCount(int(snapshot.Counters.TorrentsPersisted)),
			Shards:      len(snapshot.Months),
		},
		Recent:          a.recentTorrents(r.Context(), feedSize),
		Pipeline:        pipelineGroups(snapshot),
		Runtime:         runtimeItems(snapshot),
		Shards:          shardRows(snapshot),
		Categories:      categoryRows(snapshot),
		History:         historyBars(snapshot.History),
		Commit:          status.Commit,
		BuildDate:       status.BuildDate,
		Uptime:          status.Uptime.String(),
		APIKey:          status.TorznabAPIKey,
		TorznabURL:      a.torznabURL(r),
		DHTAddress:      status.DHTAddress,
		DataDirectory:   a.dataDir,
		RetentionMonths: cfg.Store.RetentionMonths,
		CrawlEnabled:    cfg.Crawler.Enabled,
	}

	a.renderPage(w, "dashboard", data)
}

// recentTorrents returns the newest rows for the live feed.
func (a *App) recentTorrents(ctx context.Context, limit int) []web.TorrentRow {
	torrents, err := a.torrentStore.Latest(ctx, limit)
	if err != nil {
		a.logger.Error("read the latest torrents", "error", err)

		return nil
	}

	return torrentRows(torrents)
}

// torrentRows converts stored torrents into the rows the templates and the feed
// JavaScript share.
func torrentRows(torrents []store.Torrent) []web.TorrentRow {
	now := time.Now()

	rows := make([]web.TorrentRow, 0, len(torrents))

	for _, torrent := range torrents {
		files := "unknown"
		if torrent.FilesCount >= 0 {
			files = humanCount(torrent.FilesCount)
		}

		rows = append(rows, web.TorrentRow{
			Hash:     hex.EncodeToString(torrent.InfoHash[:]),
			Name:     torrent.Name,
			Category: classify.CategoryName(torrent.Category),
			Size:     humanBytes(torrent.Size),
			Files:    files,
			Seeders:  torrent.Seeders,
			Leechers: torrent.Leechers,
			Age:      humanAge(now, torrent.CreatedAt),
		})
	}

	return rows
}

// pipelineGroups lays the crawler's counters out in the groups the overview
// shows.
func pipelineGroups(snapshot statsSnapshot) []web.CounterGroup {
	counters := snapshot.Counters

	return []web.CounterGroup{
		{
			Name: "Discovery",
			Items: []web.CounterItem{
				{Key: "nodes_discovered", Label: "Nodes discovered", Value: humanCount(int(counters.NodesDiscovered))},
				{Key: "samples_received", Label: "Samples received", Value: humanCount(int(counters.SamplesReceived))},
				{Key: "hashes_discovered", Label: "Hashes discovered", Value: humanCount(int(counters.HashesDiscovered))},
				{Key: "hashes_triaged", Label: "Hashes triaged", Value: humanCount(int(counters.HashesTriaged))},
				{Key: "ktable_nodes", Label: "Routing table", Value: humanCount(snapshot.Nodes)},
				{Key: "ktable_hashes", Label: "Hashes tracked", Value: humanCount(snapshot.Hashes)},
			},
		},
		{
			Name: "Metadata",
			Items: []web.CounterItem{
				{Key: "peer_lookups", Label: "Peer lookups", Value: humanCount(int(counters.PeerLookups))},
				{Key: "peer_lookups_empty", Label: "No peers", Value: humanCount(int(counters.PeerLookupsEmpty))},
				{Key: "peer_lookups_failed", Label: "Lookup failures", Value: humanCount(int(counters.PeerLookupsFailed))},
				{Key: "metadata_fetched", Label: "Metadata fetched", Value: humanCount(int(counters.MetadataFetched))},
				{Key: "metadata_failed", Label: "Metadata failed", Value: humanCount(int(counters.MetadataFailed))},
				{Key: "persist_failures", Label: "Persist failures", Value: humanCount(int(counters.PersistFailures))},
			},
		},
		{
			Name: "Outcomes",
			Items: []web.CounterItem{
				{Key: "torrents_persisted", Label: "Indexed", Value: humanCount(int(counters.TorrentsPersisted))},
				{Key: "infohashes_blocked", Label: "Blocked", Value: humanCount(int(counters.InfohashesBlocked))},
				{Key: "infohashes_deceptive", Label: "Deceptive", Value: humanCount(int(counters.InfohashesDeceptive))},
				{Key: "torrents_dropped", Label: "Dropped adult", Value: humanCount(int(counters.TorrentsDropped))},
				{Key: "torrents_dropped_script", Label: "Dropped script", Value: humanCount(int(counters.TorrentsDroppedScript))},
			},
		},
		tmdbGroup(counters),
	}
}

// tmdbGroup is the TMDB confirmation counters.
func tmdbGroup(counters dhtcrawler.Stats) web.CounterGroup {
	return web.CounterGroup{
		Name: "TMDB",
		Items: []web.CounterItem{
			{Key: "tmdb_calls", Label: "Calls", Value: humanCount(int(counters.TMDBCalls))},
			{Key: "tmdb_matched", Label: "Matched", Value: humanCount(int(counters.TMDBMatched))},
			{Key: "tmdb_corrected", Label: "Corrected", Value: humanCount(int(counters.TMDBCorrected))},
			{Key: "tmdb_adult", Label: "Flagged adult", Value: humanCount(int(counters.TMDBAdult))},
			{Key: "tmdb_failures", Label: "Failures", Value: humanCount(int(counters.TMDBFailures))},
		},
	}
}

// runtimeItems is the process footprint shown in the status card.
func runtimeItems(snapshot statsSnapshot) []web.CounterItem {
	return []web.CounterItem{
		{Key: "heap", Label: "Heap", Value: humanBytes(int64(snapshot.HeapAlloc))},
		{Key: "rss", Label: "Peak memory", Value: humanBytes(int64(snapshot.RSSBytes))},
		{Key: "goroutines", Label: "Goroutines", Value: humanCount(snapshot.Goroutines)},
		{Key: "cpu", Label: "CPU", Value: strconv.FormatFloat(snapshot.CPUPercent, 'f', 1, 64) + "%"},
	}
}

// shardRows is the per-month storage table, newest month first.
func shardRows(snapshot statsSnapshot) []web.ShardRow {
	rows := make([]web.ShardRow, 0, len(snapshot.Shards))

	for _, shard := range snapshot.Shards {
		rows = append(rows, web.ShardRow{
			Month:    shard.Month,
			Rows:     humanCount(shard.Rows),
			Database: humanBytes(shard.DBBytes),
			WAL:      humanBytes(shard.WALBytes),
		})
	}

	return rows
}

// categoryRows is the stored category breakdown, largest first.
func categoryRows(snapshot statsSnapshot) []web.CategoryRow {
	type entry struct {
		category int
		rows     int
	}

	entries := make([]entry, 0, len(snapshot.Categories))

	for category, rows := range snapshot.Categories {
		entries = append(entries, entry{category: category, rows: rows})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].rows != entries[j].rows {
			return entries[i].rows > entries[j].rows
		}

		return entries[i].category < entries[j].category
	})

	total := snapshot.StoreRows

	rows := make([]web.CategoryRow, 0, len(entries))

	for _, entry := range entries {
		share := ""

		if total > 0 {
			share = strconv.FormatFloat(float64(entry.rows)/float64(total)*100, 'f', 1, 64) + "%"
		}

		rows = append(rows, web.CategoryRow{
			Name:  classify.CategoryName(entry.category),
			Rows:  humanCount(entry.rows),
			Share: share,
		})
	}

	return rows
}

// historyBars turns the sample history into bar heights for the activity chart.
// The bars are the torrents persisted between samples, scaled to the busiest
// interval in the window.
func historyBars(history []rateSample) []int {
	if len(history) < 2 {
		return nil
	}

	// One bar per sample beyond the first, limited to the chart width.
	start := 1
	if len(history)-1 > sparklineBars {
		start = len(history) - sparklineBars
	}

	heights := make([]int, 0, len(history)-1)

	for i := start; i < len(history); i++ {
		delta := history[i].persisted - history[i-1].persisted
		if delta > math.MaxInt32 {
			delta = 0
		}

		heights = append(heights, int(delta))
	}

	peak := 0

	for _, height := range heights {
		if height > peak {
			peak = height
		}
	}

	for i, height := range heights {
		switch {
		case peak == 0:
			heights[i] = 0
		case height == 0:
			heights[i] = 1
		default:
			heights[i] = 4 + height*96/peak
		}
	}

	return heights
}

// handleFeed returns the live counters and newest torrents for the feed poll.
func (a *App) handleFeed(w http.ResponseWriter, r *http.Request) {
	if !a.Configured() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not configured"})

		return
	}

	if !a.validSession(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})

		return
	}

	limit := feedSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	snapshot := a.snapshot()
	status := a.Status()

	response := feedResponse{
		Crawling:     status.Crawling,
		CrawlEnabled: a.currentConfig().Crawler.Enabled,
		Stats: feedStats{
			Stored:        snapshot.StoreRows,
			RatePerMinute: math.Round(snapshot.RatePerMinute*10) / 10,
			Bytes:         snapshot.StoreBytes,
			Shards:        len(snapshot.Months),
			Persisted:     snapshot.Counters.TorrentsPersisted,
			BytesPerRow:   math.Round(snapshot.BytesPerRow),
		},
		Counters: feedCountersFrom(snapshot.Counters),
		KTable: feedKTable{
			Nodes:  snapshot.Nodes,
			Hashes: snapshot.Hashes,
		},
		Runtime: feedRuntime{
			HeapBytes:  snapshot.HeapAlloc,
			RSSBytes:   snapshot.RSSBytes,
			Goroutines: snapshot.Goroutines,
			CPUPercent: snapshot.CPUPercent,
		},
		History: historyBars(snapshot.History),
	}

	torrents, err := a.torrentStore.Latest(r.Context(), limit)
	if err != nil {
		a.logger.Error("read the feed", "error", err)
	} else {
		response.Torrents = make([]feedTorrent, 0, len(torrents))

		for _, torrent := range torrents {
			response.Torrents = append(response.Torrents, feedTorrent{
				Hash:       hex.EncodeToString(torrent.InfoHash[:]),
				Name:       torrent.Name,
				Category:   classify.CategoryName(torrent.Category),
				Size:       torrent.Size,
				Files:      torrent.FilesCount,
				Seeders:    torrent.Seeders,
				Leechers:   torrent.Leechers,
				CreatedAt:  torrent.CreatedAt.Unix(),
				CategoryID: torrent.Category,
			})
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// searchParams is a parsed search request.
type searchParams struct {
	Query    string
	Category int
	Sort     string
	Page     int
}

func (a *App) parseSearchParams(r *http.Request) searchParams {
	query := r.URL.Query()

	params := searchParams{
		Query: strings.TrimSpace(query.Get("q")),
		Sort:  "newest",
		Page:  1,
	}

	if category, err := strconv.Atoi(query.Get("cat")); err == nil {
		params.Category = category
	}

	if query.Get("sort") == "relevance" {
		params.Sort = "relevance"
	}

	if page, err := strconv.Atoi(query.Get("page")); err == nil && page > 0 {
		params.Page = page
	}

	return params
}

// searchURL builds a search link with the parameters the page carries.
func searchURL(params searchParams, page int) string {
	values := url.Values{}

	if params.Query != "" {
		values.Set("q", params.Query)
	}

	if params.Category > 0 {
		values.Set("cat", strconv.Itoa(params.Category))
	}

	if params.Sort == "relevance" {
		values.Set("sort", "relevance")
	}

	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}

	if len(values) == 0 {
		return "/search"
	}

	return "/search?" + values.Encode()
}

// handleSearch renders the search page.
func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	params := a.parseSearchParams(r)

	query := store.NewQuery()
	query.Text = params.Query
	query.Limit = pageSize
	query.Offset = (params.Page - 1) * pageSize
	query.Count = true
	query.Sort = store.SortNewest

	if params.Sort == "relevance" && query.Text != "" {
		query.Sort = store.SortRelevance
	}

	if params.Category > 0 {
		query.Categories = []int{params.Category}
	}

	data := web.SearchData{
		Shell:            a.shell("search", "Search"),
		Query:            params.Query,
		Categories:       categoryOptions,
		SelectedCategory: params.Category,
		SelectedSort:     params.Sort,
		Page:             params.Page,
		HasPrev:          params.Page > 1,
		PrevURL:          searchURL(params, params.Page-1),
	}

	result, err := a.torrentStore.Search(r.Context(), query)
	if err != nil {
		a.logger.Error("search the index", "error", err)

		data.Error = "The index could not be searched. Try again."
	} else {
		data.Results = torrentRows(result.Torrents)
		data.Total = result.Total
		data.HasNext = params.Page*pageSize < result.Total
		data.NextURL = searchURL(params, params.Page+1)
	}

	a.renderPage(w, "search", data)
}

// handleTorrent renders one torrent.
func (a *App) handleTorrent(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	raw := strings.ToLower(strings.TrimSpace(r.PathValue("hash")))

	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != 20 {
		a.renderNotFound(w, r, raw)

		return
	}

	var hash [20]byte
	copy(hash[:], decoded)

	torrent, found, err := a.torrentStore.Get(r.Context(), hash)
	if err != nil {
		a.logger.Error("read a torrent", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	if !found {
		a.renderNotFound(w, r, raw)

		return
	}

	files := "unknown"
	if torrent.FilesCount >= 0 {
		files = humanCount(torrent.FilesCount)
	}

	contentID := "none"
	if torrent.ContentID > 0 {
		contentID = strconv.FormatInt(torrent.ContentID, 10)
	}

	data := web.TorrentData{
		Shell:       a.shell("", "Torrent"),
		Name:        torrent.Name,
		Hash:        raw,
		Magnet:      torznab.Magnet(torrent.InfoHash, torrent.Name, torrent.Size),
		Category:    classify.CategoryName(torrent.Category),
		Size:        humanBytes(torrent.Size),
		Created:     humanDate(torrent.CreatedAt),
		Files:       files,
		PrimaryFile: torrent.PrimaryFile,
		Seasons:     formatSeasons(torrent.Seasons),
		Episodes:    formatEpisodes(torrent.Episodes),
		AirDate:     torrent.AirDate,
		ContentID:   contentID,
		Seeders:     strconv.Itoa(torrent.Seeders),
		Leechers:    strconv.Itoa(torrent.Leechers),
	}

	a.renderPage(w, "torrent", data)
}

// renderNotFound renders the torrent-not-found page.
func (a *App) renderNotFound(w http.ResponseWriter, r *http.Request, hash string) {
	a.renderPageStatus(w, http.StatusNotFound, "notfound", web.NotFoundData{
		Shell: a.shell("", "Torrent Not Found"),
		Hash:  hash,
	})
}

// feedStats is the counter block of the feed response.
type feedStats struct {
	Stored        int     `json:"stored"`
	RatePerMinute float64 `json:"rate_per_minute"`
	Bytes         int64   `json:"bytes"`
	Shards        int     `json:"shards"`
	Persisted     uint64  `json:"persisted"`
	BytesPerRow   float64 `json:"bytes_per_row"`
}

// feedCounters is the crawler's process-lifetime counters.
type feedCounters struct {
	NodesDiscovered       uint64 `json:"nodes_discovered"`
	SamplesReceived       uint64 `json:"samples_received"`
	HashesDiscovered      uint64 `json:"hashes_discovered"`
	HashesTriaged         uint64 `json:"hashes_triaged"`
	PeerLookups           uint64 `json:"peer_lookups"`
	PeerLookupsEmpty      uint64 `json:"peer_lookups_empty"`
	PeerLookupsFailed     uint64 `json:"peer_lookups_failed"`
	MetadataFetched       uint64 `json:"metadata_fetched"`
	MetadataFailed        uint64 `json:"metadata_failed"`
	InfohashesBlocked     uint64 `json:"infohashes_blocked"`
	InfohashesDeceptive   uint64 `json:"infohashes_deceptive"`
	TorrentsPersisted     uint64 `json:"torrents_persisted"`
	TorrentsDropped       uint64 `json:"torrents_dropped"`
	TorrentsDroppedScript uint64 `json:"torrents_dropped_script"`
	PersistFailures       uint64 `json:"persist_failures"`
	TMDBCalls             uint64 `json:"tmdb_calls"`
	TMDBMatched           uint64 `json:"tmdb_matched"`
	TMDBCorrected         uint64 `json:"tmdb_corrected"`
	TMDBAdult             uint64 `json:"tmdb_adult"`
	TMDBFailures          uint64 `json:"tmdb_failures"`
}

func feedCountersFrom(counters dhtcrawler.Stats) feedCounters {
	return feedCounters{
		NodesDiscovered:       counters.NodesDiscovered,
		SamplesReceived:       counters.SamplesReceived,
		HashesDiscovered:      counters.HashesDiscovered,
		HashesTriaged:         counters.HashesTriaged,
		PeerLookups:           counters.PeerLookups,
		PeerLookupsEmpty:      counters.PeerLookupsEmpty,
		PeerLookupsFailed:     counters.PeerLookupsFailed,
		MetadataFetched:       counters.MetadataFetched,
		MetadataFailed:        counters.MetadataFailed,
		InfohashesBlocked:     counters.InfohashesBlocked,
		InfohashesDeceptive:   counters.InfohashesDeceptive,
		TorrentsPersisted:     counters.TorrentsPersisted,
		TorrentsDropped:       counters.TorrentsDropped,
		TorrentsDroppedScript: counters.TorrentsDroppedScript,
		PersistFailures:       counters.PersistFailures,
		TMDBCalls:             counters.TMDBCalls,
		TMDBMatched:           counters.TMDBMatched,
		TMDBCorrected:         counters.TMDBCorrected,
		TMDBAdult:             counters.TMDBAdult,
		TMDBFailures:          counters.TMDBFailures,
	}
}

// feedKTable is the routing table occupancy.
type feedKTable struct {
	Nodes  int `json:"nodes"`
	Hashes int `json:"hashes"`
}

// feedRuntime is the process footprint.
type feedRuntime struct {
	HeapBytes  uint64  `json:"heap_bytes"`
	RSSBytes   uint64  `json:"rss_bytes"`
	Goroutines int     `json:"goroutines"`
	CPUPercent float64 `json:"cpu_percent"`
}

// feedTorrent is one row of the feed response.
type feedTorrent struct {
	Hash       string `json:"hash"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	Size       int64  `json:"size"`
	Files      int    `json:"files"`
	Seeders    int    `json:"seeders"`
	Leechers   int    `json:"leechers"`
	CreatedAt  int64  `json:"created_at"`
	CategoryID int    `json:"category_id"`
}

// feedResponse is the body of GET /api/feed.
type feedResponse struct {
	Crawling     bool          `json:"crawling"`
	CrawlEnabled bool          `json:"crawl_enabled"`
	Stats        feedStats     `json:"stats"`
	Counters     feedCounters  `json:"counters"`
	KTable       feedKTable    `json:"ktable"`
	Runtime      feedRuntime   `json:"runtime"`
	History      []int         `json:"history"`
	Torrents     []feedTorrent `json:"torrents"`
}
