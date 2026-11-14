package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/app"
)

// statusWithoutUptime returns the instance snapshot with the uptime zeroed, so
// two snapshots taken at different moments can be compared.
func statusWithoutUptime(application *app.App) app.Status {
	status := application.Status()
	status.Uptime = 0

	return status
}

// TestAdminFormRejectsBadChanges drives the administrator form through the HTTP
// handler. Every rejected change must leave the account, the session key and the
// instance status untouched.
func TestAdminFormRejectsBadChanges(t *testing.T) {
	application, baseURL := start(t)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	before := statusWithoutUptime(application)

	usernameBefore, ok := application.SettingValue("web.admin_username")
	require.True(t, ok)
	assert.Equal(t, "admin", usernameBefore)

	hashBefore, ok := application.SettingValue("web.admin_password_hash")
	require.True(t, ok)

	secretBefore, ok := application.SettingValue("web.session_secret")
	require.True(t, ok)

	// unchanged reports that a rejected request changed nothing: the status,
	// the stored account and the session key are as they were, and the session
	// signed before the attempt still opens the settings page.
	unchanged := func(t *testing.T) {
		t.Helper()

		assert.Equal(t, before, statusWithoutUptime(application), "the instance status is unchanged")

		usernameAfter, _ := application.SettingValue("web.admin_username")
		hashAfter, _ := application.SettingValue("web.admin_password_hash")
		secretAfter, _ := application.SettingValue("web.session_secret")

		assert.Equal(t, usernameBefore, usernameAfter, "the administrator username is unchanged")
		assert.Equal(t, hashBefore, hashAfter, "the password hash is unchanged")
		assert.Equal(t, secretBefore, secretAfter, "the session key is unchanged")

		response := get(t, noRedirect(), baseURL+"/settings", cookieHeader(session))
		require.Equal(t, http.StatusOK, response.StatusCode, "the old session still works")
		response.Body.Close()
	}

	validPassword := "another-horse-battery"

	t.Run("wrong current password", func(t *testing.T) {
		form := url.Values{
			"username":             {"peter"},
			"current_password":     {"not-the-current-password"},
			"new_password":         {validPassword},
			"new_password_confirm": {validPassword},
		}

		response := postForm(t, noRedirect(), baseURL, "/settings/admin", form, session)
		require.Equal(t, http.StatusForbidden, response.StatusCode)
		assert.Contains(t, body(t, response), "current password is wrong")

		unchanged(t)
	})

	t.Run("short new password", func(t *testing.T) {
		form := url.Values{
			"username":             {"admin"},
			"current_password":     {"correct-horse-battery"},
			"new_password":         {"short"},
			"new_password_confirm": {"short"},
		}

		response := postForm(t, noRedirect(), baseURL, "/settings/admin", form, session)
		require.Equal(t, http.StatusBadRequest, response.StatusCode)
		assert.Contains(t, body(t, response), "at least 10 characters")

		unchanged(t)
	})

	t.Run("mismatched confirmation", func(t *testing.T) {
		form := url.Values{
			"username":             {"admin"},
			"current_password":     {"correct-horse-battery"},
			"new_password":         {validPassword},
			"new_password_confirm": {"another-horse-batterx"},
		}

		response := postForm(t, noRedirect(), baseURL, "/settings/admin", form, session)
		require.Equal(t, http.StatusBadRequest, response.StatusCode)
		assert.Contains(t, body(t, response), "do not match")

		unchanged(t)
	})

	t.Run("blank username", func(t *testing.T) {
		form := url.Values{
			"username":             {"   "},
			"current_password":     {"correct-horse-battery"},
			"new_password":         {validPassword},
			"new_password_confirm": {validPassword},
		}

		response := postForm(t, noRedirect(), baseURL, "/settings/admin", form, session)
		require.Equal(t, http.StatusBadRequest, response.StatusCode)
		assert.Contains(t, body(t, response), "Choose a username")

		unchanged(t)
	})

	t.Run("overlong username", func(t *testing.T) {
		form := url.Values{
			"username":             {strings.Repeat("a", 65)},
			"current_password":     {"correct-horse-battery"},
			"new_password":         {validPassword},
			"new_password_confirm": {validPassword},
		}

		response := postForm(t, noRedirect(), baseURL, "/settings/admin", form, session)
		require.Equal(t, http.StatusBadRequest, response.StatusCode)
		assert.Contains(t, body(t, response), "64 characters or fewer")

		unchanged(t)
	})

	// The original credentials still sign in, which proves the username and
	// password were never written.
	signIn(t, baseURL, "admin", "correct-horse-battery")
}

// TestSettingsSaveRejectsBadValues checks that a value the config parser
// rejects is refused and that an unknown key is ignored.
func TestSettingsSaveRejectsBadValues(t *testing.T) {
	application, baseURL := start(t)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	before := statusWithoutUptime(application)

	workersBefore, ok := application.SettingValue("crawler.workers.metadata")
	require.True(t, ok)
	assert.Equal(t, 200, workersBefore, "the default metadata worker count")

	timeoutBefore, ok := application.SettingValue("crawler.metadata_timeout")
	require.True(t, ok)
	assert.Equal(t, 45*time.Second, timeoutBefore)

	// The parser rejects a value its setting's type cannot hold. The worker
	// counts are advanced settings, so the form has to ask for them.
	form := url.Values{
		"group":                    {"crawler"},
		"advanced":                 {"1"},
		"crawler.workers.metadata": {"many"},
	}

	response := postForm(t, noRedirect(), baseURL, "/settings", form, session)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, body(t, response), "invalid integer")

	workersAfter, _ := application.SettingValue("crawler.workers.metadata")
	assert.Equal(t, workersBefore, workersAfter, "a rejected value is not applied")
	assert.Equal(t, before, statusWithoutUptime(application), "a rejected value changes nothing")

	// An unknown key is ignored: the save reports success and writes nothing.
	form = url.Values{
		"group":             {"crawler"},
		"advanced":          {"1"},
		"crawler.not_a_key": {"1"},
	}

	response = postForm(t, noRedirect(), baseURL, "/settings", form, session)
	require.Equal(t, http.StatusSeeOther, response.StatusCode)
	response.Body.Close()
	assert.Contains(t, response.Header.Get("Location"), "saved=1")

	timeoutAfter, _ := application.SettingValue("crawler.metadata_timeout")
	assert.Equal(t, timeoutBefore, timeoutAfter)

	workersAfter, _ = application.SettingValue("crawler.workers.metadata")
	assert.Equal(t, workersBefore, workersAfter)

	assert.Equal(t, before, statusWithoutUptime(application), "an unknown key changes nothing")
}
