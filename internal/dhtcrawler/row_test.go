package dhtcrawler

import (
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo"
	mi "github.com/anacrolix/torrent/metainfo"
)

func TestRowFromInfoSingleFile(t *testing.T) {
	createdAt := time.Unix(1_700_000_000, 0).UTC()

	row := rowFromInfo(testHash(1), metainfo.Info{
		Name:   "Single.File.mkv",
		Length: 4096,
	}, 2, 3, createdAt)

	if row.Name != "Single.File.mkv" {
		t.Errorf("name = %q, want %q", row.Name, "Single.File.mkv")
	}

	if row.Size != 4096 {
		t.Errorf("size = %d, want 4096", row.Size)
	}

	if row.FilesCount != 1 {
		t.Errorf("files count = %d, want 1", row.FilesCount)
	}

	if row.PrimaryFile != "Single.File.mkv" {
		t.Errorf("primary file = %q, want %q", row.PrimaryFile, "Single.File.mkv")
	}

	if row.Seeders != 2 || row.Leechers != 3 {
		t.Errorf("seeders/leechers = %d/%d, want 2/3", row.Seeders, row.Leechers)
	}

	if !row.CreatedAt.Equal(createdAt) {
		t.Errorf("created_at = %s, want %s", row.CreatedAt, createdAt)
	}

	// The classifier owns these fields, so they must stay unset here.
	if row.Category != 0 || row.ContentID != 0 || row.Seasons != nil || row.Episodes != nil || row.AirDate != "" {
		t.Errorf("classification fields are not zero: %+v", row)
	}
}

func TestRowFromInfoMultiFile(t *testing.T) {
	row := rowFromInfo(testHash(1), metainfo.Info{
		Name: "Multi.File.Torrent",
		Files: []mi.FileInfo{
			{Length: 10, Path: []string{"a", "one.mkv"}},
			{Length: 20, Path: []string{"b", "two.mkv"}},
		},
	}, 0, 0, time.Now())

	if row.Size != 30 {
		t.Errorf("size = %d, want 30", row.Size)
	}

	if row.FilesCount != 2 {
		t.Errorf("files count = %d, want 2", row.FilesCount)
	}

	if row.PrimaryFile != "b/two.mkv" {
		t.Errorf("primary file = %q, want %q", row.PrimaryFile, "b/two.mkv")
	}
}

func TestRowFromInfoPrimaryFileTieKeepsFirst(t *testing.T) {
	row := rowFromInfo(testHash(1), metainfo.Info{
		Name: "Tied.File.Torrent",
		Files: []mi.FileInfo{
			{Length: 20, Path: []string{"first.mkv"}},
			{Length: 20, Path: []string{"second.mkv"}},
		},
	}, 0, 0, time.Now())

	if row.PrimaryFile != "first.mkv" {
		t.Errorf("primary file = %q, want %q", row.PrimaryFile, "first.mkv")
	}
}

func TestRowFromInfoPrefersUtf8Name(t *testing.T) {
	row := rowFromInfo(testHash(1), metainfo.Info{
		Name:     "fallback name",
		NameUtf8: "preferred name",
		Length:   4096,
	}, 0, 0, time.Now())

	if row.Name != "preferred name" {
		t.Errorf("name = %q, want %q", row.Name, "preferred name")
	}
}
