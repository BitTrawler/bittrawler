package app_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/app"
	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/version"
)

// noRedirect is a client that reports redirects instead of following them, so a
// test can check the status and the cookies.
func noRedirect() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 5 * time.Second,
	}
}

func start(t *testing.T) (*app.App, string) {
	t.Helper()

	t.Setenv("BITTRAWLER_CRAWLER_ENABLED", "false")
	t.Setenv("BITTRAWLER_CRAWLER_BOOTSTRAP_NODES", "")

	application, err := app.New(
		context.Background(),
		config.Overrides{
			DataDirectory: t.TempDir(),
			WebAddress:    "127.0.0.1:0",
			DHTAddress:    "127.0.0.1:0",
			LogLevel:      "error",
		},
		version.Info{Version: "test", Commit: "test", Date: "today"},
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)

	require.NoError(t, application.Start(context.Background()))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		assert.NoError(t, application.Stop(ctx))
	})

	return application, "http://" + application.WebAddress()
}

func get(t *testing.T, client *http.Client, target string, headers map[string]string) *http.Response {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, target, nil)
	require.NoError(t, err)

	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := client.Do(request)
	require.NoError(t, err)

	return response
}

func body(t *testing.T, response *http.Response) string {
	t.Helper()

	defer response.Body.Close()

	content, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	return string(content)
}

func TestSetupLoginAndTorznab(t *testing.T) {
	application, baseURL := start(t)

	assert.False(t, application.Configured())

	// The health check answers before setup.
	response := get(t, noRedirect(), baseURL+"/healthz", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)

	var health map[string]any

	require.NoError(t, json.NewDecoder(response.Body).Decode(&health))
	response.Body.Close()

	assert.Equal(t, "ok", health["status"])
	assert.Equal(t, false, health["configured"])

	// The root redirects to setup while there is no account.
	response = get(t, noRedirect(), baseURL+"/", nil)
	assert.Equal(t, http.StatusSeeOther, response.StatusCode)
	assert.Equal(t, "/setup", response.Header.Get("Location"))
	response.Body.Close()

	// The setup page offers the generated Torznab key.
	response = get(t, noRedirect(), baseURL+"/setup", nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)

	setupPage := body(t, response)
	assert.Contains(t, setupPage, "Set Up BitTrawler")
	assert.Contains(t, setupPage, application.Status().TorznabAPIKey)

	// A form post from another origin is refused.
	form := url.Values{
		"username":         {"peter"},
		"password":         {"correct-horse-battery"},
		"password_confirm": {"correct-horse-battery"},
	}

	request, err := http.NewRequest(http.MethodPost, baseURL+"/setup", strings.NewReader(form.Encode()))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "http://evil.example")

	response, err = noRedirect().Do(request)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, response.StatusCode)
	response.Body.Close()

	// A short password is rejected with a message.
	shortForm := url.Values{
		"username":         {"peter"},
		"password":         {"short"},
		"password_confirm": {"short"},
	}

	request, err = http.NewRequest(http.MethodPost, baseURL+"/setup", strings.NewReader(shortForm.Encode()))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", baseURL)

	response, err = noRedirect().Do(request)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, body(t, response), "at least 10 characters")

	// The real submission creates the account and signs in.
	request, err = http.NewRequest(http.MethodPost, baseURL+"/setup", strings.NewReader(form.Encode()))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", baseURL)

	response, err = noRedirect().Do(request)
	require.NoError(t, err)
	assert.Equal(t, http.StatusSeeOther, response.StatusCode)
	assert.True(t, application.Configured())

	var session *http.Cookie

	for _, cookie := range response.Cookies() {
		if cookie.Name == "bittrawler_session" {
			session = cookie
		}
	}

	require.NotNil(t, session, "setup signs the browser in")
	response.Body.Close()

	// A new browser has to sign in.
	response = get(t, noRedirect(), baseURL+"/", nil)
	assert.Equal(t, http.StatusSeeOther, response.StatusCode)
	assert.Equal(t, "/login", response.Header.Get("Location"))
	response.Body.Close()

	// A wrong password is rejected.
	wrongForm := url.Values{"username": {"peter"}, "password": {"wrong-password"}}

	request, err = http.NewRequest(http.MethodPost, baseURL+"/login", strings.NewReader(wrongForm.Encode()))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", baseURL)

	response, err = noRedirect().Do(request)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
	response.Body.Close()

	// The right password signs in and the dashboard shows the key.
	rightForm := url.Values{"username": {"peter"}, "password": {"correct-horse-battery"}}

	request, err = http.NewRequest(http.MethodPost, baseURL+"/login", strings.NewReader(rightForm.Encode()))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", baseURL)

	response, err = noRedirect().Do(request)
	require.NoError(t, err)
	assert.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	session = nil

	for _, cookie := range response.Cookies() {
		if cookie.Name == "bittrawler_session" {
			session = cookie
		}
	}

	require.NotNil(t, session)

	response = get(t, noRedirect(), baseURL+"/", map[string]string{
		"Cookie": session.Name + "=" + session.Value,
	})
	assert.Equal(t, http.StatusOK, response.StatusCode)

	dashboard := body(t, response)
	assert.Contains(t, dashboard, "Overview")
	assert.Contains(t, dashboard, application.Status().TorznabAPIKey)

	// The Torznab endpoint serves caps to Prowlarr with the generated key.
	response = get(t, noRedirect(), baseURL+"/torznab/api?t=caps&apikey="+application.Status().TorznabAPIKey, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Contains(t, body(t, response), "<caps")

	// The version endpoint reports the build.
	response = get(t, noRedirect(), baseURL+"/version", nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)

	var versionInfo map[string]any

	require.NoError(t, json.NewDecoder(response.Body).Decode(&versionInfo))
	response.Body.Close()

	assert.Equal(t, "test", versionInfo["version"])
}

func TestCrawlCanBePausedAndResumed(t *testing.T) {
	application, baseURL := start(t)

	// Setup first: the crawl only runs once the installation is configured.
	form := url.Values{
		"username":         {"peter"},
		"password":         {"correct-horse-battery"},
		"password_confirm": {"correct-horse-battery"},
	}

	request, err := http.NewRequest(http.MethodPost, baseURL+"/setup", strings.NewReader(form.Encode()))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", baseURL)

	response, err := noRedirect().Do(request)
	require.NoError(t, err)
	response.Body.Close()

	require.True(t, application.Configured())
	assert.False(t, application.Status().Crawling, "crawling is disabled by the test environment")

	require.NoError(t, application.SetCrawl(context.Background(), true))
	require.Eventually(t, func() bool {
		return application.Status().Crawling
	}, 5*time.Second, 20*time.Millisecond)

	require.NoError(t, application.SetCrawl(context.Background(), false))
	assert.False(t, application.Status().Crawling)
}

func TestDataDirectoryIsLocked(t *testing.T) {
	t.Setenv("BITTRAWLER_CRAWLER_ENABLED", "false")

	dir := t.TempDir()

	overrides := config.Overrides{
		DataDirectory: dir,
		WebAddress:    "127.0.0.1:0",
		DHTAddress:    "127.0.0.1:0",
		LogLevel:      "error",
	}

	first, err := app.New(context.Background(), overrides, version.Info{Version: "test"}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_ = first.Stop(ctx)
	}()

	_, err = app.New(context.Background(), overrides, version.Info{Version: "test"}, slog.New(slog.DiscardHandler))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in use by another process")
}

// completeSetup creates the administrator account through the setup form.
func completeSetup(t *testing.T, baseURL string) {
	t.Helper()

	form := url.Values{
		"username":         {"admin"},
		"password":         {"correct-horse-battery"},
		"password_confirm": {"correct-horse-battery"},
	}

	response := postForm(t, noRedirect(), baseURL, "/setup", form, nil)
	defer response.Body.Close()

	require.Equal(t, http.StatusSeeOther, response.StatusCode)
}

// signIn signs in and returns the session cookie.
func signIn(t *testing.T, baseURL, username, password string) *http.Cookie {
	t.Helper()

	form := url.Values{"username": {username}, "password": {password}}

	response := postForm(t, noRedirect(), baseURL, "/login", form, nil)
	defer response.Body.Close()

	require.Equal(t, http.StatusSeeOther, response.StatusCode)

	for _, cookie := range response.Cookies() {
		if cookie.Name == "bittrawler_session" {
			return cookie
		}
	}

	require.FailNow(t, "no session cookie in the sign-in response")

	return nil
}

// postForm submits a form with the origin header a browser would send.
func postForm(t *testing.T, client *http.Client, baseURL, path string, form url.Values, cookie *http.Cookie) *http.Response {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(form.Encode()))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", baseURL)

	if cookie != nil {
		request.AddCookie(cookie)
	}

	response, err := client.Do(request)
	require.NoError(t, err)

	return response
}

func cookieHeader(cookie *http.Cookie) map[string]string {
	if cookie == nil {
		return nil
	}

	return map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}
}

func TestSettingsPageAndActions(t *testing.T) {
	application, baseURL := start(t)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	// The static assets are served, which an earlier failure in this test caught.
	response := get(t, noRedirect(), baseURL+"/static/app.css", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Contains(t, body(t, response), "--accent")

	for _, asset := range []string{"/static/app.js", "/static/icon.svg"} {
		response = get(t, noRedirect(), baseURL+asset, nil)
		assert.Equal(t, http.StatusOK, response.StatusCode, asset)
		response.Body.Close()
	}

	// The settings page renders the declaration.
	response = get(t, noRedirect(), baseURL+"/settings", cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	page := body(t, response)
	assert.Contains(t, page, "Storage")
	assert.Contains(t, page, "API Key")
	assert.Contains(t, page, application.Status().TorznabAPIKey)
	assert.NotContains(t, page, "Seen Hashes Size", "advanced settings are hidden by default")

	response = get(t, noRedirect(), baseURL+"/settings?advanced=1", cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	advancedPage := body(t, response)
	assert.Contains(t, advancedPage, "Seen Hashes Size")
	assert.Contains(t, advancedPage, "Metadata")

	// Saving one group writes only what changed.
	form := url.Values{
		"group":                    {"crawler"},
		"advanced":                 {"0"},
		"crawler.enabled":          {"true"},
		"crawler.metadata_timeout": {"60s"},
	}

	response = postForm(t, noRedirect(), baseURL, "/settings", form, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	assert.Contains(t, response.Header.Get("Location"), "saved=1")

	value, ok := application.SettingValue("crawler.metadata_timeout")
	require.True(t, ok)
	assert.Equal(t, 60*time.Second, value)

	// Rotating the key invalidates the old one immediately.
	oldKey := application.Status().TorznabAPIKey

	response = postForm(t, noRedirect(), baseURL, "/settings/torznab/rotate", nil, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	newKey := application.Status().TorznabAPIKey
	require.NotEqual(t, oldKey, newKey)

	response = get(t, noRedirect(), baseURL+"/torznab/api?t=caps&apikey="+newKey, nil)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Contains(t, body(t, response), "<caps")

	response = get(t, noRedirect(), baseURL+"/torznab/api?t=caps&apikey="+oldKey, nil)
	assert.Contains(t, body(t, response), "invalid API key")

	// Changing the administrator account rotates the session key and signs the
	// browser back in with a new cookie.
	adminForm := url.Values{
		"username":             {"peter"},
		"current_password":     {"correct-horse-battery"},
		"new_password":         {"another-horse-battery"},
		"new_password_confirm": {"another-horse-battery"},
	}

	response = postForm(t, noRedirect(), baseURL, "/settings/admin", adminForm, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	fresh := signIn(t, baseURL, "peter", "another-horse-battery")
	assert.NotNil(t, fresh)
}

func TestCrawlCanBeToggledFromTheDashboard(t *testing.T) {
	application, baseURL := start(t)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	response := postForm(t, noRedirect(), baseURL, "/crawl", url.Values{"action": {"resume"}}, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	require.Eventually(t, func() bool {
		return application.Status().Crawling
	}, 5*time.Second, 20*time.Millisecond)

	response = postForm(t, noRedirect(), baseURL, "/crawl", url.Values{"action": {"pause"}}, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	require.Eventually(t, func() bool {
		return !application.Status().Crawling
	}, 5*time.Second, 20*time.Millisecond)
}

func TestSavingRespectsOverridesAndAdvanced(t *testing.T) {
	application, baseURL := start(t)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	// The crawler switch is pinned by the environment the test started with,
	// and the worker counts are advanced settings that this form does not show.
	form := url.Values{
		"group":                    {"crawler"},
		"advanced":                 {"0"},
		"crawler.enabled":          {"true"},
		"crawler.metadata_timeout": {"50s"},
		"crawler.workers.metadata": {"40"},
	}

	response := postForm(t, noRedirect(), baseURL, "/settings", form, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	enabled, _ := application.SettingValue("crawler.enabled")
	assert.Equal(t, false, enabled, "an environment override is not written by the form")

	workers, _ := application.SettingValue("crawler.workers.metadata")
	assert.Equal(t, 32, workers, "an advanced setting is not written by the basic form")

	timeout, _ := application.SettingValue("crawler.metadata_timeout")
	assert.Equal(t, 50*time.Second, timeout)

	// The advanced form does carry the worker count.
	form = url.Values{
		"group":                    {"crawler"},
		"advanced":                 {"1"},
		"crawler.enabled":          {"true"},
		"crawler.workers.metadata": {"40"},
	}

	response = postForm(t, noRedirect(), baseURL, "/settings", form, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	workers, _ = application.SettingValue("crawler.workers.metadata")
	assert.Equal(t, 40, workers)
}
