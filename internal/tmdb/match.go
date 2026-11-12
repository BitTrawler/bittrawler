package tmdb

import (
	"context"
	"strings"
	"unicode"
)

// Lookup searches TMDB for a title and returns a confident match.
//
// It returns found=false when TMDB has no candidate whose title and year match,
// and also when both a film and a show match about equally well: the classifier
// is not asked to guess. An error means TMDB could not answer; the caller keeps
// its existing classification.
func (c *Client) Lookup(ctx context.Context, title string, year int) (Match, bool, error) {
	query := strings.TrimSpace(title)
	normalized := normalizeTitle(query)
	if normalized == "" {
		return Match{}, false, nil
	}

	movies, err := c.searchCached(ctx, MediumMovie, query, normalized)
	if err != nil {
		return Match{}, false, err
	}

	shows, err := c.searchCached(ctx, MediumTV, query, normalized)
	if err != nil {
		return Match{}, false, err
	}

	match, found := chooseMatch(normalized, year, movies, shows)
	if !found {
		c.misses.Add(1)

		return Match{}, false, nil
	}

	return match, true, nil
}

// searchCached answers one search, from the cache when it can.
func (c *Client) searchCached(
	ctx context.Context,
	medium Medium,
	query string,
	normalized string,
) ([]candidate, error) {
	key := string(medium) + "|" + normalized

	if candidates, ok := c.cached(key); ok {
		c.cacheHits.Add(1)

		return candidates, nil
	}

	var (
		candidates []candidate
		err        error
	)

	if medium == MediumMovie {
		candidates, err = c.searchMovie(ctx, query)
	} else {
		candidates, err = c.searchTV(ctx, query)
	}

	if err != nil {
		return nil, err
	}

	c.store(key, candidates)

	return candidates, nil
}

// chooseMatch picks a match from the two result sets. A film wins when TMDB
// flags it adult: that flag is the strongest evidence available. Otherwise the
// more popular medium wins, but only by a clear margin; a near tie is no match
// at all.
func chooseMatch(query string, year int, movies, shows []candidate) (Match, bool) {
	film := bestCandidate(query, year, movies)
	show := bestCandidate(query, year, shows)

	switch {
	case film == nil && show == nil:
		return Match{}, false
	case film != nil && show == nil:
		return matchOf(*film, MediumMovie), true
	case show != nil && film == nil:
		return matchOf(*show, MediumTV), true
	}

	if film.adult {
		return matchOf(*film, MediumMovie), true
	}

	// The margin keeps remakes and same-name titles from flipping a medium on a
	// popularity tie.
	switch {
	case film.popularity >= 2*show.popularity:
		return matchOf(*film, MediumMovie), true
	case show.popularity >= 2*film.popularity:
		return matchOf(*show, MediumTV), true
	default:
		return Match{}, false
	}
}

// bestCandidate returns the most popular candidate whose title matches exactly
// and whose year is within a year of the requested one.
func bestCandidate(query string, year int, candidates []candidate) *candidate {
	var best *candidate

	for i := range candidates {
		candidate := candidates[i]

		if normalizeTitle(candidate.title) != query {
			continue
		}

		if year > 0 {
			if candidate.year == 0 || abs(candidate.year-year) > 1 {
				continue
			}
		}

		if best == nil || candidate.popularity > best.popularity {
			best = &candidate
		}
	}

	return best
}

// matchOf converts a candidate to a Match.
func matchOf(candidate candidate, medium Medium) Match {
	return Match{
		ID:         candidate.id,
		Medium:     medium,
		Adult:      candidate.adult,
		Title:      candidate.title,
		Year:       candidate.year,
		Popularity: candidate.popularity,
	}
}

// normalizeTitle reduces a title to lowercase letters and digits for
// comparison, so "Some.Movie" and "some movie" are the same title.
func normalizeTitle(title string) string {
	var b strings.Builder

	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}

	return b.String()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}
