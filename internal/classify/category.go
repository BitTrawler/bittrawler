package classify

import "github.com/BitTrawler/bittrawler/internal/classify/scene"

// Newznab categories. The stored category is a leaf where the classifier can
// justify one and a parent otherwise; category filters match a parent against
// its children, so searching "Movies" finds every film whatever its resolution.
//
// The list follows the standard tree the Servarr applications know (Prowlarr's
// NewznabStandardCategory), limited to the categories a BitTorrent crawl can
// actually identify. Categories that only a release-site operator can assign,
// such as Console/Wiiware or TV/Sport, and leaves that would replace a
// resolution with a source tag, are deliberately absent.
const (
	CategoryOther     = 8000
	CategoryOtherMisc = 8010

	CategoryConsole    = 1000
	CategoryConsoleNDS = 1010
	CategoryConsolePSP = 1020

	CategoryConsoleWii     = 1030
	CategoryConsoleXbox    = 1040
	CategoryConsoleXbox360 = 1050
	CategoryConsolePS3     = 1080
	CategoryConsoleOther   = 1090
	CategoryConsole3DS     = 1110
	CategoryConsoleVita    = 1120
	CategoryConsoleWiiU    = 1130
	CategoryConsoleXboxOne = 1140
	CategoryConsolePS4     = 1180

	CategoryMovies       = 2000
	CategoryMoviesSD     = 2030
	CategoryMoviesHD     = 2040
	CategoryMoviesUHD    = 2045
	CategoryMoviesBluRay = 2050
	CategoryMovies3D     = 2060
	CategoryMoviesDVD    = 2070
	CategoryMoviesWEB    = 2080
	CategoryMoviesx265   = 2090

	CategoryAudio         = 3000
	CategoryAudioMP3      = 3010
	CategoryAudiobook     = 3030
	CategoryAudioLossless = 3040
	CategoryAudioOther    = 3050

	CategoryPC        = 4000
	CategoryPCISO     = 4020
	CategoryPCMac     = 4030
	CategoryPCGames   = 4050
	CategoryPCiOS     = 4060
	CategoryPCAndroid = 4070

	CategoryTV     = 5000
	CategoryTVWEB  = 5010
	CategoryTVSD   = 5030
	CategoryTVHD   = 5040
	CategoryTVUHD  = 5045
	CategoryTVx265 = 5090

	CategoryXXX         = 6000
	CategoryXXXDVD      = 6010
	CategoryXXXWMV      = 6020
	CategoryXXXXviD     = 6030
	CategoryXXXx264     = 6040
	CategoryXXXUHD      = 6045
	CategoryXXXImageSet = 6060
	CategoryXXXOther    = 6070
	CategoryXXXSD       = 6080
	CategoryXXXWEB      = 6090

	CategoryBooks          = 7000
	CategoryBooksMags      = 7010
	CategoryEbook          = 7020
	CategoryComics         = 7030
	CategoryBooksTechnical = 7040
	CategoryBooksOther     = 7050
)

// contentKind is what a torrent is, before the Newznab category is chosen.
type contentKind int

const (
	kindMovie contentKind = iota
	kindTV
	kindMusic
	kindAudiobook
	kindEbook
	kindComic
	kindSoftware
	kindGame
	kindXXX
	kindOther
)

// categoryOf maps a content kind and the parsed resolution to the category
// that is stored. refineCategory narrows the result further where the name and
// files carry a signal.
func categoryOf(kind contentKind, resolution scene.Resolution) int {
	switch kind {
	case kindMovie:
		switch resolution {
		case scene.ResolutionSD:
			return CategoryMoviesSD
		case scene.ResolutionHD:
			return CategoryMoviesHD
		case scene.ResolutionUHD:
			return CategoryMoviesUHD
		default:
			return CategoryMovies
		}
	case kindTV:
		switch resolution {
		case scene.ResolutionSD:
			return CategoryTVSD
		case scene.ResolutionHD:
			return CategoryTVHD
		case scene.ResolutionUHD:
			return CategoryTVUHD
		default:
			return CategoryTV
		}
	case kindMusic:
		return CategoryAudio
	case kindAudiobook:
		return CategoryAudiobook
	case kindEbook:
		return CategoryEbook
	case kindComic:
		return CategoryComics
	case kindSoftware:
		return CategoryPC
	case kindGame:
		return CategoryPCGames
	case kindXXX:
		return CategoryXXX
	default:
		return CategoryOther
	}
}

// ParentCategory returns the top-level category a category belongs to, so a
// stored leaf still matches a filter for its parent. A category that is
// already a parent is returned unchanged.
func ParentCategory(category int) int {
	return category / 1000 * 1000
}

// categoryNames are the labels shown in the UI.
var categoryNames = map[int]string{
	CategoryConsole:        "Console",
	CategoryConsoleNDS:     "Console/NDS",
	CategoryConsolePSP:     "Console/PSP",
	CategoryConsoleWii:     "Console/Wii",
	CategoryConsoleXbox:    "Console/Xbox",
	CategoryConsoleXbox360: "Console/Xbox 360",
	CategoryConsolePS3:     "Console/PS3",
	CategoryConsoleOther:   "Console/Other",
	CategoryConsole3DS:     "Console/3DS",
	CategoryConsoleVita:    "Console/PS Vita",
	CategoryConsoleWiiU:    "Console/WiiU",
	CategoryConsoleXboxOne: "Console/Xbox One",
	CategoryConsolePS4:     "Console/PS4",

	CategoryMovies:       "Movies",
	CategoryMoviesSD:     "Movies/SD",
	CategoryMoviesHD:     "Movies/HD",
	CategoryMoviesUHD:    "Movies/UHD",
	CategoryMoviesBluRay: "Movies/BluRay",
	CategoryMovies3D:     "Movies/3D",
	CategoryMoviesDVD:    "Movies/DVD",
	CategoryMoviesWEB:    "Movies/WEB-DL",
	CategoryMoviesx265:   "Movies/x265",

	CategoryAudio:         "Audio",
	CategoryAudioMP3:      "Audio/MP3",
	CategoryAudiobook:     "Audio/Audiobook",
	CategoryAudioLossless: "Audio/Lossless",
	CategoryAudioOther:    "Audio/Other",

	CategoryPC:        "PC",
	CategoryPCISO:     "PC/ISO",
	CategoryPCMac:     "PC/Mac",
	CategoryPCGames:   "PC/Games",
	CategoryPCiOS:     "PC/Mobile-iOS",
	CategoryPCAndroid: "PC/Mobile-Android",

	CategoryTV:     "TV",
	CategoryTVWEB:  "TV/WEB-DL",
	CategoryTVSD:   "TV/SD",
	CategoryTVHD:   "TV/HD",
	CategoryTVUHD:  "TV/UHD",
	CategoryTVx265: "TV/x265",

	CategoryXXX:         "XXX",
	CategoryXXXDVD:      "XXX/DVD",
	CategoryXXXWMV:      "XXX/WMV",
	CategoryXXXXviD:     "XXX/XviD",
	CategoryXXXx264:     "XXX/x264",
	CategoryXXXUHD:      "XXX/UHD",
	CategoryXXXImageSet: "XXX/ImageSet",
	CategoryXXXSD:       "XXX/SD",
	CategoryXXXWEB:      "XXX/WEB-DL",

	CategoryBooks:          "Books",
	CategoryBooksMags:      "Books/Mags",
	CategoryEbook:          "Books/EBook",
	CategoryComics:         "Books/Comics",
	CategoryBooksTechnical: "Books/Technical",

	CategoryOther: "Other",
}

// CategoryNames returns a copy of the category labels this build knows, keyed
// by id. The Torznab capability document is generated from it.
func CategoryNames() map[int]string {
	names := make(map[int]string, len(categoryNames))
	for category, name := range categoryNames {
		names[category] = name
	}

	return names
}

// CategoryName returns the Newznab name of a category, or "Unknown" for a
// category this build does not know about.
func CategoryName(category int) string {
	if name, ok := categoryNames[category]; ok {
		return name
	}

	return "Unknown"
}
