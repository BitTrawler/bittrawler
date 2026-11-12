package scene

import "testing"

func TestParseTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		year  int
	}{
		{"Some.Movie.2020.1080p.BluRay.x264-GRP", "Some Movie", 2020},
		{"Some Movie (2020) [1080p]", "Some Movie", 2020},
		{"Blade.Runner.2049.2017.1080p.BluRay", "Blade Runner 2049", 2017},
		{"2001.A.Space.Odyssey.1968.1080p", "2001 A Space Odyssey", 1968},
		{"The.100.S01E02.1080p.WEB-DL", "The 100", 0},
		{"Some.Show.S02.1080p", "Some Show", 0},
		{"[Erai-raws] Megami no Cafe Terrace - 11 [1080p][Multiple Subtitle]", "Megami no Cafe Terrace 11", 0},
		{"[SubsPlease] Sousou no Frieren - 10 (1080p)", "Sousou no Frieren 10", 0},
		{"Scrat.Gone nutty.2002.BDRip 720p.mkv", "Scrat Gone nutty", 2002},
		{"The.Vampire.And.The.Ballerina.1960.BluRay.1080p", "The Vampire And The Ballerina", 1960},
		{"A.Day.With.The.Boys.1969.BDRip.mkv", "A Day With The Boys", 1969},
		{"寒战2.mp4", "寒战2", 0},
		{"寄生兽 完全版 第1卷～第4卷", "寄生兽 完全版 第1卷 第4卷", 0},
		{"Legacies", "Legacies", 0},
		{"", "", 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := ParseTitle(test.name)

			if got.Title != test.title {
				t.Errorf("title = %q, want %q", got.Title, test.title)
			}

			if got.Year != test.year {
				t.Errorf("year = %d, want %d", got.Year, test.year)
			}
		})
	}
}

func TestParseTitleRejectsNoise(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"1080p", "2020", "....", "x"} {
		if title := ParseTitle(name).Title; title != "" {
			t.Errorf("ParseTitle(%q).Title = %q, want empty", name, title)
		}
	}
}
