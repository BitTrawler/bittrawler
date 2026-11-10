package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
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
	for i := range got {
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
