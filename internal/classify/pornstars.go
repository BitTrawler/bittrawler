package classify

import (
	_ "embed"
	"slices"
	"strings"
	"unicode"
)

// pornstarNames are performers whose full name identifies a release as adult.
// The list lives in data/pornstars.txt so it can grow without touching code:
// full names only, matched case-insensitively with any separator, and one name
// per line. A first or last name alone is not listed, because it collides with
// ordinary words and mainstream titles.
//
//go:embed data/pornstars.txt
var pornstarList string

// pornstarNames is the parsed list.
var pornstarNames = parsePornstarNames(pornstarList)

// parsePornstarNames reads one name per line and ignores blank lines and #
// comments.
func parsePornstarNames(text string) []string {
	var names []string

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		names = append(names, line)
	}

	return names
}

// pornstarIndex maps a first token to the token sequences that start with it,
// so a release name is one pass over its words rather than one regex per name.
var pornstarIndex = buildPornstarIndex(pornstarNames)

// buildPornstarIndex indexes the names by their first token. A single-word name
// is skipped: it would match ordinary words.
func buildPornstarIndex(names []string) map[string][][]string {
	index := make(map[string][][]string, len(names))

	for _, name := range names {
		tokens := nameTokens(name)
		if len(tokens) < 2 {
			continue
		}

		index[tokens[0]] = append(index[tokens[0]], tokens)
	}

	return index
}

// nameTokens splits text into lowercase words of letters and digits, so the
// separators a release name uses do not matter.
func nameTokens(text string) []string {
	tokens := make([]string, 0, 8)

	var word strings.Builder

	flush := func() {
		if word.Len() == 0 {
			return
		}

		tokens = append(tokens, strings.ToLower(word.String()))
		word.Reset()
	}

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)

			continue
		}

		flush()
	}

	flush()

	return tokens
}

// pornstarText reports whether text contains a listed performer's full name.
func pornstarText(text string) bool {
	tokens := nameTokens(text)

	for i := 0; i < len(tokens); i++ {
		for _, sequence := range pornstarIndex[tokens[i]] {
			if i+len(sequence) > len(tokens) {
				continue
			}

			if slices.Equal(tokens[i:i+len(sequence)], sequence) {
				return true
			}
		}
	}

	return false
}
