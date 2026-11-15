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
			ID:   classify.CategoryConsole,
			Name: classify.CategoryName(classify.CategoryConsole),
			Sub: []capsCategory{
				{ID: classify.CategoryConsoleNDS, Name: classify.CategoryName(classify.CategoryConsoleNDS)},
				{ID: classify.CategoryConsolePSP, Name: classify.CategoryName(classify.CategoryConsolePSP)},
				{ID: classify.CategoryConsoleWii, Name: classify.CategoryName(classify.CategoryConsoleWii)},
				{ID: classify.CategoryConsoleXbox, Name: classify.CategoryName(classify.CategoryConsoleXbox)},
				{ID: classify.CategoryConsoleXbox360, Name: classify.CategoryName(classify.CategoryConsoleXbox360)},
				{ID: classify.CategoryConsolePS3, Name: classify.CategoryName(classify.CategoryConsolePS3)},
				{ID: classify.CategoryConsoleOther, Name: classify.CategoryName(classify.CategoryConsoleOther)},
				{ID: classify.CategoryConsole3DS, Name: classify.CategoryName(classify.CategoryConsole3DS)},
				{ID: classify.CategoryConsoleVita, Name: classify.CategoryName(classify.CategoryConsoleVita)},
				{ID: classify.CategoryConsoleWiiU, Name: classify.CategoryName(classify.CategoryConsoleWiiU)},
				{ID: classify.CategoryConsoleXboxOne, Name: classify.CategoryName(classify.CategoryConsoleXboxOne)},
				{ID: classify.CategoryConsolePS4, Name: classify.CategoryName(classify.CategoryConsolePS4)},
			},
		},
		{
			ID:   classify.CategoryMovies,
			Name: classify.CategoryName(classify.CategoryMovies),
			Sub: []capsCategory{
				{ID: classify.CategoryMoviesSD, Name: classify.CategoryName(classify.CategoryMoviesSD)},
				{ID: classify.CategoryMoviesHD, Name: classify.CategoryName(classify.CategoryMoviesHD)},
				{ID: classify.CategoryMoviesUHD, Name: classify.CategoryName(classify.CategoryMoviesUHD)},
				{ID: classify.CategoryMoviesBluRay, Name: classify.CategoryName(classify.CategoryMoviesBluRay)},
				{ID: classify.CategoryMovies3D, Name: classify.CategoryName(classify.CategoryMovies3D)},
				{ID: classify.CategoryMoviesDVD, Name: classify.CategoryName(classify.CategoryMoviesDVD)},
				{ID: classify.CategoryMoviesWEB, Name: classify.CategoryName(classify.CategoryMoviesWEB)},
				{ID: classify.CategoryMoviesx265, Name: classify.CategoryName(classify.CategoryMoviesx265)},
			},
		},
		{
			ID:   classify.CategoryAudio,
			Name: classify.CategoryName(classify.CategoryAudio),
			Sub: []capsCategory{
				{ID: classify.CategoryAudioMP3, Name: classify.CategoryName(classify.CategoryAudioMP3)},
				{ID: classify.CategoryAudiobook, Name: classify.CategoryName(classify.CategoryAudiobook)},
				{ID: classify.CategoryAudioLossless, Name: classify.CategoryName(classify.CategoryAudioLossless)},
				{ID: classify.CategoryAudioOther, Name: classify.CategoryName(classify.CategoryAudioOther)},
			},
		},
		{
			ID:   classify.CategoryPC,
			Name: classify.CategoryName(classify.CategoryPC),
			Sub: []capsCategory{
				{ID: classify.CategoryPCISO, Name: classify.CategoryName(classify.CategoryPCISO)},
				{ID: classify.CategoryPCMac, Name: classify.CategoryName(classify.CategoryPCMac)},
				{ID: classify.CategoryPCGames, Name: classify.CategoryName(classify.CategoryPCGames)},
				{ID: classify.CategoryPCiOS, Name: classify.CategoryName(classify.CategoryPCiOS)},
				{ID: classify.CategoryPCAndroid, Name: classify.CategoryName(classify.CategoryPCAndroid)},
			},
		},
		{
			ID:   classify.CategoryTV,
			Name: classify.CategoryName(classify.CategoryTV),
			Sub: []capsCategory{
				{ID: classify.CategoryTVWEB, Name: classify.CategoryName(classify.CategoryTVWEB)},
				{ID: classify.CategoryTVSD, Name: classify.CategoryName(classify.CategoryTVSD)},
				{ID: classify.CategoryTVHD, Name: classify.CategoryName(classify.CategoryTVHD)},
				{ID: classify.CategoryTVUHD, Name: classify.CategoryName(classify.CategoryTVUHD)},
				{ID: classify.CategoryTVx265, Name: classify.CategoryName(classify.CategoryTVx265)},
			},
		},
		{
			ID:   classify.CategoryXXX,
			Name: classify.CategoryName(classify.CategoryXXX),
			Sub: []capsCategory{
				{ID: classify.CategoryXXXDVD, Name: classify.CategoryName(classify.CategoryXXXDVD)},
				{ID: classify.CategoryXXXWMV, Name: classify.CategoryName(classify.CategoryXXXWMV)},
				{ID: classify.CategoryXXXXviD, Name: classify.CategoryName(classify.CategoryXXXXviD)},
				{ID: classify.CategoryXXXx264, Name: classify.CategoryName(classify.CategoryXXXx264)},
				{ID: classify.CategoryXXXUHD, Name: classify.CategoryName(classify.CategoryXXXUHD)},
				{ID: classify.CategoryXXXImageSet, Name: classify.CategoryName(classify.CategoryXXXImageSet)},
				{ID: classify.CategoryXXXSD, Name: classify.CategoryName(classify.CategoryXXXSD)},
				{ID: classify.CategoryXXXWEB, Name: classify.CategoryName(classify.CategoryXXXWEB)},
			},
		},
		{
			ID:   classify.CategoryBooks,
			Name: classify.CategoryName(classify.CategoryBooks),
			Sub: []capsCategory{
				{ID: classify.CategoryBooksMags, Name: classify.CategoryName(classify.CategoryBooksMags)},
				{ID: classify.CategoryEbook, Name: classify.CategoryName(classify.CategoryEbook)},
				{ID: classify.CategoryComics, Name: classify.CategoryName(classify.CategoryComics)},
				{ID: classify.CategoryBooksTechnical, Name: classify.CategoryName(classify.CategoryBooksTechnical)},
			},
		},
		{
			ID:   classify.CategoryOther,
			Name: classify.CategoryName(classify.CategoryOther),
		},
	}
}
