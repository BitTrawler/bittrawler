package app

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/web"
)

// handleSetupForm shows the first-run page until an administrator account
// exists.
func (a *App) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	if a.Configured() {
		http.Redirect(w, r, "/", http.StatusSeeOther)

		return
	}

	a.renderSetup(w, r, "", http.StatusOK)
}

func (a *App) renderSetup(w http.ResponseWriter, r *http.Request, message string, status int) {
	data := web.SetupData{
		Title:      "Set Up BitTrawler",
		Version:    a.info.Version,
		Assets:     a.assetVersion(),
		Error:      message,
		TorznabURL: a.torznabURL(r),
		APIKey:     a.currentConfig().Torznab.APIKey,
	}

	if err := a.pages.Render(w, status, "setup", data); err != nil {
		a.logger.Error("render setup page", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// handleSetupSubmit creates the administrator account and stores the optional
// TMDB token.
func (a *App) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if a.Configured() {
		http.Redirect(w, r, "/", http.StatusSeeOther)

		return
	}

	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)

		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)

	if err := r.ParseForm(); err != nil {
		a.renderSetup(w, r, "The form could not be read. Try again.", http.StatusBadRequest)

		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("password_confirm")
	token := strings.TrimSpace(r.PostFormValue("tmdb_token"))

	if err := validateUsername(username); err != nil {
		a.renderSetup(w, r, err.Error(), http.StatusBadRequest)

		return
	}

	if err := validatePassword(password, confirm); err != nil {
		a.renderSetup(w, r, err.Error(), http.StatusBadRequest)

		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		a.logger.Error("hash the administrator password", "error", err)
		a.renderSetup(w, r, "The account could not be created. Check the logs.", http.StatusInternalServerError)

		return
	}

	values := map[string]string{
		"web.admin_username":      username,
		"web.admin_password_hash": string(hash),
	}

	if token != "" {
		values["tmdb.token"] = token
	}

	if err := a.settingsStore.SetMany(r.Context(), values); err != nil {
		a.logger.Error("save setup", "error", err)
		a.renderSetup(w, r, "The settings could not be saved. Check the data directory and the logs.", http.StatusInternalServerError)

		return
	}

	a.setConfig(func(cfg *config.Config) {
		cfg.Web.AdminUsername = username
		cfg.Web.AdminPasswordHash = string(hash)

		if token != "" {
			cfg.TMDB.Token = token
		}
	})
	a.refreshAuth()

	if a.currentConfig().Crawler.Enabled {
		if err := a.startCrawl(); err != nil {
			a.logger.Error("crawl did not start", "error", err)
		}
	}

	a.logger.Info("setup completed", "username", username, "tmdb", token != "")

	if err := a.setSessionCookie(w, r); err != nil {
		a.logger.Error("sign in after setup", "error", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)

		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleLoginForm shows the sign-in page.
func (a *App) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if !a.Configured() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)

		return
	}

	next := safeNext(r.URL.Query().Get("next"))

	if a.validSession(r) {
		http.Redirect(w, r, nextOrHome(next), http.StatusSeeOther)

		return
	}

	a.renderPage(w, "login", web.LoginData{Title: "Sign In", Version: a.info.Version, Assets: a.assetVersion(), Next: next})
}

// handleLoginSubmit checks the credentials and signs the browser in.
func (a *App) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if !a.Configured() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)

		return
	}

	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)

		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)

	if err := r.ParseForm(); err != nil {
		a.renderLogin(w, "The form could not be read. Try again.", http.StatusBadRequest, "", "")

		return
	}

	next := safeNext(r.PostFormValue("next"))
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	address := clientAddress(r)

	if !a.limiter.allow(address, time.Now()) {
		a.renderLogin(w, "Too many attempts. Try again in a few minutes.", http.StatusTooManyRequests, username, next)

		return
	}

	if !a.verifyCredentials(username, password) {
		a.renderLogin(w, "Wrong username or password.", http.StatusUnauthorized, username, next)

		return
	}

	a.limiter.reset(address)

	if err := a.setSessionCookie(w, r); err != nil {
		a.logger.Error("sign in", "error", err)
		a.renderLogin(w, "Could not sign in. Check the logs.", http.StatusInternalServerError, username, next)

		return
	}

	http.Redirect(w, r, nextOrHome(next), http.StatusSeeOther)
}

// handleLogout clears the session cookie.
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)

		return
	}

	clearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) renderLogin(w http.ResponseWriter, message string, status int, username, next string) {
	data := web.LoginData{Title: "Sign In", Version: a.info.Version, Assets: a.assetVersion(), Error: message, Username: username, Next: next}

	if err := a.pages.Render(w, status, "login", data); err != nil {
		a.logger.Error("render login page", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// loginPath is where an unauthenticated visitor is sent. A page request keeps
// its destination so the sign-in form can return there; anything else, such as
// a form submission, cannot be replayed and goes to the plain sign-in page.
func loginPath(r *http.Request) string {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return "/login"
	}

	next := safeNext(r.URL.RequestURI())
	if next == "" {
		return "/login"
	}

	return "/login?next=" + url.QueryEscape(next)
}

// safeNext returns the local page to return to after signing in, or "" when
// the value is anything else: a foreign host, a scheme, or a path that a
// browser could read as protocol-relative.
func safeNext(next string) string {
	if next == "" || next == "/" || len(next) > 2000 {
		return ""
	}

	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") {
		return ""
	}

	parsed, err := url.Parse(next)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return ""
	}

	return next
}

// nextOrHome is the redirect target after a successful sign-in.
func nextOrHome(next string) string {
	if next == "" {
		return "/"
	}

	return next
}

// validateUsername checks the account name a user chose.
func validateUsername(username string) error {
	switch {
	case username == "":
		return errors.New("Choose a username.")
	case utf8.RuneCountInString(username) > 64:
		return errors.New("The username must be 64 characters or fewer.")
	case strings.ContainsAny(username, " :\t\r\n"):
		return errors.New("The username cannot contain spaces or colons.")
	default:
		return nil
	}
}

// validatePassword checks the password a user chose.
func validatePassword(password, confirm string) error {
	switch {
	case len(password) < passwordMinLength:
		return fmt.Errorf("The password must be at least %d characters.", passwordMinLength)
	case len(password) > bcryptMaxPassword:
		return fmt.Errorf("The password must be at most %d bytes.", bcryptMaxPassword)
	case password != confirm:
		return errors.New("The passwords do not match.")
	default:
		return nil
	}
}
