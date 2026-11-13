package torznab

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func parseGet(t *testing.T, rawQuery string) (SearchRequest, error) {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/torznab?"+rawQuery, nil)

	return ParseRequest(r, 100, 100)
}

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  SearchRequest
	}{
		{
			name:  "defaults",
			query: "t=search&q=hello+world",
			want: SearchRequest{
				Function: FunctionSearch,
				Query:    "hello world",
				Season:   unset,
				Episode:  unset,
				Limit:    100,
			},
		},
		{
			name:  "caps",
			query: "t=caps&apikey=secret",
			want: SearchRequest{
				Function: FunctionCaps,
				Season:   unset,
				Episode:  unset,
				Limit:    100,
				APIKey:   "secret",
			},
		},
		{
			name:  "tv episode",
			query: "t=tvsearch&q=The+Show&season=2&ep=5",
			want: SearchRequest{
				Function: FunctionTV,
				Query:    "The Show",
				Season:   2,
				Episode:  5,
				Limit:    100,
			},
		},
		{
			name:  "daily episode",
			query: "t=tvsearch&q=The+Daily+Show&season=2024&ep=03%2F25",
			want: SearchRequest{
				Function: FunctionTV,
				Query:    "The Daily Show",
				AirDate:  "2024-03-25",
				Season:   unset,
				Episode:  unset,
				Limit:    100,
			},
		},
		{
			name:  "daily episode with a dash",
			query: "t=tvsearch&season=2024&ep=03-25",
			want: SearchRequest{
				Function: FunctionTV,
				AirDate:  "2024-03-25",
				Season:   unset,
				Episode:  unset,
				Limit:    100,
			},
		},
		{
			name:  "movie",
			query: "t=movie&q=Film&tmdbid=603",
			want: SearchRequest{
				Function: FunctionMovie,
				Query:    "Film",
				Season:   unset,
				Episode:  unset,
				TMDBID:   603,
				Limit:    100,
			},
		},
		{
			name:  "categories in one parameter and in several",
			query: "t=search&cat=2000,5030&cat=3030",
			want: SearchRequest{
				Function:   FunctionSearch,
				Categories: []int{2000, 5030, 3030},
				Season:     unset,
				Episode:    unset,
				Limit:      100,
			},
		},
		{
			name:  "category zero means no filter",
			query: "t=search&cat=0",
			want: SearchRequest{
				Function: FunctionSearch,
				Season:   unset,
				Episode:  unset,
				Limit:    100,
			},
		},
		{
			name:  "page is clamped",
			query: "t=search&limit=500&offset=20",
			want: SearchRequest{
				Function: FunctionSearch,
				Season:   unset,
				Episode:  unset,
				Limit:    100,
				Offset:   20,
			},
		},
		{
			name:  "empty page values are ignored",
			query: "t=search&limit=&offset=&cat=",
			want: SearchRequest{
				Function: FunctionSearch,
				Season:   unset,
				Episode:  unset,
				Limit:    100,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseGet(t, test.query)
			if err != nil {
				t.Fatalf("ParseRequest() error = %v", err)
			}

			if got.Function != test.want.Function {
				t.Errorf("Function = %q, want %q", got.Function, test.want.Function)
			}

			if got.Query != test.want.Query {
				t.Errorf("Query = %q, want %q", got.Query, test.want.Query)
			}

			if got.Season != test.want.Season || got.Episode != test.want.Episode {
				t.Errorf("Season, Episode = %d, %d, want %d, %d",
					got.Season, got.Episode, test.want.Season, test.want.Episode)
			}

			if got.AirDate != test.want.AirDate {
				t.Errorf("AirDate = %q, want %q", got.AirDate, test.want.AirDate)
			}

			if got.TMDBID != test.want.TMDBID {
				t.Errorf("TMDBID = %d, want %d", got.TMDBID, test.want.TMDBID)
			}

			if got.Limit != test.want.Limit || got.Offset != test.want.Offset {
				t.Errorf("Limit, Offset = %d, %d, want %d, %d",
					got.Limit, got.Offset, test.want.Limit, test.want.Offset)
			}

			if got.APIKey != test.want.APIKey {
				t.Errorf("APIKey = %q, want %q", got.APIKey, test.want.APIKey)
			}

			if len(got.Categories) != len(test.want.Categories) {
				t.Fatalf("Categories = %v, want %v", got.Categories, test.want.Categories)
			}

			for i := range got.Categories {
				if got.Categories[i] != test.want.Categories[i] {
					t.Fatalf("Categories = %v, want %v", got.Categories, test.want.Categories)
				}
			}
		})
	}
}

func TestParseRequestAPIKeyHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/torznab?t=caps", nil)
	r.Header.Set("X-Api-Key", "from-header")

	got, err := ParseRequest(r, 100, 100)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if got.APIKey != "from-header" {
		t.Fatalf("APIKey = %q, want %q", got.APIKey, "from-header")
	}
}

func TestParseRequestErrors(t *testing.T) {
	tests := []struct {
		name  string
		query string
		code  int
	}{
		{name: "missing function", query: "q=film", code: CodeMissingParameter},
		{name: "unknown category", query: "t=search&cat=film", code: CodeIncorrectParameter},
		{name: "negative category", query: "t=search&cat=-1", code: CodeIncorrectParameter},
		{name: "bad season", query: "t=tvsearch&season=one", code: CodeIncorrectParameter},
		{name: "negative season", query: "t=tvsearch&season=-1", code: CodeIncorrectParameter},
		{name: "episode without season", query: "t=tvsearch&ep=2", code: CodeIncorrectParameter},
		{name: "bad episode", query: "t=tvsearch&season=1&ep=two", code: CodeIncorrectParameter},
		{name: "bad daily episode", query: "t=tvsearch&season=2024&ep=13/40", code: CodeIncorrectParameter},
		{name: "unparseable daily month", query: "t=tvsearch&season=2024&ep=mm/12", code: CodeIncorrectParameter},
		{name: "unparseable daily day", query: "t=tvsearch&season=2024&ep=03/dd", code: CodeIncorrectParameter},
		{name: "zero limit", query: "t=search&limit=0", code: CodeIncorrectParameter},
		{name: "bad limit", query: "t=search&limit=many", code: CodeIncorrectParameter},
		{name: "negative offset", query: "t=search&offset=-1", code: CodeIncorrectParameter},
		{name: "bad tmdbid", query: "t=movie&tmdbid=six", code: CodeIncorrectParameter},
		{name: "negative tmdbid", query: "t=movie&tmdbid=-6", code: CodeIncorrectParameter},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseGet(t, test.query)

			var protocolError Error
			if !errors.As(err, &protocolError) {
				t.Fatalf("ParseRequest() error = %v, want a torznab Error", err)
			}

			if protocolError.Code != test.code {
				t.Fatalf("error code = %d, want %d (%s)", protocolError.Code, test.code, protocolError.Description)
			}
		})
	}
}

func TestParseRequestAcceptsUnknownFunction(t *testing.T) {
	// The handler rejects an unknown function with code 202; parsing only reads
	// the parameters and leaves the dispatch to the handler.
	got, err := parseGet(t, "t=get")
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if got.Function != "get" {
		t.Fatalf("Function = %q, want %q", got.Function, "get")
	}
}

func TestParseRequestTrimsQuery(t *testing.T) {
	got, err := parseGet(t, "t=search&q=++spaced+out++")
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if got.Query != "spaced out" {
		t.Fatalf("Query = %q, want %q", got.Query, "spaced out")
	}
}
