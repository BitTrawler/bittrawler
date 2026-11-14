package app_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDropScriptsIsACheckboxList checks that the writing-system setting is a
// list of boxes rather than a free-text field, and that ticked and cleared
// boxes both reach the stored setting.
func TestDropScriptsIsACheckboxList(t *testing.T) {
	application, baseURL := start(t)

	completeSetup(t, baseURL)
	session := signIn(t, baseURL, "admin", "correct-horse-battery")

	response := get(t, noRedirect(), baseURL+"/settings", cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	page := body(t, response)
	assert.Contains(t, page, `type="checkbox" name="classify.drop_scripts"`)
	assert.Contains(t, page, `value="cjk"`)
	assert.Contains(t, page, `value="nonlatin"`)
	assert.NotContains(t, page, `<textarea name="classify.drop_scripts"`)

	save := func(values url.Values) {
		t.Helper()

		form := url.Values{
			"group":      {"classification"},
			"advanced":   {"0"},
			"drop_adult": {"false"},
		}

		for key, value := range values {
			form[key] = value
		}

		response := postForm(t, noRedirect(), baseURL, "/settings", form, session)
		assert.Equal(t, http.StatusSeeOther, response.StatusCode, "form %v", form)
		response.Body.Close()
	}

	save(url.Values{"classify.drop_scripts": {"cjk", "hebrew"}})

	value, ok := application.SettingValue("classify.drop_scripts")
	require.True(t, ok)
	assert.Equal(t, []string{"cjk", "hebrew"}, value)

	// The saved values come back ticked.
	response = get(t, noRedirect(), baseURL+"/settings", cookieHeader(session))
	require.Equal(t, http.StatusOK, response.StatusCode)

	page = body(t, response)
	assert.Contains(t, page, `value="cjk" checked`)
	assert.Contains(t, page, `value="hebrew" checked`)
	assert.NotContains(t, page, `value="greek" checked`)

	// Clearing every box clears the setting.
	save(url.Values{})

	value, ok = application.SettingValue("classify.drop_scripts")
	require.True(t, ok)
	assert.Empty(t, value)
}
