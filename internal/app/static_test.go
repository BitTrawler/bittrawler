package app_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStaticAssetsRevalidate checks that the stylesheet and script revalidate
// on every load. With a long cache an upgraded instance keeps serving its old
// script to browsers that loaded it before the update.
func TestStaticAssetsRevalidate(t *testing.T) {
	_, baseURL := start(t)

	for _, asset := range []string{"/static/app.css", "/static/app.js"} {
		response := get(t, noRedirect(), baseURL+asset, nil)
		require.Equal(t, http.StatusOK, response.StatusCode, asset)
		assert.Equal(t, "no-cache", response.Header.Get("Cache-Control"), asset)
		response.Body.Close()
	}
}
