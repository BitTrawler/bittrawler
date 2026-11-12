// Package scene reads the parts of a release name that classification needs:
// the season and episode numbers, the air date of a daily show, and the video
// resolution.
//
// Names come from the DHT and are not normalised. Dots, spaces, underscores and
// hyphens are all used as separators, release groups bracket the name, and any
// of the fields may be missing. The parsers therefore accept a set of optional
// separators rather than one grammar, and every parser is a pure function of
// the lowercased name.
package scene

import (
	"strconv"
	"strings"
)

// Resolution is the video resolution stated in a release name.
type Resolution int

const (
	// ResolutionUnknown means the name states no resolution.
	ResolutionUnknown Resolution = iota
	// ResolutionSD covers 480p, 540p, 576p and lower.
	ResolutionSD
	// ResolutionHD covers 720p, 1080p and 1440p.
	ResolutionHD
	// ResolutionUHD covers 2160p and above.
	ResolutionUHD
)

func (r Resolution) String() string {
	switch r {
	case ResolutionSD:
		return "SD"
	case ResolutionHD:
		return "HD"
	case ResolutionUHD:
		return "UHD"
	default:
		return "unknown"
	}
}

// Episodes is the season and episode information parsed from a name.
//
// Seasons lists the season numbers. Episodes lists packed numbers of the form
// season*10000+episode, which is the encoding the store indexes. A season pack
// has seasons and no episodes; a daily episode has neither and is identified by
// its air date instead.
type Episodes struct {
	Seasons  []int
	Episodes []int
}

// Empty reports whether the name carried no season or episode marking.
func (e Episodes) Empty() bool {
	return len(e.Seasons) == 0 && len(e.Episodes) == 0
}

// Name is everything classification reads from a release name alone.
type Name struct {
	// Episodes is the parsed season and episode information.
	Episodes Episodes
	// AirDate is the air date of a daily episode in ISO form, or "".
	AirDate string
	// Resolution is the stated video resolution.
	Resolution Resolution
	// Series is true for a release marked as a complete series or season, which
	// is a television release with no parsable season number.
	Series bool
}

// Parse reads a release name.
func Parse(name string) Name {
	lower := strings.ToLower(name)

	return Name{
		Episodes:   parseEpisodes(lower),
		AirDate:    parseDate(lower),
		Resolution: parseResolution(lower),
		Series:     seriesRegex.MatchString(lower),
	}
}

// atoi converts ASCII digits to an int, ignoring errors: the callers have
// already restricted the input with a regex.
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}

	return n
}
