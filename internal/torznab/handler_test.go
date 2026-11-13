package torznab

import (
	"bytes"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BitTrawler/bittrawler/internal/store"
)

func onlyQuery(t *testing.T, searcher *fakeSearcher) store.Query {
	t.Helper()

	if len(searcher.queries) != 1 {
		t.Fatalf("searcher ran %d queries, want 1", len(searcher.queries))
	}

	return searcher.queries[0]
}

func TestSearchQueryMapping(t *testing.T) {
	searcher := &fakeSearcher{}
	h := newTestHandler(t, Config{Searcher: searcher})

	rec := get(t, h, "t=tvsearch&q=The+Show&season=2&ep=5&cat=5030&limit=10&offset=5")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	q := onlyQuery(t, searcher)

	if q.Text != "The Show" {
		t.Errorf("Text = %q, want %q", q.Text, "The Show")
	}

	if q.Season != 2 || q.Episode != 5 {
		t.Errorf("Season, Episode = %d, %d, want 2, 5", q.Season, q.Episode)
	}

	if len(q.Categories) != 1 || q.Categories[0] != 5030 {
		t.Errorf("Categories = %v, want [5030]", q.Categories)
	}

	if q.Limit != 10 || q.Offset != 5 {
		t.Errorf("Limit, Offset = %d, %d, want 10, 5", q.Limit, q.Offset)
	}

	if q.Sort != store.SortRelevance {
		t.Errorf("Sort = %v, want relevance", q.Sort)
	}

	if q.Count {
		t.Errorf("the endpoint should not count the total")
	}
}

func TestSearchFunctionDefaultsToItsCategory(t *testing.T) {
	tests := []struct {
		query string
		want  []int
	}{
		{query: "t=movie&q=Film", want: []int{2000}},
		{query: "t=tvsearch&q=Show", want: []int{5000}},
		{query: "t=music&q=Album", want: []int{3000}},
		{query: "t=book&q=Novel", want: []int{7000, 3030}},
		{query: "t=search&q=Anything", want: nil},
	}

	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			searcher := &fakeSearcher{}
			get(t, newTestHandler(t, Config{Searcher: searcher}), test.query)

			q := onlyQuery(t, searcher)

			if len(q.Categories) != len(test.want) {
				t.Fatalf("Categories = %v, want %v", q.Categories, test.want)
			}

			for i := range q.Categories {
				if q.Categories[i] != test.want[i] {
					t.Fatalf("Categories = %v, want %v", q.Categories, test.want)
				}
			}
		})
	}
}

func TestSearchUsesNewestOrderWithoutQuery(t *testing.T) {
	searcher := &fakeSearcher{}
	get(t, newTestHandler(t, Config{Searcher: searcher}), "t=search&cat=2000")

	if q := onlyQuery(t, searcher); q.Sort != store.SortNewest {
		t.Fatalf("Sort = %v, want newest", q.Sort)
	}
}

func TestSearchDailyEpisode(t *testing.T) {
	searcher := &fakeSearcher{}
	get(t, newTestHandler(t, Config{Searcher: searcher}), "t=tvsearch&season=2024&ep=03%2F25")

	q := onlyQuery(t, searcher)

	if q.DailyDate != "2024-03-25" {
		t.Fatalf("DailyDate = %q, want %q", q.DailyDate, "2024-03-25")
	}

	if q.Season != store.AnySeason || q.Episode != store.AnyEpisode {
		t.Fatalf("Season, Episode = %d, %d, want both unfiltered", q.Season, q.Episode)
	}
}

func TestSearchTMDBIDFilter(t *testing.T) {
	searcher := &fakeSearcher{}
	get(t, newTestHandler(t, Config{Searcher: searcher}), "t=movie&q=Film&tmdbid=603")

	if q := onlyQuery(t, searcher); q.ContentID != 603 {
		t.Fatalf("ContentID = %d, want 603", q.ContentID)
	}
}

func TestSearchPageDefaults(t *testing.T) {
	searcher := &fakeSearcher{}
	get(t, newTestHandler(t, Config{DefaultLimit: 40, MaxLimit: 80, Searcher: searcher}), "t=search")

	if q := onlyQuery(t, searcher); q.Limit != 40 {
		t.Fatalf("Limit = %d, want 40", q.Limit)
	}
}

func TestSearchRendersEmptyFeed(t *testing.T) {
	h := newTestHandler(t, Config{Searcher: &fakeSearcher{}})

	rec := get(t, h, "t=search&q=nothing&offset=30")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	feed := decodeFeed(t, rec.Body.Bytes())
	if len(feed.Channel.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(feed.Channel.Items))
	}

	if feed.Channel.Response.Offset != 30 {
		t.Fatalf("response offset = %d, want 30", feed.Channel.Response.Offset)
	}
}

func TestAPIKey(t *testing.T) {
	h := newTestHandler(t, Config{APIKey: "secret"})

	t.Run("missing key", func(t *testing.T) {
		rec := get(t, h, "t=caps")

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		e := decodeError(t, rec.Body.Bytes())
		if e.Code != CodeInvalidKey {
			t.Fatalf("error code = %d, want %d", e.Code, CodeInvalidKey)
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		e := decodeError(t, get(t, h, "t=caps&apikey=wrong").Body.Bytes())
		if e.Code != CodeInvalidKey {
			t.Fatalf("error code = %d, want %d", e.Code, CodeInvalidKey)
		}
	})

	t.Run("query parameter", func(t *testing.T) {
		caps := decodeCaps(t, get(t, h, "t=caps&apikey=secret").Body.Bytes())
		if caps.Server.Title != "BitTrawler" {
			t.Fatalf("server title = %q, want BitTrawler", caps.Server.Title)
		}
	})

	t.Run("header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/torznab?t=caps", nil)
		r.Header.Set("X-Api-Key", "secret")

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)

		caps := decodeCaps(t, rec.Body.Bytes())
		if caps.Server.Title != "BitTrawler" {
			t.Fatalf("server title = %q, want BitTrawler", caps.Server.Title)
		}
	})
}

func TestProtocolErrors(t *testing.T) {
	h := newTestHandler(t, Config{})

	tests := []struct {
		name  string
		query string
		code  int
	}{
		{name: "missing function", query: "q=film", code: CodeMissingParameter},
		{name: "unknown function", query: "t=get", code: CodeNoSuchFunction},
		{name: "bad category", query: "t=search&cat=film", code: CodeIncorrectParameter},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := get(t, h, test.query)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 for a protocol error", rec.Code)
			}

			if got, want := rec.Header().Get("Content-Type"), "application/xml; charset=utf-8"; got != want {
				t.Fatalf("Content-Type = %q, want %q", got, want)
			}

			e := decodeError(t, rec.Body.Bytes())
			if e.Code != test.code {
				t.Fatalf("error code = %d, want %d (%s)", e.Code, test.code, e.Description)
			}

			if e.Description == "" {
				t.Fatal("error description is empty")
			}
		})
	}
}

func TestSearchFailureIsHTTPError(t *testing.T) {
	h := newTestHandler(t, Config{Searcher: &fakeSearcher{err: errors.New("store is down")}})

	rec := get(t, h, "t=search&q=film")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newTestHandler(t, Config{})

	r := httptest.NewRequest(http.MethodPost, "/torznab?t=caps", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}

	if got, want := rec.Header().Get("Allow"), "GET, HEAD"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
}

func TestResponseIsXML(t *testing.T) {
	rec := get(t, newTestHandler(t, Config{}), "t=caps")

	if got, want := rec.Header().Get("Content-Type"), "application/xml; charset=utf-8"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}

	body := rec.Body.Bytes()

	if !bytes.HasPrefix(body, []byte(xml.Header)) {
		t.Fatalf("body does not start with the XML declaration: %q", body)
	}

	var document struct {
		XMLName xml.Name `xml:"caps"`
	}

	if err := xml.Unmarshal(body, &document); err != nil {
		t.Fatalf("body is not well formed XML: %v", err)
	}
}

func TestNewHandlerRequiresSearcher(t *testing.T) {
	if _, err := NewHandler(Config{}); err == nil {
		t.Fatal("NewHandler() has no error without a Searcher")
	}
}

func TestNewHandlerDefaults(t *testing.T) {
	tests := []struct {
		name             string
		config           Config
		wantDefaultLimit int
		wantMaxLimit     int
	}{
		{
			name:             "empty config",
			config:           Config{Searcher: &fakeSearcher{}},
			wantDefaultLimit: 100,
			wantMaxLimit:     100,
		},
		{
			name:             "only a default limit",
			config:           Config{DefaultLimit: 25, Searcher: &fakeSearcher{}},
			wantDefaultLimit: 25,
			wantMaxLimit:     25,
		},
		{
			name:             "default above the maximum",
			config:           Config{DefaultLimit: 500, MaxLimit: 50, Searcher: &fakeSearcher{}},
			wantDefaultLimit: 50,
			wantMaxLimit:     50,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newTestHandler(t, test.config)
			caps := decodeCaps(t, get(t, h, "t=caps").Body.Bytes())

			if caps.Limits.Default != test.wantDefaultLimit || caps.Limits.Max != test.wantMaxLimit {
				t.Fatalf("limits = default %d max %d, want default %d max %d",
					caps.Limits.Default, caps.Limits.Max, test.wantDefaultLimit, test.wantMaxLimit)
			}
		})
	}
}

func TestDefaultTitle(t *testing.T) {
	caps := decodeCaps(t, get(t, newTestHandler(t, Config{}), "t=caps").Body.Bytes())

	if caps.Server.Title != "BitTrawler" {
		t.Fatalf("server title = %q, want BitTrawler", caps.Server.Title)
	}
}

func TestWriteErrorForAnUnexpectedFailure(t *testing.T) {
	h, err := NewHandler(Config{Searcher: &fakeSearcher{}})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	rec := httptest.NewRecorder()
	h.(*handler).writeError(rec, errors.New("encoding failure"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
