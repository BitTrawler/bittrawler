package torznab

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BitTrawler/bittrawler/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()

	s, err := store.Open(store.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}

	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})

	return s
}

func testTimestamp(day int) time.Time {
	return time.Date(2026, 11, day, 12, 0, 0, 0, time.UTC)
}

func testHash(n byte) [20]byte {
	var hash [20]byte
	hash[0] = n

	return hash
}

// seedStore writes one of each kind of release the endpoint has to serve.
func seedStore(t *testing.T) *store.Store {
	t.Helper()

	s := openTestStore(t)

	rows := []store.Row{
		{
			InfoHash:   testHash(1),
			Name:       "The Show S01E02 1080p WEB-DL",
			Size:       1_500_000_000,
			CreatedAt:  testTimestamp(5),
			Category:   5040,
			Seasons:    []int{1},
			Episodes:   []int{10002},
			FilesCount: 3,
			Seeders:    10,
			Leechers:   2,
		},
		{
			InfoHash:   testHash(2),
			Name:       "The Show S01 Complete 1080p",
			Size:       20_000_000_000,
			CreatedAt:  testTimestamp(4),
			Category:   5040,
			Seasons:    []int{1},
			FilesCount: 10,
			Seeders:    5,
			Leechers:   1,
		},
		{
			InfoHash:   testHash(3),
			Name:       "The Movie 2024 1080p BluRay",
			Size:       8_000_000_000,
			CreatedAt:  testTimestamp(3),
			Category:   2040,
			ContentID:  603,
			FilesCount: 1,
			Seeders:    20,
			Leechers:   5,
		},
		{
			InfoHash:   testHash(4),
			Name:       "The Daily Show 2024-03-25 720p",
			Size:       500_000_000,
			CreatedAt:  testTimestamp(2),
			Category:   5040,
			AirDate:    "2024-03-25",
			FilesCount: 2,
			Seeders:    3,
			Leechers:   0,
		},
		{
			InfoHash:   testHash(5),
			Name:       "Some Album 320kbps",
			Size:       100_000_000,
			CreatedAt:  testTimestamp(1),
			Category:   3000,
			FilesCount: 12,
			Seeders:    7,
			Leechers:   0,
		},
	}

	inserted, err := s.PutBatch(context.Background(), rows)
	if err != nil {
		t.Fatalf("PutBatch() error = %v", err)
	}

	if inserted != len(rows) {
		t.Fatalf("PutBatch() inserted %d rows, want %d", inserted, len(rows))
	}

	return s
}

// testSiteURL is the address the endpoint is configured with.
const testSiteURL = "http://bittrawler.test:6684"

func seedHandler(t *testing.T) http.Handler {
	t.Helper()

	return newTestHandler(t, Config{Searcher: seedStore(t), SiteURL: testSiteURL})
}

func titles(items []parsedItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}

	return out
}

func TestEndpointTVSearch(t *testing.T) {
	rec := get(t, seedHandler(t), "t=tvsearch&q=The+Show&season=1&ep=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	items := decodeFeed(t, rec.Body.Bytes()).Channel.Items
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1: %v", len(items), titles(items))
	}

	if items[0].Title != "The Show S01E02 1080p WEB-DL" {
		t.Fatalf("title = %q", items[0].Title)
	}

	attrs := attributes(items[0])
	if attrs["season"] != "1" || attrs["episode"] != "2" {
		t.Fatalf("season, episode attributes = %q, %q, want 1, 2", attrs["season"], attrs["episode"])
	}
}

func TestEndpointSeasonSearchIncludesPacksAndEpisodes(t *testing.T) {
	items := decodeFeed(t, get(t, seedHandler(t), "t=tvsearch&season=1").Body.Bytes()).Channel.Items

	if len(items) != 2 {
		t.Fatalf("items = %d, want 2: %v", len(items), titles(items))
	}

	// The pack comes first, because it is the row that answers a season search;
	// the episodes follow it in date order.
	if items[0].Title != "The Show S01 Complete 1080p" {
		t.Fatalf("first title = %q, want the pack", items[0].Title)
	}

	if items[1].Title != "The Show S01E02 1080p WEB-DL" {
		t.Fatalf("second title = %q, want the episode", items[1].Title)
	}

	packAttrs := attributes(items[0])
	if packAttrs["season"] != "1" {
		t.Fatalf("pack season attribute = %q, want 1", packAttrs["season"])
	}

	if _, ok := packAttrs["episode"]; ok {
		t.Fatalf("a pack should carry no episode attribute")
	}
}

func TestEndpointMovieSearch(t *testing.T) {
	items := decodeFeed(t, get(t, seedHandler(t), "t=movie&q=The+Movie").Body.Bytes()).Channel.Items

	if len(items) != 1 {
		t.Fatalf("items = %d, want 1: %v", len(items), titles(items))
	}

	if items[0].Title != "The Movie 2024 1080p BluRay" {
		t.Fatalf("title = %q", items[0].Title)
	}

	if items[0].Category != "2040" {
		t.Fatalf("category = %q, want 2040", items[0].Category)
	}

	if got := attributes(items[0])["tmdbid"]; got != "603" {
		t.Fatalf("tmdbid = %q, want 603", got)
	}
}

func TestEndpointMovieSearchByTMDBID(t *testing.T) {
	items := decodeFeed(t, get(t, seedHandler(t), "t=movie&tmdbid=603").Body.Bytes()).Channel.Items

	if len(items) != 1 || items[0].Title != "The Movie 2024 1080p BluRay" {
		t.Fatalf("items = %v, want the film", titles(items))
	}
}

func TestEndpointTextSearch(t *testing.T) {
	items := decodeFeed(t, get(t, seedHandler(t), "t=search&q=Show").Body.Bytes()).Channel.Items

	if len(items) != 3 {
		t.Fatalf("items = %d, want 3: %v", len(items), titles(items))
	}

	got := make(map[string]bool, len(items))
	for _, it := range items {
		got[it.Title] = true
	}

	for _, want := range []string{
		"The Show S01E02 1080p WEB-DL",
		"The Show S01 Complete 1080p",
		"The Daily Show 2024-03-25 720p",
	} {
		if !got[want] {
			t.Errorf("missing %q from %v", want, titles(items))
		}
	}
}

func TestEndpointCategoryFilterMatchesChildren(t *testing.T) {
	items := decodeFeed(t, get(t, seedHandler(t), "t=search&cat=5000").Body.Bytes()).Channel.Items

	if len(items) != 3 {
		t.Fatalf("items = %d, want 3: %v", len(items), titles(items))
	}

	if items[0].Title != "The Show S01E02 1080p WEB-DL" {
		t.Fatalf("first title = %q, want the newest TV release", items[0].Title)
	}
}

func TestEndpointDailySearch(t *testing.T) {
	items := decodeFeed(t, get(t, seedHandler(t), "t=tvsearch&season=2024&ep=03%2F25").Body.Bytes()).Channel.Items

	if len(items) != 1 {
		t.Fatalf("items = %d, want 1: %v", len(items), titles(items))
	}

	if items[0].Title != "The Daily Show 2024-03-25 720p" {
		t.Fatalf("title = %q", items[0].Title)
	}
}

func TestEndpointPaging(t *testing.T) {
	// The TV rows are, newest first: the episode, the season pack, the daily
	// episode.
	want := []string{
		"The Show S01E02 1080p WEB-DL",
		"The Show S01 Complete 1080p",
		"The Daily Show 2024-03-25 720p",
	}

	h := seedHandler(t)

	for offset, title := range want {
		query := "t=search&cat=5000&limit=1&offset=" + strconv.Itoa(offset)
		items := decodeFeed(t, get(t, h, query).Body.Bytes()).Channel.Items

		if len(items) != 1 {
			t.Fatalf("offset %d: items = %d, want 1", offset, len(items))
		}

		if items[0].Title != title {
			t.Fatalf("offset %d: title = %q, want %q", offset, items[0].Title, title)
		}
	}
}

func TestEndpointNoMatch(t *testing.T) {
	items := decodeFeed(t, get(t, seedHandler(t), "t=search&q=zzzznotfound").Body.Bytes()).Channel.Items

	if len(items) != 0 {
		t.Fatalf("items = %d, want 0: %v", len(items), titles(items))
	}
}

// TestFeedConformance checks the wire format a client parses: namespaces,
// required elements and the attributes Sonarr and Prowlarr read.
func TestFeedConformance(t *testing.T) {
	rec := get(t, seedHandler(t), "t=search")

	raw := string(rec.Body.Bytes())

	for _, want := range []string{
		`xmlns:atom="` + atomNamespace + `"`,
		`xmlns:torznab="` + torznabNamespace + `"`,
		newznabNamespace,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("feed is missing %q", want)
		}
	}

	feed := decodeFeed(t, rec.Body.Bytes())

	if feed.XMLName.Local != "rss" || feed.Version != "2.0" {
		t.Fatalf("root = %s version %q, want rss 2.0", feed.XMLName.Local, feed.Version)
	}

	if len(feed.Channel.Items) != 5 {
		t.Fatalf("items = %d, want 5", len(feed.Channel.Items))
	}

	capsIDs := advertisedCategoryIDs(t)

	for _, it := range feed.Channel.Items {
		if it.Title == "" {
			t.Error("item has no title")
		}

		if len(it.GUID) != 40 {
			t.Errorf("guid %q is not a hex infohash", it.GUID)
		} else if _, err := hex.DecodeString(it.GUID); err != nil {
			t.Errorf("guid %q is not hex: %v", it.GUID, err)
		}

		if !capsIDs[it.Category] {
			t.Errorf("item category %q is not advertised in caps", it.Category)
		}

		if it.Size <= 0 {
			t.Errorf("item %q has size %d", it.Title, it.Size)
		}

		if !strings.HasPrefix(it.Enclosure.URL, "magnet:?xt=urn:btih:"+it.GUID) {
			t.Errorf("enclosure url %q does not name %q", it.Enclosure.URL, it.GUID)
		}

		if it.Enclosure.Type != magnetMIMEType {
			t.Errorf("enclosure type = %q, want %q", it.Enclosure.Type, magnetMIMEType)
		}

		if it.Enclosure.Length != it.Size {
			t.Errorf("enclosure length = %d, want size %d", it.Enclosure.Length, it.Size)
		}

		if _, err := time.Parse(rssDateLayout, it.PubDate); err != nil {
			t.Errorf("pubDate %q is not parseable: %v", it.PubDate, err)
		}

		attrs := attributes(it)

		for _, name := range []string{"infohash", "magneturl", "category", "size", "seeders", "leechers", "peers", "publishdate"} {
			if attrs[name] == "" {
				t.Errorf("item %q has no %s attribute", it.Title, name)
			}
		}

		if want := testSiteURL + "/torrent/" + it.GUID; it.Comments != want {
			t.Errorf("comments = %q, want %q", it.Comments, want)
		}

		if attrs["infohash"] != it.GUID {
			t.Errorf("infohash attribute = %q, want guid %q", attrs["infohash"], it.GUID)
		}

		if attrs["magneturl"] != it.Enclosure.URL {
			t.Errorf("magneturl attribute = %q, want enclosure url %q", attrs["magneturl"], it.Enclosure.URL)
		}

		if attrs["category"] != it.Category {
			t.Errorf("category attribute = %q, want %q", attrs["category"], it.Category)
		}
	}
}

func advertisedCategoryIDs(t *testing.T) map[string]bool {
	t.Helper()

	caps := decodeCaps(t, get(t, newTestHandler(t, Config{}), "t=caps").Body.Bytes())
	out := make(map[string]bool)

	for _, category := range caps.Categories.Categories {
		out[strconv.Itoa(category.ID)] = true

		for _, sub := range category.Sub {
			out[strconv.Itoa(sub.ID)] = true
		}
	}

	return out
}
