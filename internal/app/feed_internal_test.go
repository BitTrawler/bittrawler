package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/web"
)

// TestHistoryBars checks the activity chart heights.
func TestHistoryBars(t *testing.T) {
	t.Parallel()

	t.Run("no history", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, historyBars(nil))
	})

	t.Run("one sample", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, historyBars([]rateSample{{persisted: 43}}))
	})

	t.Run("all-zero counters", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []int{0, 0}, historyBars([]rateSample{{}, {}, {}}))
	})

	t.Run("scaled to the peak", func(t *testing.T) {
		t.Parallel()

		bars := historyBars([]rateSample{
			{persisted: 0},
			{persisted: 10},
			{persisted: 40},
			{persisted: 100},
		})

		// Deltas of 10, 30 and 60 against a peak of 60 scale to 4+96*delta/peak.
		assert.Equal(t, []int{20, 52, 100}, bars)
		assert.Equal(t, 100, bars[len(bars)-1], "the busiest interval is full height")
	})

	t.Run("minimum heights", func(t *testing.T) {
		t.Parallel()

		// A flat interval draws as one per cent.
		assert.Equal(t, []int{1, 100}, historyBars([]rateSample{
			{persisted: 5},
			{persisted: 5},
			{persisted: 1005},
		}))

		// A busy interval never falls below four per cent, whatever the peak.
		assert.Equal(t, []int{4, 100}, historyBars([]rateSample{
			{persisted: 0},
			{persisted: 1},
			{persisted: 1_000_001},
		}))
	})

	t.Run("counter reset", func(t *testing.T) {
		t.Parallel()

		// A restart resets the persisted counter; the unsigned difference is
		// enormous, so the interval is drawn empty rather than full.
		assert.Equal(t, []int{0}, historyBars([]rateSample{
			{persisted: 100},
			{persisted: 40},
		}))
	})

	t.Run("limited to the chart width", func(t *testing.T) {
		t.Parallel()

		history := make([]rateSample, 0, sparklineBars+10)
		for i := 0; i < sparklineBars+10; i++ {
			history = append(history, rateSample{persisted: uint64(i * 10)})
		}

		bars := historyBars(history)
		require.Len(t, bars, sparklineBars, "only the newest samples are drawn")

		for _, height := range bars {
			assert.Equal(t, 100, height, "every interval has the same delta as the peak")
		}
	})
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
