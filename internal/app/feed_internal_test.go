package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/drops"
	"github.com/BitTrawler/bittrawler/internal/web"
)

// TestHistoryBars checks the activity chart heights.
func TestHistoryBars(t *testing.T) {
	t.Parallel()

	t.Run("no history", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, historyBars(historyDeltas(nil)))
	})

	t.Run("one sample", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, historyBars(historyDeltas([]rateSample{{persisted: 43}})))
	})

	t.Run("all-zero counters", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []int{0, 0}, historyBars(historyDeltas([]rateSample{{}, {}, {}})))
	})

	t.Run("scaled to the peak", func(t *testing.T) {
		t.Parallel()

		bars := historyBars(historyDeltas([]rateSample{
			{persisted: 0},
			{persisted: 10},
			{persisted: 40},
			{persisted: 100},
		}))

		// Deltas of 10, 30 and 60 against a peak of 60 scale to 4+96*delta/peak.
		assert.Equal(t, []int{20, 52, 100}, bars)
		assert.Equal(t, 100, bars[len(bars)-1], "the busiest interval is full height")
	})

	t.Run("minimum heights", func(t *testing.T) {
		t.Parallel()

		// A flat interval draws as one per cent.
		assert.Equal(t, []int{1, 100}, historyBars(historyDeltas([]rateSample{
			{persisted: 5},
			{persisted: 5},
			{persisted: 1005},
		})))

		// A busy interval never falls below four per cent, whatever the peak.
		assert.Equal(t, []int{4, 100}, historyBars(historyDeltas([]rateSample{
			{persisted: 0},
			{persisted: 1},
			{persisted: 1_000_001},
		})))
	})

	t.Run("counter reset", func(t *testing.T) {
		t.Parallel()

		// A restart resets the persisted counter; the unsigned difference is
		// enormous, so the interval is drawn empty rather than full.
		assert.Equal(t, []int{0}, historyBars(historyDeltas([]rateSample{
			{persisted: 100},
			{persisted: 40},
		})))
	})

	t.Run("limited to the chart width", func(t *testing.T) {
		t.Parallel()

		history := make([]rateSample, 0, sparklineBars+10)
		for i := 0; i < sparklineBars+10; i++ {
			history = append(history, rateSample{persisted: uint64(i * 10)})
		}

		bars := historyBars(historyDeltas(history))
		require.Len(t, bars, sparklineBars, "only the newest samples are drawn")

		for _, height := range bars {
			assert.Equal(t, 100, height, "every interval has the same delta as the peak")
		}
	})
}

// TestHistorySummary checks the chart description uses the real counts rather
// than the scaled bar heights.
func TestHistorySummary(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "No ingest activity samples yet.", historySummary(nil))

	deltas := historyDeltas([]rateSample{
		{persisted: 0},
		{persisted: 10},
		{persisted: 40},
		{persisted: 100},
	})

	assert.Equal(t, []int{10, 30, 60}, deltas)
	assert.Equal(t,
		"Ingest activity over the last 15 minutes: up to 60 torrents between samples, 60 since the last sample.",
		historySummary(deltas))
}

// TestCategoryRows checks the classification table: families ordered by their
// total, the subcategories nested beneath them, and the share column.
func TestCategoryRows(t *testing.T) {
	t.Parallel()

	t.Run("families largest first with their subcategories", func(t *testing.T) {
		t.Parallel()

		rows := categoryRows(statsSnapshot{
			StoreRows: 2734,
			Categories: map[int]int{
				classify.CategoryAudio:    300,
				classify.CategoryTV:       600,
				classify.CategoryMoviesHD: 600,
				classify.CategoryBooks:    0,
				classify.CategoryOther:    1234,
			},
		})

		assert.Equal(t, []web.CategoryRow{
			{Name: "Other", Rows: "1,234", Share: "45.1%"},
			{
				Name:  "Movies",
				Rows:  "600",
				Share: "21.9%",
				// The family is on the row above, so the subcategory carries
				// only its own part of the name.
				Children: []web.CategoryRow{{Name: "HD", Rows: "600", Share: "21.9%"}},
			},
			// Equal totals are ordered by category id, so Movies precedes TV.
			{Name: "TV", Rows: "600", Share: "21.9%"},
			{Name: "Audio", Rows: "300", Share: "11.0%"},
			{Name: "Books", Rows: "0", Share: "0.0%"},
		}, rows)
	})

	t.Run("rows stored at the family itself are a subcategory", func(t *testing.T) {
		t.Parallel()

		rows := categoryRows(statsSnapshot{
			StoreRows: 10000,
			Categories: map[int]int{
				classify.CategoryMovies:   5067,
				classify.CategoryMoviesHD: 3002,
			},
		})

		assert.Equal(t, []web.CategoryRow{{
			Name:  "Movies",
			Rows:  "8,069",
			Share: "80.7%",
			Children: []web.CategoryRow{
				{Name: "Movies", Rows: "5,067", Share: "50.7%"},
				{Name: "HD", Rows: "3,002", Share: "30.0%"},
			},
		}}, rows)
	})

	t.Run("a family total adds its leaves", func(t *testing.T) {
		t.Parallel()

		rows := categoryRows(statsSnapshot{
			StoreRows: 1000,
			Categories: map[int]int{
				classify.CategoryMoviesHD:  437,
				classify.CategoryMoviesUHD: 12,
			},
		})

		assert.Equal(t, []web.CategoryRow{{
			Name:  "Movies",
			Rows:  "449",
			Share: "44.9%",
			Children: []web.CategoryRow{
				{Name: "HD", Rows: "437", Share: "43.7%"},
				{Name: "UHD", Rows: "12", Share: "1.2%"},
			},
		}}, rows)
	})

	t.Run("share rounds to one decimal", func(t *testing.T) {
		t.Parallel()

		rows := categoryRows(statsSnapshot{
			StoreRows: 3,
			Categories: map[int]int{
				classify.CategoryMovies: 1,
				classify.CategoryTV:     2,
			},
		})

		assert.Equal(t, []web.CategoryRow{
			{Name: "TV", Rows: "2", Share: "66.7%"},
			{Name: "Movies", Rows: "1", Share: "33.3%"},
		}, rows)
	})

	t.Run("empty store has no share", func(t *testing.T) {
		t.Parallel()

		rows := categoryRows(statsSnapshot{
			Categories: map[int]int{classify.CategoryMovies: 5},
		})

		assert.Equal(t, []web.CategoryRow{{Name: "Movies", Rows: "5", Share: ""}}, rows)
	})

	t.Run("unknown category", func(t *testing.T) {
		t.Parallel()

		rows := categoryRows(statsSnapshot{
			StoreRows:  1,
			Categories: map[int]int{9999: 1},
		})

		assert.Equal(t, []web.CategoryRow{{Name: "Unknown", Rows: "1", Share: "100.0%"}}, rows)
	})
}

// TestContentLinks checks the catalogue links a stored content reference gets.
func TestContentLinks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		category int
		content  int64
		tvdb     int64
		tmdb     string
		tvdbURL  string
	}{
		{
			name:     "film",
			category: classify.CategoryMoviesHD,
			content:  414906,
			tmdb:     "https://www.themoviedb.org/movie/414906",
		},
		{
			name:     "show",
			category: classify.CategoryTVHD,
			content:  1396,
			tvdb:     81189,
			tmdb:     "https://www.themoviedb.org/tv/1396",
			tvdbURL:  "https://thetvdb.com/?tab=series&id=81189",
		},
		{
			name:     "show without a TVDB id",
			category: classify.CategoryTV,
			content:  1396,
			tmdb:     "https://www.themoviedb.org/tv/1396",
		},
		{
			name:     "no reference",
			category: classify.CategoryMoviesHD,
		},
		{
			name:     "not a catalogue category",
			category: classify.CategoryAudioMP3,
			content:  414906,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tmdbURL, tvdbURL := contentLinks(testCase.category, testCase.content, testCase.tvdb)

			assert.Equal(t, testCase.tmdb, tmdbURL)
			assert.Equal(t, testCase.tvdbURL, tvdbURL)
		})
	}
}

// TestConfiguredRejections turns the settings page's reason names into the
// classifier's reasons. An empty result means the feed is switched off, which is
// what the overview checks before rendering it.
func TestConfiguredRejections(t *testing.T) {
	t.Parallel()

	assert.Nil(t, configuredRejections(nil), "no reasons switched on turns the feed off")
	assert.Nil(t, configuredRejections([]string{}))

	assert.Equal(t, []classify.Drop{
		classify.DropBanned,
		classify.DropAdult,
	}, configuredRejections([]string{"banned", "adult"}))

	// An unknown name is ignored rather than widening the selection.
	assert.Equal(t, []classify.Drop{classify.DropScript}, configuredRejections([]string{"klingon", "script"}))

	// The blocking checks that run before the classifier have their own reason,
	// so they can be switched separately from the content rules.
	assert.Equal(t, []classify.Drop{classify.DropBlocked}, configuredRejections([]string{"blocked"}))
}

// TestRejectedRows checks the words and formatting the overview uses for the
// rejected feed.
func TestRejectedRows(t *testing.T) {
	t.Parallel()

	now := time.Now()

	application := &App{rejected: drops.NewRecorder(10)}

	application.rejected.Add(drops.Event{
		Name:   "Some.Bait.2024.1080p",
		Size:   1536,
		Reason: classify.DropDeceptive,
		At:     now.Add(-2 * time.Minute),
	})
	application.rejected.Add(drops.Event{
		Name:   "寄生兽 完全版",
		Size:   1024,
		Reason: classify.DropScript,
		At:     now,
	})
	application.rejected.Add(drops.Event{
		Name:   "1Kb",
		Size:   1024,
		Reason: classify.DropBlocked,
		At:     now,
	})

	all := configuredRejections([]string{"banned", "deceptive", "adult", "script", "blocked"})

	assert.Equal(t, []web.RejectedRow{
		{Name: "1Kb", Reason: "Blocked", Size: "1.0 KiB", Age: "now"},
		{Name: "寄生兽 完全版", Reason: "Foreign script", Size: "1.0 KiB", Age: "now"},
		{Name: "Some.Bait.2024.1080p", Reason: "Deceptive", Size: "1.5 KiB", Age: "2m"},
	}, application.rejectedRows(10, all))
}

// TestRejectedRowsFilterBeforeTheLimit checks that the configured reasons are
// applied before the limit, so a filtered list is not cut short by rejections
// of other reasons.
func TestRejectedRowsFilterBeforeTheLimit(t *testing.T) {
	t.Parallel()

	recorder := drops.NewRecorder(10)

	for i := 0; i < 5; i++ {
		recorder.Add(drops.Event{Name: "adult", Reason: classify.DropAdult})
		recorder.Add(drops.Event{Name: "banned", Reason: classify.DropBanned})
	}

	application := &App{rejected: recorder}

	rows := application.rejectedRows(3, configuredRejections([]string{"banned"}))
	require.Len(t, rows, 3)

	for _, row := range rows {
		assert.Equal(t, "Banned", row.Reason)
	}
}

// TestRefreshMillis checks the interval the page's script runs on.
func TestRefreshMillis(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 3000, refreshMillis(3*time.Second))
	assert.Equal(t, 60000, refreshMillis(time.Minute))

	// A page rendered without settings still gets a usable interval.
	assert.Equal(t, 1000, refreshMillis(0))
	assert.Equal(t, 1000, refreshMillis(time.Millisecond))
}
