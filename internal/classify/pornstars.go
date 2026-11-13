package classify

import (
	"slices"
	"strings"
	"unicode"
)

// pornstarNames are performers whose full name identifies a release as adult.
// Only full names are listed: a first or last name alone collides with ordinary
// words and mainstream titles. The list is deliberately a seed for measured
// tuning: add names here as the instance's Other rows show them, and remove any
// that turn out to collide with legitimate releases. Matching ignores case and
// the separators release names use, so "Cory.Chase", "Cory_Chase" and
// "Cory Chase" all match. The index is built once at start up.
var pornstarNames = []string{
	"Cory Chase",
	"Mia Khalifa",
	"Riley Reid",
	"Lana Rhoades",
	"Abella Danger",
	"Angela White",
	"Brandi Love",
	"Lisa Ann",
	"Sasha Grey",
	"Tori Black",
	"Jenna Jameson",
	"Asa Akira",
	"Adriana Chechik",
	"Alexis Texas",
	"Bree Olson",
	"Bonnie Rotten",
	"Dani Daniels",
	"Dillion Harper",
	"Elsa Jean",
	"Eva Lovia",
	"Gina Gerson",
	"India Summer",
	"Jada Stevens",
	"Janice Griffith",
	"Jenna Haze",
	"Kendra Lust",
	"Kayden Kross",
	"Lexi Belle",
	"Little Caprice",
	"Madison Ivy",
	"Mia Malkova",
	"Nicole Aniston",
	"Peta Jensen",
	"Phoenix Marie",
	"Piper Perri",
	"Rachel Starr",
	"Romi Rain",
	"Sara Jay",
	"Skin Diamond",
	"Stoya",
	"Sunny Leone",
	"Tera Patrick",
	"Valentina Nappi",
	"Veronica Avluv",
	"Ava Addams",
	"Aidra Fox",
	"August Ames",
	"Autumn Falls",
	"Anikka Albrite",
	"Ariana Marie",
	"Keisha Grey",
	"Christy Mack",
	"Chanel Preston",
	"Holly Michaels",
	"Kendra Sunderland",
	"Jillian Janson",
	"Jayden Jaymes",
	"Jade Nile",
	"Jasmine Jae",
	"Katrina Jade",
	"Kleio Valentien",
	"Malena Morgan",
	"Marica Hase",
	"Mila Azul",
	"Natalia Starr",
	"Nina Hartley",
	"Rebeca Linares",
	"Savannah Bond",
	"Whitney Westgate",
	"Yurizan Beltran",
	"AJ Applegate",
	"Alina Li",
	"Abigail Mac",
	"Adria Rae",
	"Alexa Grace",
	"Alice March",
	"Amara Romani",
	"Anya Olsen",
	"Aria Alexander",
	"Aubrey Kate",
	"Bella Rolland",
	"Cecilia Lion",
	"Chloe Cherry",
	"Gabbie Carter",
	"Gia Paige",
	"Gianna Dior",
	"Gianna Michaels",
	"Harley Dean",
	"Holly Hendrix",
	"Jia Lissa",
	"Jill Kassidy",
	"Joanna Angel",
	"Karma Rx",
	"Kelsi Monroe",
	"Kenna James",
	"Kristina Rose",
	"Larkin Love",
	"Lena Paul",
	"Lily Labeau",
	"Luna Star",
	"Mandy Muse",
	"Megan Rain",
	"Mia Melano",
	"Nicole Doshi",
	"Nicolette Shea",
	"Saya Song",
	"Scarlett Sage",
	"Siri Dahl",
	"Veronica Rodriguez",
	"Vina Sky",
	"Winter Jade",
	"James Deen",
	"Johnny Sins",
	"Manuel Ferrara",
	"Mick Blue",
	"Keiran Lee",
	"Rocco Siffredi",
	"Nacho Vidal",
	"Markus Dupree",
	"Xander Corvus",
	"Steve Holmes",
	"Erik Everhard",
	"Lexington Steele",
	"Ramon Nomar",
	"Toni Ribas",
	"Jordi El Nino Polla",
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
