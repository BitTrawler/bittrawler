package torznab

import (
	"encoding/xml"
	"testing"

	"github.com/BitTrawler/bittrawler/internal/classify"
)

type parsedCaps struct {
	XMLName    xml.Name            `xml:"caps"`
	Server     parsedCapsServer    `xml:"server"`
	Limits     parsedCapsLimits    `xml:"limits"`
	Searching  parsedCapsSearching `xml:"searching"`
	Categories parsedCapsCategory  `xml:"categories"`
}

type parsedCapsServer struct {
	Title string `xml:"title,attr"`
}

type parsedCapsLimits struct {
	Max     int `xml:"max,attr"`
	Default int `xml:"default,attr"`
}

type parsedCapsSearch struct {
	Available       string `xml:"available,attr"`
	SupportedParams string `xml:"supportedParams,attr"`
}

type parsedCapsSearching struct {
	Search      parsedCapsSearch `xml:"search"`
	TVSearch    parsedCapsSearch `xml:"tv-search"`
	MovieSearch parsedCapsSearch `xml:"movie-search"`
	MusicSearch parsedCapsSearch `xml:"music-search"`
	BookSearch  parsedCapsSearch `xml:"book-search"`
	AudioSearch parsedCapsSearch `xml:"audio-search"`
}

type parsedCapsCategory struct {
	Categories []parsedCapsCategoryEntry `xml:"category"`
}

type parsedCapsCategoryEntry struct {
	ID   int                       `xml:"id,attr"`
	Name string                    `xml:"name,attr"`
	Sub  []parsedCapsCategoryEntry `xml:"subcat"`
}

func decodeCaps(t *testing.T, body []byte) parsedCaps {
	t.Helper()

	var c parsedCaps
	if err := xml.Unmarshal(body, &c); err != nil {
		t.Fatalf("decode caps: %v\n%s", err, body)
	}

	return c
}

func TestCapsDocument(t *testing.T) {
	h := newTestHandler(t, Config{Title: "TestTrawler", DefaultLimit: 50, MaxLimit: 200})
	rec := get(t, h, "t=caps")

	if got, want := rec.Code, 200; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	if got, want := rec.Header().Get("Content-Type"), "application/xml; charset=utf-8"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}

	caps := decodeCaps(t, rec.Body.Bytes())

	if caps.Server.Title != "TestTrawler" {
		t.Errorf("server title = %q, want %q", caps.Server.Title, "TestTrawler")
	}

	if caps.Limits.Max != 200 || caps.Limits.Default != 50 {
		t.Errorf("limits = max %d default %d, want max 200 default 50", caps.Limits.Max, caps.Limits.Default)
	}

	if caps.Searching.Search.Available != "yes" ||
		caps.Searching.Search.SupportedParams != supportedParamsFor(FunctionSearch) {
		t.Errorf("search = %q/%q, want yes/%q",
			caps.Searching.Search.Available, caps.Searching.Search.SupportedParams, supportedParamsFor(FunctionSearch))
	}

	if caps.Searching.TVSearch.Available != "yes" ||
		caps.Searching.TVSearch.SupportedParams != supportedParamsFor(FunctionTV) {
		t.Errorf("tv-search = %q/%q, want yes/%q",
			caps.Searching.TVSearch.Available, caps.Searching.TVSearch.SupportedParams, supportedParamsFor(FunctionTV))
	}

	if caps.Searching.MovieSearch.Available != "yes" {
		t.Errorf("movie-search available = %q, want yes", caps.Searching.MovieSearch.Available)
	}

	if caps.Searching.AudioSearch.Available != "no" {
		t.Errorf("audio-search available = %q, want no", caps.Searching.AudioSearch.Available)
	}
}

// TestCapsCategoriesAreExactlyTheStoredOnes keeps the advertised tree in step
// with the classifier: a client should be able to map everything it can receive
// and see nothing it cannot.
func TestCapsCategoriesAreExactlyTheStoredOnes(t *testing.T) {
	want := classify.CategoryNames()

	caps := decodeCaps(t, get(t, newTestHandler(t, Config{}), "t=caps").Body.Bytes())

	got := make(map[int]string)

	for _, category := range caps.Categories.Categories {
		got[category.ID] = category.Name

		for _, sub := range category.Sub {
			got[sub.ID] = sub.Name
		}
	}

	if len(got) != len(want) {
		t.Fatalf("advertised %d categories, want %d: %v", len(got), len(want), got)
	}

	for id, name := range want {
		advertised, ok := got[id]
		if !ok {
			t.Errorf("category %d (%s) is not advertised", id, name)

			continue
		}

		if advertised != name {
			t.Errorf("category %d name = %q, want %q", id, advertised, name)
		}
	}

	for id := range got {
		if _, ok := want[id]; !ok {
			t.Errorf("category %d is advertised but never stored", id)
		}
	}
}

func TestCapsParentsHaveTheirSubcategories(t *testing.T) {
	caps := decodeCaps(t, get(t, newTestHandler(t, Config{}), "t=caps").Body.Bytes())

	wantSubs := map[int][]int{
		classify.CategoryConsole: {
			classify.CategoryConsoleNDS, classify.CategoryConsolePSP,
			classify.CategoryConsoleWii, classify.CategoryConsoleXbox,
			classify.CategoryConsoleXbox360, classify.CategoryConsolePS3,
			classify.CategoryConsoleOther, classify.CategoryConsole3DS,
			classify.CategoryConsoleVita, classify.CategoryConsoleWiiU,
			classify.CategoryConsoleXboxOne, classify.CategoryConsolePS4,
		},
		classify.CategoryMovies: {
			classify.CategoryMoviesSD, classify.CategoryMoviesHD,
			classify.CategoryMoviesUHD, classify.CategoryMoviesBluRay,
			classify.CategoryMovies3D, classify.CategoryMoviesDVD,
			classify.CategoryMoviesWEB, classify.CategoryMoviesx265,
		},
		classify.CategoryAudio: {
			classify.CategoryAudioMP3, classify.CategoryAudiobook,
			classify.CategoryAudioLossless, classify.CategoryAudioOther,
		},
		classify.CategoryPC: {
			classify.CategoryPCISO, classify.CategoryPCMac,
			classify.CategoryPCGames, classify.CategoryPCiOS,
			classify.CategoryPCAndroid,
		},
		classify.CategoryTV: {
			classify.CategoryTVWEB, classify.CategoryTVSD,
			classify.CategoryTVHD, classify.CategoryTVUHD,
			classify.CategoryTVx265,
		},
		classify.CategoryXXX: {
			classify.CategoryXXXDVD, classify.CategoryXXXWMV,
			classify.CategoryXXXXviD, classify.CategoryXXXx264,
			classify.CategoryXXXUHD, classify.CategoryXXXImageSet,
			classify.CategoryXXXSD, classify.CategoryXXXWEB,
		},
		classify.CategoryBooks: {
			classify.CategoryBooksMags, classify.CategoryEbook,
			classify.CategoryComics, classify.CategoryBooksTechnical,
		},
	}

	for _, category := range caps.Categories.Categories {
		want, ok := wantSubs[category.ID]
		if !ok {
			if len(category.Sub) > 0 {
				t.Errorf("category %d has unexpected subcategories", category.ID)
			}

			continue
		}

		if len(category.Sub) != len(want) {
			t.Errorf("category %d has %d subcategories, want %d", category.ID, len(category.Sub), len(want))

			continue
		}

		for i, sub := range category.Sub {
			if sub.ID != want[i] {
				t.Errorf("category %d subcategory %d id = %d, want %d", category.ID, i, sub.ID, want[i])
			}
		}
	}
}
