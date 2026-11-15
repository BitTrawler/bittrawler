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
	"github.com/BitTrawler/bittrawler/internal/tmdb"
	"github.com/BitTrawler/bittrawler/internal/torznab"
	"github.com/BitTrawler/bittrawler/internal/web"
)

const (
	// pageSize is the number of search results per page.
	pageSize = 50
	// feedSize is the number of torrents on the live feed.
	feedSize = 20
)

// categoryFamilyOrder is the order the category families appear in the search
// page's filter.
var categoryFamilyOrder = []int{
	classify.CategoryMovies,
	classify.CategoryTV,
	classify.CategoryAudio,
	classify.CategoryBooks,
	classify.CategoryPC,
	classify.CategoryConsole,
	classify.CategoryXXX,
	classify.CategoryOther,
}

// categoryGroups are the search page's category filter: one group per family,
// with the family option first and each leaf beneath it. A family matches every
// leaf, so choosing "Movies" still finds every film, and choosing "Movies/HD"
// narrows to 720p, 1080p and 1440p.
var categoryGroups = buildCategoryGroups()

// buildCategoryGroups builds the filter from the classifier's category names, so
// a category that is added there appears here without a second list to edit.
func buildCategoryGroups() []web.CategoryGroup {
	names := classify.CategoryNames()

	groups := make([]web.CategoryGroup, 0, len(categoryFamilyOrder))

	for _, family := range categoryFamilyOrder {
		group := web.CategoryGroup{Value: family, Label: names[family]}

		leaves := make([]int, 0, len(names))
		for category := range names {
			if category != family && classify.ParentCategory(category) == family {
				leaves = append(leaves, category)
			}
		}

		sort.Ints(leaves)

		for _, leaf := range leaves {
			group.Options = append(group.Options, web.CategoryOption{Value: leaf, Label: names[leaf]})
		}

		groups = append(groups, group)
	}

	return groups
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
		notice = "Crawling started."
	case "paused":
		notice = "Crawling paused."
	}

	switch r.URL.Query().Get("removed") {
	case "removed":
		notice = "Torrent removed from the index."
	case "blocked":
		notice = "Torrent removed from the index and its hash blocked."
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

		tmdbURL, _ := contentLinks(torrent.Category, torrent.ContentID, 0)

		rows = append(rows, web.TorrentRow{
			Hash:        hex.EncodeToString(torrent.InfoHash[:]),
			Name:        torrent.Name,
			Category:    classify.CategoryName(torrent.Category),
			Size:        humanBytes(torrent.Size),
			Files:       files,
			Seeders:     torrent.Seeders,
			Leechers:    torrent.Leechers,
			Age:         humanAge(now, torrent.CreatedAt),
			Magnet:      torznab.Magnet(torrent.InfoHash, torrent.Name, torrent.Size),
			Added:       humanDate(torrent.CreatedAt),
			Peers:       strconv.Itoa(torrent.Seeders) + " seeding, " + strconv.Itoa(torrent.Leechers) + " leeching",
			PrimaryFile: torrent.PrimaryFile,
			Seasons:     formatSeasons(torrent.Seasons),
			Episodes:    formatEpisodes(torrent.Episodes),
			AirDate:     torrent.AirDate,
			TMDBURL:     tmdbURL,
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

// categoryRows is the classification table: the stored categories grouped under
// their Newznab families, largest family first, with each family's
// subcategories nested beneath it.
func categoryRows(snapshot statsSnapshot) []web.CategoryRow {
	type family struct {
		category int
		rows     int
		children map[int]int
	}

	// A category this build does not know has no parent it can be grouped
	// under, so it stands on its own rather than being gathered into a family
	// labelled "Unknown".
	names := classify.CategoryNames()

	families := make(map[int]*family)

	for category, rows := range snapshot.Categories {
		parent := classify.ParentCategory(category)
		if _, known := names[parent]; !known {
			parent = category
		}

		entry, ok := families[parent]
		if !ok {
			entry = &family{category: parent, children: make(map[int]int)}
			families[parent] = entry
		}

		entry.rows += rows

		// Rows stored at the family itself are a subcategory too: they are the
		// ones the classifier could not refine, and without them the
		// subcategories would not add up to the family total.
		entry.children[category] = rows
	}

	total := snapshot.StoreRows

	// share is the family's or subcategory's part of every stored row. Without a
	// row count there is nothing to take a share of.
	share := func(rows int) string {
		if total <= 0 {
			return ""
		}

		return strconv.FormatFloat(float64(rows)/float64(total)*100, 'f', 1, 64) + "%"
	}

	ordered := make([]*family, 0, len(families))
	for _, entry := range families {
		ordered = append(ordered, entry)
	}

	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].rows != ordered[j].rows {
			return ordered[i].rows > ordered[j].rows
		}

		return ordered[i].category < ordered[j].category
	})

	rows := make([]web.CategoryRow, 0, len(ordered))

	for _, entry := range ordered {
		name := classify.CategoryName(entry.category)

		row := web.CategoryRow{Name: name, Rows: humanCount(entry.rows), Share: share(entry.rows)}

		children := make([]int, 0, len(entry.children))
		for category := range entry.children {
			// A family with nothing beneath it is one row, not a family with a
			// single child of its own name.
			if category == entry.category && len(entry.children) == 1 {
				continue
			}

			children = append(children, category)
		}

		sort.Slice(children, func(i, j int) bool {
			if entry.children[children[i]] != entry.children[children[j]] {
				return entry.children[children[i]] > entry.children[children[j]]
			}

			return children[i] < children[j]
		})

		for _, child := range children {
			label := name

			if child != entry.category {
				// The family is already on the row above, so a subcategory only
				// needs its own part of the name.
				label = strings.TrimPrefix(classify.CategoryName(child), name+"/")
			}

			row.Children = append(row.Children, web.CategoryRow{
				Name:  label,
				Rows:  humanCount(entry.children[child]),
				Share: share(entry.children[child]),
			})
		}

		rows = append(rows, row)
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
		History:    historyBars(snapshot.History),
		Shards:     feedShards(shardRows(snapshot)),
		Categories: feedCategories(categoryRows(snapshot)),
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
		CategoryGroups:   categoryGroups,
		SelectedCategory: params.Category,
		SelectedSort:     params.Sort,
		Page:             params.Page,
		PageCount:        1,
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
		data.TotalLabel = humanCount(result.Total)
		data.PageCount = max(1, (result.Total+pageSize-1)/pageSize)
		data.HasNext = params.Page < data.PageCount
		data.NextURL = searchURL(params, params.Page+1)
	}

	a.renderPage(w, "search", data)
}

// handleTorrent renders one torrent.
func (a *App) handleTorrent(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	hash, ok := infoHashFromPath(r)
	if !ok {
		a.renderNotFound(w, r, strings.ToLower(strings.TrimSpace(r.PathValue("hash"))))

		return
	}

	raw := hex.EncodeToString(hash[:])

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

	// The TVDB id is the only thing on the page that is not in the store. The
	// client caches it, so this costs one request per show rather than one per
	// view, and a failure leaves the link out instead of failing the page.
	var tvdb int64
	if torrent.ContentID > 0 && classify.ParentCategory(torrent.Category) == classify.CategoryTV {
		tvdb = a.tvdbID(r.Context(), torrent.ContentID)
	}

	tmdbURL, tvdbURL := contentLinks(torrent.Category, torrent.ContentID, tvdb)

	data := web.TorrentData{
		Shell:       a.shell("", "Torrent"),
		Name:        torrent.Name,
		Hash:        raw,
		Magnet:      torznab.Magnet(torrent.InfoHash, torrent.Name, torrent.Size),
		Category:    classify.CategoryName(torrent.Category),
		Size:        humanBytes(torrent.Size),
		Added:       humanDate(torrent.CreatedAt),
		Files:       files,
		PrimaryFile: torrent.PrimaryFile,
		Seasons:     formatSeasons(torrent.Seasons),
		Episodes:    formatEpisodes(torrent.Episodes),
		AirDate:     torrent.AirDate,
		Peers:       strconv.Itoa(torrent.Seeders) + " seeding, " + strconv.Itoa(torrent.Leechers) + " leeching",
		TMDBURL:     tmdbURL,
		TVDBURL:     tvdbURL,
	}

	a.renderPage(w, "torrent", data)
}

// contentLinks returns the catalogue links for a stored content reference. The
// category decides whether the id is a film id or a show id, because TMDB keeps
// the two spaces separate. A row with no reference, or one classified outside
// film and television, has no links.
func contentLinks(category int, contentID, tvdbID int64) (tmdbURL, tvdbURL string) {
	if contentID <= 0 {
		return "", ""
	}

	switch classify.ParentCategory(category) {
	case classify.CategoryMovies:
		return "https://www.themoviedb.org/movie/" + strconv.FormatInt(contentID, 10), ""
	case classify.CategoryTV:
		tmdbURL = "https://www.themoviedb.org/tv/" + strconv.FormatInt(contentID, 10)
		if tvdbID > 0 {
			tvdbURL = "https://thetvdb.com/?tab=series&id=" + strconv.FormatInt(tvdbID, 10)
		}

		return tmdbURL, tvdbURL
	default:
		return "", ""
	}
}

// tvdbLookupTimeout bounds the one catalogue lookup a torrent page may make.
const tvdbLookupTimeout = 2 * time.Second

// tvdbID asks TMDB for the TVDB id of a show, which is what the TVDB link needs.
func (a *App) tvdbID(ctx context.Context, contentID int64) int64 {
	client := a.tmdbClient()
	if client == nil {
		return 0
	}

	lookupCtx, cancel := context.WithTimeout(ctx, tvdbLookupTimeout)
	defer cancel()

	ids, ok, err := client.ExternalIDs(lookupCtx, tmdb.MediumTV, contentID)
	if err != nil {
		a.logger.Debug("look up a TVDB id", "error", err)

		return 0
	}

	if !ok {
		return 0
	}

	return ids.TVDB
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
	SampleFailures        uint64 `json:"sample_failures"`
	SampleUnsupported     uint64 `json:"sample_unsupported"`
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
		SampleFailures:        counters.SampleFailures,
		SampleUnsupported:     counters.SampleUnsupported,
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
	Crawling     bool           `json:"crawling"`
	CrawlEnabled bool           `json:"crawl_enabled"`
	Stats        feedStats      `json:"stats"`
	Counters     feedCounters   `json:"counters"`
	KTable       feedKTable     `json:"ktable"`
	Runtime      feedRuntime    `json:"runtime"`
	History      []int          `json:"history"`
	Shards       []feedShard    `json:"shards"`
	Categories   []feedCategory `json:"categories"`
	Torrents     []feedTorrent  `json:"torrents"`
}

// feedShard is one row of the storage table, formatted as the page renders it.
type feedShard struct {
	Month    string `json:"month"`
	Rows     string `json:"rows"`
	Database string `json:"database"`
	WAL      string `json:"wal"`
}

// feedCategory is one row of the classification table, formatted the same way:
// a family with its total and the subcategories beneath it.
type feedCategory struct {
	Name     string         `json:"name"`
	Rows     string         `json:"rows"`
	Share    string         `json:"share"`
	Children []feedCategory `json:"children,omitempty"`
}

func feedShards(rows []web.ShardRow) []feedShard {
	shards := make([]feedShard, 0, len(rows))

	for _, row := range rows {
		shards = append(shards, feedShard{
			Month:    row.Month,
			Rows:     row.Rows,
			Database: row.Database,
			WAL:      row.WAL,
		})
	}

	return shards
}

func feedCategories(rows []web.CategoryRow) []feedCategory {
	categories := make([]feedCategory, 0, len(rows))

	for _, row := range rows {
		entry := feedCategory{Name: row.Name, Rows: row.Rows, Share: row.Share}

		for _, child := range row.Children {
			entry.Children = append(entry.Children, feedCategory{
				Name:  child.Name,
				Rows:  child.Rows,
				Share: child.Share,
			})
		}

		categories = append(categories, entry)
	}

	return categories
}
