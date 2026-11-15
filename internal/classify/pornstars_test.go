package classify

import (
	"strings"
	"testing"
)

// TestPornstarText checks the performer matcher's separators and its limits.
func TestPornstarText(t *testing.T) {
	t.Parallel()

	matches := []string{
		"Cory Chase",
		"Cory.Chase.And.Friend.2024.1080p",
		"cory_chase_2023_1080p",
		"CORY-CHASE-720p",
		"Riley.Reid.In.The.Mood",
		"Jordi.El.Nino.Polla.2022",
		"Mia Khalifa 2019",
		"Sunny.Leone.Family.Vacation",
	}

	for _, name := range matches {
		if !pornstarText(name) {
			t.Errorf("pornstarText(%q) = false, want true", name)
		}
	}

	nonMatches := []string{
		"Cory.2020.1080p",
		"Chase.2019.1080p.BluRay",
		"Reid.2020.1080p",
		"Chase Cory 2020",
		"Some.Movie.2020.1080p.BluRay.x264-GRP",
		"Riley.2020.1080p",
	}

	for _, name := range nonMatches {
		if pornstarText(name) {
			t.Errorf("pornstarText(%q) = true, want false", name)
		}
	}
}

// TestPornstarClassifiesAdult checks that a performer's full name decides the
// classification even when nothing else in the name is an adult marker.
func TestPornstarClassifiesAdult(t *testing.T) {
	t.Parallel()

	classifier := New(Config{})

	tests := []string{
		"Cory.Chase.2024.02.15.1080p.WEB-DL",
		"Riley.Reid.And.Friends.2023.1080p",
		"Mia_Khalifa_2019_1080p",
		"Jordi.El.Nino.Polla.2022.1080p",
	}

	for _, title := range tests {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			result := classifier.Classify(title, filesOf("mp4", 2<<30))

			if ParentCategory(result.Category) != CategoryXXX {
				t.Errorf("category = %d (%s), want the XXX family",
					result.Category, CategoryName(result.Category))
			}

			if result.AdultEvidence != AdultEvidenceStrong {
				t.Errorf("evidence = %v, want strong", result.AdultEvidence)
			}
		})
	}
}

// TestPornstarImageSet checks that an image set named after a performer is
// adult rather than an unknown image collection.
func TestPornstarImageSet(t *testing.T) {
	t.Parallel()

	result := New(Config{}).Classify("Cory.Chase.Photo.Set.2023", filesOf("jpg", 200<<20))

	if ParentCategory(result.Category) != CategoryXXX {
		t.Errorf("category = %d (%s), want the XXX family", result.Category, CategoryName(result.Category))
	}

	if result.AdultEvidence != AdultEvidenceStrong {
		t.Errorf("evidence = %v, want strong", result.AdultEvidence)
	}
}

// TestPornstarIsDroppedWhenAsks checks that the adult drop covers a performer
// match, since the name is strong evidence on its own.
func TestPornstarIsDroppedWhenAsks(t *testing.T) {
	t.Parallel()

	result := New(Config{DropAdult: true}).Classify("Cory.Chase.2024.1080p", filesOf("mp4", 2<<30))

	if result.Drop != DropAdult {
		t.Errorf("drop = %v, want adult", result.Drop)
	}
}

// TestOrdinaryNamesAreNotAdult checks that a single name token, or an ordinary
// title with a performer's first or last name in it, is not treated as adult.
func TestOrdinaryNamesAreNotAdult(t *testing.T) {
	t.Parallel()

	classifier := New(Config{})

	for _, title := range []string{
		"Cory.2020.1080p.BluRay.x264-GRP",
		"The.Chase.2019.1080p.BluRay.x264-GRP",
		"Chase.S01E01.1080p.WEB-DL",
	} {
		if result := classifier.Classify(title, filesOf("mkv", 8<<30)); result.Category == CategoryXXX {
			t.Errorf("%q classified XXX, want a film or television", title)
		}
	}
}

// TestPornstarListIsBroad checks the list's shape: it is large, every entry is
// a full name the matcher can use, and no two entries canonicalise alike.
func TestPornstarListIsBroad(t *testing.T) {
	t.Parallel()

	if len(pornstarNames) < 1000 {
		t.Fatalf("pornstarNames has %d entries, want a broad list", len(pornstarNames))
	}

	seen := make(map[string]bool, len(pornstarNames))

	for _, name := range pornstarNames {
		tokens := nameTokens(name)

		if len(tokens) < 2 {
			t.Errorf("%q is a single token and can never match", name)
		}

		key := strings.Join(tokens, " ")
		if seen[key] {
			t.Errorf("%q appears twice", name)
		}

		seen[key] = true
	}
}

// TestAdultInternetCreatorsAreAdult checks that the creators added alongside the
// performer list are recognised.
func TestAdultInternetCreatorsAreAdult(t *testing.T) {
	t.Parallel()

	classifier := New(Config{})

	for _, title := range []string{
		"Belle.Delphine.Photo.Set.2024",
		"Sophie.Rain.2024.1080p",
		"Corinna.Kopf.OnlyFans.2023",
	} {
		result := classifier.Classify(title, filesOf("jpg", 200<<20))

		if ParentCategory(result.Category) != CategoryXXX || result.AdultEvidence != AdultEvidenceStrong {
			t.Errorf("%q classified as %s with evidence %v, want adult strong",
				title, CategoryName(result.Category), result.AdultEvidence)
		}
	}
}
