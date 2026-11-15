package classify

import "testing"

// TestRefineCategory covers the leaf assignment: the classification decides the
// family, and this decides which Newznab leaf within it describes the release.
func TestRefineCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		files    []File
		category int
	}{
		// Films: the resolution leaf wins; 3D is a format of its own; the source
		// leaves are fallbacks when the resolution is absent.
		{name: "film hd stays hd", title: "Some.Movie.2020.1080p.BluRay.x264-GRP", files: filesOf("mkv", 8<<30), category: CategoryMoviesHD},
		{name: "film 3d", title: "Some.Movie.2020.1080p.3D.HSBS", files: filesOf("mkv", 8<<30), category: CategoryMovies3D},
		{name: "bluray image", title: "Some.Movie.2020.BluRay", files: filesOf("iso", 30<<30), category: CategoryMoviesBluRay},
		{name: "dvd image", title: "Some.Movie.2004.PAL.DVDR-TGP", files: filesOf("iso", 4<<30), category: CategoryMoviesDVD},
		{name: "web without resolution", title: "Some.Movie.2024.WEB-DL", files: filesOf("mkv", 4<<30), category: CategoryMoviesWEB},
		{name: "x265 without resolution", title: "Some.Movie.2024.HEVC", files: filesOf("mkv", 4<<30), category: CategoryMoviesx265},

		// Television keeps its resolution leaf, which is what Sonarr asks for;
		// the source leaves only fill in when no resolution is stated.
		{name: "tv hd stays hd", title: "Some.Show.S01E02.1080p.WEB-DL", files: filesOf("mkv", 1<<30), category: CategoryTVHD},
		{name: "tv web without resolution", title: "Some.Show.S01E02.WEB-DL", files: filesOf("mkv", 1<<30), category: CategoryTVWEB},
		{name: "tv x265 without resolution", title: "Some.Show.S01E02.HEVC", files: filesOf("mkv", 1<<30), category: CategoryTVx265},

		// Audio: the file format is the leaf.
		{name: "flac album", title: "Some Artist - Some Album (2020)", files: filesOf("flac", 40<<20), category: CategoryAudioLossless},
		{name: "mp3 album", title: "Some Artist - Some Album (2020)", files: filesOf("mp3", 8<<20), category: CategoryAudioMP3},
		{name: "other audio", title: "Some Artist - Some Album (2020)", files: filesOf("m4a", 8<<20), category: CategoryAudioOther},
		{name: "audiobook untouched", title: "Some Book (2020) Audiobook", files: filesOf("m4b", 200<<20), category: CategoryAudiobook},

		// PC: the file type decides where a leaf exists.
		{name: "ios package", title: "Some App 1.0", files: filesOf("ipa", 200<<20), category: CategoryPCiOS},
		{name: "android package", title: "Some App 1.0", files: filesOf("apk", 200<<20), category: CategoryPCAndroid},
		{name: "mac package", title: "Some App 1.0", files: filesOf("dmg", 200<<20), category: CategoryPCMac},
		{name: "software image", title: "Adobe Photoshop 2024", files: filesOf("iso", 3<<30), category: CategoryPCISO},
		{name: "plain installer", title: "Some App 1.0", files: filesOf("exe", 80<<20), category: CategoryPC},

		// Games: a named console gets its leaf, everything else stays PC/Games.
		{name: "ps4 game", title: "Some.Game.PS4.ISO", files: filesOf("iso", 30<<30), category: CategoryConsolePS4},
		{name: "xbox 360 game", title: "Some.Game.Xbox.360-RUNE", files: filesOf("iso", 8<<30), category: CategoryConsoleXbox360},
		{name: "switch package", title: "Some.Game [NSZ]", files: filesOf("nsz", 2<<30), category: CategoryConsoleOther},
		{name: "pc game", title: "Some.Game-FitGirl Repack", files: filesOf("exe", 8<<30), category: CategoryPCGames},

		// Adult content: the format leaves, with the parent as the fallback.
		{name: "adult image set", title: "Sexy Models Vol 3", files: filesOf("jpg", 200<<20), category: CategoryXXXImageSet},
		{name: "adult xvid", title: "Some.Title.XXX.480p.XviD", files: filesOf("avi", 700<<20), category: CategoryXXXXviD},
		{name: "adult x264", title: "Some.Title.XXX.1080p.x264", files: filesOf("mp4", 2<<30), category: CategoryXXXx264},
		{name: "adult web", title: "Some.Title.XXX.1080p.WEB-DL", files: filesOf("mp4", 2<<30), category: CategoryXXXWEB},
		{name: "adult dvd", title: "Some.Title.XXX.DVDRip", files: filesOf("avi", 700<<20), category: CategoryXXXDVD},
		{name: "adult uhd", title: "Some.Title.XXX.2160p", files: filesOf("mp4", 8<<30), category: CategoryXXXUHD},
		{name: "adult unknown format", title: "Some.Title.XXX", files: filesOf("mp4", 2<<30), category: CategoryXXX},

		// Books: the subject narrows the ebook leaf.
		{name: "magazine", title: "Some Magazine Issue 42", files: filesOf("pdf", 80<<20), category: CategoryBooksMags},
		{name: "technical book", title: "Python Programming for Beginners", files: filesOf("epub", 8<<20), category: CategoryBooksTechnical},
		{name: "plain ebook", title: "Some Novel (2020)", files: filesOf("epub", 2<<20), category: CategoryEbook},

		// Other: model weights are data for a program, so PC.
		{name: "model weights", title: "zoobot-encoder-convnext_nano", files: filesOf("safetensors", 57<<20), category: CategoryPC},
		{name: "genuinely other", title: "Some Dataset 2024", files: filesOf("csv", 2<<20), category: CategoryOther},
	}

	classifier := New(Config{})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result := classifier.Classify(test.title, test.files)
			if result.Drop != DropNone {
				t.Fatalf("drop = %v, want none", result.Drop)
			}

			if result.Category != test.category {
				t.Fatalf("category = %d (%s), want %d (%s)",
					result.Category, CategoryName(result.Category),
					test.category, CategoryName(test.category))
			}
		})
	}
}

func TestParentCategory(t *testing.T) {
	t.Parallel()

	tests := map[int]int{
		CategoryConsolePS4:     CategoryConsole,
		CategoryMovies3D:       CategoryMovies,
		CategoryAudioLossless:  CategoryAudio,
		CategoryPCMac:          CategoryPC,
		CategoryTVWEB:          CategoryTV,
		CategoryXXXImageSet:    CategoryXXX,
		CategoryBooksTechnical: CategoryBooks,
		CategoryOther:          CategoryOther,
		CategoryMovies:         CategoryMovies,
	}

	for category, want := range tests {
		if got := ParentCategory(category); got != want {
			t.Errorf("ParentCategory(%d) = %d, want %d", category, got, want)
		}
	}
}
