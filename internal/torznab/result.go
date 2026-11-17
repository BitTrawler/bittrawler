package torznab

import (
	"encoding/hex"
	"encoding/xml"
	"strconv"
	"strings"
	"time"

	"github.com/BitTrawler/bittrawler/internal/store"
)

const (
	atomNamespace    = "http://www.w3.org/2005/Atom"
	torznabNamespace = "http://torznab.com/schemas/2015/feed"
	newznabNamespace = "http://www.newznab.com/DTD/2010/feeds/attributes/"

	// rssDateLayout is the RSS 2.0 date format the clients parse.
	rssDateLayout = "Mon, 02 Jan 2006 15:04:05 -0700"

	// magnetMIMEType is the enclosure type Sonarr and Prowlarr expect for a
	// magnet link.
	magnetMIMEType = "application/x-bittorrent;x-scheme-handler/magnet"
)

// feed is the RSS document returned by a search.
type feed struct {
	XMLName   xml.Name  `xml:"rss"`
	Version   string    `xml:"version,attr"`
	AtomNS    namespace `xml:"xmlns:atom,attr"`
	TorznabNS namespace `xml:"xmlns:torznab,attr"`
	Channel   channel   `xml:"channel"`
}

// namespace is an attribute whose value is a namespace URI. The field tag names
// the prefixed attribute, which is how the namespace declarations are written.
type namespace string

func (n namespace) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	return xml.Attr{Name: name, Value: string(n)}, nil
}

type channel struct {
	Title       string   `xml:"title"`
	Description string   `xml:"description,omitempty"`
	Language    string   `xml:"language,omitempty"`
	Response    response `xml:"http://www.newznab.com/DTD/2010/feeds/attributes/ response"`
	Items       []item   `xml:"item"`
}

type response struct {
	Offset int `xml:"offset,attr"`
}

type item struct {
	Title string `xml:"title"`
	GUID  string `xml:"guid"`
	// Comments is the release's page on this site. Clients read it as the
	// release's info link: Sonarr, Radarr and Prowlarr all take it from here.
	Comments   string      `xml:"comments,omitempty"`
	PubDate    string      `xml:"pubDate"`
	Category   string      `xml:"category"`
	Size       int64       `xml:"size"`
	Enclosure  enclosure   `xml:"enclosure"`
	Attributes []attribute `xml:"torznab:attr"`
}

type enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type attribute struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// buildFeed renders one page of results. siteURL is the address of the web
// interface, used for the link back to each release's page.
func buildFeed(req SearchRequest, title, siteURL string, torrents []store.Torrent) feed {
	items := make([]item, 0, len(torrents))

	for _, t := range torrents {
		rendered := itemFor(t)
		rendered.Comments = torrentPageURL(siteURL, t.InfoHash)
		items = append(items, rendered)
	}

	return feed{
		Version:   "2.0",
		AtomNS:    atomNamespace,
		TorznabNS: torznabNamespace,
		Channel: channel{
			Title:       title,
			Description: "BitTrawler torrent index",
			Language:    "en",
			Response:    response{Offset: req.Offset},
			Items:       items,
		},
	}
}

// itemFor renders one torrent. The enclosure is the magnet URI and the same URI
// is repeated as the magneturl attribute, which is where the clients look.
func itemFor(t store.Torrent) item {
	magnet := Magnet(t.InfoHash, t.Name, t.Size)

	return item{
		Title:    t.Name,
		GUID:     hex.EncodeToString(t.InfoHash[:]),
		PubDate:  rssDate(t.CreatedAt),
		Category: strconv.Itoa(t.Category),
		Size:     t.Size,
		Enclosure: enclosure{
			URL:    magnet,
			Length: t.Size,
			Type:   magnetMIMEType,
		},
		Attributes: attributesFor(t, magnet),
	}
}

// torrentPageURL is the torrent's page on this site. Clients show it as the
// release's info link, which is what makes a result in Prowlarr or Sonarr
// clickable back to the instance that indexed it.
func torrentPageURL(siteURL string, hash [20]byte) string {
	siteURL = strings.TrimRight(siteURL, "/")
	if siteURL == "" {
		return ""
	}

	return siteURL + "/torrent/" + hex.EncodeToString(hash[:])
}

// attributesFor lists the torznab attributes the clients use for matching,
// ranking and display.
func attributesFor(t store.Torrent, magnet string) []attribute {
	attrs := []attribute{
		{Name: "infohash", Value: hex.EncodeToString(t.InfoHash[:])},
		{Name: "magneturl", Value: magnet},
		{Name: "category", Value: strconv.Itoa(t.Category)},
		{Name: "size", Value: strconv.FormatInt(t.Size, 10)},
		{Name: "seeders", Value: strconv.Itoa(t.Seeders)},
		{Name: "leechers", Value: strconv.Itoa(t.Leechers)},
		{Name: "peers", Value: strconv.Itoa(t.Seeders + t.Leechers)},
		{Name: "publishdate", Value: rssDate(t.CreatedAt)},
	}

	if t.FilesCount >= 0 {
		attrs = append(attrs, attribute{Name: "files", Value: strconv.Itoa(t.FilesCount)})
	}

	attrs = append(attrs, episodeAttributes(t)...)

	if id := tmdbID(t); id > 0 {
		attrs = append(attrs, attribute{Name: "tmdbid", Value: strconv.FormatInt(id, 10)})
	}

	return attrs
}

// episodeAttributes reports the first season and episode a release covers. The
// episodes are stored packed as season*10000+episode.
func episodeAttributes(t store.Torrent) []attribute {
	if len(t.Episodes) > 0 {
		packed := t.Episodes[0]

		return []attribute{
			{Name: "season", Value: strconv.Itoa(packed / 10000)},
			{Name: "episode", Value: strconv.Itoa(packed % 10000)},
		}
	}

	if len(t.Seasons) > 0 {
		return []attribute{{Name: "season", Value: strconv.Itoa(t.Seasons[0])}}
	}

	return nil
}

// tmdbID returns the content reference when it belongs to a film or a show.
// Adult rows carry no reference, and the category says which kind an id is.
func tmdbID(t store.Torrent) int64 {
	if t.ContentID <= 0 {
		return 0
	}

	top := t.Category / 1000
	if top == 2 || top == 5 {
		return t.ContentID
	}

	return 0
}

func rssDate(t time.Time) string {
	return t.UTC().Format(rssDateLayout)
}
