package scene

import (
	"reflect"
	"testing"
)

func TestParseEpisodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		seasons  []int
		episodes []int
	}{
		{
			name:     "single episode",
			input:    "the.show.s01e02.1080p.web-dl.x264-grp",
			seasons:  []int{1},
			episodes: []int{10002},
		},
		{
			name:     "dot between season and episode",
			input:    "the.show.s01.e02.1080p",
			seasons:  []int{1},
			episodes: []int{10002},
		},
		{
			name:     "multiple episode markers",
			input:    "the.show.s01e02e03e04.1080p",
			seasons:  []int{1},
			episodes: []int{10002, 10003, 10004},
		},
		{
			name:     "episode range",
			input:    "the.show.s01e02-e04.1080p",
			seasons:  []int{1},
			episodes: []int{10002, 10003, 10004},
		},
		{
			name:     "short episode range",
			input:    "the.show.s01e02-04.1080p",
			seasons:  []int{1},
			episodes: []int{10002, 10003, 10004},
		},
		{
			name:     "season pack",
			input:    "the.show.s02.1080p.bluray",
			seasons:  []int{2},
			episodes: nil,
		},
		{
			name:     "season spelled out",
			input:    "the.show.season.2.complete.1080p",
			seasons:  []int{2},
			episodes: nil,
		},
		{
			name:     "season and episode spelled out",
			input:    "the.show.season.2.episode.3.1080p",
			seasons:  []int{2},
			episodes: []int{20003},
		},
		{
			name:     "x format",
			input:    "the.show.1x02.720p",
			seasons:  []int{1},
			episodes: []int{10002},
		},
		{
			name:     "x format range",
			input:    "the.show.1x02-04.720p",
			seasons:  []int{1},
			episodes: []int{10002, 10003, 10004},
		},
		{
			name:     "season range",
			input:    "the.show.s01-s03.1080p",
			seasons:  []int{1, 2, 3},
			episodes: nil,
		},
		{
			name:     "two digit season",
			input:    "the.show.s12e24.1080p",
			seasons:  []int{12},
			episodes: []int{120024},
		},
		{
			name:    "spanish season",
			input:   "los.simpsons.temporada.34.hdtv720p",
			seasons: []int{34},
		},
		{
			name:  "resolution is not an episode",
			input: "some.movie.2019.1080p.bluray.x264",
		},
		{
			name:  "dimensions are not an episode",
			input: "some.movie.1920x1080.x264",
		},
		{
			name:     "frame rate is not an episode",
			input:    "the.show.s01e02.1080p60",
			seasons:  []int{1},
			episodes: []int{10002},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			parsed := parseEpisodes(test.input)

			if !reflect.DeepEqual(parsed.Seasons, test.seasons) {
				t.Errorf("seasons = %v, want %v", parsed.Seasons, test.seasons)
			}

			if !reflect.DeepEqual(parsed.Episodes, test.episodes) {
				t.Errorf("episodes = %v, want %v", parsed.Episodes, test.episodes)
			}
		})
	}
}

func TestParseEpisodesBounded(t *testing.T) {
	t.Parallel()

	// A range that would produce nine thousand entries must be bounded.
	parsed := parseEpisodes("the.show.s01e01-e9999.1080p")

	if len(parsed.Episodes) > maxEpisodes {
		t.Fatalf("episodes = %d, want at most %d", len(parsed.Episodes), maxEpisodes)
	}

	parsed = parseEpisodes("the.show.s01-s99.1080p")

	if len(parsed.Seasons) > maxSeasons {
		t.Fatalf("seasons = %d, want at most %d", len(parsed.Seasons), maxSeasons)
	}
}

func TestParseEpisodesCommaSeparated(t *testing.T) {
	t.Parallel()

	// Commas are not exercised as episode separators; the name must still parse
	// the first pair rather than nothing.
	parsed := parseEpisodes("the.show.s01e02,e03.1080p")

	if !reflect.DeepEqual(parsed.Seasons, []int{1}) {
		t.Fatalf("seasons = %v, want [1]", parsed.Seasons)
	}
}

func FuzzParseEpisodes(f *testing.F) {
	for _, seed := range []string{
		"the.show.s01e02.1080p",
		"the.show.s01e02-e04.1080p",
		"the.show.1x02.720p",
		"the.show.s01-s03.1080p",
		"the.show.season.2.complete.1080p",
		"",
		".....",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		parsed := parseEpisodes(name)

		if len(parsed.Seasons) > maxSeasons {
			t.Fatalf("seasons = %d, want at most %d", len(parsed.Seasons), maxSeasons)
		}

		if len(parsed.Episodes) > maxEpisodes {
			t.Fatalf("episodes = %d, want at most %d", len(parsed.Episodes), maxEpisodes)
		}

		for _, season := range parsed.Seasons {
			if season < 0 || season > 99 {
				t.Fatalf("season out of range: %d", season)
			}
		}

		for _, packed := range parsed.Episodes {
			if packed < 0 || packed > 99*10000+9999 {
				t.Fatalf("packed episode out of range: %d", packed)
			}
		}
	})
}
