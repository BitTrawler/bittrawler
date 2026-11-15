package classify

import (
	"strings"

	"github.com/BitTrawler/bittrawler/internal/classify/scene"
)

// File is one file of a torrent. Path is the display path; only the extension
// and the name are read.
type File struct {
	Path string
	Size int64
}

// Drop says why a torrent must not be indexed. A dropped torrent is never
// written: the crawl discards it and, for DropBanned, records the infohash in
// the blocklist so it cannot come back.
type Drop uint8

const (
	// DropNone means the torrent is indexed.
	DropNone Drop = iota
	// DropBanned means the name or a file path matches the blocking rules.
	DropBanned
	// DropDeceptive means the files are executables while the name claims to
	// be media. That is malware with a release name, not a misclassification.
	DropDeceptive
	// DropAdult means adult content was dropped by configuration.
	DropAdult
	// DropScript means the name is written in a script the configuration filters
	// out. Like the adult drop, it is a setting, not a block.
	DropScript
)

func (d Drop) String() string {
	switch d {
	case DropBanned:
		return "banned"
	case DropDeceptive:
		return "deceptive"
	case DropAdult:
		return "adult"
	case DropScript:
		return "script"
	default:
		return "none"
	}
}

// AdultEvidence says how strong the evidence for the adult classification was.
// A weak classification is a judgement call that an external catalogue may
// overturn; a strong one is not.
type AdultEvidence uint8

const (
	// AdultEvidenceNone means the result is not an adult classification.
	AdultEvidenceNone AdultEvidence = iota
	// AdultEvidenceWeak means the result rests on a general word such as "sex"
	// or "milf", which appears in mainstream titles and performer names.
	AdultEvidenceWeak
	// AdultEvidenceStrong means the result rests on a marker that only adult
	// releases carry: a studio name, an explicit term, a studio code.
	AdultEvidenceStrong
)

// Result is the classification of one torrent.
type Result struct {
	// Category is the Newznab category to store. Parent categories are used
	// when the leaf cannot be justified, so 2000 means "a film of unknown
	// resolution" and 2040 means "a film in 720p, 1080p or 1440p".
	Category int
	// Seasons lists the season numbers of a television release.
	Seasons []int
	// Episodes lists packed episode numbers, season*10000+episode.
	Episodes []int
	// AirDate is the air date of a daily episode in ISO form, or "".
	AirDate string
	// Drop reports that the torrent must not be indexed.
	Drop Drop
	// AdultEvidence reports why the torrent was classified adult.
	AdultEvidence AdultEvidence
}

// Config controls the classifier.
type Config struct {
	// DropAdult drops adult torrents instead of indexing them. It is off by
	// default: the category is advisory and users may want the content.
	DropAdult bool
	// DropScripts drops torrents whose names are written mostly in one of the
	// listed scripts. Supported names: cjk, hangul, cyrillic, arabic, hebrew,
	// thai, devanagari, greek, and nonlatin for any non-Latin dominant script.
	// A mixed name such as "Batman 蝙蝠侠" has no dominant script and is kept.
	// Empty keeps everything.
	DropScripts []string
}

// Classifier classifies torrents. It is immutable and safe for concurrent use.
type Classifier struct {
	cfg          Config
	dropScripts  [scriptOther + 1]bool
	dropNonLatin bool
	filterScript bool
}

// New returns a classifier with the given configuration.
func New(cfg Config) *Classifier {
	c := &Classifier{cfg: cfg}

	for _, name := range cfg.DropScripts {
		name = strings.ToLower(strings.TrimSpace(name))

		if name == "nonlatin" {
			c.dropNonLatin = true
			c.filterScript = true

			continue
		}

		if s, ok := scriptNames[name]; ok {
			c.dropScripts[s] = true
			c.filterScript = true
		}
	}

	return c
}

// Classify decides what a torrent is. The file list may be nil, in which case
// the rules fall back to the name alone.
//
// The name is read first for illegal content. The files are then read for the
// two answers that must not come from the name: whether the torrent is malware
// dressed up as media (see deceptive), and what the content actually is (see
// byFiles). Explicit adult content is checked on the name before the census is
// used, because the name is where adult markers live.
func (c *Classifier) Classify(name string, files []File) Result {
	result := c.classify(name, files)
	if result.Drop != DropNone {
		return result
	}

	result.Category = refineCategory(result.Category, name, files)

	return result
}

// classify decides the family of a torrent and whether it must be dropped. The
// caller refines the category to a leaf.
func (c *Classifier) classify(name string, files []File) Result {
	if isBanned(name) {
		return Result{Drop: DropBanned}
	}

	if isBait(name) {
		return Result{Drop: DropDeceptive}
	}

	if c.scriptFiltered(name) {
		return Result{Drop: DropScript}
	}

	text := name
	if len(files) > 0 {
		text = scanText(name, files, scanTextFiles, scanTextBytes)

		if isBanned(text) {
			return Result{Drop: DropBanned}
		}

		if isBait(text) {
			return Result{Drop: DropDeceptive}
		}
	}

	cen := newCensus(files)

	if c.deceptive(name, text, cen) {
		return Result{Drop: DropDeceptive}
	}

	// A strong adult marker in the name is decisive. The same marker in a file
	// path is weaker evidence, because paths carry advertising URLs and release
	// structure, so there it classifies the row but is marked weak. The TMDB
	// stage can then overturn it if the title turns out to be a mainstream
	// release.
	if adultStrongText(name) {
		return c.adultResult(AdultEvidenceStrong)
	}

	if adultStrongText(text) {
		return c.adultResult(AdultEvidenceWeak)
	}

	return c.byFiles(name, text, cen)
}

// scriptFiltered reports whether the configuration filters out the name's
// script. It reads the name only: a filter on writing system has no business
// walking files, and it runs before the more expensive rules.
func (c *Classifier) scriptFiltered(name string) bool {
	if !c.filterScript {
		return false
	}

	// The extension is Latin letters in every name and would otherwise skew a
	// short title such as "寒战2.mp4".
	s := dominantScript(extensionSuffixRegex.ReplaceAllString(name, ""))
	if s == scriptNone || s == scriptLatin {
		return false
	}

	if c.dropNonLatin {
		return true
	}

	return c.dropScripts[s]
}

// deceptive reports a torrent whose files are executables while its name is
// dressed up as a film or television release. That is malware, not a
// misclassification, and it is dropped and blocklisted.
//
// The bar for "dressed up" is a release naming convention, not a stray word.
// A season or episode marker counts on its own. A film claim needs a stated
// resolution together with a source, codec or container marker, which is what
// a scene release looks like; a resolution alone is ordinary advertising for
// software ("4K Video Downloader"), and a source word alone is just a word. An
// adult name counts when it also states a resolution.
func (c *Classifier) deceptive(name, text string, cen census) bool {
	if cen.dominant() != bucketSoftware {
		return false
	}

	parsed := scene.Parse(name)
	if !parsed.Episodes.Empty() || parsed.Series {
		return true
	}

	if parsed.Resolution != scene.ResolutionUnknown && sourceKeywordsRegex.MatchString(name) {
		return true
	}

	// A program under an adult title is bait whether or not the title carries a
	// resolution: adult content whose files are mostly executable is not adult
	// content.
	return adultStrongText(name) || adultStrongText(text)
}

// resultFor completes a classification whose content kind is known.
func (c *Classifier) resultFor(kind contentKind, parsed scene.Name) Result {
	if kind == kindXXX {
		return c.adultResult(AdultEvidenceWeak)
	}

	result := Result{Category: categoryOf(kind, parsed.Resolution)}

	if kind == kindTV {
		result.Seasons = parsed.Episodes.Seasons
		result.Episodes = parsed.Episodes.Episodes
		result.AirDate = parsed.AirDate
	}

	return result
}

// adultResult is the classification of adult content, which is always stored
// under the XXX category and dropped only when configured to be.
func (c *Classifier) adultResult(evidence AdultEvidence) Result {
	result := Result{Category: CategoryXXX, AdultEvidence: evidence}
	if c.cfg.DropAdult {
		result.Drop = DropAdult
	}

	return result
}

// Limits for the path text the keyword rules read. The keyword rules only need
// to see a marker if one is there, and markers appear in names and folder names,
// not deep inside a list; eight paths keep the cost flat however many files a
// torrent carries.
const (
	scanTextFiles = 8
	scanTextBytes = 2 << 10
)

// scanText is the text the blocking and adult keyword rules read: the name and
// a bounded prefix of the file paths.
func scanText(name string, files []File, maxFiles, maxBytes int) string {
	if len(files) == 0 {
		return name
	}

	if len(name) > maxBytes {
		name = name[:maxBytes]
	}

	b := make([]byte, 0, min(len(name)+len(files)*32, maxBytes))
	b = append(b, name...)

	for _, file := range files[:min(len(files), maxFiles)] {
		if len(b)+len(file.Path)+1 > maxBytes {
			break
		}

		b = append(b, ' ')
		b = append(b, file.Path...)
	}

	return string(b)
}
