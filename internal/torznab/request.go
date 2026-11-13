package torznab

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// unset marks a filter the request did not ask for.
const unset = -1

// SearchRequest is one parsed Torznab request.
type SearchRequest struct {
	// Function is the t parameter.
	Function Function
	// Query is the q parameter, the release text to look for.
	Query string
	// Categories are the requested Newznab category numbers. A parent number
	// matches its children in the store.
	Categories []int
	// Season and Episode filter television, unset when the request does not
	// ask for them.
	Season  int
	Episode int
	// AirDate is the ISO date of a daily episode. Sonarr sends one in the
	// episode parameter when the season is a year.
	AirDate string
	// TMDBID is the id filter for a film or show, 0 when unset.
	TMDBID int64
	// Limit and Offset are the requested page.
	Limit  int
	Offset int
	// APIKey authenticates the request.
	APIKey string
}

// ParseRequest parses a Torznab request. A malformed request returns an Error
// rather than an HTTP status: the clients expect to read the error element out
// of a successful response.
func ParseRequest(r *http.Request, defaultLimit, maxLimit int) (SearchRequest, error) {
	values := r.URL.Query()

	req := SearchRequest{
		Function: Function(strings.TrimSpace(values.Get(ParamFunction))),
		Query:    strings.TrimSpace(values.Get(ParamQuery)),
		Season:   unset,
		Episode:  unset,
		Limit:    defaultLimit,
		APIKey:   values.Get(ParamAPIKey),
	}

	if req.APIKey == "" {
		req.APIKey = r.Header.Get("X-Api-Key")
	}

	if req.Function == "" {
		return req, Error{Code: CodeMissingParameter, Description: "missing parameter (t)"}
	}

	categories, err := parseCategories(values[ParamCategory])
	if err != nil {
		return req, err
	}

	req.Categories = categories

	if err := parseEpisodeFilters(&req, values); err != nil {
		return req, err
	}

	if raw := values.Get(ParamTMDBID); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 0 {
			return req, Error{Code: CodeIncorrectParameter, Description: fmt.Sprintf("incorrect parameter (tmdbid=%s)", raw)}
		}

		req.TMDBID = id
	}

	if err := parsePage(&req, values, maxLimit); err != nil {
		return req, err
	}

	return req, nil
}

// parseCategories reads every cat parameter, each of which may hold a comma
// separated list.
func parseCategories(raw []string) ([]int, error) {
	var out []int

	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			cat, err := strconv.Atoi(part)
			if err != nil || cat < 0 {
				return nil, Error{Code: CodeIncorrectParameter, Description: fmt.Sprintf("incorrect parameter (cat=%s)", part)}
			}

			// cat=0 is how some clients mean "no filter".
			if cat == 0 {
				continue
			}

			out = append(out, cat)
		}
	}

	return out, nil
}

// parseEpisodeFilters reads season and ep. Sonarr sends season as a number for
// a normal episode, and sends the year with ep as MM/dd for a daily one.
func parseEpisodeFilters(req *SearchRequest, values url.Values) error {
	raw := values.Get(ParamSeason)
	if raw != "" {
		season, err := strconv.Atoi(raw)
		if err != nil || season < 0 {
			return Error{Code: CodeIncorrectParameter, Description: fmt.Sprintf("incorrect parameter (season=%s)", raw)}
		}

		req.Season = season
	}

	raw = values.Get(ParamEpisode)
	if raw == "" {
		return nil
	}

	if req.Season == unset {
		return Error{Code: CodeIncorrectParameter, Description: "incorrect parameter (ep without season)"}
	}

	if episode, ok := parseNumber(raw); ok {
		req.Episode = episode

		return nil
	}

	airDate, ok := parseDailyEpisode(raw, req.Season)
	if !ok {
		return Error{Code: CodeIncorrectParameter, Description: fmt.Sprintf("incorrect parameter (ep=%s)", raw)}
	}

	// The season was the year, not a season number, so it must not also filter
	// the season tokens.
	req.AirDate = airDate
	req.Season = unset

	return nil
}

// parseDailyEpisode reads the MM/dd form Sonarr sends for a daily episode and
// returns the ISO date. The year comes from the season parameter.
func parseDailyEpisode(ep string, year int) (string, bool) {
	parts := strings.FieldsFunc(ep, func(r rune) bool { return r == '/' || r == '-' })

	if len(parts) != 2 {
		return "", false
	}

	month, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", false
	}

	day, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", false
	}

	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if int(date.Month()) != month || date.Day() != day {
		return "", false
	}

	return date.Format("2006-01-02"), true
}

// parseNumber reads a non-negative integer.
func parseNumber(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}

	return n, true
}

// parsePage reads limit and offset and clamps the page size to what the
// endpoint will serve.
func parsePage(req *SearchRequest, values url.Values, maxLimit int) error {
	if raw := values.Get(ParamLimit); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return Error{Code: CodeIncorrectParameter, Description: fmt.Sprintf("incorrect parameter (limit=%s)", raw)}
		}

		req.Limit = limit
	}

	if req.Limit > maxLimit {
		req.Limit = maxLimit
	}

	if raw := values.Get(ParamOffset); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return Error{Code: CodeIncorrectParameter, Description: fmt.Sprintf("incorrect parameter (offset=%s)", raw)}
		}

		req.Offset = offset
	}

	return nil
}
