package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetupGatesPagesBeforeSetup checks that a new installation sends every
// signed-in page to the setup form.
func TestSetupGatesPagesBeforeSetup(t *testing.T) {
	application, baseURL := start(t)

	require.False(t, application.Configured())

	cases := []struct {
		name string
		path string
	}{
		{"root", "/"},
		{"settings", "/settings"},
		{"search", "/search"},
		{"login", "/login"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := get(t, noRedirect(), baseURL+tc.path, nil)
			assert.Equal(t, http.StatusSeeOther, response.StatusCode)
			assert.Equal(t, "/setup", response.Header.Get("Location"))
			response.Body.Close()
		})
	}

	// The setup form itself is served.
	response := get(t, noRedirect(), baseURL+"/setup", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Contains(t, body(t, response), "Set Up BitTrawler")
}

// TestSetupRejectsInvalidAccounts checks every validation path of the setup
// form through the handler. A refused form must leave the installation
// unconfigured.
func TestSetupRejectsInvalidAccounts(t *testing.T) {
	application, baseURL := start(t)

	validPassword := "correct-horse-battery"

	cases := []struct {
		name   string
		form   url.Values
		phrase string
	}{
		{
			name: "short password",
			form: url.Values{
				"username":         {"peter"},
				"password":         {"short"},
				"password_confirm": {"short"},
			},
			phrase: "at least 10 characters",
		},
		{
			name: "mismatched confirmation",
			form: url.Values{
				"username":         {"peter"},
				"password":         {validPassword},
				"password_confirm": {"another-horse-battery"},
			},
			phrase: "do not match",
		},
		{
			name: "blank username",
			form: url.Values{
				"username":         {"   "},
				"password":         {validPassword},
				"password_confirm": {validPassword},
			},
			phrase: "Choose a username",
		},
		{
			name: "overlong username",
			form: url.Values{
				"username":         {strings.Repeat("a", 65)},
				"password":         {validPassword},
				"password_confirm": {validPassword},
			},
			phrase: "64 characters or fewer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := postForm(t, noRedirect(), baseURL, "/setup", tc.form, nil)
			require.Equal(t, http.StatusBadRequest, response.StatusCode)
			assert.Contains(t, body(t, response), tc.phrase)

			// Still unconfigured: the setup page can be retried.
			assert.False(t, application.Configured())
			assert.False(t, application.Status().Configured)

			response = get(t, noRedirect(), baseURL+"/setup", nil)
			assert.Equal(t, http.StatusOK, response.StatusCode)
			response.Body.Close()
		})
	}
}

// TestSetupThenLoginGating checks the pages once an administrator exists and
// the sign-in behaviour.
func TestSetupThenLoginGating(t *testing.T) {
	application, baseURL := start(t)

	completeSetup(t, baseURL)
	require.True(t, application.Configured())

	// Setup is finished, so the page is no longer offered.
	response := get(t, noRedirect(), baseURL+"/setup", nil)
	assert.Equal(t, http.StatusSeeOther, response.StatusCode)
	assert.Equal(t, "/", response.Header.Get("Location"))
	response.Body.Close()

	// A new browser is shown the sign-in form.
	response = get(t, noRedirect(), baseURL+"/login", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)

	loginPage := body(t, response)
	assert.Contains(t, loginPage, "Sign In")
	assert.Contains(t, loginPage, `action="/login"`)

	// A wrong password is refused and issues no session cookie.
	wrongForm := url.Values{"username": {"admin"}, "password": {"wrong-password"}}

	response = postForm(t, noRedirect(), baseURL, "/login", wrongForm, nil)
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	assert.Contains(t, body(t, response), "Wrong username or password")
	assert.Empty(t, response.Cookies(), "a refused sign-in sets no cookie")

	// Nothing was signed in, so the settings page still asks for a sign-in,
	// and remembers where the visitor was going.
	response = get(t, noRedirect(), baseURL+"/settings", nil)
	assert.Equal(t, http.StatusSeeOther, response.StatusCode)
	assert.Equal(t, "/login?next="+url.QueryEscape("/settings"), response.Header.Get("Location"))
	response.Body.Close()

	// The right password signs in and the page opens.
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	response = get(t, noRedirect(), baseURL+"/settings", cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)
	response.Body.Close()
}
