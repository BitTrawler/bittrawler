// Package torznab serves the index as a Torznab endpoint: an RSS feed with the
// torznab attributes that Prowlarr, Sonarr and Radarr read.
//
// The endpoint is one HTTP handler. It answers t=caps, turns the search
// functions into store queries and renders one page of results. Releases carry
// a magnet URI rather than a .torrent file, because the store keeps no piece
// hashes and cannot rebuild a torrent from a row.
package torznab
