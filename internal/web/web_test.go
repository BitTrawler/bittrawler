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

func TestRenderDashboard(t *testing.T) {
	recorder := render(t, "dashboard", web.DashboardData{
		Shell: web.Shell{
			Title:    "Overview",
			Active:   "dashboard",
			Version:  "1.2.3",
			Crawling: true,
			Uptime:   "2h0m",
		},
		Commit:          "abc1234",
		BuildDate:       "2026-11-13",
		APIKey:          "0123456789abcdef0123456789abcdef",
		TorznabURL:      "http://nas:6684/torznab/api",
		DHTAddress:      "0.0.0.0:6684",
		DataDirectory:   "/data",
		RetentionMonths: 12,
	})

	body := recorder.Body.String()

	assert.Contains(t, body, `class="sidebar"`)
	assert.Contains(t, body, `class="nav-link is-active" href="/">Overview</a>`)
	assert.Contains(t, body, "Crawling")
	assert.Contains(t, body, "Pause Crawling")
	assert.Contains(t, body, "12 months")
	assert.Contains(t, body, "http://nas:6684/torznab/api")
	assert.Contains(t, body, `action="/logout"`)
}

func TestRenderPausedDashboard(t *testing.T) {
	recorder := render(t, "dashboard", web.DashboardData{
		Shell: web.Shell{Title: "Dashboard", Active: "dashboard", Version: "dev", Crawling: false, Uptime: "0s"},
	})

	body := recorder.Body.String()

	assert.Contains(t, body, "Resume Crawling")
	assert.NotContains(t, body, "Pause Crawling")
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
