package torznab

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BitTrawler/bittrawler/internal/store"
)

// fakeSearcher records the queries the handler runs and returns a fixed page.
type fakeSearcher struct {
	queries []store.Query
	result  store.Result
	err     error
}

func (f *fakeSearcher) Search(_ context.Context, q store.Query) (store.Result, error) {
	f.queries = append(f.queries, q)

	return f.result, f.err
}

func newTestHandler(t *testing.T, cfg Config) http.Handler {
	t.Helper()

	if cfg.Searcher == nil {
		cfg.Searcher = &fakeSearcher{}
	}

	h, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	return h
}

func get(t *testing.T, h http.Handler, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/torznab?"+rawQuery, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	return rec
}

// parsedFeed is the wire format as a consumer reads it. The elements are
// matched by local name, which is how a namespace-aware parser sees them.
type parsedFeed struct {
	XMLName xml.Name      `xml:"rss"`
	Version string        `xml:"version,attr"`
	Channel parsedChannel `xml:"channel"`
}

type parsedChannel struct {
	Title    string         `xml:"title"`
	Response parsedResponse `xml:"response"`
	Items    []parsedItem   `xml:"item"`
}

type parsedResponse struct {
	Offset int `xml:"offset,attr"`
}

type parsedItem struct {
	Title     string          `xml:"title"`
	GUID      string          `xml:"guid"`
	PubDate   string          `xml:"pubDate"`
	Category  string          `xml:"category"`
	Size      int64           `xml:"size"`
	Enclosure parsedEnclosure `xml:"enclosure"`
	Attrs     []parsedAttr    `xml:"attr"`
}

type parsedEnclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type parsedAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

func decodeFeed(t *testing.T, body []byte) parsedFeed {
	t.Helper()

	var f parsedFeed
	if err := xml.Unmarshal(body, &f); err != nil {
		t.Fatalf("decode feed: %v\n%s", err, body)
	}

	return f
}

// attributes maps an item's torznab attributes by name.
func attributes(it parsedItem) map[string]string {
	out := make(map[string]string, len(it.Attrs))
	for _, attr := range it.Attrs {
		out[attr.Name] = attr.Value
	}

	return out
}

// parsedError is the Newznab error document.
type parsedError struct {
	XMLName     xml.Name `xml:"error"`
	Code        int      `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}

func decodeError(t *testing.T, body []byte) parsedError {
	t.Helper()

	var e parsedError
	if err := xml.Unmarshal(body, &e); err != nil {
		t.Fatalf("decode error: %v\n%s", err, body)
	}

	return e
}
