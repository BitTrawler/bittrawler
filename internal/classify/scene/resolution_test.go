package scene

import (
	"testing"
	"time"
)

func TestParseResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  Resolution
	}{
		{"movie.2020.1080p.bluray", ResolutionHD},
		{"movie.2020.720p.web-dl", ResolutionHD},
		{"movie.2020.2160p.uhd", ResolutionUHD},
		{"movie.2020.4k.hdr", ResolutionUHD},
		{"movie.2020.8k", ResolutionUHD},
		{"movie.2020.1080i", ResolutionHD},
		{"movie.2020.1440p", ResolutionHD},
		{"movie.2020.480p", ResolutionSD},
		{"movie.2020.576p", ResolutionSD},
		{"movie.2020.sd.xvid", ResolutionSD},
		{"movie.2020.hd", ResolutionHD},
		{"movie.2020.1920x1080", ResolutionHD},
		{"movie.2020.3840x2160", ResolutionUHD},
		{"movie.2020.720x480", ResolutionSD},
		{"movie.2020.1080p60", ResolutionHD},
		{"movie.2020.2160p.1080p", ResolutionUHD},
		{"movie.2020.hdtv.x264", ResolutionUnknown},
		{"movie.2020.x265.hevc", ResolutionUnknown},
		{"movie.2020.hdr", ResolutionUnknown},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()

			if got := parseResolution(test.input); got != test.want {
				t.Errorf("parseResolution(%q) = %v, want %v", test.input, got, test.want)
			}
		})
	}
}

func TestResolutionString(t *testing.T) {
	t.Parallel()

	for resolution, want := range map[Resolution]string{
		ResolutionUnknown: "unknown",
		ResolutionSD:      "SD",
		ResolutionHD:      "HD",
		ResolutionUHD:     "UHD",
	} {
		if got := resolution.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", int(resolution), got, want)
		}
	}
}

func TestDimensions(t *testing.T) {
	t.Parallel()

	if width, height, ok := dimensions("1920x1080"); !ok || width != 1920 || height != 1080 {
		t.Fatalf("dimensions = %d, %d, %v; want 1920, 1080, true", width, height, ok)
	}

	for _, token := range []string{"x265", "1920", "1920x", "x1080", "1920x1080p", "1920x1080x720"} {
		if _, _, ok := dimensions(token); ok {
			t.Errorf("dimensions(%q) accepted", token)
		}
	}
}

func FuzzParseName(f *testing.F) {
	for _, seed := range []string{
		"movie.2020.1080p.bluray.x264-grp",
		"the.show.s01e02.720p",
		"the.show.2023.02.01.1080p",
		"",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		parsed := Parse(name)

		if parsed.Resolution < ResolutionUnknown || parsed.Resolution > ResolutionUHD {
			t.Fatalf("resolution out of range: %d", parsed.Resolution)
		}

		if parsed.AirDate != "" {
			if _, err := time.Parse(time.DateOnly, parsed.AirDate); err != nil {
				t.Fatalf("invalid air date: %q", parsed.AirDate)
			}
		}
	})
}
