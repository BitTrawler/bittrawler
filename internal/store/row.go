package store

import (
	"strconv"
	"strings"
	"time"
)

// Row is one torrent as it is written to a shard. Fields that are unknown are
// represented by zero values: ContentID 0, FilesCount -1, AirDate "".
type Row struct {
	InfoHash    [20]byte
	Name        string
	Size        int64
	CreatedAt   time.Time
	Category    int
	ContentID   int64
	Seasons     []int // season numbers, e.g. {5} or {1,2,3}
	Episodes    []int // packed season*10000+episode; empty for a season pack
	AirDate     string
	FilesCount  int
	PrimaryFile string
	Seeders     int
	Leechers    int
}

// Torrent is one stored torrent as returned by a query.
type Torrent struct {
	InfoHash    [20]byte
	Name        string
	Size        int64
	CreatedAt   time.Time
	Category    int
	ContentID   int64
	Seasons     []int
	Episodes    []int
	AirDate     string
	FilesCount  int
	PrimaryFile string
	Seeders     int
	Leechers    int
}

func monthKey(t time.Time) string {
	return t.UTC().Format("2006-01")
}

func monthStart(key string) (time.Time, error) {
	return time.Parse("2006-01", key)
}

// seasonsToken renders the season numbers as FTS5 tokens: {5,6} becomes
// "s5 s6". It returns nil when there are no seasons.
func seasonsToken(seasons []int) any {
	if len(seasons) == 0 {
		return nil
	}

	parts := make([]string, 0, len(seasons))
	for _, s := range seasons {
		parts = append(parts, "s"+strconv.Itoa(s))
	}

	return strings.Join(parts, " ")
}

// episodesToken renders packed episodes as FTS5 tokens: {50001} becomes
// "e50001". A season pack has no episode tokens, which is what keeps packs out
// of episode searches.
func episodesToken(episodes []int) any {
	if len(episodes) == 0 {
		return nil
	}

	parts := make([]string, 0, len(episodes))
	for _, e := range episodes {
		parts = append(parts, "e"+strconv.Itoa(e))
	}

	return strings.Join(parts, " ")
}

// kindToken marks a release as a season pack or an episode release, so a season
// search can rank packs first without another index.
func kindToken(seasons, episodes []int) any {
	switch {
	case len(episodes) > 0:
		return "ep"
	case len(seasons) > 0:
		return "pack"
	default:
		return nil
	}
}

func timeFromUnix(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}

// parseTokens converts synthetic FTS tokens back to numbers: "s5 s6" -> {5,6}.
func parseTokens(s string, prefix byte) []int {
	if s == "" {
		return nil
	}

	parts := strings.Fields(s)
	out := make([]int, 0, len(parts))

	for _, p := range parts {
		if len(p) < 2 || p[0] != prefix {
			continue
		}

		n, err := strconv.Atoi(p[1:])
		if err != nil {
			continue
		}

		out = append(out, n)
	}

	return out
}
