package dhtcrawler

import (
	"context"
	"sync"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/classify/scene"
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/store"
	"github.com/BitTrawler/bittrawler/internal/tmdb"
)

// TMDBSearcher confirms a title against TMDB. *tmdb.Client implements it.
type TMDBSearcher interface {
	Lookup(ctx context.Context, title string, year int) (tmdb.Match, bool, error)
}

// tmdbJob is a classified torrent waiting to be confirmed against TMDB. The row
// is complete except for the categories and identifier TMDB may correct.
type tmdbJob struct {
	row  store.Row
	name string
}

// startTMDB starts the confirmation workers. It does nothing when no searcher
// is configured.
func (c *Crawler) startTMDB(ctx context.Context, wg *sync.WaitGroup) {
	if c.tmdb == nil {
		return
	}

	startPool(ctx, wg, c.cfg.TMDBWorkers, c.forTMDB, &c.inFlight.tmdb, c.confirm)
}

// tmdbCandidate reports whether a classification is one TMDB can confirm or
// correct: a film or show, or an adult classification that rests only on weak
// markers and may therefore be a mainstream title. A strong adult
// classification is never sent, and neither is anything else.
func tmdbCandidate(result classify.Result) bool {
	switch {
	case result.Category >= classify.CategoryMovies && result.Category < classify.CategoryAudio:
		return true
	case result.Category >= classify.CategoryTV && result.Category < classify.CategoryXXX:
		return true
	case result.Category == classify.CategoryXXX:
		return result.AdultEvidence == classify.AdultEvidenceWeak
	default:
		return false
	}
}

// confirm looks one torrent up and forwards it, corrected if TMDB disagrees. A
// lookup error or a miss sends the row on unchanged: TMDB can confirm or
// correct, never invent.
func (c *Crawler) confirm(ctx context.Context, job tmdbJob) {
	parsed := scene.ParseTitle(job.name)
	if parsed.Title == "" {
		send(ctx, c.forPersist, job.row)

		return
	}

	lookupCtx, cancel := context.WithTimeout(ctx, c.cfg.TMDBTimeout)
	defer cancel()

	c.stats.tmdbCalls.Add(1)

	match, found, err := c.tmdb.Lookup(lookupCtx, parsed.Title, parsed.Year)
	if err != nil {
		if ctx.Err() == nil {
			c.stats.tmdbFailures.Add(1)
			c.logger.Debug("tmdb lookup failed", "title", parsed.Title, "year", parsed.Year, "error", err)
		}

		send(ctx, c.forPersist, job.row)

		return
	}

	if !found {
		c.logger.Debug("tmdb miss", "title", parsed.Title, "year", parsed.Year)
		send(ctx, c.forPersist, job.row)

		return
	}

	c.stats.tmdbMatched.Add(1)

	row, dropped := c.applyTMDBMatch(job, match)
	if dropped {
		c.stats.torrentsDropped.Add(1)
		c.logger.Debug("dropped adult torrent after tmdb match",
			"info_hash", protocol.ID(job.row.InfoHash).String(), "title", match.Title)

		return
	}

	if row.Category == classify.CategoryXXX && job.row.Category != classify.CategoryXXX {
		c.stats.tmdbAdult.Add(1)
	}

	if row.Category != job.row.Category {
		c.stats.tmdbCorrected.Add(1)
		c.logger.Debug("corrected classification against tmdb",
			"title", match.Title,
			"medium", match.Medium,
			"adult", match.Adult,
			"from", job.row.Category,
			"to", row.Category)
	}

	send(ctx, c.forPersist, row)
}

// applyTMDBMatch returns the corrected row. The second result reports that the
// row must not be stored because adult content is configured to be dropped.
//
// The rules are deliberately asymmetric. An adult flag is believed outright; a
// film found where a show was expected is believed; a show found for a film is
// believed. A show found for a weak adult classification proves nothing about
// adult content and is ignored, and a film found for a show only counts when the
// name carried no episode information.
func (c *Crawler) applyTMDBMatch(job tmdbJob, match tmdb.Match) (store.Row, bool) {
	row, dropped := c.applyTMDBMatchFamily(job, match)
	if dropped {
		return row, dropped
	}

	// The correction may have landed on a family parent; narrow it back to the
	// leaf the name and primary file justify.
	row.Category = classify.RefineCategory(row.Category, job.name, tmdbFiles(row))

	return row, false
}

// applyTMDBMatchFamily applies the confirmation rules and leaves the category
// on a family parent or resolution leaf for applyTMDBMatch to narrow.
func (c *Crawler) applyTMDBMatchFamily(job tmdbJob, match tmdb.Match) (store.Row, bool) {
	row := job.row
	current := row.Category
	adult := classify.ParentCategory(current) == classify.CategoryXXX

	switch {
	case match.Adult:
		row.Category = classify.CategoryXXX
		row.ContentID = 0
		row.Seasons = nil
		row.Episodes = nil
		row.AirDate = ""

		if c.cfg.Classification.DropAdult {
			return row, true
		}

		return row, false
	case adult:
		// A non-adult film match on a weak adult classification rescues the
		// title. A show match does not say anything about adult content.
		if match.Medium == tmdb.MediumMovie {
			row.Category = filmCategory(job.name)
			row.ContentID = match.ID
		}

		return row, false
	}

	row.ContentID = match.ID

	switch {
	case isFilmCategory(current) && match.Medium == tmdb.MediumTV:
		row.Category = tvCategoryFor(current)
	case isTVCategory(current) && match.Medium == tmdb.MediumMovie &&
		len(row.Seasons) == 0 && len(row.Episodes) == 0:
		row.Category = movieCategoryFor(current)
		row.AirDate = ""
	}

	return row, false
}

// tmdbFiles rebuilds the file list the classification leaf rules look at: the
// stored row only has the primary file, which carries the format.
func tmdbFiles(row store.Row) []classify.File {
	if row.FilesCount <= 0 || row.PrimaryFile == "" {
		return nil
	}

	return []classify.File{{Path: row.PrimaryFile, Size: row.Size}}
}

// flushTMDBQueue writes the jobs that were still queued when the crawl stopped.
// Their classification was already decided; only the confirmation was skipped,
// and losing them would lose complete work. Rows that were being looked up when
// the context was cancelled are not recovered; they are not stored, so the next
// crawl discovers them again.
func (c *Crawler) flushTMDBQueue() {
	if c.tmdb == nil {
		return
	}

	var rows []store.Row

	for {
		select {
		case job := <-c.forTMDB:
			rows = append(rows, job.row)
		default:
			if len(rows) == 0 {
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
			inserted, err := c.store.PutBatch(ctx, rows)
			cancel()

			if err != nil {
				c.stats.persistFailures.Add(1)
				c.logger.Error("failed to persist queued torrents at shutdown", "rows", len(rows), "error", err)

				return
			}

			c.stats.torrentsPersisted.Add(uint64(inserted))
			c.logger.Debug("persisted queued torrents at shutdown", "rows", inserted)

			return
		}
	}
}

// isFilmCategory reports whether a category is a film category.
func isFilmCategory(category int) bool {
	return category >= classify.CategoryMovies && category < classify.CategoryAudio
}

// isTVCategory reports whether a category is a television category.
func isTVCategory(category int) bool {
	return category >= classify.CategoryTV && category < classify.CategoryXXX
}

// tvCategoryFor keeps the resolution leaf when a film turns out to be a show.
func tvCategoryFor(category int) int {
	switch category {
	case classify.CategoryMoviesSD:
		return classify.CategoryTVSD
	case classify.CategoryMoviesHD:
		return classify.CategoryTVHD
	case classify.CategoryMoviesUHD:
		return classify.CategoryTVUHD
	default:
		return classify.CategoryTV
	}
}

// movieCategoryFor keeps the resolution leaf when a show turns out to be a film.
func movieCategoryFor(category int) int {
	switch category {
	case classify.CategoryTVSD:
		return classify.CategoryMoviesSD
	case classify.CategoryTVHD:
		return classify.CategoryMoviesHD
	case classify.CategoryTVUHD:
		return classify.CategoryMoviesUHD
	default:
		return classify.CategoryMovies
	}
}

// filmCategory returns the film category for a name, using the resolution it
// states. It is used when a weak adult classification is corrected to a film,
// where the original category carried no resolution information.
func filmCategory(name string) int {
	switch scene.Parse(name).Resolution {
	case scene.ResolutionSD:
		return classify.CategoryMoviesSD
	case scene.ResolutionHD:
		return classify.CategoryMoviesHD
	case scene.ResolutionUHD:
		return classify.CategoryMoviesUHD
	default:
		return classify.CategoryMovies
	}
}
