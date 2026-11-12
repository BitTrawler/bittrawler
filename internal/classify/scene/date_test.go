package scene

import "testing"

func TestParseDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"the.show.2023-02-01.1080p", "2023-02-01"},
		{"exclusive release (may 5, 2016) 1080p", "2016-05-05"},
		{"the.show.2023.02.01.1080p", "2023-02-01"},
		{"the.show.01-02-2023.1080p", "2023-02-01"},
		{"the.show.01.feb.2023.1080p", "2023-02-01"},
		{"the.show.feb.01.2023.1080p", "2023-02-01"},
		{"the.show.2023.february.01.1080p", "2023-02-01"},
		{"the.show.23.02.01.1080p", "2023-02-01"},
		{"mp3-daily-2019-july-16-disco", "2019-07-16"},
		{"the.movie.2017.10.13.1080p", "2017-10-13"},
		{"the.55th.annual.grammy.awards.2023.02.05.1080p", "2023-02-05"},
		{"movie.2019.1080p.bluray", ""},
		{"movie.2019.05.1080p", ""},
		{"movie.s01e02.1080p", ""},
		{"adobe.photoshop.2025.26.6.1", ""},
		{"unity.2021.3.10f1", ""},
		{"the.100.s01e01.1080p", ""},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()

			if got := parseDate(test.input); got != test.want {
				t.Errorf("parseDate(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestParseDateRejectsInvalidCalendarDates(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"the.show.2023.02.31.1080p",
		"the.show.2023.13.01.1080p",
		"the.show.2023.00.01.1080p",
	} {
		if got := parseDate(input); got != "" {
			t.Errorf("parseDate(%q) = %q, want no date", input, got)
		}
	}
}

func TestFormatDate(t *testing.T) {
	t.Parallel()

	if got, ok := formatDate(2024, 2, 29); !ok || got != "2024-02-29" {
		t.Fatalf("leap day = %q, %v; want 2024-02-29, true", got, ok)
	}

	if _, ok := formatDate(2023, 2, 29); ok {
		t.Fatal("accepted 29 February in a non-leap year")
	}

	if _, ok := formatDate(1899, 12, 31); ok {
		t.Fatal("accepted a year before 1900")
	}
}
