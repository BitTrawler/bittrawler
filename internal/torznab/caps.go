package torznab

import (
	"encoding/xml"
	"strings"

	"github.com/BitTrawler/bittrawler/internal/classify"
)

// caps are the capabilities the endpoint advertises: the search functions it
// answers and the categories it can return.
type caps struct {
	XMLName    xml.Name       `xml:"caps"`
	Server     capsServer     `xml:"server"`
	Limits     capsLimits     `xml:"limits"`
	Searching  capsSearching  `xml:"searching"`
	Categories capsCategories `xml:"categories"`
}

type capsServer struct {
	Title string `xml:"title,attr"`
}

type capsLimits struct {
	Max     int `xml:"max,attr"`
	Default int `xml:"default,attr"`
}

type capsSearch struct {
	Available       string `xml:"available,attr"`
	SupportedParams string `xml:"supportedParams,attr,omitempty"`
}

type capsSearching struct {
	Search      capsSearch `xml:"search"`
	TVSearch    capsSearch `xml:"tv-search"`
	MovieSearch capsSearch `xml:"movie-search"`
	MusicSearch capsSearch `xml:"music-search"`
	BookSearch  capsSearch `xml:"book-search"`
	AudioSearch capsSearch `xml:"audio-search"`
}

type capsCategories struct {
	Categories []capsCategory `xml:"category"`
}

type capsCategory struct {
	ID   int            `xml:"id,attr"`
	Name string         `xml:"name,attr"`
	Sub  []capsCategory `xml:"subcat,omitempty"`
}

// searchParams is the single source of truth for the supportedParams attribute:
// the parameters each search function implements, spelled as the Torznab
// specification spells them. The handler parses all of them for every
// function; season and ep describe an episode and tmdbid selects a work the
// TMDB stage identified, so they are advertised only where they mean
// something. cat, limit and offset are the generic search parameters every
// function honours. The caps test checks each advertised parameter changes the
// store query, so the advert cannot promise a filter the search ignores.
var searchParams = map[Function][]string{
	FunctionSearch: {ParamQuery, ParamCategory, ParamLimit, ParamOffset},
	FunctionTV:     {ParamQuery, ParamSeason, ParamEpisode, ParamTMDBID, ParamCategory, ParamLimit, ParamOffset},
	FunctionMovie:  {ParamQuery, ParamTMDBID, ParamCategory, ParamLimit, ParamOffset},
	FunctionMusic:  {ParamQuery, ParamCategory, ParamLimit, ParamOffset},
	FunctionBook:   {ParamQuery, ParamCategory, ParamLimit, ParamOffset},
}

// supportedParamsFor renders the supportedParams attribute for a search
// function.
func supportedParamsFor(function Function) string {
	return strings.Join(searchParams[function], ",")
}

// searchCaps renders one searching element for a function the endpoint
// answers.
func searchCaps(function Function) capsSearch {
	return capsSearch{Available: "yes", SupportedParams: supportedParamsFor(function)}
}

// capsFor builds the capability document. Only the categories the classifier
// stores are advertised, so a client maps exactly what it can receive.
func capsFor(cfg Config) caps {
	return caps{
		Server: capsServer{Title: cfg.Title},
		Limits: capsLimits{Max: cfg.MaxLimit, Default: cfg.DefaultLimit},
		Searching: capsSearching{
			Search:      searchCaps(FunctionSearch),
			TVSearch:    searchCaps(FunctionTV),
			MovieSearch: searchCaps(FunctionMovie),
			MusicSearch: searchCaps(FunctionMusic),
			BookSearch:  searchCaps(FunctionBook),
			// The endpoint answers no audio function; t=music covers the audio
			// categories it serves.
			AudioSearch: capsSearch{Available: "no"},
		},
		Categories: capsCategories{Categories: capsCategoryTree()},
	}
}

// capsCategoryTree is the Newznab category tree, limited to categories the
// classifier stores.
func capsCategoryTree() []capsCategory {
	return []capsCategory{
		{
			ID:   classify.CategoryMovies,
			Name: classify.CategoryName(classify.CategoryMovies),
			Sub: []capsCategory{
				{ID: classify.CategoryMoviesSD, Name: classify.CategoryName(classify.CategoryMoviesSD)},
				{ID: classify.CategoryMoviesHD, Name: classify.CategoryName(classify.CategoryMoviesHD)},
				{ID: classify.CategoryMoviesUHD, Name: classify.CategoryName(classify.CategoryMoviesUHD)},
			},
		},
		{
			ID:   classify.CategoryAudio,
			Name: classify.CategoryName(classify.CategoryAudio),
			Sub: []capsCategory{
				{ID: classify.CategoryAudiobook, Name: classify.CategoryName(classify.CategoryAudiobook)},
			},
		},
		{
			ID:   classify.CategoryPC,
			Name: classify.CategoryName(classify.CategoryPC),
			Sub: []capsCategory{
				{ID: classify.CategoryPCGames, Name: classify.CategoryName(classify.CategoryPCGames)},
			},
		},
		{
			ID:   classify.CategoryTV,
			Name: classify.CategoryName(classify.CategoryTV),
			Sub: []capsCategory{
				{ID: classify.CategoryTVSD, Name: classify.CategoryName(classify.CategoryTVSD)},
				{ID: classify.CategoryTVHD, Name: classify.CategoryName(classify.CategoryTVHD)},
				{ID: classify.CategoryTVUHD, Name: classify.CategoryName(classify.CategoryTVUHD)},
			},
		},
		{
			ID:   classify.CategoryXXX,
			Name: classify.CategoryName(classify.CategoryXXX),
		},
		{
			ID:   classify.CategoryBooks,
			Name: classify.CategoryName(classify.CategoryBooks),
			Sub: []capsCategory{
				{ID: classify.CategoryEbook, Name: classify.CategoryName(classify.CategoryEbook)},
				{ID: classify.CategoryComics, Name: classify.CategoryName(classify.CategoryComics)},
			},
		},
		{
			ID:   classify.CategoryOther,
			Name: classify.CategoryName(classify.CategoryOther),
		},
	}
}
