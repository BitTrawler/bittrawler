package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer is a TMDB API stub. It answers the two search endpoints with the
// JSON it is given and counts requests.
type fakeServer struct {
	movies  string
	shows   string
	status  int
	header  http.Header
	server  *httptest.Server
	request atomic.Int64
}

func newFake(t *testing.T, movies, shows string) *fakeServer {
	t.Helper()

	fake := &fakeServer{movies: movies, shows: shows}

	fake.server = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fake.request.Add(1)

		if token := r.Header.Get("Authorization"); token != "Bearer test-token" {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)

			return
		}

		if fake.status != 0 {
			rw.WriteHeader(fake.status)

			return
		}

		rw.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/search/movie":
			_, _ = rw.Write([]byte(fake.movies))
		case "/search/tv":
			_, _ = rw.Write([]byte(fake.shows))
		default:
			http.NotFound(rw, r)
		}
	}))

	t.Cleanup(fake.server.Close)

	return fake
}

func (f *fakeServer) client(t *testing.T) *Client {
	t.Helper()

	client, err := New(Config{
		Token:     "test-token",
		BaseURL:   f.server.URL,
		RateLimit: 1000,
		Burst:     10,
		CacheSize: 100,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	return client
}

const (
	filmFixture = `{"results":[{"id":13523,"title":"Sex Drive","release_date":"2008-10-16","adult":false,"popularity":20.5}]}`
	adultFilm   = `{"results":[{"id":99,"title":"Anal Teens 5","release_date":"2020-01-01","adult":true,"popularity":5}]}`
	noResults   = `{"results":[]}`
)

func TestLookupFilm(t *testing.T) {
	t.Parallel()

	fake := newFake(t, filmFixture, noResults)

	match, found, err := fake.client(t).Lookup(context.Background(), "Sex.Drive", 2008)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}

	if !found {
		t.Fatal("no match found")
	}

	if match.ID != 13523 || match.Medium != MediumMovie || match.Adult {
		t.Fatalf("match = %+v, want id 13523, movie, not adult", match)
	}
}

func TestLookupAdultFilm(t *testing.T) {
	t.Parallel()

	fake := newFake(t, adultFilm, noResults)

	match, found, err := fake.client(t).Lookup(context.Background(), "Anal Teens 5", 2020)
	if err != nil || !found {
		t.Fatalf("lookup = %v, %v", found, err)
	}

	if !match.Adult {
		t.Fatalf("match = %+v, want adult", match)
	}
}

func TestLookupShow(t *testing.T) {
	t.Parallel()

	fake := newFake(t, noResults, `{"results":[{"id":79744,"name":"Legacies","first_air_date":"2018-10-25","popularity":60}]}`)

	match, found, err := fake.client(t).Lookup(context.Background(), "Legacies", 0)
	if err != nil || !found {
		t.Fatalf("lookup = %v, %v", found, err)
	}

	if match.Medium != MediumTV || match.ID != 79744 {
		t.Fatalf("match = %+v, want show 79744", match)
	}
}

func TestLookupAmbiguousMediumIsNotAMatch(t *testing.T) {
	t.Parallel()

	fake := newFake(t,
		`{"results":[{"id":1,"title":"Same Name","release_date":"2010-01-01","adult":false,"popularity":10}]}`,
		`{"results":[{"id":2,"name":"Same Name","first_air_date":"2011-01-01","popularity":12}]}`,
	)

	if _, found, err := fake.client(t).Lookup(context.Background(), "Same Name", 0); err != nil || found {
		t.Fatalf("found = %v, err = %v; want no confident match", found, err)
	}
}

func TestLookupPopularMediumWins(t *testing.T) {
	t.Parallel()

	fake := newFake(t,
		`{"results":[{"id":1,"title":"Same Name","release_date":"2010-01-01","adult":false,"popularity":10}]}`,
		`{"results":[{"id":2,"name":"Same Name","first_air_date":"2010-01-01","popularity":100}]}`,
	)

	match, found, err := fake.client(t).Lookup(context.Background(), "Same Name", 2010)
	if err != nil || !found {
		t.Fatalf("lookup = %v, %v", found, err)
	}

	if match.Medium != MediumTV {
		t.Fatalf("medium = %v, want tv", match.Medium)
	}
}

func TestLookupAdultFilmBeatsShow(t *testing.T) {
	t.Parallel()

	fake := newFake(t,
		`{"results":[{"id":1,"title":"Same Name","release_date":"2010-01-01","adult":true,"popularity":1}]}`,
		`{"results":[{"id":2,"name":"Same Name","first_air_date":"2010-01-01","popularity":100}]}`,
	)

	match, found, err := fake.client(t).Lookup(context.Background(), "Same Name", 2010)
	if err != nil || !found {
		t.Fatalf("lookup = %v, %v", found, err)
	}

	if match.Medium != MediumMovie || !match.Adult {
		t.Fatalf("match = %+v, want adult film", match)
	}
}

func TestLookupYearWindow(t *testing.T) {
	t.Parallel()

	fake := newFake(t, `{"results":[{"id":7,"title":"Some Film","release_date":"2007-05-01","adult":false,"popularity":3}]}`, noResults)

	if _, found, _ := fake.client(t).Lookup(context.Background(), "Some Film", 2008); !found {
		t.Fatal("a one-year difference was rejected")
	}

	other := newFake(t, `{"results":[{"id":7,"title":"Some Film","release_date":"2001-05-01","adult":false,"popularity":3}]}`, noResults)

	if _, found, _ := other.client(t).Lookup(context.Background(), "Some Film", 2008); found {
		t.Fatal("a seven-year difference was accepted")
	}
}

func TestLookupRejectsDifferentTitle(t *testing.T) {
	t.Parallel()

	fake := newFake(t, `{"results":[{"id":7,"title":"Another Film","release_date":"2008-05-01","adult":false,"popularity":9}]}`, noResults)

	if _, found, _ := fake.client(t).Lookup(context.Background(), "Some Film", 2008); found {
		t.Fatal("a different title was accepted")
	}
}

func TestLookupCachesResults(t *testing.T) {
	t.Parallel()

	fake := newFake(t, filmFixture, noResults)
	client := fake.client(t)

	if _, found, err := client.Lookup(context.Background(), "Sex Drive", 2008); !found || err != nil {
		t.Fatalf("first lookup = %v, %v", found, err)
	}

	requests := fake.request.Load()

	if _, found, err := client.Lookup(context.Background(), "Sex Drive", 2008); !found || err != nil {
		t.Fatalf("second lookup = %v, %v", found, err)
	}

	if got := fake.request.Load(); got != requests {
		t.Fatalf("requests = %d after cached lookup, want %d", got, requests)
	}

	if hits := client.Stats().CacheHits; hits != 2 {
		t.Fatalf("cache hits = %d, want 2", hits)
	}
}

func TestLookupCachesMisses(t *testing.T) {
	t.Parallel()

	fake := newFake(t, noResults, noResults)
	client := fake.client(t)

	if _, found, _ := client.Lookup(context.Background(), "Nothing Here", 0); found {
		t.Fatal("unexpected match")
	}

	requests := fake.request.Load()

	if _, found, _ := client.Lookup(context.Background(), "Nothing Here", 0); found {
		t.Fatal("unexpected match")
	}

	if got := fake.request.Load(); got != requests {
		t.Fatalf("requests = %d after cached miss, want %d", got, requests)
	}
}

func TestLookupBreaker(t *testing.T) {
	t.Parallel()

	fake := newFake(t, noResults, noResults)
	fake.status = http.StatusInternalServerError

	client, err := New(Config{
		Token:           "test-token",
		BaseURL:         fake.server.URL,
		RateLimit:       1000,
		Burst:           10,
		FailureLimit:    1,
		FailureCooldown: time.Minute,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if _, _, err := client.Lookup(context.Background(), "Some Film", 0); err == nil {
		t.Fatal("first lookup did not fail")
	}

	requests := fake.request.Load()

	if _, _, err := client.Lookup(context.Background(), "Some Film", 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second lookup error = %v, want ErrUnavailable", err)
	}

	if got := fake.request.Load(); got != requests {
		t.Fatalf("requests = %d with the breaker open, want %d", got, requests)
	}
}

func TestLookupRetriesTooManyRequests(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			rw.Header().Set("Retry-After", "0")
			rw.WriteHeader(http.StatusTooManyRequests)

			return
		}

		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte(filmFixture))
	}))
	t.Cleanup(server.Close)

	client, err := New(Config{
		Token:     "test-token",
		BaseURL:   server.URL,
		RateLimit: 1000,
		Burst:     10,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	if _, found, err := client.Lookup(context.Background(), "Sex Drive", 2008); !found || err != nil {
		t.Fatalf("lookup after retry = %v, %v", found, err)
	}
}

func TestNewRequiresToken(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{}); err == nil {
		t.Fatal("a client was created without a token")
	}
}

func TestMatchJSONShape(t *testing.T) {
	t.Parallel()

	// Guard the fixture assumptions: the fields matched by the client must be
	// present in the search responses.
	var response struct {
		Results []movieResult `json:"results"`
	}

	if err := json.Unmarshal([]byte(filmFixture), &response); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	if len(response.Results) != 1 || response.Results[0].Title != "Sex Drive" {
		t.Fatalf("fixture parsed as %+v", response.Results)
	}
}
