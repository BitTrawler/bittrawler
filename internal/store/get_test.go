package store

import (
	"context"
	"testing"
	"time"
)

func TestLatestReturnsNewestFirst(t *testing.T) {
	s, _ := newStore(t, Config{})
	base := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)

	put(t, s,
		Row{InfoHash: testHash(1), Name: "Older.Release", CreatedAt: base, Category: 2040, Seeders: 1},
		Row{InfoHash: testHash(2), Name: "Newer.Release", CreatedAt: base.Add(time.Hour), Category: 5040, Seeders: 2},
	)

	torrents, err := s.Latest(context.Background(), 1)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}

	if len(torrents) != 1 {
		t.Fatalf("latest: got %d torrents, want 1", len(torrents))
	}

	if torrents[0].Name != "Newer.Release" {
		t.Fatalf("latest: got %q, want the newest torrent", torrents[0].Name)
	}
}

func TestGetReturnsTheTorrent(t *testing.T) {
	s, _ := newStore(t, Config{})
	hash := testHash(3)
	created := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)

	put(t, s, Row{
		InfoHash:    hash,
		Name:        "Some.Release.2024.1080p",
		Size:        1234567,
		CreatedAt:   created,
		Category:    2040,
		FilesCount:  3,
		PrimaryFile: "some.release.mkv",
		Seeders:     11,
		Leechers:    2,
	})

	torrent, found, err := s.Get(context.Background(), hash)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !found {
		t.Fatal("get: torrent not found")
	}

	if torrent.Name != "Some.Release.2024.1080p" || torrent.Size != 1234567 ||
		torrent.Category != 2040 || torrent.Seeders != 11 || torrent.Leechers != 2 ||
		torrent.FilesCount != 3 || torrent.PrimaryFile != "some.release.mkv" ||
		!torrent.CreatedAt.Equal(created) {
		t.Fatalf("get: unexpected torrent %+v", torrent)
	}
}

func TestGetMissingTorrent(t *testing.T) {
	s, _ := newStore(t, Config{})

	_, found, err := s.Get(context.Background(), testHash(9))
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if found {
		t.Fatal("get: found a torrent that was never stored")
	}
}
