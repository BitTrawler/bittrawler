package dhtcrawler

import (
	"context"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// persistTimeout bounds one batch write, including the final flush on shutdown.
const persistTimeout = 30 * time.Second

// runPersist writes crawled torrents in batches. Batching is what keeps the
// per-torrent write cost down: one transaction with one fsync per batch instead
// of one per torrent.
func (c *Crawler) runPersist(ctx context.Context) {
	batch := make([]store.Row, 0, c.cfg.PersistBatchSize)
	ticker := time.NewTicker(c.cfg.PersistBatchWait)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		c.persistBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// Write the rows already queued before leaving: they are complete
			// work and the batch that holds them would otherwise be lost.
			for {
				select {
				case row := <-c.forPersist:
					batch = append(batch, row)
				default:
					flush()

					return
				}
			}
		case row := <-c.forPersist:
			batch = append(batch, row)

			if len(batch) >= c.cfg.PersistBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// persistBatch writes one batch. A failure is logged with the number of rows
// that were lost; it is not retried, because the usual cause is the database
// being unavailable and a retry loop would only multiply the noise.
func (c *Crawler) persistBatch(batch []store.Row) {
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()

	inserted, err := c.store.PutBatch(ctx, batch)
	if err != nil {
		c.stats.persistFailures.Add(1)
		c.logger.Error("failed to persist torrent batch", "rows", len(batch), "inserted", inserted, "error", err)

		return
	}

	c.stats.torrentsPersisted.Add(uint64(inserted))

	c.logger.Debug("persisted torrents", "rows", inserted, "batch", len(batch))
}

// filesFromInfo converts the metadata file list into the classifier's input.
// The metadata is in memory here and is never stored, so this is the only point
// at which the complete file list is available; the classifier uses it for the
// extension census and for the blocking rules.
func filesFromInfo(info metainfo.Info) []classify.File {
	metadataFiles := info.UpvertedFiles()
	files := make([]classify.File, 0, len(metadataFiles))

	for _, file := range metadataFiles {
		files = append(files, classify.File{
			Path: file.DisplayPath(&info),
			Size: file.Length,
		})
	}

	return files
}

// rowFromInfo builds the stored row for a torrent that has just been crawled.
func rowFromInfo(
	hash protocol.ID,
	info metainfo.Info,
	files []classify.File,
	class classify.Result,
	seeders, leechers int,
	createdAt time.Time,
) store.Row {
	primaryFile := primaryFileOf(files)

	return store.Row{
		InfoHash:    hash,
		Name:        info.BestName(),
		Size:        info.TotalLength(),
		CreatedAt:   createdAt,
		Category:    class.Category,
		Seasons:     class.Seasons,
		Episodes:    class.Episodes,
		AirDate:     class.AirDate,
		FilesCount:  len(files),
		PrimaryFile: primaryFile,
		Seeders:     seeders,
		Leechers:    leechers,
	}
}

// primaryFileOf returns the largest file, which is what the UI shows as the
// torrent's main file. Ties keep the first file, matching metadata order.
func primaryFileOf(files []classify.File) string {
	var (
		primaryFile string
		primarySize int64 = -1
	)

	for _, file := range files {
		if file.Size > primarySize {
			primarySize = file.Size
			primaryFile = file.Path
		}
	}

	return primaryFile
}
