package app_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStaticAssetsAreImmutable checks that the stylesheet, script and icons are
// cached for good. Their URLs carry the build stamp, so a new build asks for a
// new URL and an upgraded instance cannot keep serving its old script to a
// browser that loaded it before the update.
func TestStaticAssetsAreImmutable(t *testing.T) {
	_, baseURL := start(t)

	for _, asset := range []string{"/static/app.css", "/static/app.js", "/static/logo.svg"} {
		response := get(t, noRedirect(), baseURL+asset, nil)
		require.Equal(t, http.StatusOK, response.StatusCode, asset)
		assert.Equal(t, "public, max-age=31536000, immutable", response.Header.Get("Cache-Control"), asset)
		response.Body.Close()
	}
}
