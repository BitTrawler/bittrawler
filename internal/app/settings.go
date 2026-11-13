package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/BitTrawler/bittrawler/internal/config"
	"github.com/BitTrawler/bittrawler/internal/web"
)

// settingsGroupSpec describes one card of the settings page.
type settingsGroupSpec struct {
	slug        string
	name        string
	description string
	sections    []string
}

// settingsGroups is the order and grouping of the settings page. The sections
// are prefixes of setting keys.
var settingsGroups = []settingsGroupSpec{
	{"storage", "Storage", "Shard location and retention.", []string{"data", "store"}},
	{"web", "Web Interface", "Listen address for this interface and Torznab.", []string{"web"}},
	{"logging", "Logging", "Log verbosity.", []string{"log"}},
	{"crawler", "Crawler", "Discovery, metadata fetching and writing.", []string{"crawler"}},
	{"dht", "DHT", "UDP node and peer responses.", []string{"dht"}},
	{"torznab", "Torznab", "Capabilities and page limits reported to clients.", []string{"torznab"}},
	{"tmdb", "TMDB", "Confirmation of film and television classifications.", []string{"tmdb"}},
	{"classification", "Classification", "Drop rules applied at intake.", []string{"classify"}},
}

func settingsGroup(slug string) (settingsGroupSpec, bool) {
	for _, spec := range settingsGroups {
		if spec.slug == slug {
			return spec, true
		}
	}

	return settingsGroupSpec{}, false
}

// shell builds the page chrome for the signed-in pages.
func (a *App) shell(active, title string) web.Shell {
	status := a.Status()

	return web.Shell{
		Title:    title,
		Active:   active,
		Version:  a.info.Version,
		Crawling: status.Crawling,
		Uptime:   status.Uptime.String(),
	}
}

// handleSettings renders the settings page.
func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	advanced := r.URL.Query().Get("advanced") == "1"

	notice := ""

	switch {
	case r.URL.Query().Get("saved") == "1":
		notice = "Saved."
		if r.URL.Query().Get("restart") == "1" {
			notice = "Saved. Some changes take effect after a restart."
		}
	case r.URL.Query().Get("rotated") == "1":
		notice = "The API key was rotated. Update Prowlarr and any other client."
	case r.URL.Query().Get("admin") == "1":
		notice = "The administrator account was updated and other sessions were signed out."
	}

	data := a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), advanced, notice, "")

	a.renderSettings(w, data, http.StatusOK)
}

// handleSettingsSave writes one group of settings.
func (a *App) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)

		return
	}

	if err := r.ParseForm(); err != nil {
		a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), false, "", "The form could not be read."), http.StatusBadRequest)

		return
	}

	advanced := r.PostFormValue("advanced") == "1"

	spec, ok := settingsGroup(r.PostFormValue("group"))
	if !ok {
		a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), advanced, "", "Unknown settings group."), http.StatusBadRequest)

		return
	}

	restart, err := a.saveSettings(r.Context(), spec, r.PostForm, advanced)
	if err != nil {
		a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), advanced, "", err.Error()), http.StatusBadRequest)

		return
	}

	query := url.Values{"saved": {"1"}}

	if restart {
		query.Set("restart", "1")
	}

	if advanced {
		query.Set("advanced", "1")
	}

	http.Redirect(w, r, "/settings?"+query.Encode(), http.StatusSeeOther)
}

// saveSettings applies one group's form values to a copy of the settings,
// validates it and writes only the values that changed.
func (a *App) saveSettings(ctx context.Context, spec settingsGroupSpec, form url.Values, advanced bool) (restart bool, err error) {
	current := a.currentConfig()
	candidate := current
	sources := a.sourceSnapshot()

	changed := make(map[string]struct{})

	for _, setting := range config.Settings() {
		if setting.Hidden || !inSections(setting.Key, spec.sections) || !setting.Persist {
			continue
		}

		if setting.Advanced && !advanced {
			continue
		}

		switch sources[setting.Key] {
		case config.SourceEnvironment, config.SourceFlag:
			continue
		}

		raw, take := formValue(setting, form)
		if !take {
			continue
		}

		if err := config.Set(&candidate, setting.Key, raw); err != nil {
			return false, fmt.Errorf("%s: %w", labelOf(setting.Key), err)
		}

		before, _ := config.Get(current, setting.Key)
		after, _ := config.Get(candidate, setting.Key)

		if reflect.DeepEqual(before, after) {
			continue
		}

		changed[setting.Key] = struct{}{}

		if setting.Restart {
			restart = true
		}
	}

	if len(changed) == 0 {
		return false, nil
	}

	if err := candidate.Validate(); err != nil {
		return false, err
	}

	values := make(map[string]string, len(changed))
	written := make([]string, 0, len(changed))

	for key := range changed {
		value, _ := config.Get(candidate, key)
		values[key] = config.Format(value)
		written = append(written, key)
	}

	if err := a.settingsStore.SetMany(ctx, values); err != nil {
		return false, fmt.Errorf("could not save the settings: %w", err)
	}

	a.replaceConfig(candidate, written)

	if _, ok := changed["crawler.enabled"]; ok {
		crawlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := a.SetCrawl(crawlCtx, candidate.Crawler.Enabled); err != nil {
			a.logger.Error("apply the crawler setting", "error", err)
		}
	}

	return restart, nil
}

// formValue extracts a setting from a submitted form. A checkbox that is not
// present is false; an empty secret means "leave it alone".
func formValue(setting config.Setting, form url.Values) (string, bool) {
	if setting.Type == config.TypeBool {
		if form.Has(setting.Key) {
			return "true", true
		}

		return "false", true
	}

	if !form.Has(setting.Key) {
		return "", false
	}

	value := form.Get(setting.Key)

	if setting.Secret && strings.TrimSpace(value) == "" {
		return "", false
	}

	return value, true
}

// buildSettingsData turns the settings declaration into the page's data.
func (a *App) buildSettingsData(cfg config.Config, sources map[string]config.Source, advanced bool, notice, problem string) web.SettingsData {
	data := web.SettingsData{
		Shell:         a.shell("settings", "Settings"),
		Advanced:      advanced,
		Notice:        notice,
		Error:         problem,
		APIKey:        cfg.Torznab.APIKey,
		AdminUsername: cfg.Web.AdminUsername,
	}

	for _, spec := range settingsGroups {
		group := web.SettingsGroup{
			Slug:        spec.slug,
			Name:        spec.name,
			Description: spec.description,
		}

		for _, setting := range config.Settings() {
			if setting.Hidden || !inSections(setting.Key, spec.sections) {
				continue
			}

			if setting.Advanced && !advanced {
				continue
			}

			group.Fields = append(group.Fields, settingsField(cfg, sources, setting))
		}

		data.Groups = append(data.Groups, group)
	}

	return data
}

// settingsField describes one setting to the template.
func settingsField(cfg config.Config, sources map[string]config.Source, setting config.Setting) web.SettingsField {
	label := setting.Label
	if label == "" {
		label = labelOf(setting.Key)
	}

	field := web.SettingsField{
		Key:      setting.Key,
		Label:    label,
		Usage:    setting.Usage,
		Restart:  setting.Restart,
		Advanced: setting.Advanced,
	}

	control := "text"

	switch {
	case setting.Type == config.TypeBool:
		control = "bool"
	case len(setting.Enum) > 0:
		control = "enum"
		field.Options = setting.Enum
	case setting.Type == config.TypeInt || setting.Type == config.TypeFloat:
		control = "number"
	case setting.Type == config.TypeStrings:
		control = "textarea"
	case setting.Secret:
		control = "password"
	}

	value, _ := config.Get(cfg, setting.Key)
	field.Value = config.Format(value)

	if setting.Type == config.TypeBool {
		field.Checked = value == true
	}

	if setting.Secret && !field.ReadOnly {
		field.Value = ""
		field.Placeholder = "Unchanged"
	}

	source := sources[setting.Key]
	if source == "" {
		source = config.SourceDefault
	}

	field.ReadOnly = !setting.Persist || source == config.SourceEnvironment || source == config.SourceFlag

	switch {
	case source == config.SourceEnvironment:
		field.SourceLabel = "from environment"
	case source == config.SourceFlag:
		field.SourceLabel = "from command line"
	case !setting.Persist:
		field.SourceLabel = "fixed"
	}

	if field.ReadOnly {
		control = "readonly"

		if setting.Secret {
			field.Value = "Set outside the interface"
		}
	}

	if setting.Persist && !field.ReadOnly && source == config.SourceDatabase {
		field.SourceLabel = ""
	}

	field.Type = control

	return field
}

// labelOf turns the last part of a setting key into a label: workers.metadata
// becomes "Metadata".
func labelOf(key string) string {
	parts := strings.Split(key, ".")
	name := parts[len(parts)-1]

	words := strings.Split(name, "_")
	for i, word := range words {
		if word == "" {
			continue
		}

		first, size := utf8.DecodeRuneInString(word)
		words[i] = string(unicode.ToUpper(first)) + word[size:]
	}

	return strings.Join(words, " ")
}

// inSections reports whether a key belongs to one of the sections.
func inSections(key string, sections []string) bool {
	section, _, _ := strings.Cut(key, ".")

	for _, candidate := range sections {
		if section == candidate {
			return true
		}
	}

	return false
}

// renderSettings writes the settings page.
func (a *App) renderSettings(w http.ResponseWriter, data web.SettingsData, status int) {
	if err := a.pages.Render(w, status, "settings", data); err != nil {
		a.logger.Error("render settings page", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// handleRotateAPIKey replaces the Torznab API key.
func (a *App) handleRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)

		return
	}

	if err := a.rotateAPIKey(r.Context()); err != nil {
		a.logger.Error("rotate the API key", "error", err)
		a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), false, "", "The key could not be rotated."), http.StatusInternalServerError)

		return
	}

	a.logger.Info("rotated the Torznab API key")
	http.Redirect(w, r, "/settings?rotated=1", http.StatusSeeOther)
}

// rotateAPIKey generates and stores a new Torznab API key.
func (a *App) rotateAPIKey(ctx context.Context) error {
	key, err := randomHex(16)
	if err != nil {
		return err
	}

	if err := a.settingsStore.Set(ctx, "torznab.api_key", key); err != nil {
		return err
	}

	a.setConfig(func(cfg *config.Config) {
		cfg.Torznab.APIKey = key
	})

	a.configMu.Lock()
	a.loaded.Sources["torznab.api_key"] = config.SourceDatabase
	a.configMu.Unlock()

	// The endpoint reads the key per request, so the new key applies at once.
	return nil
}

// handleAdminSave changes the administrator account.
func (a *App) handleAdminSave(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)

		return
	}

	if err := r.ParseForm(); err != nil {
		a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), false, "", "The form could not be read."), http.StatusBadRequest)

		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	currentPassword := r.PostFormValue("current_password")
	newPassword := r.PostFormValue("new_password")
	confirmPassword := r.PostFormValue("new_password_confirm")

	state := a.auth.Load()
	if state == nil {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)

		return
	}

	if !a.verifyCredentials(state.username, currentPassword) {
		a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), false, "", "The current password is wrong."), http.StatusForbidden)

		return
	}

	if err := validateUsername(username); err != nil {
		a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), false, "", err.Error()), http.StatusBadRequest)

		return
	}

	values := map[string]string{
		"web.admin_username": username,
	}

	hash := state.hash

	if newPassword != "" {
		if err := validatePassword(newPassword, confirmPassword); err != nil {
			a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), false, "", err.Error()), http.StatusBadRequest)

			return
		}

		hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
		if err != nil {
			a.logger.Error("hash the administrator password", "error", err)
			a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), false, "", "The password could not be saved."), http.StatusInternalServerError)

			return
		}

		hash = string(hashed)
		values["web.admin_password_hash"] = hash
	}

	// A new session key signs out every other browser.
	secret, err := randomHex(32)
	if err != nil {
		a.logger.Error("generate the session key", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	values["web.session_secret"] = secret

	if err := a.settingsStore.SetMany(r.Context(), values); err != nil {
		a.logger.Error("save the administrator account", "error", err)
		a.renderSettings(w, a.buildSettingsData(a.currentConfig(), a.sourceSnapshot(), false, "", "The account could not be saved."), http.StatusInternalServerError)

		return
	}

	a.setConfig(func(cfg *config.Config) {
		cfg.Web.AdminUsername = username
		cfg.Web.AdminPasswordHash = hash
		cfg.Web.SessionSecret = secret
	})

	a.configMu.Lock()
	a.loaded.Sources["web.admin_username"] = config.SourceDatabase
	a.loaded.Sources["web.admin_password_hash"] = config.SourceDatabase
	a.loaded.Sources["web.session_secret"] = config.SourceDatabase
	a.configMu.Unlock()

	a.refreshAuth()

	if err := a.setSessionCookie(w, r); err != nil {
		a.logger.Error("sign in after the password change", "error", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)

		return
	}

	a.logger.Info("administrator account updated", "username", username)
	http.Redirect(w, r, "/settings?admin=1", http.StatusSeeOther)
}

// handleCrawl pauses or resumes crawling from the dashboard.
func (a *App) handleCrawl(w http.ResponseWriter, r *http.Request) {
	if !a.authorised(w, r) {
		return
	}

	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)

		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}

	enable := r.PostFormValue("action") == "resume"

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if err := a.SetCrawl(ctx, enable); err != nil {
		a.logger.Error("change the crawl state", "error", err)
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// authorised checks the session and sends the browser to sign in when it is
// missing.
func (a *App) authorised(w http.ResponseWriter, r *http.Request) bool {
	if !a.Configured() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)

		return false
	}

	if !a.validSession(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)

		return false
	}

	return true
}
