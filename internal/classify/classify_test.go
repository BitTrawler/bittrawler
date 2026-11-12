package classify

import (
	"reflect"
	"testing"
)

// mkv, mp3 and friends build a file list of one file with the given extension.
func filesOf(extension string, size int64) []File {
	return []File{{Path: "release/" + extension + "." + extension, Size: size}}
}

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		files    []File
		category int
		seasons  []int
		episodes []int
		airDate  string
		drop     Drop
	}{
		{
			name:     "film hd",
			title:    "Some.Movie.2020.1080p.BluRay.x264-GRP",
			files:    filesOf("mkv", 8<<30),
			category: CategoryMoviesHD,
		},
		{
			name:     "film uhd",
			title:    "Some.Movie.2020.2160p.UHD.BluRay.x265-GRP",
			files:    filesOf("mkv", 40<<30),
			category: CategoryMoviesUHD,
		},
		{
			name:     "film sd",
			title:    "Some.Movie.2020.480p.DVDRip.XviD-GRP",
			files:    filesOf("avi", 700<<20),
			category: CategoryMoviesSD,
		},
		{
			name:     "film with unknown resolution",
			title:    "Some.Movie.2020.BluRay.x264-GRP",
			files:    filesOf("mkv", 8<<30),
			category: CategoryMovies,
		},
		{
			name:     "film iso by name",
			title:    "Some.Movie.2004.PAL.DVDR-TGP",
			files:    filesOf("iso", 4<<30),
			category: CategoryMovies,
		},
		{
			name:     "season pack",
			title:    "Some.Show.S02.1080p.BluRay.x264-GRP",
			files:    filesOf("mkv", 8<<30),
			category: CategoryTVHD,
			seasons:  []int{2},
		},
		{
			name:     "episode",
			title:    "Some.Show.S01E02.1080p.WEB-DL.x264-GRP",
			files:    filesOf("mkv", 2<<30),
			category: CategoryTVHD,
			seasons:  []int{1},
			episodes: []int{10002},
		},
		{
			name:     "multi episode",
			title:    "Some.Show.S01E02E03.720p.HDTV.x264-GRP",
			files:    filesOf("mkv", 2<<30),
			category: CategoryTVHD,
			seasons:  []int{1},
			episodes: []int{10002, 10003},
		},
		{
			name:     "daily episode",
			title:    "The.Daily.Show.2023.05.01.1080p.WEB-DL",
			files:    filesOf("mkv", 1<<30),
			category: CategoryTVHD,
			airDate:  "2023-05-01",
		},
		{
			name:     "complete series",
			title:    "Some.Show.Complete.Series.1080p.BluRay",
			files:    filesOf("mkv", 8<<30),
			category: CategoryTVHD,
		},
		{
			name:     "music album",
			title:    "Some Artist - Some Album (2020) FLAC",
			files:    filesOf("flac", 40<<20),
			category: CategoryAudio,
		},
		{
			name:     "music mp3 album",
			title:    "Some Artist - Some Album (2020) MP3 320kbps",
			files:    filesOf("mp3", 8<<20),
			category: CategoryAudio,
		},
		{
			name:     "audiobook by name",
			title:    "Some Book (2020) [Unabridged] MP3",
			files:    filesOf("mp3", 8<<20),
			category: CategoryAudiobook,
		},
		{
			name:     "audiobook by file extension",
			title:    "Some Book (2020)",
			files:    filesOf("m4b", 8<<20),
			category: CategoryAudiobook,
		},
		{
			name:     "ebook",
			title:    "Some Book (2020).epub",
			files:    filesOf("epub", 2<<20),
			category: CategoryEbook,
		},
		{
			name:     "comic",
			title:    "Some Comic 001 (2020) (Digital).cbz",
			files:    filesOf("cbz", 80<<20),
			category: CategoryComics,
		},
		{
			name:     "software",
			title:    "Some Suite 2024 v25.1 Installer",
			files:    filesOf("exe", 2<<30),
			category: CategoryPC,
		},
		{
			name:     "game repack",
			title:    "Some Game-FitGirl Repack",
			files:    filesOf("exe", 8<<30),
			category: CategoryPCGames,
		},
		{
			name:     "game disc image",
			title:    "Some.Game.PS4.ISO",
			files:    filesOf("iso", 30<<30),
			category: CategoryPCGames,
		},
		{
			name:     "adult video",
			title:    "Some.Title.XXX.1080p.WEB-DL",
			files:    filesOf("mp4", 2<<30),
			category: CategoryXXX,
		},
		{
			name:     "adult image set",
			title:    "Sexy Models Vol 3",
			files:    filesOf("jpg", 2<<20),
			category: CategoryXXX,
		},
		{
			name:     "unidentifiable",
			title:    "Some Dataset 2024",
			files:    filesOf("csv", 2<<20),
			category: CategoryOther,
		},
		{
			name:     "banned strong",
			title:    "some pthc 2023 1080p",
			files:    filesOf("mp4", 2<<30),
			category: 0,
			drop:     DropBanned,
		},
		{
			name:     "banned weak with context",
			title:    "Preteen Models (2020) 1080p",
			files:    filesOf("mp4", 2<<30),
			category: 0,
			drop:     DropBanned,
		},
	}

	classifier := New(Config{})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := classifier.Classify(test.title, test.files)

			if result.Drop != test.drop {
				t.Fatalf("drop = %v, want %v", result.Drop, test.drop)
			}

			if result.Category != test.category {
				t.Errorf("category = %d (%s), want %d (%s)",
					result.Category, CategoryName(result.Category),
					test.category, CategoryName(test.category))
			}

			if !reflect.DeepEqual(result.Seasons, test.seasons) {
				t.Errorf("seasons = %v, want %v", result.Seasons, test.seasons)
			}

			if !reflect.DeepEqual(result.Episodes, test.episodes) {
				t.Errorf("episodes = %v, want %v", result.Episodes, test.episodes)
			}

			if result.AirDate != test.airDate {
				t.Errorf("air date = %q, want %q", result.AirDate, test.airDate)
			}
		})
	}
}

func TestClassifyFilesOverrideTheName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		files    []File
		category int
	}{
		{
			name:     "music claim with video files",
			title:    "Some Artist - Some Album (2020) FLAC",
			files:    filesOf("mkv", 8<<30),
			category: CategoryMovies,
		},
		{
			// A disk image says nothing about its contents, so the name decides.
			name:     "film claim with a disk image",
			title:    "Some.Movie.2004.PAL.DVDR-TGP",
			files:    filesOf("iso", 4<<30),
			category: CategoryMovies,
		},
		{
			// Images are not a film, whatever the name says.
			name:     "film claim with screenshots",
			title:    "Some.Movie.2020.1080p.BluRay",
			files:    filesOf("jpg", 2<<20),
			category: CategoryOther,
		},
		{
			name:     "explained by files, no name markers",
			title:    "Some Release 2020",
			files:    filesOf("mkv", 8<<30),
			category: CategoryMovies,
		},
		{
			name:     "audio explained by files",
			title:    "Some Release 2020",
			files:    filesOf("mp3", 8<<20),
			category: CategoryAudio,
		},
	}

	classifier := New(Config{})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := classifier.Classify(test.title, test.files)

			if result.Category != test.category {
				t.Errorf("category = %d (%s), want %d (%s)",
					result.Category, CategoryName(result.Category),
					test.category, CategoryName(test.category))
			}
		})
	}
}

func TestClassifyDropsDeceptiveContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		files    []File
		drop     Drop
		category int
	}{
		{
			// Malware released under a scene name.
			name:  "film claim with an executable",
			title: "Some.Movie.2020.1080p.BluRay.x264-GRP.exe",
			files: filesOf("exe", 700<<20),
			drop:  DropDeceptive,
		},
		{
			name:  "episode claim with an executable",
			title: "Some.Show.S01E02.1080p.WEB-DL.exe",
			files: filesOf("exe", 200<<20),
			drop:  DropDeceptive,
		},
		{
			name:  "adult claim with an executable",
			title: "Some.Title.XXX.1080p.WEB-DL",
			files: filesOf("exe", 300<<20),
			drop:  DropDeceptive,
		},
		{
			name:     "reader program named after a format",
			title:    "Some Ebook Reader Pro 5.31 RePack",
			files:    filesOf("exe", 80<<20),
			category: CategoryPC,
		},
		{
			name:     "converter advertising a resolution",
			title:    "4K Video Downloader 4.30",
			files:    filesOf("exe", 120<<20),
			category: CategoryPC,
		},

		// A program with an ordinary name is a program, not a lie.
		{
			name:     "program with a plain name",
			title:    "Some.Release.2020",
			files:    filesOf("exe", 50<<20),
			category: CategoryPC,
		},
		{
			name:     "game repack",
			title:    "Some Game-FitGirl Repack",
			files:    filesOf("exe", 8<<30),
			category: CategoryPCGames,
		},
		{
			name:     "application",
			title:    "Adobe Photoshop 2024 v25.1",
			files:    filesOf("exe", 3<<30),
			category: CategoryPC,
		},

		// A small bundled installer does not make a film a lie.
		{
			name:  "film with a bundled installer",
			title: "Some.Movie.2020.1080p.BluRay.x264-GRP",
			files: []File{
				{Path: "Some.Movie.2020.1080p.mkv", Size: 8 << 30},
				{Path: "setup.exe", Size: 5 << 20},
			},
			category: CategoryMoviesHD,
		},
	}

	classifier := New(Config{})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := classifier.Classify(test.title, test.files)

			if result.Drop != test.drop {
				t.Fatalf("drop = %v, want %v", result.Drop, test.drop)
			}

			if result.Category != test.category {
				t.Errorf("category = %d (%s), want %d (%s)",
					result.Category, CategoryName(result.Category),
					test.category, CategoryName(test.category))
			}
		})
	}
}

func TestClassifyRealWorldCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		files    []File
		category int
		airDate  string
		seasons  []int
	}{
		{
			name:     "jav label code",
			title:    "SMDV-26 立川理恵",
			files:    filesOf("mp4", 1<<30),
			category: CategoryXXX,
		},
		{
			name:     "jav code with trailing letter",
			title:    "WANZ598C",
			files:    filesOf("mp4", 1<<30),
			category: CategoryXXX,
		},
		{
			name:     "short site code in a dated release",
			title:    "fka.17.04.18.chrissy.fox.mp4",
			files:    filesOf("mp4", 1<<30),
			category: CategoryXXX,
		},
		{
			name:     "dated adult release",
			title:    "Anastasia Brokelyn - Cum Over Coffee (20.02.2020)_1080p.mp4",
			files:    filesOf("mp4", 1<<30),
			category: CategoryXXX,
		},
		{
			name:     "chinese adult title",
			title:    "14.无水印_PerfectGonzo-Ania_Kinski_捅烂丝袜熟女骚肛",
			files:    filesOf("mp4", 1<<30),
			category: CategoryXXX,
		},
		{
			name:     "sports event kept as television",
			title:    "Miami Dolphins at New York Jets 25.10.2026.mkv",
			files:    filesOf("mkv", 4<<30),
			category: CategoryTV,
			airDate:  "2026-10-25",
		},
		{
			name:     "anime episode numbered without a season",
			title:    "[Erai-raws] Megami no Cafe Terrace - 11 [1080p][Multiple Subtitle]",
			files:    filesOf("mkv", 1<<30),
			category: CategoryTVHD,
		},
		{
			name:     "spanish season number",
			title:    "Los Simpsons - Temporada 34 [HDTV 720p][Cap.3408]",
			files:    filesOf("mkv", 2<<30),
			category: CategoryTVHD,
			seasons:  []int{34},
		},
		{
			name:     "rmvb film",
			title:    "【巴黎最后的探戈】CD2.rm",
			files:    filesOf("rm", 700<<20),
			category: CategoryMovies,
		},
		{
			name:     "kink keywords",
			title:    "NewPrincess Mia Scat smearing facesitting - Princess Mia and toilet slave - Full HD 1080 (Femdom) 260420",
			files:    filesOf("mp4", 3<<30),
			category: CategoryXXX,
		},
		{
			name:     "squirt keyword",
			title:    "Some Site - Veronica Leal - A Squirt In The Streets 1080p",
			files:    filesOf("mp4", 2<<30),
			category: CategoryXXX,
		},
		{
			name:     "code only jav name",
			title:    "DVDMS233C.mp4",
			files:    filesOf("mp4", 1<<30),
			category: CategoryXXX,
		},
		{
			name:     "code only with digit prefix",
			title:    "259LUXU-1901.mp4",
			files:    filesOf("mp4", 1<<30),
			category: CategoryXXX,
		},
		{
			name:     "code only behind a domain",
			title:    "dccdom.com@JUR014C",
			files:    filesOf("avi", 700<<20),
			category: CategoryXXX,
		},
		{
			name:     "cjk comic volume",
			title:    "寄生兽 完全版 第1卷～第4卷",
			files:    []File{{Path: "vol1/01.webp", Size: 1 << 20}, {Path: "vol2/01.webp", Size: 1 << 20}},
			category: CategoryComics,
		},
	}

	classifier := New(Config{})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := classifier.Classify(test.title, test.files)

			if result.Category != test.category {
				t.Errorf("category = %d (%s), want %d (%s)",
					result.Category, CategoryName(result.Category),
					test.category, CategoryName(test.category))
			}

			if result.AirDate != test.airDate {
				t.Errorf("air date = %q, want %q", result.AirDate, test.airDate)
			}

			if !reflect.DeepEqual(result.Seasons, test.seasons) {
				t.Errorf("seasons = %v, want %v", result.Seasons, test.seasons)
			}
		})
	}
}

func TestAdultLabelsDoNotMatchOrdinaryWords(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"Hindustan Times documentary",
		"Paco (2015) drama",
		"Star Wars 1977 1080p",
		"HND Computing Course 2020",
		"The Predator 1987 1080p",
	} {
		if adultStrongText(name) {
			t.Errorf("%q matched an adult marker", name)
		}
	}
}

func TestCodeOnlyName(t *testing.T) {
	t.Parallel()

	matches := []string{
		"DVDMS233C", "DVDMS233C.mp4", "MIAB-573-U.mp4", "259LUXU-1901",
		"dccdom.com@JUR014C", "NPS-006 NPS-008", "Carib-080923-001",
	}

	for _, name := range matches {
		if !isCodeOnlyName(name) {
			t.Errorf("isCodeOnlyName(%q) = false, want true", name)
		}
	}

	nonMatches := []string{
		"Some.Movie.2020.1080p.BluRay.x264-GRP",
		"3DMGAME_CCWR",
		"Adobe Photoshop 2024",
		"The.100.S01E02",
		"",
		"1920x1080",
	}

	for _, name := range nonMatches {
		if isCodeOnlyName(name) {
			t.Errorf("isCodeOnlyName(%q) = true, want false", name)
		}
	}
}

func TestClassifyWithoutFiles(t *testing.T) {
	t.Parallel()

	classifier := New(Config{})

	tests := map[string]int{
		"Some.Show.S01E02.1080p.WEB-DL":        CategoryTVHD,
		"Some.Movie.2020.1080p.BluRay.x264":    CategoryMoviesHD,
		"Some.Movie.2020.BluRay.x264-GRP":      CategoryMovies,
		"Some Artist - Some Album (2020) FLAC": CategoryAudio,
		"Some Book (2020).epub":                CategoryEbook,
		"Some Comic 001 (2020).cbz":            CategoryComics,
	}

	for name, want := range tests {
		if got := classifier.Classify(name, nil).Category; got != want {
			t.Errorf("Classify(%q) category = %d (%s), want %d (%s)",
				name, got, CategoryName(got), want, CategoryName(want))
		}
	}
}

func TestClassifyDropsAdultWhenConfigured(t *testing.T) {
	t.Parallel()

	name := "Some.Title.XXX.1080p.WEB-DL"
	files := filesOf("mp4", 2<<30)

	kept := New(Config{}).Classify(name, files)
	if kept.Drop != DropNone || kept.Category != CategoryXXX {
		t.Fatalf("default classify = category %d, drop %v; want XXX kept", kept.Category, kept.Drop)
	}

	dropped := New(Config{DropAdult: true}).Classify(name, files)
	if dropped.Drop != DropAdult {
		t.Fatalf("drop = %v, want %v", dropped.Drop, DropAdult)
	}
}

func TestClassifyIsDeterministic(t *testing.T) {
	t.Parallel()

	classifier := New(Config{})
	name := "Some.Show.S01E02.1080p.WEB-DL.x264-GRP"
	files := []File{
		{Path: "Some.Show.S01E02.1080p.mkv", Size: 2 << 30},
		{Path: "Some.Show.S01E02.srt", Size: 40 << 10},
	}

	want := classifier.Classify(name, files)

	for range 100 {
		if got := classifier.Classify(name, files); !reflect.DeepEqual(got, want) {
			t.Fatalf("classify changed between runs: %+v != %+v", got, want)
		}
	}
}

func TestBannedPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title string
		want  bool
	}{
		// Strong markers, illegal on their own.
		{"some pthc 2023 1080p", true},
		{"paedophile documentary 1080p", true},
		{"child porn 1080p", true},
		{"kiddie porn 2020", true},
		{"young video models 2023", true},
		{"10yo video 1080p", true},
		{"15 yo girl", true},
		{"hebefilia collection", true},

		// Weak markers with corroborating context.
		{"preteen models 2020", true},
		{"underage girls video", true},
		{"lsm lolita collection", true},

		// Weak markers that are legal on their own.
		{"Underage (2019) 1080p.BluRay", false},
		{"Preteen (2019) 1080p", false},
		{"LSM.2023.1080p.WEB-DL", false},
		{"Some.Movie.2023.1080p-YVM", false},

		// Words that contain a marker as a substring must not match.
		{"Torpedo (2019) 1080p", false},
		{"Paedobaptism documentary 2023", false},
		{"The Pedestrian (2020) 1080p", false},
		{"Class Of 1984 1080p", false},
		{"10 Year Old (2019) 1080p", false},
		{"Javascript Course 2024", false},
		{"Some Movie 2020 1080p BluRay x264-GRP", false},
	}

	for _, test := range tests {
		t.Run(test.title, func(t *testing.T) {
			t.Parallel()

			if got := isBanned(test.title); got != test.want {
				t.Errorf("isBanned(%q) = %v, want %v", test.title, got, test.want)
			}
		})
	}
}

func TestCategoryName(t *testing.T) {
	t.Parallel()

	if got := CategoryName(CategoryMoviesHD); got != "Movies/HD" {
		t.Errorf("CategoryName = %q, want Movies/HD", got)
	}

	if got := CategoryName(1234); got != "Unknown" {
		t.Errorf("CategoryName = %q, want Unknown", got)
	}
}

func TestExtensionOf(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"release/file.mkv":        "mkv",
		"release/file.MKV":        "mkv",
		"release/file":            "",
		"release/file.":           "",
		"release.dir/file.tar.gz": "gz",
		"file.mp4":                "mp4",
	}

	for path, want := range tests {
		if got := extensionOf(path); got != want {
			t.Errorf("extensionOf(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestCensusDominant(t *testing.T) {
	t.Parallel()

	// A film with a subtitle file: video dominates.
	cen := newCensus([]File{
		{Path: "movie.mkv", Size: 8 << 30},
		{Path: "movie.srt", Size: 40 << 10},
	})

	if got := cen.dominant(); got != bucketVideo {
		t.Errorf("dominant = %d, want video", got)
	}

	// A music album with cover art: audio dominates.
	cen = newCensus([]File{
		{Path: "track01.flac", Size: 40 << 20},
		{Path: "cover.jpg", Size: 2 << 20},
	})

	if got := cen.dominant(); got != bucketAudio {
		t.Errorf("dominant = %d, want audio", got)
	}

	if newCensus(nil).dominant() != bucketOther {
		t.Error("empty census did not resolve to other")
	}
}

func BenchmarkClassify(b *testing.B) {
	files := []File{
		{Path: "Some.Show.S01E02.1080p.WEB-DL.x264-GRP/Some.Show.S01E02.1080p.mkv", Size: 2 << 30},
		{Path: "Some.Show.S01E02.1080p.WEB-DL.x264-GRP/Some.Show.S01E02.1080p.srt", Size: 40 << 10},
	}
	name := "Some.Show.S01E02.1080p.WEB-DL.x264-GRP"

	classifier := New(Config{})
	b.ReportAllocs()

	for b.Loop() {
		classifier.Classify(name, files)
	}
}

func BenchmarkClassifyAdultNameWithLargeFileList(b *testing.B) {
	// The name settles it; the file list must not be walked at all.
	files := make([]File, 2000)
	for i := range files {
		files[i] = File{Path: "Some.Adult.Release.XXX.1080p/scene.mkv", Size: 8 << 20}
	}

	classifier := New(Config{})
	b.ReportAllocs()

	for b.Loop() {
		classifier.Classify("Some.Adult.Release.XXX.1080p", files)
	}
}

func BenchmarkClassifyLargeFileList(b *testing.B) {
	files := make([]File, 2000)
	for i := range files {
		files[i] = File{
			Path: "Some.Movie.2020.1080p.BluRay-GRP/Some.Movie.2020.1080p.BluRay-GRP.mkv",
			Size: 8 << 20,
		}
	}

	classifier := New(Config{})
	b.ReportAllocs()

	for b.Loop() {
		classifier.Classify("Some.Movie.2020.1080p.BluRay-GRP", files)
	}
}
