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

const (
	// reportEvery is how many scanned rows pass between progress reports.
	reportEvery = 1000
	// sampleLimit is how many changed rows are kept as examples.
	sampleLimit = 20
)

// Options controls one pass.
type Options struct {
	// OnlyCategory limits the pass to rows currently in this category; 0 means
	// every row.
	OnlyCategory int
	// Limit caps the number of scannned rows; 0 means all of them.
	Limit int
	// Apply writes the changes. Without it the pass is a dry run.
	Apply bool
}

// Sample is one row whose classification would change.
type Sample struct {
	InfoHash [20]byte
	Name     string
	From     int
	To       int
}

// Progress is a snapshot of a pass.
type Progress struct {
	Scanned      int
	Matched      int
	Changed      int
	Written      int
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

		if slices.Equal(result.Seasons, torrent.Seasons) &&
			slices.Equal(result.Episodes, torrent.Episodes) &&
			result.Category == torrent.Category &&
			result.AirDate == torrent.AirDate {
			return nil
		}

		progress.Changed++

		key := fmt.Sprintf("%s -> %s", classify.CategoryName(torrent.Category), classify.CategoryName(result.Category))
		progress.ByTransition[key]++

		if len(progress.Samples) < sampleLimit {
			progress.Samples = append(progress.Samples, Sample{
				InfoHash: torrent.InfoHash,
				Name:     torrent.Name,
				From:     torrent.Category,
				To:       result.Category,
			})
		}

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

	return out
}
