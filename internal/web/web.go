// Package web renders the web interface. The pages are server-rendered HTML
// with one stylesheet and no JavaScript, so the interface works with a plain
// HTTP client and needs no build step.
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

// Pages is the parsed template set.
type Pages struct {
	templates map[string]*template.Template
}

// New parses the templates for every page.
func New() (*Pages, error) {
	names := []string{"setup", "login", "home"}
	pages := make(map[string]*template.Template, len(names))

	for _, name := range names {
		tmpl, err := template.New("layout").ParseFS(
			assets,
			"templates/layout.html",
			"templates/"+name+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("web: parse %s: %w", name, err)
		}

		pages[name] = tmpl
	}

	return &Pages{templates: pages}, nil
}

// Static returns the embedded stylesheet directory.
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
	tmpl, ok := p.templates[name]
	if !ok {
		return fmt.Errorf("web: no page named %q", name)
	}

	var buffer bytes.Buffer

	if err := tmpl.ExecuteTemplate(&buffer, "layout", data); err != nil {
		return fmt.Errorf("web: render %s: %w", name, err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)

	_, err := w.Write(buffer.Bytes())

	return err
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

// HomeData is the page shown once the installation is configured.
type HomeData struct {
	Title      string
	Version    string
	Commit     string
	BuildDate  string
	Uptime     string
	APIKey     string
	TorznabURL string
	DHTAddress string
	Crawling   bool
}
