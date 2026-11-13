// Package web renders the web interface. The pages are server-rendered HTML
// with one stylesheet and one icon, so the interface works with a plain HTTP
// client and needs no build step.
package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html static/*
var assets embed.FS

// pageTemplates is the layout template that wraps each page.
type pageTemplates struct {
	layout *template.Template
	base   string
}

// Pages is the parsed template set.
type Pages struct {
	pages map[string]pageTemplates
}

// New parses the templates for every page. The signed-out pages use the
// centred authentication layout; the signed-in pages use the application shell.
func New() (*Pages, error) {
	app := []string{"dashboard", "settings"}
	auth := []string{"setup", "login"}

	pages := make(map[string]pageTemplates, len(app)+len(auth))

	for _, name := range app {
		tmpl, err := template.New("layout").ParseFS(assets,
			"templates/layout.html",
			"templates/"+name+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("web: parse %s: %w", name, err)
		}

		pages[name] = pageTemplates{layout: tmpl, base: "layout"}
	}

	for _, name := range auth {
		tmpl, err := template.New("auth").ParseFS(assets,
			"templates/auth.html",
			"templates/"+name+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("web: parse %s: %w", name, err)
		}

		pages[name] = pageTemplates{layout: tmpl, base: "auth"}
	}

	return &Pages{pages: pages}, nil
}

// Static returns the embedded asset directory.
func Static() fs.FS {
	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(fmt.Sprintf("web: static assets: %v", err))
	}

	return static
}

// Render executes a page and writes it with the headers the interface needs.
// The page is rendered to memory first, so a template error produces a 500
// rather than half a page.
func (p *Pages) Render(w http.ResponseWriter, status int, name string, data any) error {
	page, ok := p.pages[name]
	if !ok {
		return fmt.Errorf("web: no page named %q", name)
	}

	var buffer bytes.Buffer

	if err := page.layout.ExecuteTemplate(&buffer, page.base, data); err != nil {
		return fmt.Errorf("web: render %s: %w", name, err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)

	_, err := w.Write(buffer.Bytes())

	return err
}

// Shell is the signed-in page chrome: the sidebar and the page title.
type Shell struct {
	Title    string
	Active   string
	Version  string
	Crawling bool
	Uptime   string
}

// SetupData is the first-run page.
type SetupData struct {
	Title      string
	Error      string
	Username   string
	APIKey     string
	TorznabURL string
}

// LoginData is the sign-in page.
type LoginData struct {
	Title    string
	Error    string
	Username string
}

// DashboardData is the status page.
type DashboardData struct {
	Shell
	Commit          string
	BuildDate       string
	APIKey          string
	TorznabURL      string
	DHTAddress      string
	DataDirectory   string
	RetentionMonths int
	CrawlEnabled    bool
}

// SettingsData is the settings page.
type SettingsData struct {
	Shell
	Advanced      bool
	Notice        string
	Error         string
	Groups        []SettingsGroup
	APIKey        string
	AdminUsername string
}

// SettingsGroup is one card of related settings.
type SettingsGroup struct {
	Slug        string
	Name        string
	Description string
	Fields      []SettingsField
}

// SettingsField is one setting's label, help text and control.
type SettingsField struct {
	Key         string
	Label       string
	Usage       string
	Type        string
	Value       string
	Placeholder string
	SourceLabel string
	Checked     bool
	ReadOnly    bool
	Advanced    bool
	Restart     bool
	Options     []string
}
