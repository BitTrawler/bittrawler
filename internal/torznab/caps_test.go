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
	want := map[int]string{
		classify.CategoryMovies:    classify.CategoryName(classify.CategoryMovies),
		classify.CategoryMoviesSD:  classify.CategoryName(classify.CategoryMoviesSD),
		classify.CategoryMoviesHD:  classify.CategoryName(classify.CategoryMoviesHD),
		classify.CategoryMoviesUHD: classify.CategoryName(classify.CategoryMoviesUHD),

		classify.CategoryAudio:     classify.CategoryName(classify.CategoryAudio),
		classify.CategoryAudiobook: classify.CategoryName(classify.CategoryAudiobook),

		classify.CategoryPC:      classify.CategoryName(classify.CategoryPC),
		classify.CategoryPCGames: classify.CategoryName(classify.CategoryPCGames),

		classify.CategoryTV:    classify.CategoryName(classify.CategoryTV),
		classify.CategoryTVSD:  classify.CategoryName(classify.CategoryTVSD),
		classify.CategoryTVHD:  classify.CategoryName(classify.CategoryTVHD),
		classify.CategoryTVUHD: classify.CategoryName(classify.CategoryTVUHD),

		classify.CategoryXXX: classify.CategoryName(classify.CategoryXXX),

		classify.CategoryBooks:  classify.CategoryName(classify.CategoryBooks),
		classify.CategoryEbook:  classify.CategoryName(classify.CategoryEbook),
		classify.CategoryComics: classify.CategoryName(classify.CategoryComics),

		classify.CategoryOther: classify.CategoryName(classify.CategoryOther),
	}

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
		classify.CategoryMovies: {classify.CategoryMoviesSD, classify.CategoryMoviesHD, classify.CategoryMoviesUHD},
		classify.CategoryTV:     {classify.CategoryTVSD, classify.CategoryTVHD, classify.CategoryTVUHD},
		classify.CategoryBooks:  {classify.CategoryEbook, classify.CategoryComics},
		classify.CategoryAudio:  {classify.CategoryAudiobook},
		classify.CategoryPC:     {classify.CategoryPCGames},
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
