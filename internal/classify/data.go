package classify

import (
	"regexp"
	"strings"

	"github.com/BitTrawler/bittrawler/internal/keywords"
)

// The keyword lists are the knowledge kept from the bitmagnet classifier,
// rewritten in this package's keyword mini-language and compiled once at start
// up. Matching is case insensitive and requires word boundaries, so "ass" does
// not match "class" and "jav" does not match "javascript".
var (
	// bannedStrongRegex matches markers that identify illegal content on their
	// own; a hit is dropped and blocklisted.
	bannedStrongRegex = keywords.MustNewRegexFromKeywords(
		"pthc",
		"ptsc",
		"pa?edo(fil*|phil*)",
		"childfugga",
		"kinderkutje",
		"hebefilia",
		"young ?video ?models",
		"child ?porn*",
		"child ?lover*",
		"porno ?child*",
		"kidd(y*|ie*) ?porn*",
		"(#|10|11|12|13|14|15|16|17) ?y ?o",
		"siberian mouse",
		"hussyfan",
		"falko video",
	)

	// bannedWeakRegex matches markers that are suspicious but appear in legal
	// content: the film "Lolita", a release group called "LSM", the Spanish
	// word "pedo", an album released under the name "Underage". They only ban
	// when the text also matches bannedContextRegex.
	//
	// "loli" is deliberately not here. It is the word in the novel and both
	// films, and it is a stage name in legal adult releases; it is an adult
	// categorisation marker (adultWeakRegex), not grounds for deletion. The
	// illegal releases it appears in carry age markers, which the strong and
	// context rules catch.
	bannedWeakRegex = keywords.MustNewRegexFromKeywords(
		"pa?edo*",
		"preteen",
		"underage",
		"lsbar",
		"lsm",
		"yvm",
		"opva",
	)

	// bannedContextRegex is the corroboration a weak marker needs: a second
	// signal that the text is about a minor. Adult words are not included.
	// "porn", "sex" and "xxx" are ordinary in legal adult releases, so treating
	// them as corroboration would delete legal content.
	bannedContextRegex = keywords.MustNewRegexFromKeywords(
		"child*",
		"kidd*",
		"toddler*",
		"infant*",
		"baby*",
		"jailbait",
		"schoolgirl*",
		"schoolboy*",
		"little girl*",
		"little boy*",
		"kindergarten",
		"kindergarden",
		"nursery",
		"years old",
		"year old",
		"yr old",
		"model*",
		"loli*",
		"girl*",
		"boy*",
		"teen*",
		"incest*",
		"nymph*",
	)

	// adultStrongRegex matches markers that are adult on their own.
	adultStrongRegex = keywords.MustNewRegexFromKeywords(
		"*xxx*",
		"*porn*",
		"onlyfans",
		"brazzers",
		"bangbros",
		"realitykings",
		"naughty america",
		"evil angel",
		"digital playground",
		"blacked",
		"vixen",
		"tushy",
		"adulttime",
		"pornhub",
		"xvideos",
		"xhamster",
		"manyvids",
		"chaturbate",
		"camsoda",
		"jav",
		"javhd",
		"fc2",
		"mofos*",
		"tokyo hot",
		"tokyohot",
		"public disgrace",
		"caribbeancom",
		"avmans",
		"anilos",
		"firstanalquest",
		"bigtitcreampie",
		"pacopacomama",
		"shemale*",
		"myfans",
		"dccdom",
		"wildoncam",
		"atkgalleria*",
		"5kporn",
		"sextury",
	)

	// adultWeakRegex matches words that are adult in the context of a video or
	// image set but are ordinary English elsewhere. A weak match only changes
	// the category, never the storage decision, so the list can afford to be
	// long; the terms that appear in mainstream titles ("sister", "mom",
	// "princess", "dirty", "slave") are deliberately absent.
	adultWeakRegex = keywords.MustNewRegexFromKeywords(
		// Acts and kinks.
		"anal", "ass", "asshole*", "bdsm", "blowjobs?", "bondage", "boob*",
		"breeding", "bukkake", "cocks?", "creampie*", "cuckold", "cuck",
		"cum*", "deepthroat*", "dicks?", "dildo*", "dominatrix", "domme",
		"ejaculat*", "enema*", "erotic*", "facesitting", "face sitting",
		"femdom", "fetish*", "fka", "fisting", "footjob*", "foursome", "futanari",
		"futa", "*fuck*", "gangbang*", "gaping", "*gloryhole*", "golden shower",
		"handjobs?", "hardcore", "hotwife", "humiliation", "incest*",
		"kink*", "ladyboy", "loli*", "doujin*", "masturbat*", "milf*",
		"milking", "nubile*", "orgasm*", "orgy", "pegging", "pissing",
		"pov", "pussy", "rimming", "rimjobs?", "scat", "seduc*", "sex*",
		"slut*", "squirting", "squirt*", "strapon", "strap on", "submissive",
		"swingers?", "tgirl", "threesome", "tits?", "tranny", "urine",
		"vibrator*", "*wank*", "whore*", "hentai", "ecchi", "uncensored",
		"ahegao", "yaoi", "yuri", "bbw", "gilf", "camgirl*",
		// Studios and sites.
		"adulttime", "anilos", "atkgalleria*", "blacked", "bongacams",
		"brazzers", "bangbros", "cam4", "camsoda", "caribbeancom",
		"chaturbate", "digital playground", "eporner", "evil angel",
		"fakeagent", "fakehub", "fansly", "femjoy", "hegre", "hqporner",
		"jav", "javhd", "letsdoeit", "livejasmin", "manyvids", "metart",
		"mofos*", "myfans", "myfreecams", "naughty america", "newsensations",
		"onlyfans", "pornhub", "public disgrace", "puretaboo", "redtube",
		"realitykings", "spankbang", "stripchat", "teamskeet", "tushy",
		"txxx", "vixen", "x-art", "xhamster", "xvideos", "5kporn", "sextury",
		"tokyo hot", "tokyohot", "avmans", "dccdom", "wildoncam",
	)

	// audiobookKeywordsRegex marks spoken-word audio.
	audiobookKeywordsRegex = keywords.MustNewRegexFromKeywords(
		"audiobooks?",
		"unabridged",
		"abridged",
		"narrated",
		"audible",
		"m4b",
		"aax",
	)

	// musicKeywordsRegex marks audio releases by name.
	musicKeywordsRegex = keywords.MustNewRegexFromKeywords(
		"flac",
		"alac",
		"ape",
		"wav",
		"mp3",
		"320kbps",
		"lossless",
		"discography",
		"various artists",
		"album",
		"ost",
		"soundtrack",
		"vinyl",
		"cdrip",
	)

	// ebookKeywordsRegex marks book releases by name.
	ebookKeywordsRegex = keywords.MustNewRegexFromKeywords(
		"epub",
		"mobi",
		"azw3?",
		"azw8?",
		"djvu",
		"ebooks?",
		"e-books?",
		"kindle",
		"chm",
	)

	// comicKeywordsRegex marks comic releases by name.
	comicKeywordsRegex = keywords.MustNewRegexFromKeywords(
		"cbr",
		"cbz",
		"cbt",
		"cb7",
		"cba",
		"comics?",
		"manga",
		"graphic novel",
	)

	// softwareKeywordsRegex marks application releases by name, used when the
	// file list is missing or not decisive.
	softwareKeywordsRegex = keywords.MustNewRegexFromKeywords(
		"installer",
		"keygen",
		"crack",
		"activator",
		"portable",
		"adobe",
		"photoshop",
		"illustrator",
		"autocad",
		"microsoft",
		"vmware",
		"virtualbox",
		"parallels",
		"windows",
		"linux",
		"ubuntu",
		"macos",
		"win64",
		"win32",
		"x64",
		"x86",
		"apk",
		"msi",
		"exe",
		"dmg",
		"vst",
	)

	// gameKeywordsRegex marks game releases by name. Some of these are release
	// group names, which is deliberate: game groups are specific to games.
	// "repack" is deliberately absent: repacks are as common for applications as
	// for games, and games have the group names below.
	gameKeywordsRegex = keywords.MustNewRegexFromKeywords(
		"fitgirl",
		"dodi",
		"gog",
		"steam",
		"codex",
		"plaza",
		"skidrow",
		"reloaded",
		"empress",
		"hoodlum",
		"elamigos",
		"razor1911",
		"tenoke",
		"xatab",
		"3dmgame",
		"rpg",
		"game of the year",
		"goty",
		"ps4",
		"ps5",
		"xbox",
		"nintendo",
		"switch",
		"nsw",
		"wii",
		"3ds",
		"psp",
		"vita",
	)

	// sourceKeywordsRegex marks video releases by name when the file list is
	// missing. The resolution parser covers the common case; these cover the
	// releases whose name states the source but not the resolution.
	sourceKeywordsRegex = keywords.MustNewRegexFromKeywords(
		"bluray",
		"blu ray",
		"bdrip",
		"bdremux",
		"brrip",
		"web-dl",
		"web dl",
		"webrip",
		"hdtv",
		"dvdrip",
		"dvd5",
		"dvd9",
		"dvdr",
		"remux",
		"x264",
		"x265",
		"h264",
		"h265",
		"hevc",
		"avc",
		"xvid",
		"divx",
		"mkv",
		"mp4",
		"avi",
		"m2ts",
		"camrip",
		"telesync",
		"telecine",
		"workprint",
		"pal",
		"ntsc",
	)
)

// isBanned reports whether the text matches the blocking rules. Strong markers
// ban on their own; weak markers need a second, corroborating word, because
// dropping a torrent is irreversible.
func isBanned(text string) bool {
	return BannedReason(text) != ""
}

// adultCJKTerms are adult markers in Chinese and Japanese. They are matched as
// substrings: the keyword expressions require word boundaries, and CJK text
// has no spaces for a boundary to sit on.
var adultCJKTerms = []string{
	"無修正", "无码", "無碼", "中出", "痴漢", "痴汉", "痴女", "素人",
	"巨乳", "自慰", "女優", "女优", "淫", "熟女", "人妻", "色情",
	"骚", "騷", "裸舞", "裸聊", "少妇",
}

// javLabels are the studio prefixes of Japanese adult releases, such as
// "SSNI-123" or "JUY-832". Releases named only by a code are common, and the
// code is the only signal they carry. The prefixes are matched both on a
// separator ("JUY-832") and attached to digits ("JUR014C"); the list is
// deliberately limited to prefixes that are not also ordinary words.
var javLabels = []string{
	"agav", "atid", "blor", "bnsps", "cawd", "cjod", "ctpn",
	"dvaj", "dvdms", "ebod", "focs", "havd", "hmad", "hunta",
	"ipx", "juf", "jufe", "jur", "juy", "ksat", "miaa", "mide", "midv",
	"mifd", "mimk", "mkmp", "mudr", "mfyd", "nhdta", "royd",
	"smdv", "snis", "sone", "ssis", "ssni", "ssks", "tki", "wanz",
	"xrw", "ymdd", "zex",
}

// javLabelRegex matches a JAV studio prefix. The prefix alone is not enough:
// "JUY" must be followed by a separator or a digit, so an ordinary word that
// starts with the prefix does not match.
var javLabelRegex = func() *regexp.Regexp {
	patterns := make([]string, 0, len(javLabels)*2+1)
	for _, label := range javLabels {
		// "JUY-832" needs the bare prefix; "JUR014C" needs the prefix attached
		// to digits.
		patterns = append(patterns, label, label+"#*")
	}

	// "paco" is also a Spanish given name, so it only counts in its coded form
	// ("paco-092415").
	patterns = append(patterns, "paco-#*")

	return keywords.MustNewRegexFromKeywords(patterns...)
}()

// adultStrongText reports whether text carries an explicit adult marker. It
// covers the ASCII keyword rules and the CJK terms.
func adultStrongText(text string) bool {
	if adultStrongRegex.MatchString(text) || javLabelRegex.MatchString(text) {
		return true
	}

	for _, term := range adultCJKTerms {
		if strings.Contains(text, term) {
			return true
		}
	}

	return false
}

// BannedReason reports which rule matched the text, or "" when none did. It
// exists for logging and for reviewing the rules against real names.
func BannedReason(text string) string {
	if bannedStrongRegex.MatchString(text) {
		return "strong"
	}

	if bannedWeakRegex.MatchString(text) && bannedContextRegex.MatchString(text) {
		return "weak+context"
	}

	return ""
}
