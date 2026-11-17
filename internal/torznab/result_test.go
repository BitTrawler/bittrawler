package torznab

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/store"
)

const fixedDate = "Wed, 04 Mar 2026 05:06:07 +0000"

func fixedTorrent() store.Torrent {
	return store.Torrent{
		InfoHash:   [20]byte{0x01, 0x02, 0x03},
		Name:       "Show.S01E02.1080p.WEB-DL",
		Size:       1_500_000,
		CreatedAt:  time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		Category:   2040,
		ContentID:  603,
		Seasons:    []int{1},
		Episodes:   []int{10002},
		FilesCount: 3,
		Seeders:    12,
		Leechers:   4,
	}
}

func TestItemFor(t *testing.T) {
	torrent := fixedTorrent()
	it := itemFor(torrent)

	if it.Title != torrent.Name {
		t.Errorf("title = %q, want %q", it.Title, torrent.Name)
	}

	if want := hex.EncodeToString(torrent.InfoHash[:]); it.GUID != want {
		t.Errorf("guid = %q, want %q", it.GUID, want)
	}

	if it.PubDate != fixedDate {
		t.Errorf("pubDate = %q, want %q", it.PubDate, fixedDate)
	}

	if it.Category != "2040" {
		t.Errorf("category = %q, want %q", it.Category, "2040")
	}

	if it.Size != torrent.Size {
		t.Errorf("size = %d, want %d", it.Size, torrent.Size)
	}

	if want := Magnet(torrent.InfoHash, torrent.Name, torrent.Size); it.Enclosure.URL != want {
		t.Errorf("enclosure url = %q, want %q", it.Enclosure.URL, want)
	}

	if it.Enclosure.Type != magnetMIMEType {
		t.Errorf("enclosure type = %q, want %q", it.Enclosure.Type, magnetMIMEType)
	}

	if it.Enclosure.Length != torrent.Size {
		t.Errorf("enclosure length = %d, want %d", it.Enclosure.Length, torrent.Size)
	}

	wantAttrs := map[string]string{
		"infohash":    hex.EncodeToString(torrent.InfoHash[:]),
		"magneturl":   Magnet(torrent.InfoHash, torrent.Name, torrent.Size),
		"category":    "2040",
		"size":        "1500000",
		"seeders":     "12",
		"leechers":    "4",
		"peers":       "16",
		"publishdate": fixedDate,
		"files":       "3",
		"season":      "1",
		"episode":     "2",
		"tmdbid":      "603",
	}

	assertAttributes(t, it, wantAttrs)
}

func TestItemForWithoutOptionalAttributes(t *testing.T) {
	torrent := store.Torrent{
		InfoHash:   [20]byte{0x09},
		Name:       "Some.Release",
		Size:       10,
		CreatedAt:  time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		Category:   8000,
		FilesCount: -1,
	}

	it := itemFor(torrent)

	for _, name := range []string{"files", "season", "episode", "tmdbid"} {
		if hasAttribute(it, name) {
			t.Errorf("attribute %q should be absent", name)
		}
	}
}

func TestItemForSeasonPackHasNoEpisode(t *testing.T) {
	torrent := fixedTorrent()
	torrent.Episodes = nil
	torrent.Seasons = []int{4}

	it := itemFor(torrent)
	attrs := itemAttributeMap(it)

	if attrs["season"] != "4" {
		t.Errorf("season = %q, want %q", attrs["season"], "4")
	}

	if _, ok := attrs["episode"]; ok {
		t.Errorf("a season pack should carry no episode attribute")
	}
}

func TestItemForContentReferenceOnlyForFilmAndTV(t *testing.T) {
	// Adult rows carry no reference, and a reference on any other category is
	// not meaningful to a client.
	for _, category := range []int{6000, 3000, 4000, 7000, 8000} {
		torrent := fixedTorrent()
		torrent.Category = category
		torrent.ContentID = 12345

		if hasAttribute(itemFor(torrent), "tmdbid") {
			t.Errorf("category %d should not expose a tmdbid", category)
		}
	}

	for _, category := range []int{2000, 2040, 5000, 5045} {
		torrent := fixedTorrent()
		torrent.Category = category
		torrent.ContentID = 12345

		attrs := itemAttributeMap(itemFor(torrent))
		if attrs["tmdbid"] != "12345" {
			t.Errorf("category %d tmdbid = %q, want %q", category, attrs["tmdbid"], "12345")
		}
	}
}

func TestBuildFeed(t *testing.T) {
	const siteURL = "https://bittrawler.example:6684"

	feed := buildFeed(SearchRequest{Offset: 20}, "TestTrawler", siteURL, []store.Torrent{fixedTorrent()})

	if feed.Version != "2.0" {
		t.Errorf("version = %q, want %q", feed.Version, "2.0")
	}

	if string(feed.AtomNS) != atomNamespace {
		t.Errorf("atom namespace = %q, want %q", feed.AtomNS, atomNamespace)
	}

	if string(feed.TorznabNS) != torznabNamespace {
		t.Errorf("torznab namespace = %q, want %q", feed.TorznabNS, torznabNamespace)
	}

	if feed.Channel.Title != "TestTrawler" {
		t.Errorf("channel title = %q, want %q", feed.Channel.Title, "TestTrawler")
	}

	if feed.Channel.Response.Offset != 20 {
		t.Errorf("response offset = %d, want 20", feed.Channel.Response.Offset)
	}

	if len(feed.Channel.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(feed.Channel.Items))
	}

	item := feed.Channel.Items[0]
	if want := siteURL + "/torrent/0102030000000000000000000000000000000000"; item.Comments != want {
		t.Errorf("comments = %q, want %q", item.Comments, want)
	}
}

func TestPublishDateIsParseable(t *testing.T) {
	parsed, err := time.Parse(rssDateLayout, rssDate(time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)))
	if err != nil {
		t.Fatalf("rss date is not parseable: %v", err)
	}

	if !parsed.Equal(time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)) {
		t.Fatalf("round tripped date = %v", parsed)
	}
}

func TestRSSDateIsUTC(t *testing.T) {
	zone := time.FixedZone("elsewhere", 5*3600)
	got := rssDate(time.Date(2026, 3, 4, 10, 6, 7, 0, zone))

	if !strings.HasSuffix(got, "+0000") {
		t.Fatalf("rss date = %q, want a UTC offset", got)
	}
}

func assertAttributes(t *testing.T, it item, want map[string]string) {
	t.Helper()

	got := make(map[string]string, len(it.Attributes))
	for _, attr := range it.Attributes {
		if _, exists := got[attr.Name]; exists {
			t.Errorf("attribute %q appears more than once", attr.Name)
		}

		got[attr.Name] = attr.Value
	}

	for name, value := range want {
		if got[name] != value {
			t.Errorf("attribute %q = %q, want %q", name, got[name], value)
		}
	}

	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected attribute %q = %q", name, got[name])
		}
	}
}

// itemAttributeMap indexes an item's attributes by name.
func itemAttributeMap(it item) map[string]string {
	out := make(map[string]string, len(it.Attributes))

	for _, attr := range it.Attributes {
		out[attr.Name] = attr.Value
	}

	return out
}

func hasAttribute(it item, name string) bool {
	for _, attr := range it.Attributes {
		if attr.Name == name {
			return true
		}
	}

	return false
}

func TestBuildFeedWithoutSiteURL(t *testing.T) {
	feed := buildFeed(SearchRequest{}, "TestTrawler", "", []store.Torrent{fixedTorrent()})

	if got := feed.Channel.Items[0].Comments; got != "" {
		t.Errorf("comments = %q, want it left out", got)
	}
}
