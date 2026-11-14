// Package reclassify re-runs the classifier over rows already in the store.
//
// It can only use what the store kept: the name, the file count and the primary
// file, not the full file list. That is enough for most rows, because the name
// and the primary file's extension carry the category signals.
package reclassify

import (
	"context"
	"fmt"
	"slices"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// reportEvery is how many scanned rows pass between progress reports.
const reportEvery = 1000

// Options controls one pass.
type Options struct {
	// OnlyCategory limits the pass to rows currently in this category; 0 means
	// every row.
	OnlyCategory int
	// Limit caps the number of scannned rows; 0 means all of them.
	Limit int
	// Apply writes the changes. Without it the pass is a dry run.
	Apply bool
	// DeleteRejected removes the rows the blocking rules now reject. It is
	// independent of Apply so a cleanup does not have to rewrite categories, and
	// it needs Apply for nothing.
	DeleteRejected bool
}

// Sample is one row whose classification would change.
type Sample struct {
	InfoHash [20]byte
	Name     string
	From     int
	To       int
}

// Rejected is a stored row that the blocking rules would reject now, with the
// category it is stored under and the reason the classifier gave.
type Rejected struct {
	InfoHash [20]byte
	Name     string
	From     int
	Reason   string
}

// Progress is a snapshot of a pass.
type Progress struct {
	// Total is the number of rows in the store when the pass started, for a
	// progress bar. Zero when the count could not be read.
	Total   int
	Scanned int
	Matched int
	Changed int
	Written int
	// Rejected lists the rows whose name or stored file now matches a blocking
	// rule. They are never rewritten: a pass either deletes them or reports them.
	Rejected []Rejected
	// Deleted counts the rejected rows removed by this pass.
	Deleted      int
	ByTransition map[string]int
	Samples      []Sample
	Done         bool
}

// Run classifies the stored rows, reports progress and, when Options.Apply is
// set, writes the changes. It streams the store, so a large index does not have
// to fit in memory.
func Run(ctx context.Context, torrents *store.Store, classifier *classify.Classifier, opts Options, report func(Progress)) (Progress, error) {
	progress := Progress{ByTransition: make(map[string]int)}
	changes := make([]store.Classified, 0)
	removals := make([]store.Removal, 0)

	if stats, err := torrents.Stats(ctx); err == nil {
		progress.Total = stats.Rows
	}

	emit := func(done bool) {
		progress.Done = done
		if report != nil {
			report(snapshot(progress))
		}
	}

	scanErr := torrents.ForEach(ctx, func(torrent store.Torrent) error {
		progress.Scanned++

		if opts.Limit > 0 && progress.Scanned > opts.Limit {
			return nil
		}

		if opts.OnlyCategory > 0 && torrent.Category != opts.OnlyCategory {
			return nil
		}

		progress.Matched++

		result := classifier.Classify(torrent.Name, storedFiles(torrent))

		// Writing a dropped classification would set the row's category to the
		// zero value, so these rows are either removed by the cleanup or
		// reported for someone to look at.
		if result.Drop != classify.DropNone {
			progress.Rejected = append(progress.Rejected, Rejected{
				InfoHash: torrent.InfoHash,
				Name:     torrent.Name,
				From:     torrent.Category,
				Reason:   result.Drop.String(),
			})

			if opts.DeleteRejected {
				removals = append(removals, store.Removal{
					InfoHash:  torrent.InfoHash,
					CreatedAt: torrent.CreatedAt,
					Category:  torrent.Category,
				})
			}

			return nil
		}

		// Adult evidence can sit in the full file list, which the store does not
		// keep. A pass can add adult evidence, but it cannot take it away.
		if torrent.Category == classify.CategoryXXX && result.Category != classify.CategoryXXX {
			return nil
		}

		if slices.Equal(result.Seasons, torrent.Seasons) &&
			slices.Equal(result.Episodes, torrent.Episodes) &&
			result.Category == torrent.Category &&
			result.AirDate == torrent.AirDate {
			return nil
		}

		progress.Changed++

		key := fmt.Sprintf("%s -> %s", classify.CategoryName(torrent.Category), classify.CategoryName(result.Category))
		progress.ByTransition[key]++

		progress.Samples = append(progress.Samples, Sample{
			InfoHash: torrent.InfoHash,
			Name:     torrent.Name,
			From:     torrent.Category,
			To:       result.Category,
		})

		if opts.Apply {
			changes = append(changes, store.Classified{
				InfoHash:  torrent.InfoHash,
				CreatedAt: torrent.CreatedAt,
				Category:  result.Category,
				Seasons:   result.Seasons,
				Episodes:  result.Episodes,
				AirDate:   result.AirDate,
			})
		}

		if progress.Scanned%reportEvery == 0 {
			emit(false)
		}

		return nil
	})

	if scanErr != nil {
		return snapshot(progress), scanErr
	}

	if opts.Limit > 0 && progress.Scanned > opts.Limit {
		progress.Scanned = opts.Limit
	}

	if opts.Apply && len(changes) > 0 {
		written, err := torrents.UpdateClassifications(ctx, changes)
		if err != nil {
			return snapshot(progress), err
		}

		progress.Written = written
	}

	if opts.DeleteRejected && len(removals) > 0 {
		deleted, err := torrents.Delete(ctx, removals)
		progress.Deleted = deleted

		if err != nil {
			return snapshot(progress), err
		}

		// The rows are gone; listing them again would only offer dead links.
		progress.Rejected = nil
	}

	emit(true)

	return snapshot(progress), nil
}

// storedFiles rebuilds the classifier's file list from the stored primary file,
// with the torrent's total size as a hint. The extension is what matters.
func storedFiles(torrent store.Torrent) []classify.File {
	if torrent.FilesCount <= 0 || torrent.PrimaryFile == "" {
		return nil
	}

	return []classify.File{{Path: torrent.PrimaryFile, Size: torrent.Size}}
}

// snapshot copies the progress so a reporting callback cannot mutate the pass.
func snapshot(progress Progress) Progress {
	out := progress
	out.ByTransition = make(map[string]int, len(progress.ByTransition))
	for key, count := range progress.ByTransition {
		out.ByTransition[key] = count
	}

	out.Samples = append([]Sample{}, progress.Samples...)
	out.Rejected = append([]Rejected{}, progress.Rejected...)

	return out
}
