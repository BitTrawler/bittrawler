package scene

import (
	"regexp"
	"strings"
)

// heightRegex reads a resolution token such as "1080p" or "1080p60". Only the
// height and the p/i suffix matter; anything after them is ignored, so bit
// depth and frame rate do not hide the resolution.
var heightRegex = regexp.MustCompile(`^(\d{3,4})[pi]`)

// parseResolution reads the video resolution from a lowercased name.
//
// The name is split into alphanumeric tokens, which is both cheaper and more
// predictable than one large expression. The highest resolution found wins, so
// a name that mentions both 1080p and 2160p is UHD.
func parseResolution(lower string) Resolution {
	best := ResolutionUnknown

	for _, token := range strings.FieldsFunc(lower, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		best = maxResolution(best, resolutionOfToken(token))
	}

	return best
}

func resolutionOfToken(token string) Resolution {
	switch token {
	case "4k", "8k", "uhd":
		return ResolutionUHD
	case "2k", "fhd", "hd":
		return ResolutionHD
	case "sd":
		return ResolutionSD
	}

	// A dimensions token such as "1920x1080". The smaller side is used as the
	// height, which gives the right answer for both landscape and portrait
	// frame sizes.
	if width, height, ok := dimensions(token); ok {
		return resolutionOfHeight(min(width, height))
	}

	if match := heightRegex.FindStringSubmatch(token); match != nil {
		return resolutionOfHeight(atoi(match[1]))
	}

	return ResolutionUnknown
}

func resolutionOfHeight(height int) Resolution {
	switch {
	case height >= 2160:
		return ResolutionUHD
	case height >= 720:
		return ResolutionHD
	case height >= 240:
		return ResolutionSD
	default:
		return ResolutionUnknown
	}
}

// dimensions parses a "1920x1080" style token. Only a token that is entirely
// two numbers separated by a single x qualifies, so "x265" and "1920x1080p"
// are left alone.
func dimensions(token string) (int, int, bool) {
	width, rest, found := strings.Cut(token, "x")
	if !found || !allDigits(width) || !allDigits(rest) {
		return 0, 0, false
	}

	if width == "" || rest == "" {
		return 0, 0, false
	}

	return atoi(width), atoi(rest), true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

func maxResolution(a, b Resolution) Resolution {
	if b > a {
		return b
	}

	return a
}
