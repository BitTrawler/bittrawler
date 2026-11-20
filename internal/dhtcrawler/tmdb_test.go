package dhtcrawler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/drops"
	"github.com/BitTrawler/bittrawler/internal/store"
	"github.com/BitTrawler/bittrawler/internal/tmdb"
)

// fakeTMDBSearcher answers lookups with a fixed result.
type fakeTMDBSearcher struct {
	match tmdb.Match
	found bool
	err   error
	calls int
	title string
	year  int
}

func (f *fakeTMDBSearcher) Lookup(_ context.Context, title string, year int) (tmdb.Match, bool, error) {
	f.calls++
	f.title = title
	f.year = year

	return f.match, f.found, f.err
}

// confirmRow runs one job through the confirmation stage and returns the row it
// forwarded. A dropped job returns ok=false.
func confirmRow(
	t *testing.T,
	tc *testCrawler,
	job tmdbJob,
) (store.Row, bool) {
	t.Helper()

	tc.crawler.confirm(context.Background(), job)

	select {
	case row := <-tc.crawler.forPersist:
		return row, true
	default:
		return store.Row{}, false
	}
}

func TestTMDBFilmBecomesShow(t *testing.T) {
	t.Parallel()

	searcher := &fakeTMDBSearcher{
		match: tmdb.Match{ID: 79744, Medium: tmdb.MediumTV, Title: "Legacies"},
		found: true,
	}

	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{TMDB: searcher})

	job := tmdbJob{
		row:  store.Row{InfoHash: testHash(1), Name: "Legacies.2018.1080p", Category: classify.CategoryMoviesHD},
		name: "Legacies.2018.1080p",
	}

	row, ok := confirmRow(t, tc, job)
	if !ok {
		t.Fatal("the row was not forwarded")
	}

	if row.Category != classify.CategoryTVHD {
		t.Fatalf("category = %d, want %d", row.Category, classify.CategoryTVHD)
	}

	if row.ContentID != 79744 {
		t.Fatalf("content id = %d, want 79744", row.ContentID)
	}

	if searcher.title != "Legacies" || searcher.year != 2018 {
		t.Fatalf("looked up %q, %d", searcher.title, searcher.year)
	}
}

func TestTMDBWeakAdultBecomesFilm(t *testing.T) {
	t.Parallel()

	searcher := &fakeTMDBSearcher{
		match: tmdb.Match{ID: 13523, Medium: tmdb.MediumMovie, Title: "Sex Drive"},
		found: true,
	}

	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{TMDB: searcher})

	job := tmdbJob{
		row: store.Row{
			InfoHash: testHash(2),
			Name:     "Sex.Drive.2008.1080p.BluRay",
			Category: classify.CategoryXXX,
		},
		name: "Sex.Drive.2008.1080p.BluRay",
	}

	row, ok := confirmRow(t, tc, job)
	if !ok {
		t.Fatal("the row was not forwarded")
	}

	if row.Category != classify.CategoryMoviesHD {
		t.Fatalf("category = %d, want %d", row.Category, classify.CategoryMoviesHD)
	}

	if row.ContentID != 13523 {
		t.Fatalf("content id = %d, want 13523", row.ContentID)
	}
}

func TestTMDBAdultFlagIsBelieved(t *testing.T) {
	t.Parallel()

	searcher := &fakeTMDBSearcher{
		match: tmdb.Match{ID: 99, Medium: tmdb.MediumMovie, Adult: true, Title: "Something"},
		found: true,
	}

	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{TMDB: searcher})

	job := tmdbJob{
		row: store.Row{
			InfoHash: testHash(3),
			Name:     "Something.2020.1080p",
			Category: classify.CategoryMoviesHD,
			Seasons:  []int{1},
		},
		name: "Something.2020.1080p",
	}

	row, ok := confirmRow(t, tc, job)
	if !ok {
		t.Fatal("the row was not forwarded")
	}

	if row.Category != classify.CategoryXXX {
		t.Fatalf("category = %d, want XXX", row.Category)
	}

	if row.ContentID != 0 {
		t.Fatalf("content id = %d, want 0", row.ContentID)
	}

	if row.Seasons != nil {
		t.Fatalf("seasons = %v, want cleared", row.Seasons)
	}
}

func TestTMDBAdultFlagDropsWhenConfigured(t *testing.T) {
	t.Parallel()

	searcher := &fakeTMDBSearcher{
		match: tmdb.Match{ID: 99, Medium: tmdb.MediumMovie, Adult: true},
		found: true,
	}

	rejected := drops.NewRecorder(10)

	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{
		TMDB:       searcher,
		Classifier: classify.NewLive(classify.Config{DropAdult: true}),
		Rejected:   rejected,
	})

	job := tmdbJob{
		row:  store.Row{InfoHash: testHash(4), Name: "Something.2020.1080p", Category: classify.CategoryMoviesHD},
		name: "Something.2020.1080p",
	}

	if _, ok := confirmRow(t, tc, job); ok {
		t.Fatal("an adult row was forwarded with adult dropping enabled")
	}

	if got := tc.crawler.Stats().TorrentsDropped; got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}

	// The row was rejected after the crawl's own classification, so the feed
	// has to be told about it from here.
	events := rejected.Recent(10, nil)
	if len(events) != 1 {
		t.Fatalf("recorded %d rejections, want 1", len(events))
	}

	if events[0].Name != "Something.2020.1080p" || events[0].Reason != classify.DropAdult {
		t.Fatalf("recorded %+v, want the adult drop", events[0])
	}
}

func TestTMDBMissKeepsClassification(t *testing.T) {
	t.Parallel()

	searcher := &fakeTMDBSearcher{found: false}

	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{TMDB: searcher})

	job := tmdbJob{
		row:  store.Row{InfoHash: testHash(5), Name: "Obscure.Film.2019.1080p", Category: classify.CategoryMoviesHD},
		name: "Obscure.Film.2019.1080p",
	}

	row, ok := confirmRow(t, tc, job)
	if !ok {
		t.Fatal("the row was not forwarded")
	}

	if row.Category != classify.CategoryMoviesHD || row.ContentID != 0 {
		t.Fatalf("row = %+v, want unchanged", row)
	}
}

func TestTMDBErrorKeepsClassification(t *testing.T) {
	t.Parallel()

	searcher := &fakeTMDBSearcher{err: errors.New("request failed")}

	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{TMDB: searcher})

	job := tmdbJob{
		row:  store.Row{InfoHash: testHash(6), Name: "Obscure.Film.2019.1080p", Category: classify.CategoryMoviesHD},
		name: "Obscure.Film.2019.1080p",
	}

	row, ok := confirmRow(t, tc, job)
	if !ok {
		t.Fatal("the row was not forwarded")
	}

	if row.Category != classify.CategoryMoviesHD {
		t.Fatalf("category = %d, want unchanged", row.Category)
	}

	if got := tc.crawler.Stats().TMDBFailures; got != 1 {
		t.Fatalf("failures = %d, want 1", got)
	}
}

func TestTMDBShowMatchKeepsWeakAdultClassification(t *testing.T) {
	t.Parallel()

	searcher := &fakeTMDBSearcher{
		match: tmdb.Match{ID: 5, Medium: tmdb.MediumTV, Title: "Some Show"},
		found: true,
	}

	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{TMDB: searcher})

	job := tmdbJob{
		row: store.Row{
			InfoHash: testHash(7),
			Name:     "Some.Show.2019.1080p",
			Category: classify.CategoryXXX,
		},
		name: "Some.Show.2019.1080p",
	}

	row, ok := confirmRow(t, tc, job)
	if !ok {
		t.Fatal("the row was not forwarded")
	}

	if row.Category != classify.CategoryXXX {
		t.Fatalf("category = %d, want XXX kept", row.Category)
	}
}

func TestTMDBCandidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result classify.Result
		want   bool
	}{
		{"film", classify.Result{Category: classify.CategoryMoviesHD}, true},
		{"show", classify.Result{Category: classify.CategoryTVHD}, true},
		{"weak adult", classify.Result{Category: classify.CategoryXXX, AdultEvidence: classify.AdultEvidenceWeak}, true},
		{"strong adult", classify.Result{Category: classify.CategoryXXX, AdultEvidence: classify.AdultEvidenceStrong}, false},
		{"music", classify.Result{Category: classify.CategoryAudio}, false},
		{"other", classify.Result{Category: classify.CategoryOther}, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := tmdbCandidate(test.result); got != test.want {
				t.Fatalf("tmdbCandidate = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFlushTMDBQueue(t *testing.T) {
	t.Parallel()

	searcher := &fakeTMDBSearcher{}
	tc := newTestCrawler(t, &fakeDHT{}, &fakeRequester{}, Config{TMDB: searcher})

	now := time.Now().UTC()

	for i := range 2 {
		tc.crawler.forTMDB <- tmdbJob{
			row: store.Row{
				InfoHash:  testHash(byte(10 + i)),
				Name:      "Queued.Release",
				CreatedAt: now,
				Category:  classify.CategoryMovies,
			},
		}
	}

	tc.crawler.flushTMDBQueue()

	stats, err := tc.store.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.Rows != 2 {
		t.Fatalf("rows = %d, want 2", stats.Rows)
	}
}
