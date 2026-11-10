package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/BitTrawler/bittrawler/internal/benchdata"
)

const (
	// benchPreloadRows is the corpus shared by the search benchmarks.
	benchPreloadRows = 30000
	// benchBatchSize is the batch written by BenchmarkPutBatch.
	benchBatchSize = 1000
)

// benchNow pins the store clock and the generated corpus to one instant, so
// shard routing does not depend on the wall clock.
var benchNow = time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)

// storeRows converts generated torrents into insertable rows. The two structs
// have the same shape, so no field is dropped.
func storeRows(torrents []benchdata.Torrent) []Row {
	rows := make([]Row, len(torrents))
	for i, torrent := range torrents {
		rows[i] = Row{
			InfoHash:    torrent.InfoHash,
			Name:        torrent.Name,
			Size:        torrent.Size,
			CreatedAt:   torrent.CreatedAt,
			Category:    torrent.Category,
			ContentID:   torrent.ContentID,
			Seasons:     torrent.Seasons,
			Episodes:    torrent.Episodes,
			AirDate:     torrent.AirDate,
			FilesCount:  torrent.FilesCount,
			PrimaryFile: torrent.PrimaryFile,
			Seeders:     torrent.Seeders,
			Leechers:    torrent.Leechers,
		}
	}

	return rows
}

// storeSetBenchHashes overwrites four infohash bytes with the benchmark
// iteration, so every iteration inserts a fresh batch instead of a page of
// rediscoveries. The first five bytes stay as generated, keeping the rows
// otherwise identical across iterations.
func storeSetBenchHashes(rows []Row, iteration int) {
	marker := uint32(iteration)
	for i := range rows {
		rows[i].InfoHash[5] = byte(marker)
		rows[i].InfoHash[6] = byte(marker >> 8)
		rows[i].InfoHash[7] = byte(marker >> 16)
		rows[i].InfoHash[8] = byte(marker >> 24)
	}
}

func storePutAll(t *testing.T, s *Store, rows []Row) {
	t.Helper()

	n, err := s.PutBatch(context.Background(), rows)
	if err != nil {
		t.Fatalf("put %d rows: %v", len(rows), err)
	}

	if n != len(rows) {
		t.Fatalf("inserted %d of %d rows (duplicate infohash or skipped row)", n, len(rows))
	}
}

func storeMustSearch(t *testing.T, s *Store, q Query) Result {
	t.Helper()

	res, err := s.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	return res
}

func storeHasHash(res Result, hash [20]byte) bool {
	for _, torrent := range res.Torrents {
		if torrent.InfoHash == hash {
			return true
		}
	}

	return false
}

// storeSearchTerm returns an alphabetic token from a generated name. FTS5
// tokenises on punctuation, so the returned word is guaranteed to be a token
// prefix in that name.
func storeSearchTerm(name string) string {
	for _, token := range strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(token) < 4 {
			continue
		}

		if strings.IndexFunc(token, func(r rune) bool { return !unicode.IsLetter(r) }) < 0 {
			return strings.ToLower(token)
		}
	}

	return ""
}

// storeKnownTerm picks the first generated row whose name contains a token the
// text search can query, and returns both the row and the token.
func storeKnownTerm(t *testing.T, rows []benchdata.Torrent) (benchdata.Torrent, string) {
	t.Helper()

	for _, row := range rows {
		if term := storeSearchTerm(row.Name); term != "" {
			return row, term
		}
	}

	t.Fatal("no generated name contained a usable search token")

	return benchdata.Torrent{}, ""
}

// TestBenchdataStoreSmoke exercises the store end to end over a generated
// corpus: monthly shard routing, text and metadata search, category matching
// and whole-month retention.
func TestBenchdataStoreSmoke(t *testing.T) {
	const count = 2000

	now := benchNow
	dir := t.TempDir()

	s, err := Open(Config{
		Dir:             dir,
		RetentionMonths: 2,
		Now:             func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	generated := benchdata.Rows(benchdata.Options{Count: count, Days: 80, Now: now})
	storePutAll(t, s, storeRows(generated))

	// The 80-day window starting 2026-08-22 spans four calendar months, and
	// each row must land in the shard for its created_at month.
	wantMonths := []string{"2026-08", "2026-09", "2026-10", "2026-11"}
	if got := s.ShardMonths(); !reflect.DeepEqual(got, wantMonths) {
		t.Fatalf("shards = %v, want %v", got, wantMonths)
	}

	// A text search for a token taken from a generated name must find that
	// row, and must only return rows whose name contains the token.
	source, term := storeKnownTerm(t, generated)

	q := NewQuery()
	q.Text = term
	q.Limit = count

	res := storeMustSearch(t, s, q)
	if len(res.Torrents) == 0 {
		t.Fatalf("text search %q returned nothing", term)
	}

	if !storeHasHash(res, source.InfoHash) {
		t.Fatalf("text search %q did not return its own source row %q", term, source.Name)
	}

	for _, torrent := range res.Torrents {
		if !strings.Contains(strings.ToLower(torrent.Name), term) {
			t.Fatalf("text search %q returned %q", term, torrent.Name)
		}
	}

	// A season query must return exactly the rows tagged season 5, packs
	// included and episodes included, with the tokens parsed back correctly.
	wantSeason := make(map[[20]byte]bool)

	for _, row := range generated {
		if slices.Contains(row.Seasons, 5) {
			wantSeason[row.InfoHash] = true
		}
	}

	if len(wantSeason) == 0 {
		t.Fatal("generated corpus has no season 5 rows")
	}

	q = NewQuery()
	q.Season = 5
	q.Limit = count

	res = storeMustSearch(t, s, q)
	if len(res.Torrents) != len(wantSeason) {
		t.Fatalf("season 5 search returned %d rows, want %d", len(res.Torrents), len(wantSeason))
	}

	packs := 0

	for _, torrent := range res.Torrents {
		if !slices.Contains(torrent.Seasons, 5) {
			t.Fatalf("season 5 search returned %q with seasons %v", torrent.Name, torrent.Seasons)
		}

		if !wantSeason[torrent.InfoHash] {
			t.Fatalf("season 5 search returned an unexpected row %q", torrent.Name)
		}

		if len(torrent.Episodes) == 0 {
			packs++
		}
	}

	if packs == 0 {
		t.Fatal("season 5 search returned no season packs")
	}

	if packs == len(res.Torrents) {
		t.Fatal("season 5 search returned no individual episodes")
	}

	// A Newznab parent category must match only its children.
	wantTV := make(map[[20]byte]bool)

	for _, row := range generated {
		if row.Category >= 5000 && row.Category <= 5999 {
			wantTV[row.InfoHash] = true
		}
	}

	if len(wantTV) == 0 {
		t.Fatal("generated corpus has no TV rows")
	}

	q = NewQuery()
	q.Categories = []int{5000}
	q.Limit = count

	res = storeMustSearch(t, s, q)
	if len(res.Torrents) != len(wantTV) {
		t.Fatalf("category 5000 search returned %d rows, want %d", len(res.Torrents), len(wantTV))
	}

	for _, torrent := range res.Torrents {
		if torrent.Category < 5000 || torrent.Category > 5999 {
			t.Fatalf("category 5000 search returned %q with category %d", torrent.Name, torrent.Category)
		}

		if !wantTV[torrent.InfoHash] {
			t.Fatalf("category 5000 search returned an unexpected row %q", torrent.Name)
		}
	}

	// Two months of retention from 2026-11 keeps October and November and
	// unlinks the August and September shard files.
	dropped, err := s.DropExpired(context.Background())
	if err != nil {
		t.Fatalf("drop expired: %v", err)
	}

	wantDropped := []string{"2026-08", "2026-09"}
	if !reflect.DeepEqual(dropped, wantDropped) {
		t.Fatalf("dropped = %v, want %v", dropped, wantDropped)
	}

	for _, month := range dropped {
		remaining, err := filepath.Glob(filepath.Join(dir, shardPrefix+month+".db*"))
		if err != nil {
			t.Fatalf("glob shard %s: %v", month, err)
		}

		if len(remaining) != 0 {
			t.Fatalf("shard %s still on disk: %v", month, remaining)
		}
	}

	if got, want := s.ShardMonths(), wantMonths[2:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("shards after retention = %v, want %v", got, want)
	}

	// Search must now see only the retained months.
	cutoff := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	kept := 0

	for _, row := range generated {
		if wantTV[row.InfoHash] && !row.CreatedAt.Before(cutoff) {
			kept++
		}
	}

	res = storeMustSearch(t, s, q)
	if len(res.Torrents) != kept {
		t.Fatalf("category 5000 search after retention returned %d rows, want %d", len(res.Torrents), kept)
	}

	for _, torrent := range res.Torrents {
		if torrent.CreatedAt.Before(cutoff) {
			t.Fatalf("search after retention returned %q from %s", torrent.Name, torrent.CreatedAt)
		}
	}
}

// shared search corpus: one store, loaded once per test process, for every
// search benchmark.
var (
	benchLoadOnce  sync.Once
	benchLoadStore *Store
	benchLoadDir   string
	benchLoadErr   error
	// benchSink keeps the benchmark result observable to the compiler.
	benchSink Result
)

// benchPreloaded returns the shared store, loading it on first use with the
// benchmark timer stopped. TestMain closes it and removes the directory.
func benchPreloaded(b *testing.B) *Store {
	b.Helper()

	b.StopTimer()
	defer b.StartTimer()

	benchLoadOnce.Do(func() {
		dir, err := os.MkdirTemp("", "bittrawler-store-bench-*")
		if err != nil {
			benchLoadErr = err

			return
		}

		benchLoadDir = dir

		s, err := Open(Config{
			Dir:         dir,
			BusyTimeout: 30 * time.Second,
			Now:         func() time.Time { return benchNow },
		})
		if err != nil {
			benchLoadErr = err

			return
		}

		// Keep the store reachable for teardown even if the load fails part
		// way through.
		benchLoadStore = s

		rows := storeRows(benchdata.Rows(benchdata.Options{Count: benchPreloadRows, Days: 80, Now: benchNow}))

		n, err := s.PutBatch(context.Background(), rows)
		if err != nil {
			benchLoadErr = err

			return
		}

		if n != len(rows) {
			benchLoadErr = fmt.Errorf("inserted %d of %d generated rows", n, len(rows))
		}
	})

	if benchLoadErr != nil {
		b.Fatalf("preload %d rows: %v", benchPreloadRows, benchLoadErr)
	}

	return benchLoadStore
}

// benchCheckSearch runs the query once outside the measured region and fails
// the benchmark if it does not return at least want rows.
func benchCheckSearch(b *testing.B, s *Store, q Query, want int) {
	b.Helper()

	b.StopTimer()
	defer b.StartTimer()

	res, err := s.Search(context.Background(), q)
	if err != nil {
		b.Fatalf("search: %v", err)
	}

	if len(res.Torrents) < want {
		b.Fatalf("search returned %d rows, want at least %d", len(res.Torrents), want)
	}
}

// TestMain gives the shared benchmark corpus a lifecycle: m.Run still runs
// every ordinary test, then the preloaded store is closed and its temporary
// directory removed.
func TestMain(m *testing.M) {
	code := m.Run()

	if benchLoadStore != nil {
		_ = benchLoadStore.Close()
	}

	if benchLoadDir != "" {
		_ = os.RemoveAll(benchLoadDir)
	}

	os.Exit(code)
}

// BenchmarkPutBatch measures writing a 1000-row batch into a live store whose
// corpus grows with each iteration.
func BenchmarkPutBatch(b *testing.B) {
	s, err := Open(Config{
		Dir:         b.TempDir(),
		BusyTimeout: 30 * time.Second,
		Now:         func() time.Time { return benchNow },
	})
	if err != nil {
		b.Fatalf("open: %v", err)
	}

	b.Cleanup(func() { _ = s.Close() })

	rows := storeRows(benchdata.Rows(benchdata.Options{Count: benchBatchSize, Days: 80, Now: benchNow}))
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		storeSetBenchHashes(rows, i)

		n, err := s.PutBatch(ctx, rows)
		if err != nil {
			b.Fatalf("put batch: %v", err)
		}

		if n != len(rows) {
			b.Fatalf("inserted %d of %d rows", n, len(rows))
		}
	}
}

// BenchmarkSearchText measures a two-word FTS5 name search, newest first.
func BenchmarkSearchText(b *testing.B) {
	s := benchPreloaded(b)

	q := NewQuery()
	q.Text = "northern lights"
	q.Limit = 100

	benchCheckSearch(b, s, q, 1)

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		res, err := s.Search(ctx, q)
		if err != nil {
			b.Fatalf("search: %v", err)
		}

		benchSink = res
	}
}

// BenchmarkSearchSeason measures a season+episode FTS5 query, newest first.
func BenchmarkSearchSeason(b *testing.B) {
	s := benchPreloaded(b)

	q := NewQuery()
	q.Season = 5
	q.Episode = 20
	q.Limit = 100

	benchCheckSearch(b, s, q, 1)

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		res, err := s.Search(ctx, q)
		if err != nil {
			b.Fatalf("search: %v", err)
		}

		benchSink = res
	}
}

// BenchmarkSearchNewest measures the newest-first feed for a parent category,
// the query behind Torznab RSS and paging.
func BenchmarkSearchNewest(b *testing.B) {
	s := benchPreloaded(b)

	q := NewQuery()
	q.Categories = []int{2000}
	q.Sort = SortNewest
	q.Limit = 100

	// A full first page must exist, or the benchmark would measure an empty
	// result set.
	benchCheckSearch(b, s, q, 100)

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		res, err := s.Search(ctx, q)
		if err != nil {
			b.Fatalf("search: %v", err)
		}

		benchSink = res
	}
}
