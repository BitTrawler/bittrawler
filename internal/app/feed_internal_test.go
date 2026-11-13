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

// TestCategoryRows checks the stored category breakdown: ordering, row counts
// and the share column.
func TestCategoryRows(t *testing.T) {
	t.Parallel()

	t.Run("largest first then by category", func(t *testing.T) {
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
			// Equal counts are ordered by category id, so 2040 precedes 5000.
			{Name: "Movies/HD", Rows: "600", Share: "21.9%"},
			{Name: "TV", Rows: "600", Share: "21.9%"},
			{Name: "Audio", Rows: "300", Share: "11.0%"},
			{Name: "Books", Rows: "0", Share: "0.0%"},
		}, rows)
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
