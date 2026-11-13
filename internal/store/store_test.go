package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/sqlite"
)

// testClock is a settable clock for rollover and retention tests.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *testClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = t
}

func newStore(t *testing.T, cfg Config) (*Store, *testClock) {
	t.Helper()

	clock := &testClock{t: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)}

	if cfg.Dir == "" {
		cfg.Dir = t.TempDir()
	}

	cfg.Now = clock.Now

	s, err := Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	return s, clock
}

func testHash(n byte) [20]byte {
	var h [20]byte

	h[0] = 0xaa
	h[1] = n
	h[19] = 0xff

	return h
}

func put(t *testing.T, s *Store, rows ...Row) {
	t.Helper()

	n, err := s.PutBatch(context.Background(), rows)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if n != len(rows) {
		t.Fatalf("put: inserted %d of %d", n, len(rows))
	}
}

func search(t *testing.T, s *Store, q Query) Result {
	t.Helper()

	res, err := s.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	return res
}

func names(res Result) []string {
	out := make([]string, 0, len(res.Torrents))
	for _, tr := range res.Torrents {
		out = append(out, tr.Name)
	}

	return out
}

func TestOpenCreatesCurrentShard(t *testing.T) {
	s, _ := newStore(t, Config{Dir: t.TempDir(), RetentionMonths: 12})

	months := s.ShardMonths()
	if len(months) != 1 || months[0] != "2026-11" {
		t.Fatalf("shards = %v, want [2026-11]", months)
	}

	path := filepath.Join(s.cfg.Dir, shardPrefix+"2026-11.db")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shard file: %v", err)
	}
}

func TestPutAndDedupe(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	row := Row{InfoHash: testHash(1), Name: "Some.Movie.2020.1080p.BluRay.x264-GRP",
		Size: 1 << 30, CreatedAt: now, Category: 2040, FilesCount: 1, Seeders: 3}

	inserted, err := s.Put(context.Background(), row)
	if err != nil || !inserted {
		t.Fatalf("first put: inserted=%v err=%v", inserted, err)
	}

	inserted, err = s.Put(context.Background(), row)
	if err != nil || inserted {
		t.Fatalf("second put: inserted=%v err=%v", inserted, err)
	}

	seen, err := s.Seen(context.Background(), row.InfoHash)
	if err != nil || !seen {
		t.Fatalf("seen: %v %v", seen, err)
	}

	res := search(t, s, Query{Text: "some movie", Limit: 10, Season: AnySeason, Episode: AnyEpisode})
	if len(res.Torrents) != 1 {
		t.Fatalf("got %d rows, want 1", len(res.Torrents))
	}

	if res.Torrents[0].InfoHash != row.InfoHash {
		t.Fatalf("wrong torrent returned")
	}
}

func TestRediscoveryAcrossShards(t *testing.T) {
	s, clock := newStore(t, Config{RetentionMonths: 12})
	ctx := context.Background()

	row := Row{InfoHash: testHash(2), Name: "Midnight.Run.S06E03.1080p",
		CreatedAt: clock.Now(), Category: 5040, Seasons: []int{6}, Episodes: []int{60003}, Seeders: 1}
	put(t, s, row)

	// The same torrent is rediscovered after the month rolls over: it must not
	// become a second row.
	clock.set(time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC))

	inserted, err := s.Put(ctx, row)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if inserted {
		t.Fatal("rediscovery was inserted twice")
	}

	res := search(t, s, Query{Text: "midnight run", Limit: 10, Season: AnySeason, Episode: AnyEpisode})
	if len(res.Torrents) != 1 {
		t.Fatalf("got %d rows, want 1", len(res.Torrents))
	}
}

func TestPrefixMatchingFindsEpisodeFromSeason(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	put(t, s,
		Row{InfoHash: testHash(3), Name: "The.Show.S51E01.1080p.WEB-DL.x264-GRP",
			CreatedAt: now, Category: 5040, Seasons: []int{51}, Episodes: []int{510001}, Seeders: 2},
		Row{InfoHash: testHash(4), Name: "Unrelated.S51E01.1080p",
			CreatedAt: now, Category: 5040, Seasons: []int{51}, Episodes: []int{510001}, Seeders: 1},
	)

	// The query "s51" must match the token s51e01 by prefix.
	res := search(t, s, Query{Text: "s51", Limit: 10, Season: AnySeason, Episode: AnyEpisode})
	if len(res.Torrents) != 2 {
		t.Fatalf("prefix search returned %d rows, want 2: %v", len(res.Torrents), names(res))
	}

	// Dropping the final character is still a prefix match for the title word.
	res = search(t, s, Query{Text: `the show s51`, Limit: 10, Season: AnySeason, Episode: AnyEpisode})
	if len(res.Torrents) != 1 || res.Torrents[0].Name != "The.Show.S51E01.1080p.WEB-DL.x264-GRP" {
		t.Fatalf("title search returned %v", names(res))
	}
}

func TestPhraseAndExclusion(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	put(t, s,
		Row{InfoHash: testHash(5), Name: "The.Matrix.1999.1080p.BluRay", CreatedAt: now, Category: 2040},
		Row{InfoHash: testHash(6), Name: "Matrix.Reloaded.2003.1080p.BluRay", CreatedAt: now, Category: 2040},
	)

	res := search(t, s, Query{Text: `"the matrix"`, Limit: 10, Season: AnySeason, Episode: AnyEpisode})
	if len(res.Torrents) != 1 {
		t.Fatalf(`phrase search returned %v`, names(res))
	}

	res = search(t, s, Query{Text: `matrix -reloaded`, Limit: 10, Season: AnySeason, Episode: AnyEpisode})
	if len(res.Torrents) != 1 || res.Torrents[0].Name != "The.Matrix.1999.1080p.BluRay" {
		t.Fatalf("exclusion search returned %v", names(res))
	}
}

func TestSeasonSearchIncludesPacksAndEpisodes(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	put(t, s,
		Row{InfoHash: testHash(7), Name: "The.Show.S05.1080p.BluRay", CreatedAt: now,
			Category: 5040, Seasons: []int{5}},
		Row{InfoHash: testHash(8), Name: "The.Show.S05E02.1080p.HDTV", CreatedAt: now,
			Category: 5040, Seasons: []int{5}, Episodes: []int{50002}},
		Row{InfoHash: testHash(9), Name: "Other.Show.S05E02.1080p.HDTV", CreatedAt: now,
			Category: 5040, Seasons: []int{5}, Episodes: []int{50002}},
	)

	q := NewQuery()
	q.Text = "the show"
	q.Season = 5

	res := search(t, s, q)
	if len(res.Torrents) != 2 {
		t.Fatalf("season search returned %v, want the pack and the episode", names(res))
	}
}

func TestEpisodeSearchExcludesPacks(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	put(t, s,
		Row{InfoHash: testHash(10), Name: "The.Show.S05.1080p.BluRay", CreatedAt: now,
			Category: 5040, Seasons: []int{5}},
		Row{InfoHash: testHash(11), Name: "The.Show.S05E02.1080p.HDTV", CreatedAt: now,
			Category: 5040, Seasons: []int{5}, Episodes: []int{50002}},
	)

	q := NewQuery()
	q.Text = "the show"
	q.Season = 5
	q.Episode = 2

	res := search(t, s, q)
	if len(res.Torrents) != 1 || res.Torrents[0].Name != "The.Show.S05E02.1080p.HDTV" {
		t.Fatalf("episode search returned %v, want only the episode", names(res))
	}
}

func TestDailyEpisodeByAirDate(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	put(t, s,
		Row{InfoHash: testHash(12), Name: "Late.Show.2026.11.06.1080p.HDTV", CreatedAt: now,
			Category: 5040, AirDate: "2026-11-06"},
		Row{InfoHash: testHash(13), Name: "Late.Show.2026.11.05.1080p.HDTV", CreatedAt: now,
			Category: 5040, AirDate: "2026-11-05"},
	)

	q := NewQuery()
	q.Text = "late show"
	q.DailyDate = "2026-11-06"

	res := search(t, s, q)
	if len(res.Torrents) != 1 {
		t.Fatalf("daily search returned %v", names(res))
	}
}

func TestCategoryParentMatching(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	put(t, s,
		Row{InfoHash: testHash(14), Name: "TV.HD.Release", CreatedAt: now, Category: 5040},
		Row{InfoHash: testHash(15), Name: "TV.SD.Release", CreatedAt: now, Category: 5030},
		Row{InfoHash: testHash(16), Name: "Movie.Release", CreatedAt: now, Category: 2040},
	)

	q := NewQuery()
	q.Categories = []int{5000}
	res := search(t, s, q)
	if len(res.Torrents) != 2 {
		t.Fatalf("parent category returned %v, want the two TV rows", names(res))
	}

	q = NewQuery()
	q.Categories = []int{5030, 5040}
	res = search(t, s, q)
	if len(res.Torrents) != 2 {
		t.Fatalf("leaf categories returned %v", names(res))
	}

	q = NewQuery()
	q.Categories = []int{2040}
	res = search(t, s, q)
	if len(res.Torrents) != 1 || res.Torrents[0].Name != "Movie.Release" {
		t.Fatalf("movie category returned %v", names(res))
	}
}

func TestNewestFirstAndPagination(t *testing.T) {
	s, clock := newStore(t, Config{RetentionMonths: 12})

	// Five rows spread over three months, inserted oldest first.
	base := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)

	rows := make([]Row, 0, 5)
	for i := range 5 {
		rows = append(rows, Row{
			InfoHash:  testHash(byte(20 + i)),
			Name:      fmt.Sprintf("Paged.Release.%d", i),
			CreatedAt: base.AddDate(0, 0, i*14),
			Category:  2040,
		})
	}

	put(t, s, rows...)
	clock.set(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))

	want := []string{"Paged.Release.4", "Paged.Release.3", "Paged.Release.2", "Paged.Release.1", "Paged.Release.0"}

	q := NewQuery()
	q.Text = "paged"
	q.Limit = 10

	got := names(search(t, s, q))
	if len(got) != 5 {
		t.Fatalf("got %v", got)
	}

	// Newest first spans shards.
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}

	// Paging.
	q = NewQuery()
	q.Text = "paged"
	q.Limit = 2
	q.Offset = 1

	got = names(search(t, s, q))
	want = want[1:3]

	// The length check is what fails when the offset is ignored: the loop
	// below would pass on a short or empty page.
	if len(got) != len(want) {
		t.Fatalf("page = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("page = %v, want %v", got, want)
		}
	}

	// Count is over all shards.
	q = NewQuery()
	q.Text = "paged"
	q.Limit = 1
	q.Count = true

	res := search(t, s, q)
	if res.Total != 5 {
		t.Fatalf("total = %d, want 5", res.Total)
	}
}

func TestRolloverCreatesNewShard(t *testing.T) {
	s, clock := newStore(t, Config{RetentionMonths: 12})

	put(t, s, Row{InfoHash: testHash(30), Name: "August.Release", CreatedAt: clock.Now(), Category: 2040})

	clock.set(time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC))
	put(t, s, Row{InfoHash: testHash(31), Name: "December.Release", CreatedAt: clock.Now(), Category: 2040})

	months := s.ShardMonths()
	want := []string{"2026-11", "2026-12"}
	if len(months) != len(want) {
		t.Fatalf("shards = %v, want %v", months, want)
	}

	for i := range want {
		if months[i] != want[i] {
			t.Fatalf("shards = %v, want %v", months, want)
		}
	}
}

func TestRetentionDropsOldShards(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 3})

	for m := 0; m < 5; m++ {
		month := time.Date(2026, time.July+time.Month(m), 15, 0, 0, 0, 0, time.UTC)
		put(t, s, Row{InfoHash: testHash(byte(40 + m)), Name: "Retention.Test", CreatedAt: month, Category: 2040})
	}

	// Now is November 2026; three months retention keeps August onwards.
	dropped, err := s.DropExpired(context.Background())
	if err != nil {
		t.Fatalf("drop: %v", err)
	}

	want := []string{"2026-07", "2026-08"}
	if len(dropped) != len(want) {
		t.Fatalf("dropped = %v, want %v", dropped, want)
	}

	for i := range want {
		if dropped[i] != want[i] {
			t.Fatalf("dropped = %v, want %v", dropped, want)
		}
	}

	for _, month := range dropped {
		if _, err := os.Stat(filepath.Join(s.cfg.Dir, shardPrefix+month+".db")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("shard %s still on disk", month)
		}
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	ctx := context.Background()

	var wg sync.WaitGroup

	errs := make(chan error, 8)
	stop := make(chan struct{})

	wg.Add(1)

	go func() {
		defer wg.Done()

		for i := 0; i < 300; i++ {
			select {
			case <-stop:
				return
			default:
			}

			_, err := s.Put(ctx, Row{
				InfoHash:  testHash(byte(100 + i%50)),
				Name:      fmt.Sprintf("Concurrent.Release.%d", i),
				CreatedAt: s.cfg.Now(),
				Category:  2040,
			})
			if err != nil {
				errs <- err

				return
			}
		}
	}()

	for r := 0; r < 4; r++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := 0; i < 100; i++ {
				q := NewQuery()
				q.Text = "concurrent"
				q.Limit = 10

				if _, err := s.Search(context.Background(), q); err != nil {
					errs <- err

					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent: %v", err)
	}
	close(stop)
}

func TestBackupByFileCopy(t *testing.T) {
	dir := t.TempDir()
	s, _ := newStore(t, Config{Dir: dir, RetentionMonths: 12})

	put(t, s, Row{InfoHash: testHash(60), Name: "Backup.Release.2020", CreatedAt: s.cfg.Now(), Category: 2040})

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	backupDir := t.TempDir()

	matches, err := filepath.Glob(filepath.Join(dir, "torrents-*.db*"))
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(matches)

	for _, m := range matches {
		if filepath.Ext(m) == ".shm" {
			continue
		}

		data, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(backupDir, filepath.Base(m)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	restored, err := Open(Config{Dir: backupDir, RetentionMonths: 12})
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}

	t.Cleanup(func() { _ = restored.Close() })

	res := search(t, restored, Query{Text: "backup", Limit: 10, Season: AnySeason, Episode: AnyEpisode})
	if len(res.Torrents) != 1 {
		t.Fatalf("restored %d rows, want 1", len(res.Torrents))
	}
}

// TestCrashRecovery kills a writer process and checks the shard reopens.
func TestCrashRecovery(t *testing.T) {
	if os.Getenv("STORE_CRASH_HELPER") == "1" {
		crashHelper()

		return
	}

	dir := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=TestCrashRecovery")
	cmd.Env = append(os.Environ(), "STORE_CRASH_HELPER=1", "STORE_CRASH_DIR="+dir)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// Wait until the helper has written something, then kill it mid-write.
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(dir, "torrents-*.db"))
		if len(matches) > 0 {
			if st, err := os.Stat(matches[0]); err == nil && st.Size() > 0 {
				break
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}

	_ = cmd.Wait()

	s, err := Open(Config{Dir: dir, RetentionMonths: 12})
	if err != nil {
		t.Fatalf("reopen after crash: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	for _, sh := range s.shardsNewestFirst() {
		var result string
		if err := sh.db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
			t.Fatalf("integrity_check: %v", err)
		}

		if result != "ok" {
			t.Fatalf("integrity_check = %q", result)
		}
	}

	// Whatever committed must still be queryable.
	q := NewQuery()
	q.Text = "crash"
	q.Limit = 1000

	if _, err := s.Search(context.Background(), q); err != nil {
		t.Fatalf("search after crash: %v", err)
	}
}

func crashHelper() {
	dir := os.Getenv("STORE_CRASH_DIR")

	s, err := Open(Config{Dir: dir, RetentionMonths: 12})
	if err != nil {
		os.Exit(2)
	}

	for i := 0; ; i++ {
		if _, err := s.Put(context.Background(), Row{
			InfoHash:  testHash(byte(200 + i%50)),
			Name:      fmt.Sprintf("Crash.Release.%d", i),
			CreatedAt: s.cfg.Now(),
			Category:  2040,
		}); err != nil {
			os.Exit(3)
		}
	}
}

func TestSplitStatementsHandlesTriggerBodies(t *testing.T) {
	src := `
CREATE TABLE a (id INTEGER);
-- +goose StatementBegin
CREATE TRIGGER a_ai AFTER INSERT ON a BEGIN
    INSERT INTO a (id) VALUES (new.id);
    INSERT INTO a (id) VALUES (new.id + 1);
END;
-- +goose StatementEnd
CREATE INDEX a_id ON a (id);
`

	got := splitStatements(src)
	if len(got) != 3 {
		t.Fatalf("got %d statements: %#v", len(got), got)
	}

	if !contains(got[1], "CREATE TRIGGER") || !contains(got[1], "END;") {
		t.Fatalf("trigger statement mangled: %q", got[1])
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}

		return false
	})()
}

func TestNegativeOnlyTextReturnsNothing(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})

	put(t, s, Row{InfoHash: testHash(70), Name: "Some.Release.x264-CAM", CreatedAt: s.cfg.Now(), Category: 2040})

	// There is no positive term to search for, so FTS5 cannot express this.
	// Returning nothing is correct; returning the whole index would not be.
	res := search(t, s, Query{Text: "-cam", Limit: 10, Season: AnySeason, Episode: AnyEpisode})
	if len(res.Torrents) != 0 {
		t.Fatalf("negative-only query returned %d rows", len(res.Torrents))
	}
}

func TestZeroCreatedAtRejected(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})

	if _, err := s.Put(context.Background(), Row{InfoHash: testHash(71), Name: "No.Date"}); err == nil {
		t.Fatal("put accepted a zero created_at")
	}

	if months := s.ShardMonths(); len(months) != 1 {
		t.Fatalf("zero created_at created shards: %v", months)
	}
}

func TestRetentionFailureLeavesStoreUsable(t *testing.T) {
	dir := t.TempDir()
	s, _ := newStore(t, Config{Dir: dir, RetentionMonths: 1})

	old := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	put(t, s, Row{InfoHash: testHash(72), Name: "Old.Release", CreatedAt: old, Category: 2040},
		Row{InfoHash: testHash(73), Name: "New.Release", CreatedAt: s.cfg.Now(), Category: 2040})

	// Make the directory read-only so unlinking fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if _, err := s.DropExpired(context.Background()); err == nil {
		t.Fatal("drop on a read-only directory returned no error")
	}

	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// The failed shard must not remain as a closed handle: queries still work.
	res := search(t, s, Query{Text: "new", Limit: 10, Season: AnySeason, Episode: AnyEpisode})
	if len(res.Torrents) != 1 {
		t.Fatalf("store unusable after failed retention: %v", names(res))
	}
}

func TestSeenBatchEmptyInput(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})

	for _, input := range [][][20]byte{nil, {}} {
		got, err := s.SeenBatch(context.Background(), input)
		if err != nil {
			t.Fatalf("seen batch: %v", err)
		}

		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	}
}

func TestSeenBatchPreservesInputOrder(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	put(t, s,
		Row{InfoHash: testHash(80), Name: "Seen.One", CreatedAt: now, Category: 2040},
		Row{InfoHash: testHash(81), Name: "Seen.Two", CreatedAt: now, Category: 2040},
	)

	hashes := [][20]byte{testHash(81), testHash(82), testHash(80), testHash(83)}

	got, err := s.SeenBatch(context.Background(), hashes)
	if err != nil {
		t.Fatalf("seen batch: %v", err)
	}

	want := []bool{true, false, true, false}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSeenBatchFindsOlderShard(t *testing.T) {
	s, clock := newStore(t, Config{RetentionMonths: 12})

	put(t, s, Row{InfoHash: testHash(90), Name: "Older.Shard.Release", CreatedAt: clock.Now(), Category: 2040})

	clock.set(time.Date(2026, 12, 4, 0, 0, 0, 0, time.UTC))
	put(t, s, Row{InfoHash: testHash(91), Name: "Current.Shard.Release", CreatedAt: clock.Now(), Category: 2040})

	hashes := [][20]byte{testHash(90), testHash(91), testHash(92)}

	got, err := s.SeenBatch(context.Background(), hashes)
	if err != nil {
		t.Fatalf("seen batch: %v", err)
	}

	want := []bool{true, true, false}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSeenBatchDuplicateHashes(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})

	put(t, s, Row{InfoHash: testHash(93), Name: "Duplicate.Release", CreatedAt: s.cfg.Now(), Category: 2040})

	hashes := [][20]byte{testHash(93), testHash(93), testHash(94), testHash(94)}

	got, err := s.SeenBatch(context.Background(), hashes)
	if err != nil {
		t.Fatalf("seen batch: %v", err)
	}

	want := []bool{true, true, false, false}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSeenBatchChunking(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	// 1203 hashes span three parameter chunks at the 500-hash boundary.
	const count = 1203

	hashes := make([][20]byte, count)

	for i := range hashes {
		hashes[i][0] = 0xbb
		hashes[i][1] = byte(i >> 8)
		hashes[i][2] = byte(i)
	}

	present := map[int]struct{}{0: {}, 600: {}, count - 1: {}}

	rows := make([]Row, 0, len(present))

	for i := range present {
		rows = append(rows, Row{
			InfoHash:  hashes[i],
			Name:      fmt.Sprintf("Chunked.Release.%d", i),
			CreatedAt: now,
			Category:  2040,
		})
	}

	put(t, s, rows...)

	got, err := s.SeenBatch(context.Background(), hashes)
	if err != nil {
		t.Fatalf("seen batch: %v", err)
	}

	if len(got) != count {
		t.Fatalf("got %d results, want %d", len(got), count)
	}

	for i := range got {
		_, want := present[i]
		if got[i] != want {
			t.Fatalf("hash %d: seen = %v, want %v", i, got[i], want)
		}
	}
}

func TestStatsCountsRowsAndSizes(t *testing.T) {
	s, clock := newStore(t, Config{RetentionMonths: 12})

	put(t, s,
		Row{InfoHash: testHash(100), Name: "Stats.One", CreatedAt: clock.Now(), Category: 2040},
		Row{InfoHash: testHash(101), Name: "Stats.Two", CreatedAt: clock.Now(), Category: 2040},
	)

	clock.set(time.Date(2026, 12, 4, 0, 0, 0, 0, time.UTC))

	put(t, s, Row{InfoHash: testHash(102), Name: "Stats.Three", CreatedAt: clock.Now(), Category: 2040})

	stats, err := s.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.Rows != 3 {
		t.Fatalf("rows = %d, want 3", stats.Rows)
	}

	if len(stats.Shards) != 2 {
		t.Fatalf("shards = %d, want 2", len(stats.Shards))
	}

	if stats.Shards[0].Month != "2026-12" {
		t.Fatalf("newest shard = %s, want 2026-12", stats.Shards[0].Month)
	}

	rows := 0
	for _, shard := range stats.Shards {
		if shard.DBBytes <= 0 {
			t.Fatalf("shard %s has no size", shard.Month)
		}

		rows += shard.Rows
	}

	if rows != stats.Rows {
		t.Fatalf("shard rows sum to %d, total is %d", rows, stats.Rows)
	}

	if stats.DBBytes <= 0 {
		t.Fatal("store has no size")
	}
}

// TestOpenRefusesShardFromANewerSchema checks that a shard written by a newer
// release is refused rather than opened with a schema this build does not know.
func TestOpenRefusesShardFromANewerSchema(t *testing.T) {
	dir := t.TempDir()

	s, _ := newStore(t, Config{Dir: dir, RetentionMonths: 12})
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	latest, err := SchemaVersion()
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}

	path := filepath.Join(dir, shardPrefix+"2026-11.db")

	db, err := sqlite.Open(path, time.Second)
	if err != nil {
		t.Fatalf("open shard: %v", err)
	}

	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", latest+1)); err != nil {
		t.Fatalf("set user_version: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close shard: %v", err)
	}

	_, err = Open(Config{Dir: dir, Now: func() time.Time { return time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC) }})
	if err == nil {
		t.Fatal("opened a shard from a newer schema")
	}

	if !strings.Contains(err.Error(), "newer than this build") {
		t.Fatalf("error = %q, want a newer-schema refusal", err)
	}
}

// TestLimitDefaultsWhenUnset documents the limit contract Search implements: a
// non-positive limit means the default page size, and there is no upper clamp.
// The store serves the number of rows it is asked for; the Torznab endpoint is
// where a client-requested page size is capped by torznab.max_limit.
func TestLimitDefaultsWhenUnset(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	const rowCount = 120

	rows := make([]Row, 0, rowCount)

	for i := range rowCount {
		rows = append(rows, Row{
			InfoHash:  testHash(byte(i)),
			Name:      fmt.Sprintf("Limit.Release.%03d", i),
			CreatedAt: now,
			Category:  2040,
		})
	}

	put(t, s, rows...)

	// Zero and negative limits both mean the documented default of 100.
	for _, limit := range []int{0, -1} {
		q := NewQuery()
		q.Text = "limit release"
		q.Limit = limit

		res := search(t, s, q)
		if len(res.Torrents) != 100 {
			t.Fatalf("limit %d returned %d rows, want the default of 100", limit, len(res.Torrents))
		}
	}

	// There is no maximum in the store, so a limit beyond the result count is
	// honoured rather than truncated: all 120 rows come back.
	q := NewQuery()
	q.Text = "limit release"
	q.Limit = 1_000_000

	res := search(t, s, q)
	if len(res.Torrents) != rowCount {
		t.Fatalf("large limit returned %d rows, want all %d", len(res.Torrents), rowCount)
	}
}

// TestHostileSearchQueries feeds FTS5 metacharacters and awkward text through
// the query builder. It must never panic and, because the index is local, it
// must never return an error from the FTS5 query parser: a user's search box
// can contain any of these.
func TestHostileSearchQueries(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	put(t, s,
		Row{InfoHash: testHash(110), Name: "The.Matrix.1999.1080p.BluRay", CreatedAt: now, Category: 2040},
		Row{InfoHash: testHash(111), Name: "東京のリリース.2026.1080p", CreatedAt: now, Category: 2040},
		Row{InfoHash: testHash(112), Name: "Rocket.🚀.Release.2026", CreatedAt: now, Category: 2040},
	)

	cases := []string{
		`"`,                         // a lone quote
		`*`,                         // a prefix marker with no term
		`(`,                         // an unbalanced group
		`AND`,                       // an FTS5 operator as a term
		`NEAR("`,                    // an operator and a quote
		`unbalanced "quote`,         // an unterminated phrase
		`-`,                         // a dash with nothing to exclude
		strings.Repeat("a", 10_000), // very long text
		`東京`,                        // CJK
		`🚀`,                         // emoji
	}

	for i, text := range cases {
		var (
			res       Result
			err       error
			recovered any
		)

		// Recover explicitly so a panic is reported as a failed assertion
		// rather than tearing down the test binary.
		func() {
			defer func() { recovered = recover() }()

			res, err = s.Search(context.Background(), Query{
				Text: text, Limit: 10, Season: AnySeason, Episode: AnyEpisode,
			})
		}()

		if recovered != nil {
			t.Fatalf("case %d (%q) panicked: %v", i, text, recovered)
		}

		if err != nil {
			t.Fatalf("case %d (%q) returned an error: %v", i, text, err)
		}

		if len(res.Torrents) > 10 {
			t.Fatalf("case %d (%q) returned %d rows, more than the limit", i, text, len(res.Torrents))
		}
	}
}

// TestOffsetPastTheEndReturnsAnEmptyPage checks paging past the last result:
// the page is empty, the query succeeds, and the count still reports the real
// number of matches.
func TestOffsetPastTheEndReturnsAnEmptyPage(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	put(t, s,
		Row{InfoHash: testHash(113), Name: "Offset.Release.One", CreatedAt: now, Category: 2040},
		Row{InfoHash: testHash(114), Name: "Offset.Release.Two", CreatedAt: now, Category: 2040},
	)

	q := NewQuery()
	q.Text = "offset release"
	q.Limit = 10
	q.Offset = 500
	q.Count = true

	res := search(t, s, q)

	if len(res.Torrents) != 0 {
		t.Fatalf("offset past the end returned %v", names(res))
	}

	if res.Total != 2 {
		t.Fatalf("total = %d, want 2: the offset must not hide the matches", res.Total)
	}

	// The same query without the offset finds both rows, proving the empty
	// page is paging and not a query that matches nothing.
	q.Offset = 0

	res = search(t, s, q)
	if len(res.Torrents) != 2 {
		t.Fatalf("unpaged query returned %d rows, want 2", len(res.Torrents))
	}
}

// shardPathFor returns the path of the shard a store opened at now would use for
// now's month. Tests create something at that path before calling Open, so Open
// finds it where a real shard would be.
func shardPathFor(dir string, now time.Time) string {
	return filepath.Join(dir, shardPrefix+monthKey(now)+".db")
}

// TestOpenFailsWhenDataDirectoryIsAFile checks that a data directory path that
// is a regular file is reported as an error rather than silently ignored.
func TestOpenFailsWhenDataDirectoryIsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")

	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write data path: %v", err)
	}

	_, err := Open(Config{Dir: path})
	if err == nil {
		t.Fatal("Open accepted a regular file as the data directory")
	}

	// The error must name the path, or an operator cannot tell what to fix.
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error = %q, want it to name %s", err, path)
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("stat data path: %v", statErr)
	}

	if !info.Mode().IsRegular() {
		t.Fatalf("%s is no longer a regular file", path)
	}
}

// TestOpenFailsWhenShardPathIsADirectory checks a shard path that is a
// directory. The glob that finds existing shards matches directories too, so
// this must surface as a shard open failure naming the month.
func TestOpenFailsWhenShardPathIsADirectory(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	if err := os.Mkdir(shardPathFor(dir, now), 0o755); err != nil {
		t.Fatalf("mkdir shard path: %v", err)
	}

	_, err := Open(Config{Dir: dir, Now: func() time.Time { return now }})
	if err == nil {
		t.Fatal("Open accepted a directory as a shard")
	}

	if want := "open shard " + monthKey(now); !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want a failure containing %q", err, want)
	}
}

// TestOpenFailsWhenShardIsNotADatabase checks that a shard file holding
// something other than SQLite is refused, and left on disk for an operator to
// inspect or replace.
func TestOpenFailsWhenShardIsNotADatabase(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	path := shardPathFor(dir, now)
	garbage := bytes.Repeat([]byte("this is not a SQLite database;"), 256)

	if err := os.WriteFile(path, garbage, 0o644); err != nil {
		t.Fatalf("write corrupt shard: %v", err)
	}

	_, err := Open(Config{Dir: dir, Now: func() time.Time { return now }})
	if err == nil {
		t.Fatal("Open accepted a shard that is not a database")
	}

	if want := "open shard " + monthKey(now); !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want a failure containing %q", err, want)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read corrupt shard back: %v", readErr)
	}

	if !bytes.Equal(data, garbage) {
		t.Fatal("Open rewrote the corrupt shard")
	}
}

// TestSchemaVersionMatchesEmbeddedMigrations checks the version reported to the
// version command, against the migrations actually embedded in the binary, and
// against the version stamped on a freshly created shard.
func TestSchemaVersionMatchesEmbeddedMigrations(t *testing.T) {
	version, err := SchemaVersion()
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}

	if version <= 0 {
		t.Fatalf("schema version = %d, want a positive version", version)
	}

	embedded, err := migrations()
	if err != nil {
		t.Fatalf("migrations: %v", err)
	}

	if len(embedded) == 0 {
		t.Fatal("no migrations are embedded")
	}

	latest := 0

	for _, m := range embedded {
		if m.version > latest {
			latest = m.version
		}
	}

	if version != latest {
		t.Fatalf("SchemaVersion = %d, latest embedded migration = %d", version, latest)
	}

	// A new shard must carry the version SchemaVersion reports; a build that
	// bumps one without the other is caught here rather than by a user.
	dir := t.TempDir()
	s, _ := newStore(t, Config{Dir: dir, RetentionMonths: 12})

	db, err := sqlite.Open(shardPathFor(dir, s.cfg.Now()), time.Second)
	if err != nil {
		t.Fatalf("open shard: %v", err)
	}

	defer func() { _ = db.Close() }()

	var stamped int
	if err := db.QueryRow("PRAGMA user_version").Scan(&stamped); err != nil {
		t.Fatalf("read user_version: %v", err)
	}

	if stamped != version {
		t.Fatalf("new shard user_version = %d, SchemaVersion = %d", stamped, version)
	}
}

// TestSplitSemicolonsEdgeCases covers the places a semicolon must not split a
// statement: inside a quoted string and inside either comment form.
func TestSplitSemicolonsEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "semicolon inside a quoted string",
			src:  `INSERT INTO t (v) VALUES ('a;b'); SELECT 1;`,
			want: []string{`INSERT INTO t (v) VALUES ('a;b')`, "SELECT 1"},
		},
		{
			name: "escaped quote inside a quoted string",
			src:  `INSERT INTO t (v) VALUES ('it''s; fine'); SELECT 1;`,
			want: []string{`INSERT INTO t (v) VALUES ('it''s; fine')`, "SELECT 1"},
		},
		{
			name: "semicolon inside a line comment",
			src:  "SELECT 1 -- one; two\n;\nSELECT 2;",
			want: []string{"SELECT 1 -- one; two", "SELECT 2"},
		},
		{
			name: "semicolon inside a block comment",
			src:  `SELECT 1 /* one; two */; SELECT 2;`,
			want: []string{"SELECT 1 /* one; two */", "SELECT 2"},
		},
		{
			name: "empty statements between queries",
			src:  ";; SELECT 1;; SELECT 2;;",
			want: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name: "only empty statements",
			src:  " ; ; ",
			want: nil,
		},
		{
			name: "empty input",
			src:  "",
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := splitSemicolons(test.src)

			if len(got) != len(test.want) {
				t.Fatalf("splitSemicolons(%q) = %#v, want %#v", test.src, got, test.want)
			}

			for i := range test.want {
				if got[i] != test.want[i] {
					t.Fatalf("statement %d = %q, want %q", i, got[i], test.want[i])
				}
			}
		})
	}
}

// TestSplitStatementsPassesQuotedSemicolonsThrough checks the full splitter,
// including its goose handling, on one plain statement containing a quoted
// semicolon and a commented one.
func TestSplitStatementsPassesQuotedSemicolonsThrough(t *testing.T) {
	src := "INSERT INTO t (v) VALUES ('a;b'); /* ; */ SELECT 1;"
	want := []string{"INSERT INTO t (v) VALUES ('a;b')", "/* ; */ SELECT 1"}

	got := splitStatements(src)

	if len(got) != len(want) {
		t.Fatalf("splitStatements = %#v, want %#v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statement %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// statsFor reads the store statistics and fails the test on error.
func statsFor(t *testing.T, s *Store) Stats {
	t.Helper()

	stats, err := s.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	return stats
}

// TestStatsFollowWrites checks that the totals are maintained by writes rather
// than read from the shards on every call.
func TestStatsFollowWrites(t *testing.T) {
	s, _ := newStore(t, Config{RetentionMonths: 12})
	now := s.cfg.Now()

	put(t, s,
		Row{InfoHash: testHash(1), Name: "One.2020.1080p", CreatedAt: now, Category: 2040},
		Row{InfoHash: testHash(2), Name: "Two.2020.1080p", CreatedAt: now, Category: 2040},
		Row{InfoHash: testHash(3), Name: "Three.2020.1080p", CreatedAt: now, Category: 5000},
	)

	stats := statsFor(t, s)
	if stats.Rows != 3 {
		t.Fatalf("rows = %d, want 3", stats.Rows)
	}

	if got := stats.Categories[2040]; got != 2 {
		t.Fatalf("category 2040 rows = %d, want 2", got)
	}

	if got := stats.Categories[5000]; got != 1 {
		t.Fatalf("category 5000 rows = %d, want 1", got)
	}

	if len(stats.Shards) != 1 || stats.Shards[0].Rows != 3 {
		t.Fatalf("shard rows = %+v, want one shard with 3 rows", stats.Shards)
	}

	// A rediscovery is not a new row.
	n, err := s.PutBatch(context.Background(), []Row{{
		InfoHash: testHash(1), Name: "One.2020.1080p", CreatedAt: now, Category: 2040,
	}})
	if err != nil {
		t.Fatalf("repeat put: %v", err)
	}

	if n != 0 {
		t.Fatalf("repeat put inserted %d rows, want 0", n)
	}

	stats = statsFor(t, s)
	if stats.Rows != 3 || stats.Categories[2040] != 2 {
		t.Fatalf("counts changed on a rediscovery: %+v", stats)
	}
}

// TestStatsReloadOnOpen checks that the in-memory totals are seeded from the
// shards at startup.
func TestStatsReloadOnOpen(t *testing.T) {
	dir := t.TempDir()
	clock := &testClock{t: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)}

	s, err := Open(Config{Dir: dir, Now: clock.Now, RetentionMonths: 12})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	put(t, s,
		Row{InfoHash: testHash(4), Name: "Four.2020.1080p", CreatedAt: clock.Now(), Category: 2040},
		Row{InfoHash: testHash(5), Name: "Five.2020.1080p", CreatedAt: clock.Now(), Category: 2040},
	)

	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(Config{Dir: dir, Now: clock.Now, RetentionMonths: 12})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}

	t.Cleanup(func() { _ = reopened.Close() })

	stats := statsFor(t, reopened)
	if stats.Rows != 2 {
		t.Fatalf("rows after reopen = %d, want 2", stats.Rows)
	}

	if got := stats.Categories[2040]; got != 2 {
		t.Fatalf("category 2040 rows after reopen = %d, want 2", got)
	}
}

// TestRetentionForgetsDroppedShardCounts checks that the totals lose the rows of
// a shard that retention unlinks.
func TestRetentionForgetsDroppedShardCounts(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)}

	s, err := Open(Config{Dir: t.TempDir(), Now: clock.Now, RetentionMonths: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	old := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	put(t, s,
		Row{InfoHash: testHash(6), Name: "Old.One.2026.1080p", CreatedAt: old, Category: 2040},
		Row{InfoHash: testHash(7), Name: "Old.Two.2026.1080p", CreatedAt: old, Category: 2040},
		Row{InfoHash: testHash(8), Name: "New.One.2026.1080p", CreatedAt: clock.Now(), Category: 5000},
	)

	before := statsFor(t, s)
	if before.Rows != 3 {
		t.Fatalf("rows before retention = %d, want 3", before.Rows)
	}

	dropped, err := s.DropExpired(context.Background())
	if err != nil {
		t.Fatalf("drop expired: %v", err)
	}

	if len(dropped) != 1 || dropped[0] != "2026-05" {
		t.Fatalf("dropped = %v, want [2026-05]", dropped)
	}

	after := statsFor(t, s)
	if after.Rows != 1 {
		t.Fatalf("rows after retention = %d, want 1", after.Rows)
	}

	if _, ok := after.Categories[2040]; ok {
		t.Fatalf("dropped shard's category is still counted: %+v", after.Categories)
	}

	if got := after.Categories[5000]; got != 1 {
		t.Fatalf("kept category rows = %d, want 1", got)
	}

	if len(after.Shards) != 1 || after.Shards[0].Month != "2026-11" {
		t.Fatalf("shards after retention = %+v, want the current month only", after.Shards)
	}
}
