package torznab

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/BitTrawler/bittrawler/internal/store"
)

// capsAdvertisedParams maps each search function, keyed by its t value, to the
// parameters its searching element advertises, read from the real caps XML.
func capsAdvertisedParams(t *testing.T) map[Function][]string {
	t.Helper()

	c := decodeCaps(t, get(t, newTestHandler(t, Config{}), "t=caps").Body.Bytes())

	return map[Function][]string{
		FunctionSearch: splitSupportedParams(c.Searching.Search.SupportedParams),
		FunctionTV:     splitSupportedParams(c.Searching.TVSearch.SupportedParams),
		FunctionMovie:  splitSupportedParams(c.Searching.MovieSearch.SupportedParams),
		FunctionMusic:  splitSupportedParams(c.Searching.MusicSearch.SupportedParams),
		FunctionBook:   splitSupportedParams(c.Searching.BookSearch.SupportedParams),
	}
}

// splitSupportedParams turns the comma separated attribute into its names,
// ignoring the spaces a hand-written caps document might carry.
func splitSupportedParams(raw string) []string {
	var out []string

	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}

	return out
}

// TestCapsAdvertiseTheParametersTheHandlerAccepts keeps the advertised search
// parameters in step with the implemented ones. The expected lists come from
// searchParams, the same source capsFor renders, and the test parses the real
// caps XML a client receives rather than the Go values behind it.
func TestCapsAdvertiseTheParametersTheHandlerAccepts(t *testing.T) {
	advertised := capsAdvertisedParams(t)

	for function, want := range searchParams {
		got := advertised[function]

		if !sameParams(got, want) {
			t.Errorf("%s advertises supportedParams %v, want %v", function, got, want)
		}
	}

	// The finding that prompted this test: movie-search implemented tmdbid
	// but advertised only q, so a caps-compliant client never sent it and the
	// filter was dead in practice.
	if !slices.Contains(advertised[FunctionMovie], ParamTMDBID) {
		t.Errorf("movie-search does not advertise %q: %v", ParamTMDBID, advertised[FunctionMovie])
	}

	// Every parameter the parser reads must be discoverable through at least
	// one function, or a client cannot know the filter exists.
	for _, param := range []string{
		ParamQuery, ParamCategory, ParamSeason, ParamEpisode, ParamTMDBID, ParamLimit, ParamOffset,
	} {
		found := false

		for _, params := range advertised {
			if slices.Contains(params, param) {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("parameter %q is parsed but never advertised", param)
		}
	}
}

// TestAdvertisedParamsAffectTheSearch checks the advert the other way round:
// every parameter a function advertises must change the store query it runs.
// An advertised parameter that changes nothing is dead code a client would
// spend a filter on.
func TestAdvertisedParamsAffectTheSearch(t *testing.T) {
	checks := map[string]struct {
		query string
		want  func(t *testing.T, q store.Query)
	}{
		ParamQuery: {"q=needle", func(t *testing.T, q store.Query) {
			if q.Text != "needle" {
				t.Errorf("Text = %q, want %q", q.Text, "needle")
			}
		}},
		ParamCategory: {"cat=5030", func(t *testing.T, q store.Query) {
			if len(q.Categories) != 1 || q.Categories[0] != 5030 {
				t.Errorf("Categories = %v, want [5030]", q.Categories)
			}
		}},
		ParamSeason: {"season=2", func(t *testing.T, q store.Query) {
			if q.Season != 2 {
				t.Errorf("Season = %d, want 2", q.Season)
			}
		}},
		ParamEpisode: {"season=2&ep=5", func(t *testing.T, q store.Query) {
			if q.Episode != 5 {
				t.Errorf("Episode = %d, want 5", q.Episode)
			}
		}},
		ParamTMDBID: {"tmdbid=603", func(t *testing.T, q store.Query) {
			if q.ContentID != 603 {
				t.Errorf("ContentID = %d, want 603", q.ContentID)
			}
		}},
		ParamLimit: {"limit=7", func(t *testing.T, q store.Query) {
			if q.Limit != 7 {
				t.Errorf("Limit = %d, want 7", q.Limit)
			}
		}},
		ParamOffset: {"offset=3", func(t *testing.T, q store.Query) {
			if q.Offset != 3 {
				t.Errorf("Offset = %d, want 3", q.Offset)
			}
		}},
	}

	for function, params := range capsAdvertisedParams(t) {
		for _, param := range params {
			t.Run(string(function)+"/"+param, func(t *testing.T) {
				check, ok := checks[param]
				if !ok {
					t.Fatalf("no test knows what %q filters; add one or stop advertising it", param)
				}

				searcher := &fakeSearcher{}
				h := newTestHandler(t, Config{Searcher: searcher})
				rec := get(t, h, "t="+string(function)+"&"+check.query)

				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
				}

				check.want(t, onlyQuery(t, searcher))
			})
		}
	}
}

// sameParams compares parameter lists without depending on their advertised
// order.
func sameParams(got, want []string) bool {
	g := slices.Clone(got)
	w := slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)

	return slices.Equal(g, w)
}
