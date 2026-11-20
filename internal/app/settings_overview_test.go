package app_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOverviewFeedSettings checks that the overview's live lists can be turned
// off, resized and slowed down from the settings page, and that a list that is
// turned off is not rendered at all. The rejected feed is also off when every
// reason is switched off: it would have nothing to show.
func TestOverviewFeedSettings(t *testing.T) {
	application, baseURL := start(t)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	response := get(t, noRedirect(), baseURL+"/settings", cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	page := body(t, response)
	assert.Contains(t, page, `name="overview.show_recent"`)
	assert.Contains(t, page, `name="overview.recent_rows"`)
	assert.Contains(t, page, `name="overview.show_rejected"`)
	assert.Contains(t, page, `name="overview.rejected_rows"`)
	assert.Contains(t, page, `class="switch" type="checkbox" name="overview.rejected_reasons"`)
	assert.Contains(t, page, `class="switch-row"><span>Foreign script</span>`)
	assert.Contains(t, page, `value="script" checked`)
	assert.Contains(t, page, `name="overview.refresh"`)

	// An interval below a second would be a request loop against the instance.
	response = postForm(t, noRedirect(), baseURL, "/settings", url.Values{
		"group":                {"overview"},
		"advanced":             {"0"},
		"overview.refresh":     {"500ms"},
		"overview.recent_rows": {"5"},
	}, session)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, body(t, response), "at least 1s")

	// A reason the feed cannot show is refused rather than silently ignored.
	response = postForm(t, noRedirect(), baseURL, "/settings", url.Values{
		"group":                     {"overview"},
		"advanced":                  {"0"},
		"overview.rejected_reasons": {"klingon"},
		"overview.recent_rows":      {"5"},
		"overview.rejected_rows":    {"4"},
		"overview.refresh":          {"3s"},
	}, session)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, body(t, response), "unknown reason")

	// The rejected list is switched on, but every reason is off, so the feed has
	// nothing to show and is not rendered at all.
	response = postForm(t, noRedirect(), baseURL, "/settings", url.Values{
		"group":                  {"overview"},
		"advanced":               {"0"},
		"overview.show_recent":   {"true"},
		"overview.show_rejected": {"true"},
		"overview.recent_rows":   {"5"},
		"overview.rejected_rows": {"4"},
		"overview.refresh":       {"5s"},
	}, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	rejectedReasons, ok := application.SettingValue("overview.rejected_reasons")
	require.True(t, ok)
	assert.Empty(t, rejectedReasons)

	response = get(t, noRedirect(), baseURL+"/", cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	overview := body(t, response)
	assert.Contains(t, overview, "Recently Added")
	assert.NotContains(t, overview, "Recently Rejected")
	assert.NotContains(t, overview, `id="rejected-feed"`)
	assert.NotContains(t, overview, `aria-label="Pause updates to Recently Rejected"`)

	// Switching both lists off hides them whether or not their reasons are on.
	response = postForm(t, noRedirect(), baseURL, "/settings", url.Values{
		"group":                     {"overview"},
		"advanced":                  {"0"},
		"overview.recent_rows":      {"5"},
		"overview.rejected_rows":    {"4"},
		"overview.rejected_reasons": {"banned"},
		"overview.refresh":          {"5s"},
	}, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	showRecent, ok := application.SettingValue("overview.show_recent")
	require.True(t, ok)
	assert.Equal(t, false, showRecent)

	recentRows, _ := application.SettingValue("overview.recent_rows")
	assert.Equal(t, 5, recentRows)

	response = get(t, noRedirect(), baseURL+"/", cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	overview = body(t, response)
	assert.NotContains(t, overview, "Recently Added")
	assert.NotContains(t, overview, "Recently Rejected")
	assert.NotContains(t, overview, `id="feed"`)
	assert.NotContains(t, overview, `id="rejected-feed"`)
	assert.NotContains(t, overview, "data-feed-toggle")

	// Turning them back on shows them again, with the new sizes and interval.
	response = postForm(t, noRedirect(), baseURL, "/settings", url.Values{
		"group":                     {"overview"},
		"advanced":                  {"0"},
		"overview.show_recent":      {"true"},
		"overview.show_rejected":    {"true"},
		"overview.recent_rows":      {"7"},
		"overview.rejected_rows":    {"3"},
		"overview.rejected_reasons": {"banned", "script"},
		"overview.refresh":          {"2s"},
	}, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()

	response = get(t, noRedirect(), baseURL+"/", cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	overview = body(t, response)
	assert.Contains(t, overview, "Recently Added")
	assert.Contains(t, overview, "Recently Rejected")
	assert.Contains(t, overview, `data-refresh-ms="2000"`)
	assert.Contains(t, overview, `aria-label="Pause updates to Recently Rejected"`)
}
