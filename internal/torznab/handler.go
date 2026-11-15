package torznab

import (
	"context"
	"crypto/subtle"
	"encoding/xml"
	"errors"
	"net/http"
	"strings"

	"github.com/BitTrawler/bittrawler/internal/classify"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// Searcher runs the store query the endpoint needs.
type Searcher interface {
	Search(ctx context.Context, q store.Query) (store.Result, error)
}

// Config configures the endpoint.
type Config struct {
	// Title is the feed and caps server title.
	Title string
	// APIKey, when set, must be sent as the apikey parameter or the X-Api-Key
	// header. Empty leaves the endpoint open.
	APIKey string
	// DefaultLimit is the page size when the request does not ask for one.
	DefaultLimit int
	// MaxLimit is the largest page the endpoint will serve.
	MaxLimit int
	// Searcher is required.
	Searcher Searcher
}

// NewHandler returns the Torznab endpoint handler.
func NewHandler(cfg Config) (http.Handler, error) {
	if cfg.Searcher == nil {
		return nil, errors.New("torznab: Config.Searcher is required")
	}

	if cfg.Title == "" {
		cfg.Title = "BitTrawler"
	}

	if cfg.DefaultLimit <= 0 {
		cfg.DefaultLimit = 100
	}

	if cfg.MaxLimit <= 0 {
		cfg.MaxLimit = cfg.DefaultLimit
	}

	if cfg.DefaultLimit > cfg.MaxLimit {
		cfg.DefaultLimit = cfg.MaxLimit
	}

	return &handler{cfg: cfg}, nil
}

type handler struct {
	cfg Config
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "torznab: method not allowed", http.StatusMethodNotAllowed)

		return
	}

	req, err := ParseRequest(r, h.cfg.DefaultLimit, h.cfg.MaxLimit)
	if err != nil {
		h.writeError(w, err)

		return
	}

	if !h.authorised(req.APIKey) {
		h.writeError(w, Error{Code: CodeInvalidKey, Description: "invalid API key"})

		return
	}

	switch req.Function {
	case FunctionCaps:
		h.writeXML(w, capsFor(h.cfg))
	case FunctionSearch, FunctionTV, FunctionMovie, FunctionMusic, FunctionBook:
		h.search(w, r, req)
	default:
		h.writeError(w, Error{
			Code:        CodeNoSuchFunction,
			Description: "no such function (" + string(req.Function) + ")",
		})
	}
}

func (h *handler) authorised(key string) bool {
	if h.cfg.APIKey == "" {
		return true
	}

	return subtle.ConstantTimeCompare([]byte(key), []byte(h.cfg.APIKey)) == 1
}

// search turns the request into a store query and renders the results.
func (h *handler) search(w http.ResponseWriter, r *http.Request, req SearchRequest) {
	query := store.NewQuery()
	query.Text = req.Query
	query.Categories = categoriesFor(req)
	query.Limit = req.Limit
	query.Offset = req.Offset

	if req.AirDate != "" {
		query.DailyDate = req.AirDate
	} else {
		if req.Season != unset {
			query.Season = req.Season
		}

		if req.Episode != unset {
			query.Episode = req.Episode
		}
	}

	if req.TMDBID > 0 {
		query.ContentID = req.TMDBID
	}

	// A season search without an episode asks for the season, and the pack is
	// the row that answers it. Packs sort ahead of the episodes.
	if req.Season != unset && req.Episode == unset && req.AirDate == "" {
		query.PacksFirst = true
	}

	// Relevance only means something when there is text to rank; without it
	// the newest releases are the useful page.
	if strings.TrimSpace(req.Query) != "" {
		query.Sort = store.SortRelevance
	}

	result, err := h.cfg.Searcher.Search(r.Context(), query)
	if err != nil {
		http.Error(w, "torznab: search failed", http.StatusInternalServerError)

		return
	}

	h.writeXML(w, buildFeed(req, h.cfg.Title, result.Torrents))
}

// categoriesFor narrows a search function to its top-level category when the
// request names none. A client that asks for movies without a category still
// expects films, not the whole index.
func categoriesFor(req SearchRequest) []int {
	if len(req.Categories) > 0 {
		return req.Categories
	}

	switch req.Function {
	case FunctionMovie:
		return []int{classify.CategoryMovies}
	case FunctionTV:
		return []int{classify.CategoryTV}
	case FunctionMusic:
		return []int{classify.CategoryAudio}
	case FunctionBook:
		return []int{classify.CategoryBooks, classify.CategoryAudiobook}
	default:
		return nil
	}
}

// writeXML writes a document with HTTP 200, which is also how a Newznab error
// is delivered.
func (h *handler) writeXML(w http.ResponseWriter, document any) {
	body, err := xml.Marshal(document)
	if err != nil {
		http.Error(w, "torznab: encode response", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

// writeError renders a Newznab error document, or an HTTP error for a failure
// that is not part of the protocol.
func (h *handler) writeError(w http.ResponseWriter, err error) {
	var protocolError Error
	if errors.As(err, &protocolError) {
		h.writeXML(w, errorDocument{
			Code:        protocolError.Code,
			Description: protocolError.Description,
		})

		return
	}

	http.Error(w, "torznab: internal error", http.StatusInternalServerError)
}
