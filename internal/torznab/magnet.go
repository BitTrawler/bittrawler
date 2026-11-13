package torznab

import (
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
)

// Magnet returns the magnet URI for a torrent. The DHT is the only source of
// peers, so the URI names no tracker: the client fetches the metadata from the
// network.
func Magnet(hash [20]byte, name string, size int64) string {
	var b strings.Builder

	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(hex.EncodeToString(hash[:]))
	b.WriteString("&dn=")
	b.WriteString(escapeName(name))

	if size > 0 {
		b.WriteString("&xl=")
		b.WriteString(strconv.FormatInt(size, 10))
	}

	return b.String()
}

// escapeName percent-encodes a display name for the dn parameter.
// url.QueryEscape turns spaces into "+", which a URI parser does not read back
// as a space, so those are turned into %20 afterwards.
func escapeName(name string) string {
	return strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
}
