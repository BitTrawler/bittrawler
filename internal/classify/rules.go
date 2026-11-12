package classify

import (
	"regexp"
	"strings"

	"github.com/BitTrawler/bittrawler/internal/classify/scene"
)

var (
	// episodeNumberRegex matches an anime episode number: " - 10", " - 01v2".
	episodeNumberRegex = regexp.MustCompile(`\s[-–]\s?(\d{1,3})(?:v\d)?(?:\s*[\[\(]|$)`)
	// yearRegex matches a four-digit year, which excludes film series numbering
	// such as "Critters - 2 (1988)".
	yearRegex = regexp.MustCompile(`(?:^|[^0-9])(?:19|20)\d{2}(?:[^0-9]|$)`)
	// domainPrefixRegex strips a domain prefix such as "dccdom.com@".
	domainPrefixRegex = regexp.MustCompile(`(?i)^(?:[a-z0-9][a-z0-9.-]*\.(?:com|net|org|cc|xyz|top|tv|me|info|club|co|cn)@)`)
	// extensionSuffixRegex strips a file extension from a name such as
	// "DVDMS233C.mp4".
	extensionSuffixRegex = regexp.MustCompile(`(?i)\.[a-z0-9]{2,4}$`)
	// codeOnlyRegex matches a name that is nothing but a studio code, optionally
	// behind a numeric prefix, for example "MIAB-573-U" or "259LUXU-1901".
	codeOnlyRegex = regexp.MustCompile(`(?i)^(?:\d{2,4}[ ._-]?)?(?:[a-z]{2,6}[-_]?\d{2,6}(?:[-_]\d{2,6})?(?:[-_]?[a-z]{1,2})?(?:[ ._-]+|$))+$`)
)

// byFiles decides a torrent when the file list is present.
//
// The census is the more trustworthy evidence: a release can be named anything,
// and a name that says "1080p" is no defence against a file list of executables.
// So an unambiguous file list wins: video, audio, software and the document
// buckets decide on their own. The name decides where the files cannot: an
// archive or a disk image says nothing about its contents, and a torrent with no
// file list has nothing else to go on.
//
// The census is a linear pass over extensions and sizes with no regular
// expressions, so walking a large file list is cheap; the keyword expressions
// only ever see a bounded prefix of paths, built by scanText.
func (c *Classifier) byFiles(name, text string, cen census) Result {
	switch cen.dominant() {
	case bucketVideo:
		return c.videoResult(name, text)
	case bucketAudio:
		if cen.audiobook || audiobookKeywordsRegex.MatchString(name) {
			return Result{Category: CategoryAudiobook}
		}

		return Result{Category: CategoryAudio}
	case bucketSoftware:
		return c.softwareResult(name)
	case bucketComic:
		return Result{Category: CategoryComics}
	case bucketEbook:
		return Result{Category: CategoryEbook}
	case bucketImage:
		return c.imageResult(name, text)
	default:
		return c.byName(name, text)
	}
}

// videoResult classifies video files. A season or episode marker makes it
// television even when the name also contains an adult word, which is what a
// series such as "Sex Education" looks like. A date alone is weaker evidence,
// so an adult marker overrides it: date-named adult releases are common. An
// anime episode numbered without a season marker is television too.
func (c *Classifier) videoResult(name, text string) Result {
	parsed := scene.Parse(name)

	if !parsed.Episodes.Empty() || parsed.Series {
		return c.resultFor(kindTV, parsed)
	}

	if adultWeakRegex.MatchString(text) || adultStrongText(text) {
		return c.adultResult()
	}

	if parsed.AirDate != "" {
		return c.resultFor(kindTV, parsed)
	}

	// A name that is only a studio code is a Japanese adult release: measured at
	// 59 of 59 on real torrents.
	if isCodeOnlyName(name) {
		return c.adultResult()
	}

	if looksLikeEpisodeNumber(name, parsed) {
		return c.resultFor(kindTV, parsed)
	}

	return c.resultFor(kindMovie, parsed)
}

// softwareResult classifies an executable file list. The name is still read for
// game markers, because a game and an application are both software releases
// and the names differ; nothing else in the name can override the files.
func (c *Classifier) softwareResult(name string) Result {
	if gameKeywordsRegex.MatchString(name) {
		return Result{Category: CategoryPCGames}
	}

	return Result{Category: CategoryPC}
}

// imageResult classifies an image file list: an adult marker makes it XXX, a
// comic or ebook marker makes it a book, and a set of images with neither is
// not a film even when the name claims to be one.
func (c *Classifier) imageResult(name, text string) Result {
	if adultWeakRegex.MatchString(text) {
		return c.adultResult()
	}

	if comicKeywordsRegex.MatchString(name) || isComicVolume(name) {
		return Result{Category: CategoryComics}
	}

	if ebookKeywordsRegex.MatchString(name) {
		return Result{Category: CategoryEbook}
	}

	return Result{Category: CategoryOther}
}

// isCodeOnlyName reports whether a name is nothing but a studio code, with an
// optional domain prefix and file extension. Codes are the only signal such
// releases carry, and no ordinary release is named this way.
func isCodeOnlyName(name string) bool {
	stem := extensionSuffixRegex.ReplaceAllString(name, "")
	stem = domainPrefixRegex.ReplaceAllString(stem, "")

	return codeOnlyRegex.MatchString(stem)
}

// isComicVolume reports a CJK volume release such as "寄生兽 完全版 第1卷～第4卷":
// an image set with a volume marker is a scanned manga volume.
func isComicVolume(name string) bool {
	for _, marker := range []string{"巻", "卷", "冊", "册"} {
		if strings.Contains(name, marker) {
			return true
		}
	}

	return false
}

// byName classifies a torrent whose files cannot decide it: an archive, a disk
// image, an unknown file type, or no file list at all. This is the original
// ordered rule set, kept for that case only.
func (c *Classifier) byName(name, text string) Result {
	parsed := scene.Parse(name)

	if !parsed.Episodes.Empty() || parsed.Series {
		return c.resultFor(kindTV, parsed)
	}

	if audiobookKeywordsRegex.MatchString(name) {
		return Result{Category: CategoryAudiobook}
	}

	if comicKeywordsRegex.MatchString(name) {
		return Result{Category: CategoryComics}
	}

	if ebookKeywordsRegex.MatchString(name) {
		return Result{Category: CategoryEbook}
	}

	if videoByName(parsed, name) {
		if adultWeakRegex.MatchString(text) || adultStrongText(text) {
			return c.adultResult()
		}

		return c.resultFor(kindMovie, parsed)
	}

	if isCodeOnlyName(name) {
		return c.adultResult()
	}

	if gameKeywordsRegex.MatchString(name) {
		return Result{Category: CategoryPCGames}
	}

	if softwareKeywordsRegex.MatchString(name) {
		return Result{Category: CategoryPC}
	}

	if musicKeywordsRegex.MatchString(name) {
		return Result{Category: CategoryAudio}
	}

	if adultWeakRegex.MatchString(text) {
		return c.adultResult()
	}

	// A date is the weakest television signal, so it is tested last.
	if parsed.AirDate != "" {
		return c.resultFor(kindTV, parsed)
	}

	return Result{Category: CategoryOther}
}

// looksLikeEpisodeNumber reports an anime episode named without a season, such
// as "Title - 10 (1080p)". It requires a stated resolution, so a film number in
// a title is not enough, and it excludes names that carry a year, which is how
// films in a series are named.
func looksLikeEpisodeNumber(name string, parsed scene.Name) bool {
	if parsed.Resolution == scene.ResolutionUnknown {
		return false
	}

	if yearRegex.MatchString(name) {
		return false
	}

	return episodeNumberRegex.MatchString(name)
}

// tvSignal reports whether the name carries a television marker: a season or
// episode number, an air date for a daily show, or a complete-series label.
func tvSignal(parsed scene.Name) bool {
	return !parsed.Episodes.Empty() || parsed.AirDate != "" || parsed.Series
}

// videoByName reports whether the name alone identifies a video release, either
// because it states a resolution or because it carries a source, codec or
// container marker.
func videoByName(parsed scene.Name, name string) bool {
	return parsed.Resolution != scene.ResolutionUnknown || sourceKeywordsRegex.MatchString(name)
}
