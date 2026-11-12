// Package tmdb confirms what a release is against The Movie Database.
//
// Classification reads names and files; TMDB knows which titles exist and
// whether they are films, shows or adult features. A match may confirm or
// correct a classification, never invent one: most adult releases are absent
// from TMDB, so a miss proves nothing.
//
// The client keeps a bounded in-memory cache of lookup results, rate limits its
// requests and stops calling after repeated failures. With no token configured
// it is not constructed at all.
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/time/rate"
)

// ErrUnavailable reports that TMDB could not answer, either because the circuit
// breaker is open or because no request could be completed. Callers keep the
// classification they already have.
var ErrUnavailable = errors.New("tmdb: unavailable")

// Medium is what a title is.
type Medium string

const (
	MediumMovie Medium = "movie"
	MediumTV    Medium = "tv"
)

// Match is a title confirmed against TMDB.
type Match struct {
	// ID is the TMDB identifier. Film and television identifiers are separate
	// spaces; the medium disambiguates.
	ID int64
	// Medium is whether the title is a film or a show.
	Medium Medium
	// Adult reports that TMDB flags the film as adult.
	Adult bool
	// Title is the matched title as TMDB spells it.
	Title string
	// Year is the matched release or first-air year, zero when unknown.
	Year int
	// Popularity is TMDB's popularity score for the title. It is only used to
	// break ties between a film and a show with the same name.
	Popularity float64
}

// Stats is the client's lifetime counters.
type Stats struct {
	// Requests counts HTTP requests made.
	Requests uint64
	// CacheHits counts lookups answered entirely from the cache.
	CacheHits uint64
	// Misses counts lookups that found no confident match.
	Misses uint64
	// Errors counts lookups that failed.
	Errors uint64
}

// Config configures the client. Only Token is required.
type Config struct {
	// Token is a TMDB v4 read access token, sent as a bearer token.
	Token string
	// BaseURL overrides the API root, for tests. Defaults to the real API.
	BaseURL string
	// HTTPClient overrides the HTTP client.
	HTTPClient *http.Client
	// Timeout bounds one HTTP request. Defaults to five seconds.
	Timeout time.Duration
	// RateLimit is the request rate in requests per second. Defaults to four.
	RateLimit float64
	// Burst is the rate limiter's burst size. Defaults to four.
	Burst int
	// CacheSize is the number of cached lookups. Defaults to 50,000.
	CacheSize int
	// PositiveTTL is how long a found match is cached. Defaults to seven days.
	PositiveTTL time.Duration
	// NegativeTTL is how long a miss is cached. Defaults to one day.
	NegativeTTL time.Duration
	// FailureLimit is how many consecutive failures open the breaker. Defaults
	// to five.
	FailureLimit int
	// FailureCooldown is how long the breaker stays open. Defaults to thirty
	// seconds.
	FailureCooldown time.Duration
}

// Client answers title lookups. It is safe for concurrent use.
type Client struct {
	baseURL      string
	token        string
	http         *http.Client
	limiter      *rate.Limiter
	cache        *lru.Cache[string, cacheEntry]
	positiveTTL  time.Duration
	negativeTTL  time.Duration
	failureLimit int
	failureCool  time.Duration

	requests  atomic.Uint64
	cacheHits atomic.Uint64
	misses    atomic.Uint64
	errors    atomic.Uint64

	breakerMu    sync.Mutex
	failures     int
	breakerUntil time.Time
}

// New validates the configuration and returns a client.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("tmdb: Token is required")
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.themoviedb.org/3"
	}

	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{}
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}

	cfg.HTTPClient.Timeout = cfg.Timeout

	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 4
	}

	if cfg.Burst <= 0 {
		cfg.Burst = 4
	}

	if cfg.CacheSize <= 0 {
		cfg.CacheSize = 50_000
	}

	if cfg.PositiveTTL <= 0 {
		cfg.PositiveTTL = 7 * 24 * time.Hour
	}

	if cfg.NegativeTTL <= 0 {
		cfg.NegativeTTL = 24 * time.Hour
	}

	if cfg.FailureLimit <= 0 {
		cfg.FailureLimit = 5
	}

	if cfg.FailureCooldown <= 0 {
		cfg.FailureCooldown = 30 * time.Second
	}

	cache, err := lru.New[string, cacheEntry](cfg.CacheSize)
	if err != nil {
		return nil, fmt.Errorf("tmdb: cache: %w", err)
	}

	return &Client{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		token:        cfg.Token,
		http:         cfg.HTTPClient,
		limiter:      rate.NewLimiter(rate.Limit(cfg.RateLimit), cfg.Burst),
		cache:        cache,
		positiveTTL:  cfg.PositiveTTL,
		negativeTTL:  cfg.NegativeTTL,
		failureLimit: cfg.FailureLimit,
		failureCool:  cfg.FailureCooldown,
	}, nil
}

// Stats returns the client's counters.
func (c *Client) Stats() Stats {
	return Stats{
		Requests:  c.requests.Load(),
		CacheHits: c.cacheHits.Load(),
		Misses:    c.misses.Load(),
		Errors:    c.errors.Load(),
	}
}

// candidate is one search result, reduced to what matching needs.
type candidate struct {
	id         int64
	title      string
	year       int
	adult      bool
	popularity float64
}

// cacheEntry is one cached search, positive or negative.
type cacheEntry struct {
	candidates []candidate
	expires    time.Time
}

// movieResult is the part of a search/movie result matching reads.
type movieResult struct {
	ID         int64   `json:"id"`
	Title      string  `json:"title"`
	Release    string  `json:"release_date"`
	Adult      bool    `json:"adult"`
	Popularity float64 `json:"popularity"`
}

// showResult is the part of a search/tv result matching reads.
type showResult struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	FirstAir   string  `json:"first_air_date"`
	Popularity float64 `json:"popularity"`
}

// searchMovie returns the film results for a query. The year is not sent to
// the API: TMDB filters it strictly, and a release year one year out is common
// in names, so the year is checked against the results instead.
func (c *Client) searchMovie(ctx context.Context, query string) ([]candidate, error) {
	values := url.Values{}
	values.Set("query", query)
	values.Set("include_adult", "true")

	body, err := c.get(ctx, "/search/movie", values)
	if err != nil {
		return nil, err
	}

	var response struct {
		Results []movieResult `json:"results"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("tmdb: decode movie results: %w", err)
	}

	out := make([]candidate, 0, len(response.Results))
	for _, result := range response.Results {
		out = append(out, candidate{
			id:         result.ID,
			title:      result.Title,
			year:       yearOf(result.Release),
			adult:      result.Adult,
			popularity: result.Popularity,
		})
	}

	return out, nil
}

// searchTV returns the show results for a query. The search/tv endpoint has no
// adult flag; adult shows are overwhelmingly absent from TMDB instead.
func (c *Client) searchTV(ctx context.Context, query string) ([]candidate, error) {
	values := url.Values{}
	values.Set("query", query)

	body, err := c.get(ctx, "/search/tv", values)
	if err != nil {
		return nil, err
	}

	var response struct {
		Results []showResult `json:"results"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("tmdb: decode tv results: %w", err)
	}

	out := make([]candidate, 0, len(response.Results))
	for _, result := range response.Results {
		out = append(out, candidate{
			id:         result.ID,
			title:      result.Name,
			year:       yearOf(result.FirstAir),
			popularity: result.Popularity,
		})
	}

	return out, nil
}

// get performs one rate-limited, breaker-protected request and returns the
// response body.
func (c *Client) get(ctx context.Context, path string, values url.Values) ([]byte, error) {
	if err := c.available(); err != nil {
		return nil, err
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	endpoint := c.baseURL + path + "?" + values.Encode()

	var lastErr error

	for attempt := 0; attempt < 2; attempt++ {
		body, retryAfter, err := c.do(ctx, endpoint)
		if err == nil {
			c.recordSuccess()

			return body, nil
		}

		lastErr = err

		if retryAfter <= 0 || ctx.Err() != nil {
			break
		}

		select {
		case <-ctx.Done():
			c.recordFailure()

			return nil, ctx.Err()
		case <-time.After(retryAfter):
		}
	}

	c.recordFailure()
	c.errors.Add(1)

	return nil, lastErr
}

// do performs one HTTP request. It returns a retry delay for a transient error.
func (c *Client) do(ctx context.Context, endpoint string) ([]byte, time.Duration, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}

	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "bittrawler")

	c.requests.Add(1)

	response, err := c.http.Do(request)
	if err != nil {
		return nil, 0, err
	}

	defer response.Body.Close()

	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		retryAfter := time.Second

		if value, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && value > 0 {
			retryAfter = time.Duration(min(value, 2)) * time.Second
		}

		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))

		return nil, retryAfter, fmt.Errorf("tmdb: status %s", response.Status)
	}

	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<10))

		return nil, 0, fmt.Errorf("tmdb: status %s", response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, 0, err
	}

	return body, 0, nil
}

// available reports an open circuit breaker as ErrUnavailable.
func (c *Client) available() error {
	c.breakerMu.Lock()
	defer c.breakerMu.Unlock()

	if time.Now().Before(c.breakerUntil) {
		return ErrUnavailable
	}

	return nil
}

func (c *Client) recordSuccess() {
	c.breakerMu.Lock()
	defer c.breakerMu.Unlock()

	c.failures = 0
	c.breakerUntil = time.Time{}
}

func (c *Client) recordFailure() {
	c.breakerMu.Lock()
	defer c.breakerMu.Unlock()

	c.failures++

	if c.failures >= c.failureLimit {
		c.breakerUntil = time.Now().Add(c.failureCool)
		c.failures = 0
	}
}

// cached returns a cached search answer, if one is still fresh.
func (c *Client) cached(key string) ([]candidate, bool) {
	entry, ok := c.cache.Get(key)
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}

	return entry.candidates, true
}

// store caches a search answer.
func (c *Client) store(key string, candidates []candidate) {
	ttl := c.positiveTTL
	if len(candidates) == 0 {
		ttl = c.negativeTTL
	}

	c.cache.Add(key, cacheEntry{candidates: candidates, expires: time.Now().Add(ttl)})
}

// yearOf parses the year from an ISO date, returning zero when absent.
func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}

	year, err := strconv.Atoi(date[:4])
	if err != nil || year < 1900 || year > 2099 {
		return 0
	}

	return year
}
