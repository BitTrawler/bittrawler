package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/BitTrawler/bittrawler/internal/version"
)

// humanBytes formats a byte count with binary units.
func humanBytes(value int64) string {
	const unit = 1024

	if value < unit {
		return fmt.Sprintf("%d B", value)
	}

	div, exponent := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exponent++
	}

	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exponent])
}

// humanCount formats an integer with thin thousands separators.
func humanCount(value int) string {
	text := strconv.Itoa(value)
	if value < 1000 {
		return text
	}

	var b strings.Builder

	for i, digit := range text {
		if i > 0 && (len(text)-i)%3 == 0 {
			b.WriteString(",")
		}

		b.WriteRune(digit)
	}

	return b.String()
}

// humanRate formats an ingest rate for a stat tile.
func humanRate(value float64) string {
	if value < 10 {
		return fmt.Sprintf("%.1f", value)
	}

	return fmt.Sprintf("%.0f", value)
}

// humanAge renders how long ago a torrent was added.
func humanAge(now, at time.Time) string {
	age := now.Sub(at)

	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	}
}

// humanDate renders an absolute time for a detail page.
func humanDate(at time.Time) string {
	return at.Local().Format("2 Jan 2006, 15:04")
}

// formatSeasons renders season numbers as "S5, S6".
func formatSeasons(seasons []int) string {
	if len(seasons) == 0 {
		return ""
	}

	parts := make([]string, 0, len(seasons))
	for _, season := range seasons {
		parts = append(parts, fmt.Sprintf("S%d", season))
	}

	return strings.Join(parts, ", ")
}

// formatEpisodes renders packed episodes as "S5E1, S5E2".
func formatEpisodes(episodes []int) string {
	if len(episodes) == 0 {
		return ""
	}

	parts := make([]string, 0, len(episodes))
	for _, episode := range episodes {
		parts = append(parts, fmt.Sprintf("S%dE%d", episode/10000, episode%10000))
	}

	return strings.Join(parts, ", ")
}

// assetStamp is the build stamp the static asset URLs carry. A build with a
// commit gets its short commit, so the browser can cache the stylesheet, script
// and icons until that build is replaced. A local build has no commit to name
// it, so it gets a stamp per run: a restarted instance is never served assets
// from the previous one.
func assetStamp(info version.Info, started time.Time) string {
	commit := info.Commit
	if commit != "" && commit != "unknown" {
		if len(commit) > 7 {
			return commit[:7]
		}

		return commit
	}

	return fmt.Sprintf("%s-%d", info.Version, started.Unix())
}
