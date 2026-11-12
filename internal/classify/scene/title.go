package scene

import (
	"regexp"
	"strings"
	"unicode"
)

// Title is the title and release year of a name, for looking the release up in
// an external catalogue. Matching a catalogue needs a title, and release names
// do not state one directly: the title is whatever comes before the first scene
// marker.
type Title struct {
	// Title is the cleaned title, or "" when the name has no usable one.
	Title string
	// Year is the release year, or 0 when the name states none.
	Year int
}

// mediaExtensionRegex strips a trailing file extension such as ".mp4" or
// ".cbz". Only extensions that appear at the end of a torrent name are listed,
// so a scene tag such as ".XviD" is not treated as one.
var mediaExtensionRegex = regexp.MustCompile(
	`(?i)\.(?:mkv|mp4|avi|mov|wmv|flv|m4v|mpg|mpeg|ts|m2ts|mts|vob|webm|rm|rmvb|divx|iso|zip|rar|7z|epub|pdf|cbz|cbr|flac|mp3|m4b|wav|ape|djvu|mobi|azw3)$`)

// leadingGroupRegex strips leading bracket groups such as "[Erai-raws]" or
// "(C87)" from a name before its title is read.
var leadingGroupRegex = regexp.MustCompile(`^\s*(?:\[[^\]]*\]|\([^\)]*\)|【[^】]*】)\s*`)

// yearTokenRegex matches a four-digit year.
var yearTokenRegex = regexp.MustCompile(`^(?:19|20)\d{2}$`)

// resolutionTokenRegex matches a resolution marker.
var resolutionTokenRegex = regexp.MustCompile(
	`(?i)^(?:\d{3,4}[pi]\d{0,2}|\d{3,4}x\d{3,4}|4k|8k|2k|uhd|fhd|hdr10?|hdr|sdr)$`)

// episodeTokenRegex matches a token that marks a season or episode.
var episodeTokenRegex = regexp.MustCompile(
	`(?i)^(?:s\d{1,2}(?:e\d{1,4})?|e\d{1,4}|\d{1,2}x\d{1,4}|season|episode|complete|series|vol|volume|part)$`)

// markerTokens are scene markers that end a title. The list is deliberately
// short: a marker that doubles as a word ("Russian", "The") must not be here.
var markerTokens = map[string]struct{}{
	"aac": {}, "ac3": {}, "atmos": {}, "avc": {}, "bdremux": {}, "bdrip": {},
	"bluray": {}, "blu": {}, "brrip": {}, "cam": {}, "camrip": {},
	"dd": {}, "ddp": {}, "divx": {}, "dts": {}, "dubbed": {}, "dvd": {},
	"dvd5": {}, "dvd9": {}, "dvdr": {}, "dvdrip": {}, "eac3": {}, "extended": {},
	"h264": {}, "h265": {}, "hdtv": {}, "hevc": {}, "internal": {}, "multi": {},
	"ntsc": {}, "pal": {}, "proper": {}, "remastered": {}, "remux": {},
	"repack": {}, "telecine": {}, "telesync": {}, "truehd": {}, "ts": {},
	"uncut": {}, "web": {}, "webdl": {}, "webrip": {}, "workprint": {},
	"x264": {}, "x265": {}, "xvid": {}, "10bit": {}, "amzn": {}, "atvp": {},
	"dsnp": {}, "hmax": {}, "nf": {}, "subbed": {},
}

// ParseTitle reads the title and year from a release name. It returns an empty
// title when there is nothing credible to search for.
func ParseTitle(name string) Title {
	name = mediaExtensionRegex.ReplaceAllString(name, "")

	for {
		trimmed := leadingGroupRegex.ReplaceAllString(name, "")
		if trimmed == name {
			break
		}

		name = trimmed
	}

	words := wordTokens(name)
	if len(words) == 0 {
		return Title{}
	}

	stop := len(words)

	for i, word := range words {
		if isMarkerWord(word) {
			stop = i

			break
		}
	}

	// A year is the release year only when it comes before the other markers;
	// "Blade Runner 2049 (2017)" has two and the later one is the release year.
	year := 0
	titleEnd := stop

	for i := 0; i < stop; i++ {
		if yearTokenRegex.MatchString(words[i]) {
			year = atoi(words[i])
			titleEnd = i
		}
	}

	title := cleanTitle(words[:titleEnd])
	if title == "" {
		return Title{}
	}

	return Title{Title: title, Year: year}
}

// isMarkerWord reports whether a token ends a title.
func isMarkerWord(word string) bool {
	lower := strings.ToLower(word)

	if resolutionTokenRegex.MatchString(word) || episodeTokenRegex.MatchString(word) {
		return true
	}

	_, ok := markerTokens[lower]

	return ok
}

// wordTokens splits a name into alphanumeric words, dropping punctuation. It
// keeps CJK characters together as one token, which is what a title search
// needs.
func wordTokens(name string) []string {
	var (
		tokens []string
		b      strings.Builder
	)

	flush := func() {
		if b.Len() > 0 {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9':
			b.WriteRune(r)
		case r > 127 && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
		default:
			flush()
		}
	}

	flush()

	return tokens
}

// cleanTitle joins the title words and rejects anything that is not a plausible
// title: a single character, punctuation only, or only digits.
func cleanTitle(words []string) string {
	if len(words) == 0 {
		return ""
	}

	title := strings.Join(words, " ")

	if len(title) < 2 {
		return ""
	}

	if strings.Trim(title, "0123456789") == "" {
		return ""
	}

	return title
}
