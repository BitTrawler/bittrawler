package reclassify

import (
	"context"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// TestRunKeepsAdultAndAddsNewEvidence covers the pass's two rules: a row already
// classified adult keeps that category, because the evidence may have been in
// the full file list the store no longer has, and a row the new rules identify
// is upgraded.
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
	}

	inserted, err := torrents.PutBatch(context.Background(), rows)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if inserted != len(rows) {
		t.Fatalf("inserted %d rows, want %d", inserted, len(rows))
	}

	_, err = Run(context.Background(), torrents, classify.New(classify.Config{}), Options{Apply: true}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	expectations := map[[20]byte]int{
		hash(1): classify.CategoryXXX,
		hash(2): classify.CategoryXXX,
		hash(3): classify.CategoryMoviesHD,
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
