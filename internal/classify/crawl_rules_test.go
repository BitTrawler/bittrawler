package classify

import "testing"

// TestRulesFromTheCrawl covers the gaps the first pull of Other rows showed:
// adult studios and performers, adult manga markers, ebook files and manga
// magazines.
func TestRulesFromTheCrawl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		files    []File
		category int
	}{
		{
			name:     "adult studio with a performer",
			title:    "Babes.com Carla Cox & Kattie Gold",
			files:    filesOf("jpg", 200<<20),
			category: CategoryXXXImageSet,
		},
		{
			name:     "adult studio alone",
			title:    "Babes.com Compilation 2024",
			files:    filesOf("jpg", 200<<20),
			category: CategoryXXXImageSet,
		},
		{
			name:     "performer from the pull",
			title:    "Carla.Cox.And.Friend.2024.1080p",
			files:    filesOf("mp4", 2<<30),
			category: CategoryXXX,
		},
		{
			name:     "adult manga marker",
			title:    "成年コミック [Yukio] Milk Caramel.zip",
			files:    filesOf("zip", 60<<20),
			category: CategoryXXX,
		},
		{
			name:     "adult video actress marker",
			title:    "AV Joyuu Danyuu Doukyo.rar",
			files:    filesOf("rar", 60<<20),
			category: CategoryXXX,
		},
		{
			name:     "uncensored marker",
			title:    "Some Release [English,Uncen].zip",
			files:    filesOf("zip", 60<<20),
			category: CategoryXXX,
		},
		{
			name:     "fictionbook file",
			title:    "Куннилингус от А до Я.fb2",
			files:    filesOf("fb2", 1<<20),
			category: CategoryEbook,
		},
		{
			name:     "manga magazine",
			title:    "Weekly Shonen Jump 2026 50.zip",
			files:    filesOf("zip", 600<<20),
			category: CategoryComics,
		},
	}

	classifier := New(Config{})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := classifier.Classify(test.title, test.files)
			if result.Category != test.category {
				t.Errorf("category = %d (%s), want %d (%s)",
					result.Category, CategoryName(result.Category),
					test.category, CategoryName(test.category))
			}
		})
	}
}
