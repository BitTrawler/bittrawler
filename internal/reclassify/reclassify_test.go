package reclassify

import (
	"context"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// TestRunDeletesRejectedRows covers the cleanup: rows the blocking rules reject
// are removed from the store and counted, without rewriting categories.
func TestRunDeletesRejectedRows(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	torrents, err := store.Open(store.Config{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = torrents.Close() })

	rows := []store.Row{
		{
			InfoHash:    hash(1),
			Name:        "(lolsonly) child cjhildporn hairless",
			Size:        25 << 20,
			CreatedAt:   now,
			Category:    classify.CategoryXXX,
			FilesCount:  1,
			PrimaryFile: "(lolsonly) child cjhildporn hairless.rar",
		},
		{
			InfoHash:    hash(2),
			Name:        "Some.Movie.2020.1080p.BluRay.x264-GRP",
			Size:        8 << 30,
			CreatedAt:   now,
			Category:    classify.CategoryOther,
			FilesCount:  1,
			PrimaryFile: "some.movie.2020.mkv",
		},
	}

	if _, err := torrents.PutBatch(context.Background(), rows); err != nil {
		t.Fatalf("put: %v", err)
	}

	progress, err := Run(context.Background(), torrents, classify.New(classify.Config{}), Options{DeleteRejected: true}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if progress.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1", progress.Deleted)
	}

	if _, ok, err := torrents.Get(context.Background(), hash(1)); err != nil || ok {
		t.Fatalf("rejected row still stored: ok=%v err=%v", ok, err)
	}

	if _, ok, err := torrents.Get(context.Background(), hash(2)); err != nil || !ok {
		t.Fatalf("kept row was removed: ok=%v err=%v", ok, err)
	}
}

// TestRunKeepsAdultAndAddsNewEvidence covers the pass's rules: a row already
// classified adult keeps that category, because the evidence may have been in
// the full file list the store no longer has; a row the new rules identify is
// upgraded; and a row they would now block is reported without being written.
func TestRunKeepsAdultAndAddsNewEvidence(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	torrents, err := store.Open(store.Config{Dir: dir})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = torrents.Close() })

	rows := []store.Row{
		{
			InfoHash:    hash(1),
			Name:        "Brutal Invasion 10",
			Size:        3 << 30,
			CreatedAt:   now,
			Category:    classify.CategoryXXX,
			FilesCount:  4,
			PrimaryFile: "brutal-invasion-10-scene-3.540p.mp4",
		},
		{
			InfoHash:    hash(2),
			Name:        "Cory.Chase.And.Friend.2024.1080p",
			Size:        2 << 30,
			CreatedAt:   now,
			Category:    classify.CategoryOther,
			FilesCount:  1,
			PrimaryFile: "clip.mp4",
		},
		{
			InfoHash:    hash(3),
			Name:        "Some.Movie.2020.1080p.BluRay.x264-GRP",
			Size:        8 << 30,
			CreatedAt:   now,
			Category:    classify.CategoryOther,
			FilesCount:  1,
			PrimaryFile: "some.movie.2020.mkv",
		},
		{
			InfoHash:    hash(4),
			Name:        "(lolsonly) child cjhildporn hairless",
			Size:        25 << 20,
			CreatedAt:   now,
			Category:    classify.CategoryOther,
			FilesCount:  1,
			PrimaryFile: "(lolsonly) child cjhildporn hairless.rar",
		},
	}

	inserted, err := torrents.PutBatch(context.Background(), rows)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if inserted != len(rows) {
		t.Fatalf("inserted %d rows, want %d", inserted, len(rows))
	}

	progress, err := Run(context.Background(), torrents, classify.New(classify.Config{}), Options{Apply: true}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// A row that now matches a blocking rule is reported, not rewritten.
	if len(progress.Rejected) != 1 {
		t.Fatalf("rejected = %d, want 1", len(progress.Rejected))
	}

	if progress.Rejected[0].Name != "(lolsonly) child cjhildporn hairless" || progress.Rejected[0].Reason != "banned" {
		t.Fatalf("rejected row = %+v", progress.Rejected[0])
	}

	expectations := map[[20]byte]int{
		hash(1): classify.CategoryXXX,
		hash(2): classify.CategoryXXX,
		hash(3): classify.CategoryMoviesHD,
		hash(4): classify.CategoryOther,
	}

	for infoHash, want := range expectations {
		torrent, ok, err := torrents.Get(context.Background(), infoHash)
		if err != nil || !ok {
			t.Fatalf("get %x: ok=%v err=%v", infoHash, ok, err)
		}

		if torrent.Category != want {
			t.Errorf("%s: category = %d (%s), want %d (%s)", torrent.Name,
				torrent.Category, classify.CategoryName(torrent.Category),
				want, classify.CategoryName(want))
		}
	}
}

// hash builds a deterministic infohash for a test row.
func hash(n byte) [20]byte {
	var id [20]byte
	id[0] = 0xab
	id[19] = n

	return id
}

// TestRunApplyMovesCategoryCounts checks that a pass which changes categories
// also moves the rows in the store's in-memory breakdown, which is what the
// overview's classification table shows while the process keeps running.
func TestRunApplyMovesCategoryCounts(t *testing.T) {
	torrents, err := store.Open(store.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = torrents.Close() })

	now := time.Now().UTC()

	_, err = torrents.PutBatch(context.Background(), []store.Row{
		{
			InfoHash:    hash(1),
			Name:        "Some.Movie.2020.1080p.BluRay.x264-GRP",
			Size:        8 << 30,
			CreatedAt:   now,
			Category:    classify.CategoryOther,
			FilesCount:  1,
			PrimaryFile: "some.movie.2020.mkv",
		},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if _, err := Run(context.Background(), torrents, classify.New(classify.Config{}), Options{Apply: true}, nil); err != nil {
		t.Fatalf("run: %v", err)
	}

	stats, err := torrents.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if _, ok := stats.Categories[classify.CategoryOther]; ok {
		t.Fatalf("row is still counted under Other: %+v", stats.Categories)
	}

	if got := stats.Categories[classify.CategoryMoviesHD]; got != 1 {
		t.Fatalf("Movies/HD rows = %d, want 1", got)
	}

	if stats.Rows != 1 {
		t.Fatalf("rows = %d, want 1", stats.Rows)
	}
}
