package classify

import (
	"unicode"
)

// script is a writing system, grouped the way a user would reason about a
// filter: Japanese and Chinese titles together, and the scripts that release
// names actually use separately.
type script int

const (
	scriptNone script = iota
	scriptLatin
	scriptCJK
	scriptHangul
	scriptCyrillic
	scriptArabic
	scriptHebrew
	scriptThai
	scriptDevanagari
	scriptGreek
	scriptOther
)

// scriptNames maps the configuration names to scripts.
var scriptNames = map[string]script{
	"cjk":        scriptCJK,
	"hangul":     scriptHangul,
	"cyrillic":   scriptCyrillic,
	"arabic":     scriptArabic,
	"hebrew":     scriptHebrew,
	"thai":       scriptThai,
	"devanagari": scriptDevanagari,
	"greek":      scriptGreek,
}

// scriptOf returns the script a letter belongs to.
func scriptOf(r rune) script {
	switch {
	case unicode.Is(unicode.Latin, r):
		return scriptLatin
	case unicode.Is(unicode.Han, r),
		unicode.Is(unicode.Hiragana, r),
		unicode.Is(unicode.Katakana, r):
		return scriptCJK
	case unicode.Is(unicode.Hangul, r):
		return scriptHangul
	case unicode.Is(unicode.Cyrillic, r):
		return scriptCyrillic
	case unicode.Is(unicode.Arabic, r):
		return scriptArabic
	case unicode.Is(unicode.Hebrew, r):
		return scriptHebrew
	case unicode.Is(unicode.Thai, r):
		return scriptThai
	case unicode.Is(unicode.Devanagari, r):
		return scriptDevanagari
	case unicode.Is(unicode.Greek, r):
		return scriptGreek
	case unicode.IsLetter(r):
		return scriptOther
	default:
		return scriptNone
	}
}

// dominantScript returns the script most of the name's letters are written in.
// A tie, or a name with no letters, has no dominant script; that keeps mixed
// releases such as "Batman 蝙蝠侠" out of a foreign-only filter.
func dominantScript(name string) script {
	var counts [scriptOther + 1]int

	for _, r := range name {
		if s := scriptOf(r); s != scriptNone {
			counts[s]++
		}
	}

	best := scriptNone
	bestCount := 0
	tied := false

	for s := scriptLatin; s <= scriptOther; s++ {
		switch {
		case counts[s] > bestCount:
			best, bestCount, tied = s, counts[s], false
		case counts[s] == bestCount && counts[s] > 0:
			tied = true
		}
	}

	if bestCount == 0 || tied {
		return scriptNone
	}

	return best
}
