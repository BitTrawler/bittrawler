package classify

import "strings"

// bucket is the general type of a file. It decides what a torrent mostly
// contains, which is the strongest signal classification has.
type bucket int

const (
	bucketOther bucket = iota
	bucketVideo
	bucketAudio
	bucketImage
	bucketEbook
	bucketComic
	bucketSoftware
	bucketArchive
	bucketCount
)

// bucketOrder is the tie-break order when two buckets hold the same number of
// bytes. It runs from the type that says the most about a torrent to the type
// that says the least.
var bucketOrder = [...]bucket{
	bucketVideo,
	bucketAudio,
	bucketComic,
	bucketEbook,
	bucketSoftware,
	bucketImage,
	bucketArchive,
	bucketOther,
}

// extensionBuckets maps a lowercase extension, without the leading dot, to its
// bucket. Extensions that are not listed are bucketOther.
var extensionBuckets = map[string]bucket{
	// Video
	"mkv": bucketVideo, "mp4": bucketVideo, "avi": bucketVideo,
	"mov": bucketVideo, "wmv": bucketVideo, "flv": bucketVideo,
	"m4v": bucketVideo, "mpg": bucketVideo, "mpeg": bucketVideo,
	"vob": bucketVideo, "ts": bucketVideo, "m2ts": bucketVideo,
	"mts": bucketVideo, "webm": bucketVideo, "m2v": bucketVideo,
	"ogv": bucketVideo, "asf": bucketVideo, "3gp": bucketVideo,
	"rm": bucketVideo, "rmvb": bucketVideo, "divx": bucketVideo,

	// Audio
	"mp3": bucketAudio, "wav": bucketAudio, "flac": bucketAudio,
	"aac": bucketAudio, "ogg": bucketAudio, "m4a": bucketAudio,
	"m4b": bucketAudio, "mid": bucketAudio, "midi": bucketAudio,
	"dsf": bucketAudio, "ape": bucketAudio, "opus": bucketAudio,
	"wma": bucketAudio, "aiff": bucketAudio, "aif": bucketAudio,
	"mka": bucketAudio, "alac": bucketAudio,

	// Image
	"jpg": bucketImage, "jpeg": bucketImage, "png": bucketImage,
	"gif": bucketImage, "bmp": bucketImage, "svg": bucketImage,
	"dds": bucketImage, "psd": bucketImage, "tif": bucketImage,
	"tiff": bucketImage, "ico": bucketImage, "webp": bucketImage,
	"heic": bucketImage, "avif": bucketImage, "raw": bucketImage,

	// Ebook
	"epub": bucketEbook, "mobi": bucketEbook, "azw": bucketEbook,
	"azw3": bucketEbook, "azw4": bucketEbook, "azw8": bucketEbook,
	"chm": bucketEbook, "djvu": bucketEbook, "lit": bucketEbook,
	"pdf": bucketEbook, "odt": bucketEbook, "rtf": bucketEbook,
	"doc": bucketEbook, "docx": bucketEbook,

	// Comic
	"cb7": bucketComic, "cba": bucketComic, "cbr": bucketComic,
	"cbt": bucketComic, "cbz": bucketComic,

	// Software
	"exe": bucketSoftware, "sh": bucketSoftware,
	"bat": bucketSoftware, "msi": bucketSoftware, "apk": bucketSoftware,
	"dmg": bucketSoftware, "pkg": bucketSoftware, "deb": bucketSoftware,
	"rpm": bucketSoftware, "jar": bucketSoftware, "dll": bucketSoftware,
	"lua": bucketSoftware, "app": bucketSoftware, "appx": bucketSoftware,
	"package": bucketSoftware, "vst": bucketSoftware, "xex": bucketSoftware,

	// Archive and disk images
	"zip": bucketArchive, "rar": bucketArchive, "tar": bucketArchive,
	"gz": bucketArchive, "tgz": bucketArchive, "7z": bucketArchive,
	"bz2": bucketArchive, "xz": bucketArchive, "zst": bucketArchive,
	"iso": bucketArchive, "img": bucketArchive, "nrg": bucketArchive,
	// .bin is a raw disk or media image as often as it is a program; treating
	// it as software made film releases that ship as bin/cue look like malware.
	"bin": bucketArchive,
	"nsp": bucketArchive, "xci": bucketArchive, "cia": bucketArchive,

	// Subtitles are deliberately bucketOther: they are tiny and must not
	// influence dominance.
}

// census counts the bytes and files per bucket.
type census struct {
	bytes     [bucketCount]int64
	files     int
	audiobook bool
}

func newCensus(files []File) census {
	var cen census

	for _, file := range files {
		ext := extensionOf(file.Path)
		b := extensionBuckets[ext]

		cen.bytes[b] += file.Size
		cen.files++

		if ext == "m4b" || ext == "aax" || ext == "aa" {
			cen.audiobook = true
		}
	}

	return cen
}

// dominant returns the bucket holding the most bytes. Ties are broken by
// bucketOrder, and an empty or unknown file list resolves to bucketOther.
func (c census) dominant() bucket {
	best := bucketOther
	var bestBytes int64

	for _, b := range bucketOrder {
		if c.bytes[b] > bestBytes {
			best = b
			bestBytes = c.bytes[b]
		}
	}

	return best
}

// extensionOf returns the lowercase extension of a path, without the dot.
func extensionOf(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}

	i := strings.LastIndexByte(path, '.')
	if i < 0 || i+1 >= len(path) {
		return ""
	}

	return strings.ToLower(path[i+1:])
}
