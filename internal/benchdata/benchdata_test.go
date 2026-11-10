package benchdata

import (
	"math"
	"reflect"
	"testing"
	"time"
)

const (
	// distributionRows is the sample size the distribution assertions use.
	distributionRows = 20000
)

// testNow is the generator's documented default instant. Passing it explicitly
// keeps the expected values independent of the wall clock.
var testNow = time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)

func percent(part, total int) float64 {
	return 100 * float64(part) / float64(total)
}

func assertWithin(t *testing.T, label string, got, want, tolerance float64) {
	t.Helper()

	if math.Abs(got-want) > tolerance {
		t.Errorf("%s = %.3f, want %.3f +/- %.3f", label, got, want, tolerance)
	}
}

// TestRowsDeterministic checks that generation depends only on Options: two
// calls with the same options produce identical rows, field for field.
func TestRowsDeterministic(t *testing.T) {
	optionSets := []Options{
		{Count: 500, Seed: 7, Days: 30, Now: testNow},
		{Count: 100}, // all defaults, including the default clock
	}

	for i, opts := range optionSets {
		first := Rows(opts)
		second := Rows(opts)

		if !reflect.DeepEqual(first, second) {
			t.Fatalf("option set %d: Rows is not deterministic", i)
		}

		if len(first) != opts.Count {
			t.Fatalf("option set %d: got %d rows, want %d", i, len(first), opts.Count)
		}
	}

	// A different seed must produce different data, otherwise the check above
	// could pass on a generator that ignores Options entirely.
	base := Options{Count: 200, Seed: 1, Now: testNow}
	other := base
	other.Seed = 2

	if reflect.DeepEqual(Rows(base), Rows(other)) {
		t.Fatal("different seeds produced identical rows")
	}
}

// TestRowsWindow checks the corpus window: rows are evenly spread, oldest
// first, from Now-Days to just short of Now.
func TestRowsWindow(t *testing.T) {
	now := testNow
	rows := Rows(Options{Count: distributionRows, Now: now})

	if len(rows) != distributionRows {
		t.Fatalf("got %d rows, want %d", len(rows), distributionRows)
	}

	span := 80 * 24 * time.Hour

	first := rows[0].CreatedAt
	if !first.Equal(now.Add(-span)) {
		t.Errorf("first created_at = %s, want %s", first, now.Add(-span))
	}

	// The last row sits one step short of Now, not at Now: exactly
	// span/Count short. Allow a second for float64 rounding in the step.
	step := span / distributionRows
	last := rows[len(rows)-1].CreatedAt

	if gap := now.Sub(last); math.Abs(float64(gap-step)) > float64(time.Second) {
		t.Errorf("distance from Now to last created_at = %s, want about %s", gap, step)
	}

	for i := 1; i < len(rows); i++ {
		if !rows[i].CreatedAt.After(rows[i-1].CreatedAt) {
			t.Fatalf("created_at not strictly ascending at row %d: %s then %s",
				i, rows[i-1].CreatedAt, rows[i].CreatedAt)
		}
	}
}

// TestRowsDistributions pins the measured corpus shape the benchmarks rely on:
// lots of unclassified rows, TV about an eighth, a third without a file list,
// low seeder counts, and TV-only season/episode metadata.
func TestRowsDistributions(t *testing.T) {
	rows := Rows(Options{Count: distributionRows, Seed: 42, Now: testNow})

	var (
		noContentType int
		tvRows        int
		noFiles       int
		seeders       int
		tvPacks       int
		tvEpisodes    int
	)

	for _, row := range rows {
		if row.Category == 8000 {
			noContentType++
		}

		isTV := row.Category >= 5000 && row.Category <= 5999

		if isTV {
			tvRows++
		}

		if row.FilesCount == -1 {
			noFiles++
		}

		seeders += row.Seeders

		if isTV {
			if len(row.Seasons) == 0 {
				t.Fatalf("TV row %q has no seasons", row.Name)
			}

			if len(row.Episodes) == 0 {
				tvPacks++
			} else {
				tvEpisodes++
			}

			continue
		}

		if len(row.Seasons) != 0 || len(row.Episodes) != 0 {
			t.Fatalf("non-TV row %q (category %d) has season/episode metadata", row.Name, row.Category)
		}
	}

	if tvRows == 0 || tvEpisodes+tvPacks == 0 {
		t.Fatalf("sample has %d TV rows; cannot check TV shape", tvRows)
	}

	noContentTypePct := percent(noContentType, len(rows))
	tvPct := percent(tvRows, len(rows))
	noFilesPct := percent(noFiles, len(rows))
	meanSeeders := float64(seeders) / float64(len(rows))
	packPct := percent(tvPacks, tvRows)

	t.Logf("no content type %.2f%%, TV %.2f%%, no file list %.2f%%, mean seeders %.3f, TV packs %.2f%%",
		noContentTypePct, tvPct, noFilesPct, meanSeeders, packPct)

	assertWithin(t, "no-content-type share (%)", noContentTypePct, 45.6, 3)
	assertWithin(t, "TV share (%)", tvPct, 12.3, 3)
	assertWithin(t, "no-file-list share (%)", noFilesPct, 34, 3)
	assertWithin(t, "mean seeders", meanSeeders, 1.2, 0.2)
	assertWithin(t, "TV season-pack share (%)", packPct, 30, 5)
}

// TestNewznabCategory covers the resolution-aware category mapping.
func TestNewznabCategory(t *testing.T) {
	cases := []struct {
		name string
		code int
		want int
	}{
		{"The.Show.S02.2160p.WEB-DL", ctTV, 5045},
		{"The.Show.S02E03.1080p.WEB-DL", ctTV, 5040},
		{"The.Show.S02E03.480p.HDTV", ctTV, 5030},
		{"The.Show.S02E03.WEB-DL", ctTV, 5040},
		{"The.Movie.2024.2160p.BluRay", ctMovie, 2045},
		{"The.Movie.2024.480p.DVDRip", ctMovie, 2030},
		{"The.Movie.2024.BluRay", ctMovie, 2040},
		{"Artist - Album (2020) [FLAC]", ctMusic, 3040},
		{"Artist - Album (2020) [MP3]", ctMusic, 3010},
		{"Some.Game.REPACK-GRP", ctGame, 1000},
		{"an.unclassified.release", 0, 8000},
	}

	for _, tc := range cases {
		if got := newznabCategory(tc.code, tc.name); got != tc.want {
			t.Errorf("newznabCategory(%d, %q) = %d, want %d", tc.code, tc.name, got, tc.want)
		}
	}
}

// TestRowsHashesUnique checks the infohashes are drawn from the whole 160-bit
// space, so a large sample does not collide.
func TestRowsHashesUnique(t *testing.T) {
	rows := Rows(Options{Count: distributionRows, Seed: 42, Now: testNow})

	seen := make(map[[20]byte]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.InfoHash]; ok {
			t.Fatalf("duplicate infohash %x", row.InfoHash)
		}

		seen[row.InfoHash] = struct{}{}
	}
}
