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

// templateFuncs are the helpers the templates share.
var templateFuncs = template.FuncMap{
	"version": versionLabel,
}

// versionLabel is the compact build label the sidebar shows: a release number
// keeps its v prefix ("v1.1"), and a development build is shown by its own
// name ("main").
func versionLabel(version string) string {
	if version == "" {
		return ""
	}

	if version[0] >= '0' && version[0] <= '9' {
		return "v" + version
	}

	return version
}

// New parses the templates for every page. The signed-out pages use the
// centred authentication layout; the signed-in pages use the application shell.
func New() (*Pages, error) {
	app := []string{"dashboard", "search", "torrent", "notfound", "settings"}
	auth := []string{"setup", "login"}

	pages := make(map[string]pageTemplates, len(app)+len(auth))

	for _, name := range app {
		tmpl, err := template.New("layout").Funcs(templateFuncs).ParseFS(assets,
			"templates/layout.html",
			"templates/"+name+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("web: parse %s: %w", name, err)
		}

		pages[name] = pageTemplates{layout: tmpl, base: "layout"}
	}

	for _, name := range auth {
		tmpl, err := template.New("auth").Funcs(templateFuncs).ParseFS(assets,
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
	// Pages revalidate before reuse, but they may stay in the browser's
	// back/forward cache: nobody should wait for the index again because they
	// pressed Back.
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)

	_, err := w.Write(buffer.Bytes())

	return err
}

// Shell is the signed-in page chrome: the sidebar and the page title.
type Shell struct {
	Title    string
	Active   string
	Version  string
	Assets   string
	Crawling bool
	Uptime   string
}

// SetupData is the first-run page.
type SetupData struct {
	Title      string
	Version    string
	Assets     string
	Error      string
	Username   string
	APIKey     string
	TorznabURL string
}

// LoginData is the sign-in page.
type LoginData struct {
	Title    string
	Version  string
	Assets   string
	Error    string
	Username string
}

// SparkBar is one rectangle of the activity chart, laid out in a 100 by 100
// view box.
type SparkBar struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// OverviewStats are the numbers on the dashboard tiles.
type OverviewStats struct {
	Stored      string
	Rate        string
	Database    string
	BytesPerRow string
	Nodes       string
	Crawled     string
}

// CounterItem is one named counter in a pipeline group or the runtime list.
// Key is the JSON field the live feed uses to refresh it.
type CounterItem struct {
	Key   string
	Label string
	Value string
}

// CounterGroup is a named group of pipeline counters.
type CounterGroup struct {
	Name  string
	Items []CounterItem
}

// ShardRow is one month of the storage table.
type ShardRow struct {
	Month    string
	Rows     string
	Database string
	WAL      string
}

// CategoryRow is one line of the classification table: a category family with
// its total, and the family's subcategories beneath it.
type CategoryRow struct {
	Name     string
	Rows     string
	Share    string
	Children []CategoryRow
}

// TorrentRow is one torrent in the live feed or in search results. The fields
// after Leechers are only used by the search page, which expands a result in
// place rather than sending the reader to the torrent page for them.
type TorrentRow struct {
	Hash     string
	Name     string
	Category string
	Size     string
	Files    string
	Age      string
	Seeders  int
	Leechers int

	Magnet      string
	Added       string
	Peers       string
	PrimaryFile string
	Seasons     string
	Episodes    string
	AirDate     string
	TMDBURL     string
}

// OverviewData is the dashboard.
type OverviewData struct {
	Shell
	Stats           OverviewStats
	Notice          string
	Warming         bool
	CrawlError      string
	Recent          []TorrentRow
	Pipeline        []CounterGroup
	Runtime         []CounterItem
	Shards          []ShardRow
	Categories      []CategoryRow
	History         []int
	HistoryBars     []SparkBar
	HistorySummary  string
	Commit          string
	BuildDate       string
	Uptime          string
	DHTAddress      string
	DataDirectory   string
	RetentionMonths int
	CrawlEnabled    bool
}

// CategoryOption is one entry of the search page's category filter.
type CategoryOption struct {
	Value int
	Label string
}

// CategoryGroup is one family of the category filter: the family itself, which
// matches every leaf beneath it, and the leaves that pick one subcategory.
type CategoryGroup struct {
	Value   int
	Label   string
	Options []CategoryOption
}

// SearchData is the search page.
type SearchData struct {
	Shell
	Query            string
	CategoryGroups   []CategoryGroup
	SelectedCategory int
	SelectedSort     string
	Results          []TorrentRow
	Total            int
	TotalLabel       string
	Page             int
	PageCount        int
	HasPrev          bool
	HasNext          bool
	PrevURL          string
	NextURL          string
	Error            string
}

// TorrentData is one torrent's detail page.
type TorrentData struct {
	Shell
	Name        string
	Hash        string
	Magnet      string
	Category    string
	Size        string
	Added       string
	Files       string
	PrimaryFile string
	Seasons     string
	Episodes    string
	AirDate     string
	Peers       string
	TMDBURL     string
	TVDBURL     string
}

// NotFoundData is the torrent-not-found page.
type NotFoundData struct {
	Shell
	Hash string
}

// SettingsData is the settings page.
type SettingsData struct {
	Shell
	Advanced      bool
	Notice        string
	Error         string
	Groups        []SettingsGroup
	APIKey        string
	TorznabURL    string
	AdminUsername string
	Reclassify    ReclassifyStatus
}

// ReclassifyStatus is the state of the maintenance pass shown on the settings
// page.
type ReclassifyStatus struct {
	Running      bool
	Apply        bool
	Cleanup      bool
	Scanned      int
	Total        int
	Percent      int
	Matched      int
	Changed      int
	Written      int
	Deleted      int
	Transitions  []string
	Samples      []ReclassifyRow
	TokenUpdates []ReclassifyRow
	Rejected     []ReclassifyRow
	Finished     string
	Failure      string
}

// ReclassifyRow is one torrent listed by the maintenance pass, linked back to
// its detail page.
type ReclassifyRow struct {
	Hash   string
	Name   string
	From   string
	To     string
	Reason string
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
	Choices     []SettingChoice
}

// SettingChoice is one checkbox of a list setting.
type SettingChoice struct {
	Value   string
	Label   string
	Checked bool
}
