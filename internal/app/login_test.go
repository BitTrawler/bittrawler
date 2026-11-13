package app_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closeIdleConnections closes the shared transport's pooled connections before
// the app is stopped. Cleanups run last-in, first-out, so this must be called
// after start. Without it http.Server.Shutdown can wait up to the server's
// ReadHeaderTimeout for a pooled keep-alive connection that never carries
// another request, which turns a 0.4s test into a 5.6s one seemingly at random.
func closeIdleConnections(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		if transport, ok := http.DefaultTransport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	})
}

// failedSignIn posts a sign-in with a username that cannot match the account.
// verifyCredentials returns before hashing the password on a wrong username, so
// the attempt is cheap; it is still a failed sign-in and still counted by the
// rate limiter, which is what this test measures.
func failedSignIn(t *testing.T, baseURL string) *http.Response {
	t.Helper()

	return postForm(t, noRedirect(), baseURL, "/login", url.Values{
		"username": {"nobody"},
		"password": {"wrong-password"},
	}, nil)
}

func TestLoginRateLimit(t *testing.T) {
	_, baseURL := start(t)

	closeIdleConnections(t)

	completeSetup(t, baseURL)

	// Nine failures, then a success. A successful sign-in clears the attempt
	// count (loginLimiter.reset), so the failures that follow must be allowed
	// rather than inherited.
	for attempt := 1; attempt <= 9; attempt++ {
		response := failedSignIn(t, baseURL)
		require.Equal(t, http.StatusUnauthorized, response.StatusCode, "attempt %d", attempt)
		assert.Contains(t, body(t, response), "Wrong username or password.")
	}

	require.NotNil(t, signIn(t, baseURL, "admin", "correct-horse-battery"))

	// The limiter allows ten failed attempts from one address. If the success
	// did not reset the count the first of these would be refused.
	for attempt := 1; attempt <= 10; attempt++ {
		response := failedSignIn(t, baseURL)
		require.Equal(t, http.StatusUnauthorized, response.StatusCode, "attempt %d after the reset", attempt)
		response.Body.Close()
	}

	// The next attempt is refused before the credentials are checked, even
	// though this time the password is right.
	response := postForm(t, noRedirect(), baseURL, "/login", url.Values{
		"username": {"admin"},
		"password": {"correct-horse-battery"},
	}, nil)

	require.Equal(t, http.StatusTooManyRequests, response.StatusCode)

	for _, cookie := range response.Cookies() {
		assert.NotEqual(t, "bittrawler_session", cookie.Name, "a refused sign-in must not set a session cookie")
	}

	assert.Contains(t, body(t, response), "Too many attempts")
}
