package app

import (
	"errors"
	"fmt"
	"net/http"
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
		Title:      "First run",
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

	if a.validSession(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)

		return
	}

	a.renderPage(w, "login", web.LoginData{Title: "Sign in"})
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
		a.renderLogin(w, "The form could not be read. Try again.", http.StatusBadRequest, "")

		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	address := clientAddress(r)

	if !a.limiter.allow(address, time.Now()) {
		a.renderLogin(w, "Too many attempts. Try again in a few minutes.", http.StatusTooManyRequests, username)

		return
	}

	if !a.verifyCredentials(username, password) {
		a.renderLogin(w, "Wrong username or password.", http.StatusUnauthorized, username)

		return
	}

	a.limiter.reset(address)

	if err := a.setSessionCookie(w, r); err != nil {
		a.logger.Error("sign in", "error", err)
		a.renderLogin(w, "Could not sign in. Check the logs.", http.StatusInternalServerError, username)

		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
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

func (a *App) renderLogin(w http.ResponseWriter, message string, status int, username string) {
	data := web.LoginData{Title: "Sign in", Error: message, Username: username}

	if err := a.pages.Render(w, status, "login", data); err != nil {
		a.logger.Error("render login page", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
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
