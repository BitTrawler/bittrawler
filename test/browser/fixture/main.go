// Command fixture writes the torrents the browser tests expect into a store
// directory. It opens the store through the same package the app uses, so a
// schema change cannot leave the tests seeding a shape the app no longer reads.
//
// Every row's info hash is the SHA-1 of "bittrawler-links-<index>", which is how
// the specs address a particular row without hardcoding forty hex characters.
package main

import (
	"context"
	"crypto/sha1"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// row is one torrent. Ages are in minutes from the time of seeding, so the
// rendered "Age" column moves but the shape of the page does not.
type row struct {
	name        string
	size        int64
	category    int
	files       int
	primaryFile string
	seeders     int
	leechers    int
	ageMinutes  int
	contentID   int64
	season      int
	episode     int
	airDate     string
}

// rows mirrors the data the screenshot harness uses: a film with a TMDB id, an
// episode, a season pack, a long name that has to wrap, and enough 1080p rows
// for the search page to fill. Several specs address rows by index, so append
// rather than reorder.
var rows = []row{
	{name: "Dune.Part.Two.2024.2160p.WEB-DL.DDP5.1.Atmos.HDR.HEVC-CMRG", size: 19_760_000_000, category: classify.CategoryMoviesUHD, files: 2, primaryFile: "Dune.Part.Two.2024.2160p.mkv", seeders: 1842, leechers: 37, ageMinutes: 6, contentID: 693134},
	{name: "Shogun.S01E03.1080p.DSNP.WEB-DL.DDP5.1.H.264-NTb", size: 4_120_000_000, category: classify.CategoryTVHD, files: 1, primaryFile: "Shogun.S01E03.1080p.mkv", seeders: 421, leechers: 18, ageMinutes: 12, contentID: 126308, season: 1, episode: 3},
	{name: "Breaking.Bad.S05E14.1080p.BluRay.x264-DEMAND", size: 3_980_000_000, category: classify.CategoryTVHD, files: 1, primaryFile: "Breaking.Bad.S05E14.1080p.mkv", seeders: 287, leechers: 9, ageMinutes: 15, contentID: 1396, season: 5, episode: 14},
	{name: "The.Bear.S03.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTb", size: 21_800_000_000, category: classify.CategoryTVHD, files: 10, primaryFile: "The.Bear.S03E01.mkv", seeders: 356, leechers: 24, ageMinutes: 18, season: 3},
	{name: "Oppenheimer.2023.1080p.BluRay.x264-SPARKS", size: 13_210_000_000, category: classify.CategoryMoviesHD, files: 3, primaryFile: "Oppenheimer.2023.1080p.mkv", seeders: 289, leechers: 12, ageMinutes: 25, contentID: 872585},
	{name: "Blade.Runner.2049.2017.2160p.UHD.BluRay.REMUX.HEVC.DTS-HD.MA.7.1-FGT", size: 61_400_000_000, category: classify.CategoryMoviesUHD, files: 2, primaryFile: "Blade.Runner.2049.2017.2160p.mkv", seeders: 214, leechers: 9, ageMinutes: 33, contentID: 335984},
	{name: "Severance.S02.2160p.ATVP.WEB-DL.DDP5.1.Atmos.HDR.HEVC", size: 48_200_000_000, category: classify.CategoryTVUHD, files: 11, primaryFile: "Severance.S02E01.mkv", seeders: 198, leechers: 14, ageMinutes: 38, contentID: 95396, season: 2},
	{name: "Radiohead.In.Rainbows.2007.FLAC.24bit", size: 1_180_000_000, category: classify.CategoryAudio, files: 12, primaryFile: "01 - 15 Step.flac", seeders: 162, leechers: 4, ageMinutes: 40},
	{name: "The.Expanse.S06.COMPLETE.1080p.BluRay.x264", size: 34_900_000_000, category: classify.CategoryTVHD, files: 13, primaryFile: "The.Expanse.S06E01.mkv", seeders: 143, leechers: 11, ageMinutes: 52, contentID: 63639, season: 6},
	{name: "Project.Hail.Mary.2026.1080p.WEB-DL.DDP5.1.H.264", size: 5_240_000_000, category: classify.CategoryMoviesHD, files: 1, primaryFile: "Project.Hail.Mary.2026.1080p.mkv", seeders: 129, leechers: 8, ageMinutes: 64},
	{name: "The.Zone.of.Interest.2023.1080p.BluRay.x264", size: 9_800_000_000, category: classify.CategoryMoviesHD, files: 2, primaryFile: "The.Zone.of.Interest.2023.mkv", seeders: 47, leechers: 1, ageMinutes: 71, contentID: 467244},
	{name: "The.Last.of.Us.S02E01.2160p.HMAX.WEB-DL.DDP5.1.H.265", size: 12_600_000_000, category: classify.CategoryTVUHD, files: 1, primaryFile: "The.Last.of.Us.S02E01.2160p.mkv", seeders: 86, leechers: 5, ageMinutes: 102, season: 2, episode: 1},
	{name: "Elden.Ring.v1.16-EMPRESS", size: 54_700_000_000, category: classify.CategoryPCGames, files: 48, primaryFile: "setup.exe", seeders: 118, leechers: 6, ageMinutes: 77},
	{name: "The.Pragmatic.Programmer.20th.Anniversary.Edition.EPUB", size: 8_400_000, category: classify.CategoryEbook, files: 1, primaryFile: "The.Pragmatic.Programmer.epub", seeders: 97, leechers: 3, ageMinutes: 89},
	{name: "Saga.Vol.11.TPB.CBZ", size: 214_000_000, category: classify.CategoryComics, files: 1, primaryFile: "Saga.Vol.11.cbz", seeders: 74, leechers: 2, ageMinutes: 118},
	{name: "Ubuntu.24.04.2.LTS.Desktop.amd64", size: 5_900_000_000, category: classify.CategoryPCISO, files: 1, primaryFile: "ubuntu-24.04.2-desktop-amd64.iso", seeders: 63, leechers: 7, ageMinutes: 133},
	{name: "Chernobyl.S01.2160p.UHD.BluRay.REMUX.HEVC", size: 78_300_000_000, category: classify.CategoryTVUHD, files: 6, primaryFile: "Chernobyl.S01E01.mkv", seeders: 58, leechers: 4, ageMinutes: 149, contentID: 87108, season: 1},
	{name: "Some.Misc.Archive.Collection.2024", size: 3_400_000_000, category: classify.CategoryOther, files: 24, primaryFile: "collection.zip", seeders: 31, leechers: 1, ageMinutes: 214},
	// Long enough to run over the columns when the name is not contained.
	{name: "New.Public.Pickups.Petite.Marie.Getty.Sucks.Dude.Behind.Counter.Then.Fucks.Her.Tight.Ass.Backroom.579.GB.XXX.1080p.WEB-DL.DDP5.1.H.264-GROUP.mp4", size: 621_000_000_000, category: classify.CategoryXXXWEB, files: 1, primaryFile: "New.Public.Pickups.Petite.Marie.1080p.mp4", seeders: 12, leechers: 2, ageMinutes: 1},
}

func main() {
	dataDir := flag.String("data", "", "store directory to seed")
	flag.Parse()

	if *dataDir == "" {
		fmt.Fprintln(os.Stderr, "usage: fixture -data <directory>")
		os.Exit(2)
	}

	if err := seed(*dataDir); err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
}

func seed(dir string) error {
	torrents, err := store.Open(store.Config{Dir: dir})
	if err != nil {
		return fmt.Errorf("open the store: %w", err)
	}

	now := time.Now().UTC()
	batch := make([]store.Row, 0, len(rows))

	for index, r := range rows {
		hash := sha1.Sum([]byte(fmt.Sprintf("bittrawler-links-%d", index)))

		var infoHash [20]byte
		copy(infoHash[:], hash[:])

		batch = append(batch, store.Row{
			InfoHash:    infoHash,
			Name:        r.name,
			Size:        r.size,
			CreatedAt:   now.Add(-time.Duration(r.ageMinutes) * time.Minute),
			Category:    r.category,
			ContentID:   r.contentID,
			Seasons:     seasons(r.season),
			Episodes:    episodes(r.season, r.episode),
			AirDate:     r.airDate,
			FilesCount:  r.files,
			PrimaryFile: r.primaryFile,
			Seeders:     r.seeders,
			Leechers:    r.leechers,
		})
	}

	inserted, err := torrents.PutBatch(context.Background(), batch)
	if err != nil {
		return fmt.Errorf("seed the store: %w", err)
	}

	if err := torrents.Close(); err != nil {
		return fmt.Errorf("close the store: %w", err)
	}

	fmt.Printf("seeded %d torrents\n", inserted)

	return nil
}

func seasons(season int) []int {
	if season == 0 {
		return nil
	}

	return []int{season}
}

// episodes stores the packed season and episode number the search tokens use.
func episodes(season, episode int) []int {
	if episode == 0 {
		return nil
	}

	return []int{season*10000 + episode}
}
