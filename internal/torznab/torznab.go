package torznab

import "fmt"

// Function is the value of the t parameter.
type Function string

// The request functions. caps is the capability document; the rest are
// searches and differ in the filters they accept.
const (
	FunctionCaps   Function = "caps"
	FunctionSearch Function = "search"
	FunctionTV     Function = "tvsearch"
	FunctionMovie  Function = "movie"
	FunctionMusic  Function = "music"
	FunctionBook   Function = "book"
)

// Query parameter names. This is the subset of the Torznab API the endpoint
// reads; unknown parameters are ignored.
const (
	ParamFunction = "t"
	ParamQuery    = "q"
	ParamCategory = "cat"
	ParamSeason   = "season"
	ParamEpisode  = "ep"
	ParamTMDBID   = "tmdbid"
	ParamLimit    = "limit"
	ParamOffset   = "offset"
	ParamAPIKey   = "apikey"
)

// Newznab error codes. A client reads an error element in a successful response
// and treats 100-199 as an authentication failure.
const (
	CodeInvalidKey         = 100
	CodeMissingParameter   = 200
	CodeIncorrectParameter = 201
	CodeNoSuchFunction     = 202
)

// Error is a Newznab error response. It is rendered as the error element with
// HTTP 200, because the clients look for it in the body.
type Error struct {
	Code        int
	Description string
}

func (e Error) Error() string {
	return fmt.Sprintf("torznab error %d: %s", e.Code, e.Description)
}

// errorDocument is the wire form of Error.
type errorDocument struct {
	XMLName     struct{} `xml:"error"`
	Code        int      `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}
