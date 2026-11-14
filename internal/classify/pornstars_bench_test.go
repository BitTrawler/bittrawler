package classify

import (
	"fmt"
	"testing"
)

func BenchmarkPornstarTextNoMatch(b *testing.B) {
	name := "Some.Movie.2020.1080p.BluRay.x264-GRP"

	for b.Loop() {
		pornstarText(name)
	}
}

func BenchmarkPornstarTextMatch(b *testing.B) {
	name := "Cory.Chase.And.Her.Friend.2024.1080p.WEB-DL"

	for b.Loop() {
		pornstarText(name)
	}
}

func BenchmarkPornstarTextLongText(b *testing.B) {
	text := ""
	for i := range 40 {
		text += fmt.Sprintf("Some.Folder.Name.%02d/Movie.File.S01E%02d.1080p.mkv ", i, i)
	}

	for b.Loop() {
		pornstarText(text)
	}
}

// BenchmarkPornstarTextBigList measures the lookup cost against a list twenty
// times the current size, which is the direction the list is heading.
func BenchmarkPornstarTextBigList(b *testing.B) {
	names := make([]string, 0, len(pornstarNames)*20)
	for i := 0; i < 20; i++ {
		for _, name := range pornstarNames {
			names = append(names, fmt.Sprintf("%s Number%d", name, i))
		}
	}

	index := buildPornstarIndex(names)
	name := "Some.Movie.2020.1080p.BluRay.x264-GRP"
	tokens := nameTokens(name)

	b.ResetTimer()

	for b.Loop() {
		for i := 0; i < len(tokens); i++ {
			for _, sequence := range index[tokens[i]] {
				_ = sequence
			}
		}
	}
}

func BenchmarkClassifyTypical(b *testing.B) {
	classifier := New(Config{})
	files := []File{{Path: "Some.Movie.2020.1080p.BluRay.x264-GRP.mkv", Size: 8 << 30}}

	for b.Loop() {
		classifier.Classify("Some.Movie.2020.1080p.BluRay.x264-GRP", files)
	}
}
