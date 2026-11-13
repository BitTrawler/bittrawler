package classify

import (
	"testing"

	"github.com/BitTrawler/bittrawler/internal/classify/scene"
)

// TestCategoryOfTable covers the mapping from a content kind and resolution to
// the stored Newznab category, including the zero value and an unknown kind.
func TestCategoryOfTable(t *testing.T) {
	tests := []struct {
		name       string
		kind       contentKind
		resolution scene.Resolution
		want       int
	}{
		{"movie with no resolution", kindMovie, scene.ResolutionUnknown, CategoryMovies},
		{"movie sd", kindMovie, scene.ResolutionSD, CategoryMoviesSD},
		{"movie hd", kindMovie, scene.ResolutionHD, CategoryMoviesHD},
		{"movie uhd", kindMovie, scene.ResolutionUHD, CategoryMoviesUHD},
		{"tv with no resolution", kindTV, scene.ResolutionUnknown, CategoryTV},
		{"tv sd", kindTV, scene.ResolutionSD, CategoryTVSD},
		{"tv hd", kindTV, scene.ResolutionHD, CategoryTVHD},
		{"tv uhd", kindTV, scene.ResolutionUHD, CategoryTVUHD},
		{"music ignores resolution", kindMusic, scene.ResolutionUHD, CategoryAudio},
		{"audiobook", kindAudiobook, scene.ResolutionUnknown, CategoryAudiobook},
		{"ebook", kindEbook, scene.ResolutionUnknown, CategoryEbook},
		{"comic", kindComic, scene.ResolutionUnknown, CategoryComics},
		{"software", kindSoftware, scene.ResolutionUnknown, CategoryPC},
		{"game", kindGame, scene.ResolutionUnknown, CategoryPCGames},
		{"xxx", kindXXX, scene.ResolutionUnknown, CategoryXXX},
		{"other", kindOther, scene.ResolutionHD, CategoryOther},
		// The zero contentKind is kindMovie, which yields the movie parent when
		// there is no resolution; it is not a special "unknown" case.
		{"zero kind", contentKind(0), scene.ResolutionUnknown, CategoryMovies},
		// A kind this build does not know falls back to Other rather than
		// panicking or returning a zero category.
		{"unknown kind", contentKind(99), scene.ResolutionHD, CategoryOther},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := categoryOf(test.kind, test.resolution)
			if got != test.want {
				t.Fatalf("categoryOf(%d, %v) = %d, want %d", test.kind, test.resolution, got, test.want)
			}

			// Every category the classifier stores must have a UI label.
			if name := CategoryName(got); name == "Unknown" {
				t.Fatalf("categoryOf(%d, %v) = %d, which has no name", test.kind, test.resolution, got)
			}
		})
	}
}

// TestCategoryNameTable covers every name the UI can show, the zero category and
// an id this build does not map.
func TestCategoryNameTable(t *testing.T) {
	tests := []struct {
		category int
		want     string
	}{
		// Parents, which category filters match against their children.
		{CategoryMovies, "Movies"},
		{CategoryAudio, "Audio"},
		{CategoryPC, "PC"},
		{CategoryTV, "TV"},
		{CategoryXXX, "XXX"},
		{CategoryBooks, "Books"},
		{CategoryOther, "Other"},
		// Leaves.
		{CategoryMoviesSD, "Movies/SD"},
		{CategoryMoviesHD, "Movies/HD"},
		{CategoryMoviesUHD, "Movies/UHD"},
		{CategoryAudiobook, "Audio/Audiobook"},
		{CategoryPCGames, "PC/Games"},
		{CategoryTVSD, "TV/SD"},
		{CategoryTVHD, "TV/HD"},
		{CategoryTVUHD, "TV/UHD"},
		{CategoryEbook, "Books/EBook"},
		{CategoryComics, "Books/Comics"},
		// The zero category is not mapped.
		{0, "Unknown"},
		// An id between two mapped categories is not mapped.
		{2001, "Unknown"},
		// A high id is not mapped.
		{9999, "Unknown"},
	}

	for _, test := range tests {
		if got := CategoryName(test.category); got != test.want {
			t.Errorf("CategoryName(%d) = %q, want %q", test.category, got, test.want)
		}
	}
}

// TestCategoryNamesAreUnique checks that two categories cannot be confused in a
// UI dropdown or a feed filter.
func TestCategoryNamesAreUnique(t *testing.T) {
	seen := make(map[string]int, len(categoryNames))

	for category, name := range categoryNames {
		if name == "" {
			t.Errorf("category %d has an empty name", category)

			continue
		}

		if other, ok := seen[name]; ok {
			t.Errorf("categories %d and %d are both named %q", other, category, name)
		}

		seen[name] = category
	}
}
