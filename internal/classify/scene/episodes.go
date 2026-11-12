package scene

import (
	"maps"
	"regexp"
	"slices"
)

// Limits keep a pathological name from expanding into an unbounded set. The
// grammar only allows two-digit seasons and four-digit episodes, so these are
// far above anything a real release name produces; they exist so that a range
// such as "S01E01-E9999" cannot allocate thousands of entries.
const (
	maxSeasons     = 100
	maxEpisodes    = 500
	maxEpisodeSpan = 100
)

var (
	// pairRegex matches a season and episode marker: "s01e02", "s01.e02",
	// "s1e2", "s01-e02". There is deliberately no trailing boundary: in
	// "s01e02e03" the next marker follows immediately and is read as an extra.
	pairRegex = regexp.MustCompile(`\bs(\d{1,2})[._ -]?e(\d{1,4})`)

	// extrasRegex matches the episode markers after the first pair. A further
	// episode needs an "e" prefix or a "-" range marker, which is what stops
	// ".1080p" from being read as episode 1080.
	extrasRegex = regexp.MustCompile(`^(?:[._ -]?e\d{1,4}|[._ -]?-[._ -]?e?\d{1,4})+`)

	// xFormatRegex matches "1x02" and "1x02-04".
	xFormatRegex = regexp.MustCompile(`\b(\d{1,2})x(\d{1,4})(?:[._ -]?-[._ -]?(?:\d{1,2}x)?(\d{1,4}))?\b`)

	// seasonRangeRegex matches "Seasons 1-3" and its translations; the short
	// form matches the far more common "S01-S03".
	seasonRangeRegex      = regexp.MustCompile(`\b(?:seasons?|temporadas?|staffel|saison|сезон)[._ -]?(\d{1,2})[._ -]?-[._ -]?s?(\d{1,2})\b`)
	shortSeasonRangeRegex = regexp.MustCompile(`\bs(\d{1,2})[._ -]?-[._ -]?s?(\d{1,2})\b`)

	// seasonEpisodeRegex matches "Season 1", "Season 1 Episode 2", "Season.1",
	// and the same forms in the languages release names use for series seasons.
	seasonEpisodeRegex = regexp.MustCompile(
		`\b(?:seasons?|temporadas?|staffel|saison|сезон)[._ -]?(\d{1,2})(?:[._ -]?(?:episodes?|eps?|e)[._ -]?(\d{1,4}))?\b`)

	// seasonBareRegex matches an "S01" season pack with no episode marker.
	seasonBareRegex = regexp.MustCompile(`\bs(\d{1,2})\b`)

	// seriesRegex matches releases that are marked as complete but carry no
	// season number.
	seriesRegex = regexp.MustCompile(`\bcomplete[._ -](?:series|seasons?)\b`)
)

// parseEpisodes reads the season and episode markers from a lowercased name.
//
// The first matching form wins. Order matters: "s01e02" must be read as an
// episode before "s01" is read as a season pack, and the x-format comes before
// the season forms so that "1x02" is not read as a season.
func parseEpisodes(lower string) Episodes {
	set := newEpisodeSet()

	if pairs := pairRegex.FindAllStringSubmatchIndex(lower, -1); len(pairs) > 0 {
		for i, pair := range pairs {
			season := atoi(lower[pair[2]:pair[3]])
			episode := atoi(lower[pair[4]:pair[5]])

			set.addEpisode(season, episode)

			if i == 0 {
				if extras := extrasRegex.FindString(lower[pair[1]:]); extras != "" {
					parseEpisodeExtras(extras, season, episode, set)
				}
			}
		}

		return set.result()
	}

	if match := xFormatRegex.FindStringSubmatch(lower); match != nil {
		season := atoi(match[1])
		start := atoi(match[2])
		end := start

		if match[3] != "" {
			end = atoi(match[3])
		}

		for episode := start; episode <= end && episode-start <= maxEpisodeSpan; episode++ {
			set.addEpisode(season, episode)
		}

		return set.result()
	}

	if match := seasonRangeRegex.FindStringSubmatch(lower); match != nil {
		addSeasonRange(set, atoi(match[1]), atoi(match[2]))

		return set.result()
	}

	if match := shortSeasonRangeRegex.FindStringSubmatch(lower); match != nil {
		addSeasonRange(set, atoi(match[1]), atoi(match[2]))

		return set.result()
	}

	if match := seasonEpisodeRegex.FindStringSubmatch(lower); match != nil {
		season := atoi(match[1])

		if match[2] != "" {
			set.addEpisode(season, atoi(match[2]))
		} else {
			set.addSeason(season)
		}

		return set.result()
	}

	if match := seasonBareRegex.FindStringSubmatch(lower); match != nil {
		set.addSeason(atoi(match[1]))
	}

	return set.result()
}

// addSeasonRange adds every season in a range such as "S01-S03". A reversed
// range is treated as a single season rather than as an error.
func addSeasonRange(set *episodeSet, start, end int) {
	if end < start {
		set.addSeason(start)
		return
	}

	for season := start; season <= end; season++ {
		set.addSeason(season)
	}
}

// parseEpisodeExtras reads the markers after the first season/episode pair:
// "e03", "-e04", "-04", possibly in sequence. A dash starts a range from the
// previous number, so "s01e02-e04" yields episodes 2, 3 and 4.
func parseEpisodeExtras(extras string, season, first int, set *episodeSet) {
	last := first

	for i := 0; i < len(extras); {
		for i < len(extras) && isSeparator(extras[i]) {
			i++
		}

		isRange := false

		if i < len(extras) && extras[i] == '-' {
			isRange = true
			i++

			for i < len(extras) && isSeparator(extras[i]) {
				i++
			}
		}

		if i < len(extras) && extras[i] == 'e' {
			i++
		}

		start := i

		for i < len(extras) && extras[i] >= '0' && extras[i] <= '9' {
			i++
		}

		if start == i {
			return
		}

		episode := atoi(extras[start:i])

		if isRange && episode > last && episode-last <= maxEpisodeSpan {
			for e := last + 1; e <= episode; e++ {
				set.addEpisode(season, e)
			}
		} else {
			set.addEpisode(season, episode)
		}

		last = episode
	}
}

func isSeparator(c byte) bool {
	return c == '.' || c == '_' || c == ' '
}

// episodeSet collects seasons and episodes, deduplicating and bounding them.
type episodeSet struct {
	seasons  map[int]struct{}
	episodes map[int]struct{}
}

func newEpisodeSet() *episodeSet {
	return &episodeSet{
		seasons:  make(map[int]struct{}),
		episodes: make(map[int]struct{}),
	}
}

func (s *episodeSet) addSeason(season int) {
	if season < 0 || season > 99 || len(s.seasons) >= maxSeasons {
		return
	}

	s.seasons[season] = struct{}{}
}

func (s *episodeSet) addEpisode(season, episode int) {
	if season < 0 || season > 99 || episode < 0 || episode > 9999 {
		return
	}

	s.addSeason(season)

	if len(s.episodes) >= maxEpisodes {
		return
	}

	s.episodes[season*10000+episode] = struct{}{}
}

func (s *episodeSet) result() Episodes {
	return Episodes{
		Seasons:  slices.Sorted(maps.Keys(s.seasons)),
		Episodes: slices.Sorted(maps.Keys(s.episodes)),
	}
}
