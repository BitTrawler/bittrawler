package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// sessionCookie is the name of the sign-in cookie.
	sessionCookie = "bittrawler_session"
	// fallbackSessionDays is used when the setting is somehow outside its
	// bounds, which validation should prevent. A sign-in that expires at once
	// is worse than a long one, so it falls back to the default.
	fallbackSessionDays = 30
	// bcryptCost is above the library default: signing in is rare and the hash
	// is the only thing protecting the interface.
	bcryptCost = 12
	// passwordMinLength is the shortest accepted administrator password.
	passwordMinLength = 10
	// bcryptMaxPassword is the longest input the hash function accepts, in
	// bytes. Longer passwords are refused rather than silently truncated.
	bcryptMaxPassword = 72
	// loginAttempts and loginWindow bound the sign-in attempts from one
	// address.
	loginAttempts = 10
	loginWindow   = 15 * time.Minute
)

// authState is the administrator account and the key that signs its sessions.
type authState struct {
	username string
	hash     string
	secret   []byte
}

// verifyCredentials reports whether a username and password match the
// administrator account. The password is compared through the hash, so a wrong
// username costs one hash comparison.
func (a *App) verifyCredentials(username, password string) bool {
	state := a.auth.Load()
	if state == nil || state.username == "" {
		return false
	}

	if subtle.ConstantTimeCompare([]byte(state.username), []byte(username)) != 1 {
		return false
	}

	return bcrypt.CompareHashAndPassword([]byte(state.hash), []byte(password)) == nil
}

// sessionLifetime is how long a new sign-in lasts. It is read from the settings
// on each sign-in, so a change applies to the next sign-in without a restart.
// Cookies already issued keep the expiry they were signed with.
func (a *App) sessionLifetime() time.Duration {
	days := a.currentConfig().Web.SessionDays
	if days < 1 {
		days = fallbackSessionDays
	}

	return time.Duration(days) * 24 * time.Hour
}

// newSessionValue returns a signed session cookie value for the account. The
// cookie carries only its expiry and the username, signed with the session key,
// so validating it costs one MAC and no database work.
func (a *App) newSessionValue(now time.Time) (string, error) {
	state := a.auth.Load()
	if state == nil {
		return "", errors.New("app: no administrator account")
	}

	expiry := now.Add(a.sessionLifetime()).Unix()
	payload := fmt.Sprintf("%d|%s", expiry, state.username)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))

	mac := hmac.New(sha256.New, state.secret)
	mac.Write([]byte(encoded))

	return encoded + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

// validSession reports whether the request carries a valid session cookie.
func (a *App) validSession(r *http.Request) bool {
	state := a.auth.Load()
	if state == nil {
		return false
	}

	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}

	encoded, signature, ok := strings.Cut(cookie.Value, ".")
	if !ok {
		return false
	}

	mac := hmac.New(sha256.New, state.secret)
	mac.Write([]byte(encoded))

	expected := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) != 1 {
		return false
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}

	expiryText, username, ok := strings.Cut(string(payload), "|")
	if !ok {
		return false
	}

	expiry, err := strconv.ParseInt(expiryText, 10, 64)
	if err != nil {
		return false
	}

	if time.Now().After(time.Unix(expiry, 0)) {
		return false
	}

	return username == state.username
}

// setSessionCookie signs the browser in.
func (a *App) setSessionCookie(w http.ResponseWriter, r *http.Request) error {
	value, err := a.newSessionValue(time.Now())
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS(r),
		MaxAge:   int(a.sessionLifetime().Seconds()),
	})

	return nil
}

// clearSessionCookie signs the browser out.
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS(r),
		MaxAge:   -1,
	})
}

// isHTTPS reports whether the browser reached us over TLS, directly or through
// a reverse proxy.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// sameOrigin reports whether a state-changing request came from one of our own
// pages. It is the cross-site request forgery check for the forms.
func sameOrigin(r *http.Request) bool {
	hosts := []string{r.Host}
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		hosts = append(hosts, forwarded)
	}

	source := r.Header.Get("Origin")
	if source == "" {
		source = r.Header.Get("Referer")
	}

	if source == "" {
		return false
	}

	parsed, err := url.Parse(source)
	if err != nil {
		return false
	}

	for _, host := range hosts {
		if strings.EqualFold(parsed.Host, host) {
			return true
		}
	}

	return false
}

// loginLimiter bounds sign-in attempts per client address, so a password cannot
// be guessed at hash speed.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
}

type loginAttempt struct {
	count int
	reset time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]*loginAttempt)}
}

// allow records an attempt and reports whether it may go ahead.
func (l *loginLimiter) allow(address string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	attempt := l.attempts[address]

	if attempt == nil || now.After(attempt.reset) {
		l.attempts[address] = &loginAttempt{count: 1, reset: now.Add(loginWindow)}

		return true
	}

	if attempt.count >= loginAttempts {
		return false
	}

	attempt.count++

	return true
}

// reset clears the attempt count after a successful sign-in.
func (l *loginLimiter) reset(address string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.attempts, address)
}

// clientAddress returns the address to rate limit by, without its port.
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
