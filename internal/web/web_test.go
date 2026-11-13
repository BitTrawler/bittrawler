package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/web"
)

func render(t *testing.T, page string, data any) *httptest.ResponseRecorder {
	t.Helper()

	pages, err := web.New()
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	require.NoError(t, pages.Render(recorder, http.StatusOK, page, data))

	return recorder
}

func TestRenderSetup(t *testing.T) {
	recorder := render(t, "setup", web.SetupData{
		Title:      "Set Up BitTrawler",
		Username:   "peter",
		APIKey:     "0123456789abcdef0123456789abcdef",
		TorznabURL: "http://nas:6684/torznab/api",
	})

	body := recorder.Body.String()

	assert.Equal(t, "text/html; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Contains(t, body, "<title>Set Up BitTrawler · BitTrawler</title>")
	assert.Contains(t, body, `value="peter"`)
	assert.Contains(t, body, "http://nas:6684/torznab/api")
	assert.Contains(t, body, "0123456789abcdef0123456789abcdef")
	assert.Contains(t, body, `action="/setup"`)
	assert.Contains(t, body, "/static/app.css")
	assert.Contains(t, body, `class="auth-shell"`)
	assert.NotContains(t, body, `class="sidebar"`)
	assert.Contains(t, body, `src="/static/app.js"`)
	assert.NotContains(t, body, "<script>", "the interface has no inline script")
}

func TestRenderSetupWithError(t *testing.T) {
	recorder := render(t, "setup", web.SetupData{
		Title: "First run",
		Error: "Passwords do not match.",
	})

	body := recorder.Body.String()

	assert.Contains(t, body, `class="notice error"`)
	assert.Contains(t, body, "Passwords do not match.")
}

func TestRenderLogin(t *testing.T) {
	recorder := render(t, "login", web.LoginData{Title: "Sign In", Username: "peter"})

	body := recorder.Body.String()

	assert.Contains(t, body, "<title>Sign In · BitTrawler</title>")
	assert.Contains(t, body, `action="/login"`)
	assert.Contains(t, body, `autocomplete="current-password"`)
	assert.Contains(t, body, "card auth-card auth-card-narrow")
}

func renderStatus(t *testing.T, status int, page string, data any) *httptest.ResponseRecorder {
	t.Helper()

	pages, err := web.New()
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	require.NoError(t, pages.Render(recorder, status, page, data))

	return recorder
}

func TestRenderOverview(t *testing.T) {
	recorder := render(t, "dashboard", web.OverviewData{
		Shell: web.Shell{
			Title:    "Overview",
			Active:   "dashboard",
			Version:  "1.2.3",
			Crawling: true,
			Uptime:   "2h0m",
		},
		Stats: web.OverviewStats{
			Stored:   "12,345",
			Rate:     "6.0",
			Database: "1.2 GiB",
			Shards:   2,
			Crawled:  "120",
		},
		Recent: []web.TorrentRow{
			{Hash: "0123456789abcdef0123456789abcdef01234567", Name: "Some.Release.2024.1080p", Category: "Movies/HD", Size: "1.2 GiB", Files: "3", Seeders: 12, Leechers: 3, Age: "4m"},
		},
		Pipeline: []web.CounterGroup{
			{Name: "Discovery", Items: []web.CounterItem{{Key: "nodes_discovered", Label: "Nodes discovered", Value: "1,204"}}},
		},
		Runtime: []web.CounterItem{
			{Key: "heap", Label: "Heap", Value: "24.1 MiB"},
		},
		Shards: []web.ShardRow{
			{Month: "2026-11", Rows: "18", Database: "76 KiB", WAL: "32 KiB"},
		},
		Categories: []web.CategoryRow{
			{Name: "Movies/HD", Rows: "7", Share: "38.9%"},
		},
		History:         []int{10, 40, 25, 80},
		Commit:          "abc1234",
		BuildDate:       "2026-11-13",
		Uptime:          "2h0m",
		APIKey:          "0123456789abcdef0123456789abcdef",
		TorznabURL:      "http://nas:6684/torznab/api",
		DHTAddress:      "0.0.0.0:6684",
		DataDirectory:   "/data",
		RetentionMonths: 12,
	})

	body := recorder.Body.String()

	assert.Contains(t, body, `class="sidebar"`)
	assert.Contains(t, body, `src="/static/logo.svg"`)
	assert.Contains(t, body, `href="/">Overview</a>`)
	assert.Contains(t, body, `href="/search">Search</a>`)
	assert.Contains(t, body, "Pause Crawling")
	assert.Contains(t, body, "Recently Added")
	assert.Contains(t, body, "Some.Release.2024.1080p")
	assert.Contains(t, body, `href="/torrent/0123456789abcdef0123456789abcdef01234567"`)
	assert.Contains(t, body, `data-stat="stored">12,345<`)
	assert.Contains(t, body, `id="feed"`)
	assert.Contains(t, body, "http://nas:6684/torznab/api")
	assert.Contains(t, body, "Ingest Activity")
	assert.Contains(t, body, `data-counter="nodes_discovered">1,204<`)
	assert.Contains(t, body, `data-runtime="heap">24.1 MiB<`)
	assert.Contains(t, body, "Monthly shards, newest first.")
	assert.Contains(t, body, "2026-11")
	assert.Contains(t, body, "Movies/HD")
	assert.Contains(t, body, `class="bar"`)
}

func TestRenderPausedOverview(t *testing.T) {
	recorder := render(t, "dashboard", web.OverviewData{
		Shell: web.Shell{Title: "Overview", Active: "dashboard", Version: "dev", Crawling: false, Uptime: "0s"},
	})

	body := recorder.Body.String()

	assert.Contains(t, body, "Resume Crawling")
	assert.NotContains(t, body, "Pause Crawling")
	assert.Contains(t, body, "Nothing indexed yet")
}

func TestRenderSearch(t *testing.T) {
	recorder := render(t, "search", web.SearchData{
		Shell:            web.Shell{Title: "Search", Active: "search", Version: "dev"},
		Query:            "batman",
		Categories:       []web.CategoryOption{{Value: 0, Label: "All categories"}, {Value: 2000, Label: "Movies"}},
		SelectedCategory: 2000,
		SelectedSort:     "newest",
		Results: []web.TorrentRow{
			{Hash: "0123456789abcdef0123456789abcdef01234567", Name: "Batman.2022.1080p", Category: "Movies/HD", Size: "2.0 GiB", Seeders: 4, Leechers: 1, Age: "2h"},
		},
		Total:   2,
		Page:    1,
		HasNext: true,
		NextURL: "/search?q=batman&cat=2000&page=2",
	})

	body := recorder.Body.String()

	assert.Contains(t, body, `class="nav-link is-active" href="/search">Search</a>`)
	assert.Contains(t, body, `action="/search"`)
	assert.Contains(t, body, `value="batman"`)
	assert.Contains(t, body, `<option value="2000" selected>Movies</option>`)
	assert.Contains(t, body, "2 torrents matched.")
	assert.Contains(t, body, "Batman.2022.1080p")
	assert.Contains(t, body, `href="/search?q=batman&amp;cat=2000&amp;page=2"`)
}

func TestRenderSearchEmpty(t *testing.T) {
	recorder := render(t, "search", web.SearchData{
		Shell: web.Shell{Title: "Search", Active: "search", Version: "dev"},
	})

	body := recorder.Body.String()

	assert.Contains(t, body, "The index is empty so far.")
	assert.NotContains(t, body, "<table")
}

func TestRenderTorrent(t *testing.T) {
	recorder := render(t, "torrent", web.TorrentData{
		Shell:       web.Shell{Title: "Torrent", Version: "dev"},
		Name:        "Some.Release.2024.1080p",
		Hash:        "0123456789abcdef0123456789abcdef01234567",
		Magnet:      "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Some.Release",
		Category:    "Movies/HD",
		Size:        "1.2 GiB",
		Created:     "13 Nov 2026, 14:00",
		Files:       "3",
		PrimaryFile: "Some.Release.2024.1080p.mkv",
		ContentID:   "12345",
		Seeders:     "12",
		Leechers:    "3",
	})

	body := recorder.Body.String()

	assert.Contains(t, body, "Some.Release.2024.1080p")
	assert.Contains(t, body, "magnet:?xt=urn:btih:")
	assert.Contains(t, body, "Some.Release.2024.1080p.mkv")
	assert.Contains(t, body, "Back to search")
	assert.Contains(t, body, `data-copy=`)
}

func TestRenderNotFound(t *testing.T) {
	recorder := renderStatus(t, http.StatusNotFound, "notfound", web.NotFoundData{
		Shell: web.Shell{Title: "Torrent Not Found", Version: "dev"},
		Hash:  "deadbeef",
	})

	body := recorder.Body.String()

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Contains(t, body, "Torrent Not Found")
	assert.Contains(t, body, "deadbeef")
}

func TestRenderSettings(t *testing.T) {
	recorder := render(t, "settings", web.SettingsData{
		Shell: web.Shell{Title: "Settings", Active: "settings", Version: "1.2.3", Crawling: true, Uptime: "1m"},
		Groups: []web.SettingsGroup{
			{
				Slug:        "storage",
				Name:        "Storage",
				Description: "Where the data lives.",
				Fields: []web.SettingsField{
					{Key: "data.directory", Label: "Directory", Usage: "Where the data lives.", Type: "readonly", Value: "/data", ReadOnly: true, SourceLabel: "from environment"},
					{Key: "store.retention_months", Label: "Retention Months", Usage: "How many months to keep.", Type: "number", Value: "12"},
					{Key: "crawler.enabled", Label: "Enabled", Usage: "Crawl whenever the process is running.", Type: "bool", Checked: true},
					{Key: "log.level", Label: "Level", Usage: "Log level.", Type: "enum", Value: "info", Options: []string{"debug", "info", "warn", "error"}},
					{Key: "web.address", Label: "Address", Usage: "Listen address.", Type: "text", Value: "0.0.0.0:6684", Restart: true},
				},
			},
		},
		Advanced:      false,
		APIKey:        "0123456789abcdef0123456789abcdef",
		AdminUsername: "peter",
	})

	body := recorder.Body.String()

	assert.Contains(t, body, `class="nav-link is-active" href="/settings">Settings</a>`)
	assert.Contains(t, body, "Show Advanced")
	assert.Contains(t, body, `action="/settings"`)
	assert.Contains(t, body, `name="group" value="storage"`)
	assert.Contains(t, body, `name="store.retention_months" value="12"`)
	assert.Contains(t, body, `<option value="info" selected>info</option>`)
	assert.Contains(t, body, `class="switch"`)
	assert.Contains(t, body, `name="crawler.enabled" value="true" checked`)
	assert.Contains(t, body, "from environment")
	assert.Contains(t, body, `class="tag warn">restart</span>`)
	assert.Contains(t, body, "0123456789abcdef0123456789abcdef")
	assert.Contains(t, body, "Rotate Key")
	assert.Contains(t, body, `action="/settings/admin"`)
}

func TestRenderAdvancedSettings(t *testing.T) {
	recorder := render(t, "settings", web.SettingsData{
		Shell:    web.Shell{Title: "Settings", Active: "settings", Version: "dev"},
		Advanced: true,
		Groups: []web.SettingsGroup{
			{Slug: "crawler", Name: "Crawler", Fields: []web.SettingsField{
				{Key: "crawler.workers.metadata", Label: "Metadata", Type: "number", Value: "32", Advanced: true},
			}},
		},
	})

	body := recorder.Body.String()

	assert.Contains(t, body, "Hide Advanced")
	assert.Contains(t, body, `name="advanced" value="1"`)
	assert.Contains(t, body, `name="crawler.workers.metadata" value="32"`)
}

func TestUnknownPage(t *testing.T) {
	pages, err := web.New()
	require.NoError(t, err)

	err = pages.Render(httptest.NewRecorder(), http.StatusOK, "missing", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestStaticAssets(t *testing.T) {
	static := web.Static()

	stylesheet, err := static.Open("app.css")
	require.NoError(t, err)

	defer stylesheet.Close()

	content := make([]byte, 64*1024)

	read, err := stylesheet.Read(content)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(content[:read]), "--accent"))

	icon, err := static.Open("icon.svg")
	require.NoError(t, err)

	defer icon.Close()

	info, err := icon.Stat()
	require.NoError(t, err)
	assert.True(t, info.Size() > 0)
}
