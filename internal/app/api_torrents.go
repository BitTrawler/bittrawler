package app

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/store"
)

const (
	// torrentsAPIDefaultLimit is the page size when the request does not name
	// one.
	torrentsAPIDefaultLimit = 100
	// torrentsAPIMaxLimit caps a page, so one request cannot ask the store for
	// an unbounded result.
	torrentsAPIMaxLimit = 500
)

// handleTorrentsAPI serves GET /api/torrents: paged, read-only rows from the
// index. It exists for rule work and monitoring: the classifier stores anything
// it cannot identify as Other, so `?cat=8000` returns the rows worth looking at.
// Authentication is the Torznab API key, like the statistics endpoint.
func (a *App) handleTorrentsAPI(w http.ResponseWriter, r *http.Request) {
	if !a.Configured() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not set up"})

		return
	}

	if !a.statsKeyValid(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid API key"})

		return
	}

	query, limit, offset, err := parseTorrentsAPIQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})

		return
	}

	result, err := a.torrentStore.Search(r.Context(), query)
	if err != nil {
		a.logger.Error("read torrents for the API", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read the index"})

		return
	}

	rows := make([]torrentsAPIRow, 0, len(result.Torrents))

	for _, torrent := range result.Torrents {
		rows = append(rows, torrentsAPIRow{
			Hash:        hex.EncodeToString(torrent.InfoHash[:]),
			Name:        torrent.Name,
			CategoryID:  torrent.Category,
			Category:    classify.CategoryName(torrent.Category),
			Size:        torrent.Size,
			Files:       torrent.FilesCount,
			PrimaryFile: torrent.PrimaryFile,
			Seeders:     torrent.Seeders,
			Leechers:    torrent.Leechers,
			CreatedAt:   torrent.CreatedAt.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, torrentsAPIResponse{
		Total:  result.Total,
		Limit:  limit,
		Offset: offset,
		Rows:   rows,
	})
}

// parseTorrentsAPIQuery turns the request's parameters into a store query. The
// category filter accepts a leaf or a parent, matching the search page.
func parseTorrentsAPIQuery(r *http.Request) (store.Query, int, int, error) {
	values := r.URL.Query()

	query := store.NewQuery()
	query.Text = strings.TrimSpace(values.Get("q"))
	query.Sort = store.SortNewest
	query.Count = true

	limit := torrentsAPIDefaultLimit

	if raw := values.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return store.Query{}, 0, 0, errors.New("limit must be a positive number")
		}

		limit = min(parsed, torrentsAPIMaxLimit)
	}

	offset := 0

	if raw := values.Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return store.Query{}, 0, 0, errors.New("offset must not be negative")
		}

		offset = parsed
	}

	if raw := values.Get("cat"); raw != "" {
		category, err := strconv.Atoi(raw)
		if err != nil || category < 0 {
			return store.Query{}, 0, 0, errors.New("cat must be a Newznab category number")
		}

		query.Categories = []int{category}
	}

	if values.Get("sort") == "relevance" && query.Text != "" {
		query.Sort = store.SortRelevance
	}

	query.Limit = limit
	query.Offset = offset

	return query, limit, offset, nil
}

// torrentsAPIResponse is the body of GET /api/torrents.
type torrentsAPIResponse struct {
	Total  int              `json:"total"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
	Rows   []torrentsAPIRow `json:"rows"`
}

// torrentsAPIRow is one torrent in the response. The magnet link can be built
// from the hash, so it is not repeated here.
type torrentsAPIRow struct {
	Hash        string `json:"hash"`
	Name        string `json:"name"`
	CategoryID  int    `json:"category_id"`
	Category    string `json:"category"`
	Size        int64  `json:"size"`
	Files       int    `json:"files"`
	PrimaryFile string `json:"primary_file"`
	Seeders     int    `json:"seeders"`
	Leechers    int    `json:"leechers"`
	CreatedAt   int64  `json:"created_at"`
}
