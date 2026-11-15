package classify

import (
	"regexp"

	"github.com/BitTrawler/bittrawler/internal/classify/scene"
)

// Category leaves are chosen here rather than in the classification rules: the
// rules decide what a torrent is, and this decides which Newznab leaf describes
// it, using the name and the file extensions. A leaf is only used when it adds
// information; an unknown resolution keeps the family parent.
var (
	// threeDRegex matches the release tags of stereoscopic films. It is only
	// consulted for rows already classified as films.
	threeDRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(3d|hsbs|h-sbs|half-sbs|hou|h-ou|half-ou|sbs|tab|mvc)(?:[^a-z0-9]|$)`)
	// webSourceRegex matches a release named for a streaming or download source.
	webSourceRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(web[ ._-]?dl|web[ ._-]?rip|webrip|webdl)(?:[^a-z0-9]|$)`)
	// bluRayRegex matches a Blu-ray release name.
	bluRayRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(blu[ ._-]?ray|bdrip|brrip|bdremux|bdmv|remux)(?:[^a-z0-9]|$)`)
	// dvdRegex matches a DVD or DVDR name.
	dvdRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(dvd|dvdr|dvd5|dvd9|dvdrip|ntsc|pal)(?:[^a-z0-9]|$)`)
	// x265Regex matches the codec tags an x265 leaf stands for.
	x265Regex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(x265|h[ ._-]?265|hevc)(?:[^a-z0-9]|$)`)
	// x264Regex matches the older codec tags.
	x264Regex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(x264|h[ ._-]?264|avc)(?:[^a-z0-9]|$)`)
	// xvidRegex matches XviD releases.
	xvidRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(xvid|divx)(?:[^a-z0-9]|$)`)
	// wmvRegex matches Windows Media releases.
	wmvRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(wmv|wma)(?:[^a-z0-9]|$)`)
	// magazineRegex marks a magazine release by name.
	magazineRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(magazines?|mags?|issue)(?:[^a-z0-9]|$)`)
	// technicalRegex marks a technical or instructional book by name.
	technicalRegex = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(programming|tutorials?|textbooks?|lectures?|certification|algorithms?|mathematics|physics|chemistry|engineering|calculus|statistics)(?:[^a-z0-9]|$)`)
)

// File extensions that identify an audio release's format.
var (
	losslessExtensions = map[string]struct{}{
		"flac": {}, "alac": {}, "wav": {}, "aiff": {}, "aif": {}, "ape": {},
		"dsf": {}, "dff": {}, "shn": {}, "tta": {}, "wv": {}, "sacd": {},
	}
	mp3Extensions   = map[string]struct{}{"mp3": {}}
	audioExtensions = map[string]struct{}{
		"m4a": {}, "aac": {}, "ogg": {}, "opus": {}, "wma": {}, "mka": {},
		"mid": {}, "midi": {},
	}
	isoExtensions     = map[string]struct{}{"iso": {}, "img": {}, "mdf": {}, "nrg": {}}
	iosExtensions     = map[string]struct{}{"ipa": {}}
	androidExtensions = map[string]struct{}{"apk": {}, "xapk": {}, "apks": {}}
	macExtensions     = map[string]struct{}{"dmg": {}, "pkg": {}, "mpkg": {}}
	// aiModelExtensions identify machine-learning weights. They are data files
	// for a model rather than something a person opens, but PC is the closest
	// Newznab family they have. .bin is deliberately absent: it is a disk image
	// and game data far more often, and is only treated as a model when the name
	// says so.
	aiModelExtensions = map[string]struct{}{
		"safetensors": {}, "gguf": {}, "ggml": {}, "ckpt": {}, "pth": {},
		"onnx": {}, "pt": {}, "h5": {},
	}
	// aiModelNameRegex marks a release by name, which catches the model weights
	// saved as generic checkpoints such as pytorch_model.bin.
	aiModelNameRegex = regexp.MustCompile(`(?i)(pytorch|safetensors|gguf|ggml|stable[ ._-]?diffusion|llama|mistral|qwen|flux|lora|textual[ ._-]?inversion)`)
)

// RefineCategory narrows a family category to a leaf where the name and files
// make one certain. It is exported for stages that correct a category after
// classification, such as the TMDB confirmation.
func RefineCategory(category int, name string, files []File) int {
	return refineCategory(category, name, files)
}

// refineCategory narrows a family category to a leaf where the name and files
// make one certain. It is applied after classification so the rules stay about
// content, not about release formats.
func refineCategory(category int, name string, files []File) int {
	switch {
	case category == CategoryOther:
		return refineOther(name, files)
	case category >= CategoryConsole && category < CategoryMovies:
		return refineGame(name, files)
	case category >= CategoryMovies && category < CategoryAudio:
		return refineMovie(category, name, files)
	case category >= CategoryAudio && category < CategoryPC:
		return refineAudio(category, files)
	case category == CategoryPC, category == CategoryPCGames:
		return refineSoftware(category, name, files)
	case category >= CategoryTV && category < CategoryXXX:
		return refineTV(category, name)
	case category >= CategoryXXX && category < CategoryBooks:
		return refineAdult(category, name, files)
	case category >= CategoryBooks && category < CategoryOther:
		return refineBook(category, name)
	}

	return category
}

// dominantFileExtension returns the extension of the largest file. The primary
// file is what the UI already treats as the release's main content.
func dominantFileExtension(files []File) string {
	var (
		extension string
		size      int64 = -1
	)

	for _, file := range files {
		if file.Size > size {
			size = file.Size
			extension = extensionOf(file.Path)
		}
	}

	return extension
}

// hasExtension reports whether any file has one of the extensions.
func hasExtension(files []File, extensions map[string]struct{}) bool {
	for _, file := range files {
		if _, ok := extensions[extensionOf(file.Path)]; ok {
			return true
		}
	}

	return false
}

// refineMovie chooses the leaf for a film release. The resolution leaf wins
// where there is one, because that is what film clients filter on; the 3D leaf
// is a distinct format and overrides it.
func refineMovie(category int, name string, files []File) int {
	if threeDRegex.MatchString(name) {
		return CategoryMovies3D
	}

	if category != CategoryMovies {
		return category
	}

	primary := dominantFileExtension(files)

	switch {
	case bluRayRegex.MatchString(name) && primary == "iso":
		return CategoryMoviesBluRay
	case dvdRegex.MatchString(name) && primary == "iso":
		return CategoryMoviesDVD
	case webSourceRegex.MatchString(name):
		return CategoryMoviesWEB
	case x265Regex.MatchString(name):
		return CategoryMoviesx265
	}

	return category
}

// refineTV narrows only the film-like fallback: a television release with a
// stated resolution keeps its resolution leaf, because the TV clients ask for
// exactly those.
func refineTV(category int, name string) int {
	if category != CategoryTV {
		return category
	}

	switch {
	case webSourceRegex.MatchString(name):
		return CategoryTVWEB
	case x265Regex.MatchString(name):
		return CategoryTVx265
	}

	return category
}

// refineAudio chooses the Audio leaf from the release's file format. Lossless
// wins when any file is lossless: a mixed release is still a lossless one.
func refineAudio(category int, files []File) int {
	if category != CategoryAudio {
		return category
	}

	primary := dominantFileExtension(files)

	switch {
	case hasExtension(files, losslessExtensions):
		return CategoryAudioLossless
	case hasExtension(files, mp3Extensions):
		return CategoryAudioMP3
	case hasExtension(files, audioExtensions):
		return CategoryAudioOther
	}

	if primary == "" {
		return category
	}

	return category
}

// refineSoftware chooses a PC leaf from the release's files.
func refineSoftware(category int, name string, files []File) int {
	if category == CategoryPCGames {
		return refineGame(name, files)
	}

	switch primary := dominantFileExtension(files); {
	case primary == "":
		return category
	case isOneOf(primary, iosExtensions):
		return CategoryPCiOS
	case isOneOf(primary, androidExtensions):
		return CategoryPCAndroid
	case isOneOf(primary, macExtensions):
		return CategoryPCMac
	case isOneOf(primary, isoExtensions):
		return CategoryPCISO
	}

	return category
}

// refineGame replaces PC/Games with a console leaf when the release names a
// platform the Newznab tree has a category for, and keeps PC/Games otherwise.
func refineGame(name string, files []File) int {
	platform := detectConsole(name, files)
	if platform == consoleNone {
		return CategoryPCGames
	}

	return platform.category()
}

// refineAdult narrows adult content. The format leaves are the standard way an
// adult indexer presents the category; an image set is a format of its own.
func refineAdult(category int, name string, files []File) int {
	if category != CategoryXXX {
		return category
	}

	cen := newCensus(files)
	if cen.dominant() == bucketImage {
		return CategoryXXXImageSet
	}

	primary := dominantFileExtension(files)

	switch {
	case isOneOf(primary, isoExtensions), dvdRegex.MatchString(name):
		return CategoryXXXDVD
	case primary == "wmv", wmvRegex.MatchString(name):
		return CategoryXXXWMV
	case xvidRegex.MatchString(name):
		return CategoryXXXXviD
	case x264Regex.MatchString(name):
		return CategoryXXXx264
	case webSourceRegex.MatchString(name):
		return CategoryXXXWEB
	}

	switch scene.Parse(name).Resolution {
	case scene.ResolutionUHD:
		return CategoryXXXUHD
	case scene.ResolutionSD:
		return CategoryXXXSD
	}

	return category
}

// refineBook narrows books to magazines and technical books. The format rules
// already chose EBook or Comics; this adds the subject.
func refineBook(category int, name string) int {
	if category != CategoryEbook {
		return category
	}

	switch {
	case magazineRegex.MatchString(name):
		return CategoryBooksMags
	case technicalRegex.MatchString(name):
		return CategoryBooksTechnical
	}

	return category
}

// refineOther gives machine-learning weights a home: they are files for a
// program, and PC is the family for programs in the Newznab tree.
func refineOther(name string, files []File) int {
	if hasExtension(files, aiModelExtensions) {
		return CategoryPC
	}

	if hasExtension(files, map[string]struct{}{"bin": {}}) && aiModelNameRegex.MatchString(name) {
		return CategoryPC
	}

	return CategoryOther
}

func isOneOf(value string, set map[string]struct{}) bool {
	_, ok := set[value]

	return ok
}

// consolePlatform is a console the Newznab tree has a category for.
type consolePlatform int

const (
	consoleNone consolePlatform = iota
	consoleNDS
	consolePSP
	consoleWii
	consoleXbox
	consoleXbox360
	consolePS3
	consoleOther
	console3DS
	consoleVita
	consoleWiiU
	consoleXboxOne
	consolePS4
)

// category returns the Newznab category for the platform.
func (p consolePlatform) category() int {
	switch p {
	case consoleNDS:
		return CategoryConsoleNDS
	case consolePSP:
		return CategoryConsolePSP
	case consoleWii:
		return CategoryConsoleWii
	case consoleXbox:
		return CategoryConsoleXbox
	case consoleXbox360:
		return CategoryConsoleXbox360
	case consolePS3:
		return CategoryConsolePS3
	case console3DS:
		return CategoryConsole3DS
	case consoleVita:
		return CategoryConsoleVita
	case consoleWiiU:
		return CategoryConsoleWiiU
	case consoleXboxOne:
		return CategoryConsoleXboxOne
	case consolePS4:
		return CategoryConsolePS4
	default:
		return CategoryConsoleOther
	}
}

// consoleNameMarkers are the platform names found in console release names,
// most specific first so "Xbox 360" is not caught by "Xbox".
var consoleNameMarkers = []struct {
	expression *regexp.Regexp
	platform   consolePlatform
}{
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])xbox[ ._-]?360(?:[^a-z0-9]|$)`), consoleXbox360},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])xbox[ ._-]?one(?:[^a-z0-9]|$)`), consoleXboxOne},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])playstation[ ._-]?3(?:[^a-z0-9]|$)`), consolePS3},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])ps3(?:[^a-z0-9]|$)`), consolePS3},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])playstation[ ._-]?4(?:[^a-z0-9]|$)`), consolePS4},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])ps4(?:[^a-z0-9]|$)`), consolePS4},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])playstation[ ._-]?vita(?:[^a-z0-9]|$)`), consoleVita},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])ps[ ._-]?vita(?:[^a-z0-9]|$)`), consoleVita},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])wii[ ._-]?u(?:[^a-z0-9]|$)`), consoleWiiU},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])wiiu(?:[^a-z0-9]|$)`), consoleWiiU},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])wii(?:[^a-z0-9]|$)`), consoleWii},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])3ds(?:[^a-z0-9]|$)`), console3DS},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])nintendo[ ._-]?ds(?:[^a-z0-9]|$)`), consoleNDS},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])nds(?:[^a-z0-9]|$)`), consoleNDS},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])psp(?:[^a-z0-9]|$)`), consolePSP},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])xbox(?:[^a-z0-9]|$)`), consoleXbox},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(switch|nintendo[ ._-]?switch|nsz|nsp|xci)(?:[^a-z0-9]|$)`), consoleOther},
	{regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(gamecube|n64|snes|super[ ._-]?nintendo|nes|dreamcast|genesis|saturn)(?:[^a-z0-9]|$)`), consoleOther},
}

// consoleExtensionPlatforms map a file extension to the console it belongs to.
var consoleExtensionPlatforms = map[string]consolePlatform{
	"nds": consoleNDS, "dsi": consoleNDS,
	"psp": consolePSP, "cso": consolePSP, "pbp": consolePSP,
	"wii": consoleWii, "wbfs": consoleWii, "wia": consoleWii, "wad": consoleWii,
	"xbe": consoleXbox, "xiso": consoleXbox,
	"xex": consoleXbox360, "god": consoleXbox360,
	"3ds": console3DS, "cia": console3DS, "3dsx": console3DS,
	"vpk": consoleVita,
	"wud": consoleWiiU, "wux": consoleWiiU,
	"nsp": consoleOther, "nsz": consoleOther, "xci": consoleOther,
	"gcm": consoleOther, "rvz": consoleOther, "n64": consoleOther, "z64": consoleOther,
	"nes": consoleOther, "sfc": consoleOther, "smc": consoleOther, "gba": consoleOther,
	"gb": consoleOther, "gbc": consoleOther, "gen": consoleOther, "32x": consoleOther,
	"gg": consoleOther, "sms": consoleOther,
}

// detectConsole finds the console a release is for, from the file extensions
// first and the release name second. Both must be inside a release already
// classified as a game, so "Switch" in a film title cannot reach here.
func detectConsole(name string, files []File) consolePlatform {
	if platform, ok := consoleExtensionPlatforms[dominantFileExtension(files)]; ok {
		return platform
	}

	for _, marker := range consoleNameMarkers {
		if marker.expression.MatchString(name) {
			return marker.platform
		}
	}

	return consoleNone
}
