package app

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/BitTrawler/bittrawler/internal/version"
)

// TestHumanCount checks the stat tile's thousands grouping.
func TestHumanCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value int
		want  string
	}{
		{"zero", 0, "0"},
		{"one", 1, "1"},
		{"below a thousand", 999, "999"},
		{"a thousand", 1000, "1,000"},
		{"fifteen hundred", 1500, "1,500"},
		{"a million", 1_000_000, "1,000,000"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, humanCount(tc.value))
		})
	}
}

// TestHumanBytes checks the binary units and the one-decimal rendering.
func TestHumanBytes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value int64
		want  string
	}{
		{"zero", 0, "0 B"},
		{"one byte", 1, "1 B"},
		{"just under a kibibyte", 1023, "1023 B"},
		{"one kibibyte", 1024, "1.0 KiB"},
		{"one and a half kibibytes", 1536, "1.5 KiB"},
		{"one gibibyte", 1 << 30, "1.0 GiB"},
		{"largest int64", math.MaxInt64, "8.0 EiB"},
		// A negative value is never scaled, whatever its magnitude.
		{"negative byte count", -1, "-1 B"},
		{"negative kibibytes", -1 << 20, "-1048576 B"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, humanBytes(tc.value))
		})
	}
}

// TestHumanRate checks the ingest rate: one decimal below ten, whole numbers
// above it.
func TestHumanRate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value float64
		want  string
	}{
		{"zero", 0, "0.0"},
		{"a fraction", 0.4, "0.4"},
		{"rounds to one decimal", 7.26, "7.3"},
		{"rounds up to ten", 9.99, "10.0"},
		{"ten", 10, "10"},
		{"large", 12345.6, "12346"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, humanRate(tc.value))
		})
	}
}

// TestHumanAge checks the buckets the feed's age column uses. Months and years
// are deliberately shown as days.
func TestHumanAge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 13, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		age  time.Duration
		want string
	}{
		{"zero", 0, "now"},
		{"seconds", 30 * time.Second, "now"},
		{"just under a minute", 59 * time.Second, "now"},
		{"one minute", time.Minute, "1m"},
		{"minutes", 5 * time.Minute, "5m"},
		{"just under an hour", 59*time.Minute + 59*time.Second, "59m"},
		{"one hour", time.Hour, "1h"},
		{"hours", 23 * time.Hour, "23h"},
		{"one day", 24 * time.Hour, "1d"},
		{"days", 40 * 24 * time.Hour, "40d"},
		{"months", 120 * 24 * time.Hour, "120d"},
		{"years", 365 * 24 * time.Hour, "365d"},
		// A row whose clock is ahead reads as "now", never as negative.
		{"future", -time.Hour, "now"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, humanAge(now, now.Add(-tc.age)))
		})
	}
}

// TestAssetStamp checks the query string the static assets carry: a build with a
// commit is named by it, so its assets can be cached for good, and a local build
// is named per run so a restart cannot be served the previous build's files.
func TestAssetStamp(t *testing.T) {
	t.Parallel()

	started := time.Unix(1_700_000_000, 0)

	assert.Equal(t, "abc1234", assetStamp(version.Info{Commit: "abc1234567890"}, started))
	assert.Equal(t, "test", assetStamp(version.Info{Version: "test", Commit: "test"}, started))
	assert.Equal(t, "dev-1700000000", assetStamp(version.Info{Version: "dev", Commit: "unknown"}, started))
	assert.Equal(t, "dev-1700000000", assetStamp(version.Info{Version: "dev"}, started))
}
