package classify

import "github.com/BitTrawler/bittrawler/internal/classify/scene"

// Newznab categories. The stored category is a leaf where the classifier can
// justify one and a parent otherwise; category filters match a parent against
// its children, so searching "Movies" finds every film whatever its resolution.
const (
	CategoryMovies    = 2000
	CategoryMoviesSD  = 2030
	CategoryMoviesHD  = 2040
	CategoryMoviesUHD = 2045

	CategoryAudio     = 3000
	CategoryAudiobook = 3030

	CategoryPC      = 4000
	CategoryPCGames = 4050

	CategoryTV    = 5000
	CategoryTVSD  = 5030
	CategoryTVHD  = 5040
	CategoryTVUHD = 5045

	CategoryXXX = 6000

	CategoryBooks  = 7000
	CategoryEbook  = 7020
	CategoryComics = 7030

	CategoryOther = 8000
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
// that is stored.
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

// categoryNames are the labels shown in the UI.
var categoryNames = map[int]string{
	CategoryMovies:    "Movies",
	CategoryMoviesSD:  "Movies/SD",
	CategoryMoviesHD:  "Movies/HD",
	CategoryMoviesUHD: "Movies/UHD",
	CategoryAudio:     "Audio",
	CategoryAudiobook: "Audio/Audiobook",
	CategoryPC:        "PC",
	CategoryPCGames:   "PC/Games",
	CategoryTV:        "TV",
	CategoryTVSD:      "TV/SD",
	CategoryTVHD:      "TV/HD",
	CategoryTVUHD:     "TV/UHD",
	CategoryXXX:       "XXX",
	CategoryBooks:     "Books",
	CategoryEbook:     "Books/EBook",
	CategoryComics:    "Books/Comics",
	CategoryOther:     "Other",
}

// CategoryName returns the Newznab name of a category, or "Unknown" for a
// category this build does not know about.
func CategoryName(category int) string {
	if name, ok := categoryNames[category]; ok {
		return name
	}

	return "Unknown"
}
