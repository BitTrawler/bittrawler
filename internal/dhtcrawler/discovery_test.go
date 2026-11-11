package dhtcrawler

import (
	"testing"
	"time"
)

func TestSampleInterval(t *testing.T) {
	honouring := &Crawler{cfg: Config{}}
	overriding := &Crawler{cfg: Config{SampleInfoHashesOverride: time.Minute}}

	tests := []struct {
		name       string
		crawler    *Crawler
		requested  int
		productive bool
		want       time.Duration
	}{
		{
			name:      "a missing interval is raised to the minimum",
			crawler:   honouring,
			requested: 0,
			want:      time.Minute,
		},
		{
			name:      "an interval below the minimum is raised to it",
			crawler:   honouring,
			requested: 10,
			want:      time.Minute,
		},
		{
			name:      "the requested interval is honoured",
			crawler:   honouring,
			requested: 3600,
			want:      time.Hour,
		},
		{
			name:       "a productive node is sampled sooner when an override is set",
			crawler:    overriding,
			requested:  3600,
			productive: true,
			want:       time.Minute,
		},
		{
			name:      "an unproductive node keeps its interval despite the override",
			crawler:   overriding,
			requested: 3600,
			want:      time.Hour,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.crawler.sampleInterval(test.requested, test.productive); got != test.want {
				t.Errorf("interval = %s, want %s", got, test.want)
			}
		})
	}
}
