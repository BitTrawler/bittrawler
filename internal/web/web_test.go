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
		Title:      "First run",
		Username:   "peter",
		APIKey:     "0123456789abcdef0123456789abcdef",
		TorznabURL: "http://nas:6684/torznab/api",
	})

	body := recorder.Body.String()

	assert.Equal(t, "text/html; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.Contains(t, body, "<title>First run · BitTrawler</title>")
	assert.Contains(t, body, `value="peter"`)
	assert.Contains(t, body, "http://nas:6684/torznab/api")
	assert.Contains(t, body, "0123456789abcdef0123456789abcdef")
	assert.Contains(t, body, `action="/setup"`)
	assert.Contains(t, body, "/static/app.css")
	assert.NotContains(t, body, "<script", "the interface uses no JavaScript")
}

func TestRenderSetupWithError(t *testing.T) {
	recorder := render(t, "setup", web.SetupData{
		Title: "First run",
		Error: "Passwords do not match.",
	})

	body := recorder.Body.String()

	assert.Contains(t, body, `role="alert"`)
	assert.Contains(t, body, "Passwords do not match.")
}

func TestRenderLogin(t *testing.T) {
	recorder := render(t, "login", web.LoginData{Title: "Sign in", Username: "peter"})

	body := recorder.Body.String()

	assert.Contains(t, body, "<title>Sign in · BitTrawler</title>")
	assert.Contains(t, body, `action="/login"`)
	assert.Contains(t, body, `autocomplete="current-password"`)
}

func TestRenderHome(t *testing.T) {
	recorder := render(t, "home", web.HomeData{
		Title:      "Dashboard",
		Version:    "1.2.3",
		Commit:     "abc1234",
		BuildDate:  "2026-11-13",
		Uptime:     "2h0m",
		APIKey:     "0123456789abcdef0123456789abcdef",
		TorznabURL: "http://nas:6684/torznab/api",
		DHTAddress: "0.0.0.0:6684",
		Crawling:   true,
	})

	body := recorder.Body.String()

	assert.Contains(t, body, "Version 1.2.3 (abc1234)")
	assert.Contains(t, body, "Crawling")
	assert.Contains(t, body, ">yes<")
	assert.Contains(t, body, `action="/logout"`)
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

	info, err := stylesheet.Stat()
	require.NoError(t, err)
	assert.True(t, info.Size() > 0)

	content := make([]byte, info.Size())

	_, err = stylesheet.Read(content)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(content), "--accent"))
}
