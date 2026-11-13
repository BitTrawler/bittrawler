package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postWithOriginHeaders submits a form with exactly the origin headers given.
// An empty origin or referer leaves that header off the request, so a caller
// can send neither, one, or both.
func postWithOriginHeaders(t *testing.T, baseURL, path string, form url.Values, cookie *http.Cookie, origin, referer string) *http.Response {
	t.Helper()

	request, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(form.Encode()))
	require.NoError(t, err)

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if origin != "" {
		request.Header.Set("Origin", origin)
	}

	if referer != "" {
		request.Header.Set("Referer", referer)
	}

	if cookie != nil {
		request.AddCookie(cookie)
	}

	response, err := noRedirect().Do(request)
	require.NoError(t, err)

	return response
}

// csrfRoute is one state-changing form route. refusedForm is posted to prove
// the origin check refuses it before it acts; genuineForm is the form that is
// allowed to act when the origin is our own.
type csrfRoute struct {
	name        string
	path        string
	refusedForm url.Values
	genuineForm url.Values
	withSession bool
	genuineCode int
}

func TestStateChangingPostsRefuseForeignOrigins(t *testing.T) {
	application, baseURL := start(t)

	closeIdleConnections(t)

	// The setup form is the only state-changing route served before there is
	// an account. A foreign or missing origin must not create one.
	setupForm := url.Values{
		"username":         {"admin"},
		"password":         {"correct-horse-battery"},
		"password_confirm": {"correct-horse-battery"},
	}

	refusedOrigins := []struct {
		name    string
		origin  string
		referer string
	}{
		{"foreign origin", "http://evil.example", ""},
		{"foreign referer without origin", "", "http://evil.example/form"},
		{"neither header", "", ""},
	}

	for _, headers := range refusedOrigins {
		response := postWithOriginHeaders(t, baseURL, "/setup", setupForm, nil, headers.origin, headers.referer)
		require.Equal(t, http.StatusForbidden, response.StatusCode, "setup with %s", headers.name)
		response.Body.Close()

		assert.False(t, application.Configured(), "a refused setup must not create the account")
	}

	// A genuine origin is not refused; the short password is what rejects this
	// submission, so the account is still unconfigured afterwards.
	shortForm := url.Values{
		"username":         {"admin"},
		"password":         {"short"},
		"password_confirm": {"short"},
	}

	response := postWithOriginHeaders(t, baseURL, "/setup", shortForm, nil, baseURL, "")
	assert.NotEqual(t, http.StatusForbidden, response.StatusCode, "a same-origin setup must not be refused")
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	response.Body.Close()
	assert.False(t, application.Configured())

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	routes := []csrfRoute{
		{
			name:        "login",
			path:        "/login",
			refusedForm: url.Values{"username": {"admin"}, "password": {"wrong-password"}},
			genuineForm: url.Values{"username": {"admin"}, "password": {"wrong-password"}},
			genuineCode: http.StatusUnauthorized,
		},
		{
			name:        "logout",
			path:        "/logout",
			withSession: true,
			genuineCode: http.StatusSeeOther,
		},
		{
			name:        "settings",
			path:        "/settings",
			refusedForm: url.Values{"group": {"storage"}},
			genuineForm: url.Values{"group": {"storage"}},
			withSession: true,
			genuineCode: http.StatusSeeOther,
		},
		{
			name:        "rotate the API key",
			path:        "/settings/torznab/rotate",
			withSession: true,
			genuineCode: http.StatusSeeOther,
		},
		{
			name:        "crawl toggle",
			path:        "/crawl",
			refusedForm: url.Values{"action": {"resume"}},
			genuineForm: url.Values{"action": {"pause"}},
			withSession: true,
			genuineCode: http.StatusSeeOther,
		},
		{
			// Last: saving the admin account rotates the session key and signs
			// this browser in again, which would invalidate the cookie used by
			// every route after it.
			name:        "admin account",
			path:        "/settings/admin",
			refusedForm: url.Values{"username": {"admin"}, "current_password": {"correct-horse-battery"}},
			genuineForm: url.Values{"username": {"admin"}, "current_password": {"correct-horse-battery"}},
			withSession: true,
			genuineCode: http.StatusSeeOther,
		},
	}

	apiKeyBefore := application.Status().TorznabAPIKey

	// Every route refuses all three kinds of foreign or missing origin.
	for _, route := range routes {
		var cookie *http.Cookie
		if route.withSession {
			cookie = session
		}

		for _, headers := range refusedOrigins {
			response := postWithOriginHeaders(t, baseURL, route.path, route.refusedForm, cookie, headers.origin, headers.referer)
			require.Equal(t, http.StatusForbidden, response.StatusCode, "%s with %s", route.name, headers.name)
			response.Body.Close()
		}
	}

	// The refused requests returned before the handlers acted. The API key and
	// the crawl state are the side effects that a missing origin check would
	// have changed.
	assert.Equal(t, apiKeyBefore, application.Status().TorznabAPIKey, "a refused rotate must leave the API key unchanged")
	assert.False(t, application.Status().Crawling, "a refused crawl toggle must not start the crawler")

	// The same routes accept a post from our own origin.
	for _, route := range routes {
		var cookie *http.Cookie
		if route.withSession {
			cookie = session
		}

		response := postWithOriginHeaders(t, baseURL, route.path, route.genuineForm, cookie, baseURL, "")
		assert.NotEqual(t, http.StatusForbidden, response.StatusCode, "%s from our own origin", route.name)
		assert.Equal(t, route.genuineCode, response.StatusCode, "%s from our own origin", route.name)
		response.Body.Close()
	}
}
