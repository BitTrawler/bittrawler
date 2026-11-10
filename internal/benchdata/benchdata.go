// Package benchdata generates a deterministic population of synthetic torrents
// for benchmarks and tests. The distributions come from a measured audit of a
// real installation: 34% of torrents have no file list, the corpus spans 80
// days, the average seeder count is about 1.2, and film, TV and adult content
// dominate the classified rows. No real torrent names or infohashes are used.
//
// It is test infrastructure, not runtime code: the storage benchmark uses it to
// measure write cost and query latency so schema changes can be compared.
package benchdata

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"
)

// Torrent is one generated row, with the same shape the store writes.
type Torrent struct {
	InfoHash    [20]byte
	Name        string
	Size        int64
	CreatedAt   time.Time
	Category    int
	ContentID   int64
	Seasons     []int
	Episodes    []int
	AirDate     string
	FilesCount  int // -1 = no file list
	PrimaryFile string
	Seeders     int
	Leechers    int
}

// Options controls generation.
type Options struct {
	Count int
	Seed  uint64
	Days  int       // age of the corpus
	Now   time.Time // newest created_at
}

var categories = []struct {
	weight  float64
	code    int // 0 = no content type
	minSize int64
	maxSize int64
	files   float64
}{
	{45.6, 0, 1 << 20, 10 << 30, 6},
	{15.0, 8, 200 << 20, 6 << 30, 8},
	{12.7, 1, 500 << 20, 20 << 30, 3},
	{12.3, 2, 150 << 20, 80 << 30, 12},
	{6.0, 3, 30 << 20, 2 << 30, 14},
	{2.0, 5, 100 << 20, 2 << 30, 30},
	{2.0, 4, 1 << 20, 200 << 20, 4},
	{2.0, 6, 1 << 30, 60 << 30, 40},
	{1.6, 7, 50 << 20, 8 << 30, 20},
	{0.8, 9, 20 << 20, 2 << 30, 30},
}

var (
	titles = []string{
		"The.Last.Signal", "Northern.Lights", "Silent.River", "Glass.House", "Midnight.Run",
		"Cold.Harbour", "The.Quiet.Ones", "Paper.Streets", "Black.Tide", "The.Garden.Party",
		"Second.Wind", "Iron.Valley", "The.Long.Way.Down", "Blue.Orchard", "Ghost.Station",
	}
	words = []string{
		"love", "war", "night", "summer", "winter", "river", "mountain", "city", "dream",
		"shadow", "fire", "ocean", "secret", "garden", "house", "road", "train", "light",
		"adventure", "telephone", "symphony", "cathedral", "province", "chemical",
		"lighthouse", "hurricane", "supernova", "wilderness", "parliament", "chocolate",
	}
	resolutions = []string{"480p", "576p", "720p", "1080p", "1080i", "2160p", ""}
	sources     = []string{"WEB-DL", "WEBRip", "BluRay", "HDTV", "DVDRip", "BDRip", "Remux", ""}
	codecs      = []string{"x264", "x265", "H264", "H265", "AV1", "XviD", ""}
	groups      = []string{"NTb", "RARBG", "SPARKS", "FLUX", "CMRG", "EDITH", "KOGi", "TEPES"}
	formats     = []string{"FLAC", "MP3", "AAC", "ALAC", "320", "V0"}
	artists     = []string{"The.Wandering.Star", "Halcyon.Bay", "Mono.Lake", "Paper.Kites", "Violet.Hour"}
	albums      = []string{"Distant.Light", "Low.Tide", "Every.Other.Day", "Slow.Burn", "The.Quiet.Year"}
	authors     = []string{"A.N.Other", "J.Whitlock", "M.Hale", "R.Sato", "P.Duval"}
	exts        = map[int]string{0: "mkv", 1: "mkv", 2: "mkv", 3: "flac", 4: "epub", 5: "m4b", 6: "iso", 7: "dmg", 8: "mp4", 9: "cbz"}
)

const (
	ctMovie = iota + 1
	ctTV
	ctMusic
	ctBook
	ctAudiobook
	ctGame
	ctSoftware
	ctXXX
	ctComic
)

// Rows returns count torrents with created_at spread evenly over the corpus
// window, oldest first, as a live crawler would produce them.
func Rows(opts Options) []Torrent {
	if opts.Count <= 0 {
		return nil
	}

	if opts.Days <= 0 {
		opts.Days = 80
	}

	if opts.Seed == 0 {
		opts.Seed = 42
	}

	if opts.Now.IsZero() {
		opts.Now = time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)
	}

	r := rand.New(rand.NewPCG(opts.Seed, 0x9e3779b97f4a7c15))
	span := time.Duration(opts.Days) * 24 * time.Hour

	out := make([]Torrent, opts.Count)
	for i := range out {
		out[i] = makeTorrent(r)
		out[i].CreatedAt = opts.Now.Add(-span + time.Duration(float64(span)*float64(i)/float64(opts.Count)))
	}

	return out
}

func makeTorrent(r *rand.Rand) Torrent {
	var t Torrent

	for i := 0; i < len(t.InfoHash); i += 4 {
		v := r.Uint32()
		t.InfoHash[i] = byte(v)
		t.InfoHash[i+1] = byte(v >> 8)
		t.InfoHash[i+2] = byte(v >> 16)
		t.InfoHash[i+3] = byte(v >> 24)
	}
	t.FilesCount = -1

	cat := pick(r)

	switch cat {
	case ctTV:
		season := 1 + r.IntN(15)
		t.Seasons = []int{season}

		if r.Float64() < 0.3 {
			t.Name = fmt.Sprintf("%s.S%02d.%s.%s-%s", title(r), season, one(r, resolutions), one(r, sources), one(r, groups))
		} else {
			episode := 1 + r.IntN(24)
			t.Episodes = []int{season*10000 + episode}
			t.Name = fmt.Sprintf("%s.S%02dE%02d.%s.%s-%s", title(r), season, episode, one(r, resolutions), one(r, sources), one(r, groups))
		}
	default:
		t.Name = name(r, cat)
	}
	t.PrimaryFile = t.Name + "." + exts[cat]
	t.Size = size(r, cat)
	t.Category = newznabCategory(cat, t.Name)

	switch cat {
	case ctMovie:
		t.ContentID = int64(r.IntN(8000))
	case ctTV:
		t.ContentID = int64(10000 + r.IntN(8000))
	}

	if r.Float64() < 0.66 {
		files := int(math.Round(math.Exp(r.NormFloat64()*0.8) * filesFor(cat)))
		t.FilesCount = max(1, min(files, 100))
	}

	// 34% have no file list; the rest are single or multi file.

	t.Seeders = skewed(r, 1.2, 5000)
	t.Leechers = skewed(r, 0.4, 2000)

	return t
}

func name(r *rand.Rand, cat int) string {
	switch cat {
	case ctMovie:
		return fmt.Sprintf("%s.%d.%s.%s.%s-%s", title(r), 1950+r.IntN(77),
			one(r, resolutions), one(r, sources), one(r, codecs), one(r, groups))
	case ctMusic:
		return fmt.Sprintf("%s - %s (%d) [%s]", one(r, artists), one(r, albums), 1960+r.IntN(67), one(r, formats))
	case ctBook, ctAudiobook:
		ext := "EPUB"
		if cat == ctAudiobook {
			ext = "M4B"
		}

		return fmt.Sprintf("%s - %s (%d) [%s]", one(r, authors), titleish(r), 1960+r.IntN(67), ext)
	case ctGame:
		return fmt.Sprintf("%s.%s-%s", titleish(r), one(r, []string{"REPACK", "GOTY", "Update", "DLC"}), one(r, groups))
	case ctSoftware:
		return fmt.Sprintf("%s.%d.%d-%s", titleish(r), 1+r.IntN(20), r.IntN(10), one(r, groups))
	case ctXXX:
		return fmt.Sprintf("%s.%s.%s", titleish(r), one(r, resolutions), one(r, sources))
	case ctComic:
		return fmt.Sprintf("%s.%03d", titleish(r), 1+r.IntN(120))
	default:
		n := 2 + r.IntN(5)
		parts := make([]string, 0, n)
		for range n {
			parts = append(parts, one(r, words))
		}

		return strings.Join(parts, ".") + "." + one(r, groups)
	}
}

func title(r *rand.Rand) string {
	if r.Float64() < 0.3 {
		return one(r, titles)
	}

	n := 2 + r.IntN(3)
	parts := make([]string, 0, n)
	for range n {
		w := one(r, words)
		parts = append(parts, strings.ToUpper(w[:1])+w[1:])
	}

	return strings.Join(parts, ".")
}

func titleish(r *rand.Rand) string {
	n := 2 + r.IntN(2)
	parts := make([]string, 0, n)
	for range n {
		w := one(r, words)
		parts = append(parts, strings.ToUpper(w[:1])+w[1:])
	}

	return strings.Join(parts, ".")
}

// newznabCategory narrows the content type's range by resolution, and defaults
// to HD when the name carries no resolution token so default *arr categories
// still match.
func newznabCategory(code int, name string) int {
	uhd := strings.Contains(name, "2160p")
	sd := strings.Contains(name, "480p") || strings.Contains(name, "576p") || strings.Contains(name, "XviD")

	switch code {
	case ctMovie:
		switch {
		case uhd:
			return 2045
		case sd:
			return 2030
		default:
			return 2040
		}
	case ctTV:
		switch {
		case uhd:
			return 5045
		case sd:
			return 5030
		default:
			return 5040
		}
	case ctMusic:
		if strings.Contains(name, "FLAC") || strings.Contains(name, "ALAC") {
			return 3040
		}

		return 3010
	case ctBook:
		return 7020
	case ctAudiobook:
		return 3030
	case ctComic:
		return 7030
	case ctGame:
		return 1000
	case ctSoftware:
		return 4000
	case ctXXX:
		return 6000
	default:
		return 8000
	}
}

func size(r *rand.Rand, cat int) int64 {
	c := category(cat)
	lo, hi := math.Log(float64(c.minSize)), math.Log(float64(c.maxSize))

	return int64(math.Exp(lo + (hi-lo)*math.Pow(r.Float64(), 1.6)))
}

func filesFor(cat int) float64 {
	return category(cat).files
}

func category(code int) struct {
	weight  float64
	code    int
	minSize int64
	maxSize int64
	files   float64
} {
	for _, c := range categories {
		if c.code == code {
			return c
		}
	}

	return categories[0]
}

func pick(r *rand.Rand) int {
	var total float64
	for _, c := range categories {
		total += c.weight
	}

	p := r.Float64() * total

	for _, c := range categories {
		if p < c.weight {
			return c.code
		}

		p -= c.weight
	}

	return 0
}

// skewed returns a count where most values are small and the mean is
// approximately mean.
func skewed(r *rand.Rand, mean float64, cap int) int {
	v := int(math.Round(-math.Log(1-r.Float64()) * mean))
	if v > cap {
		return cap
	}

	return v
}

func one(r *rand.Rand, list []string) string {
	return list[r.IntN(len(list))]
}
