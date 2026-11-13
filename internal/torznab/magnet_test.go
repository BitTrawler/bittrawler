package torznab

import (
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
)

func TestMagnetFormat(t *testing.T) {
	hash := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a,
		0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}

	got := Magnet(hash, "Ubuntu 24.04 Desktop amd64", 6_500_000_000)
	want := "magnet:?xt=urn:btih:" + hex.EncodeToString(hash[:]) +
		"&dn=Ubuntu%2024.04%20Desktop%20amd64&xl=6500000000"

	if got != want {
		t.Fatalf("Magnet() = %q, want %q", got, want)
	}
}

func TestMagnetOmitsUnknownSize(t *testing.T) {
	got := Magnet([20]byte{0xaa}, "release", 0)

	if strings.Contains(got, "&xl=") {
		t.Fatalf("Magnet() = %q, want no xl parameter for size 0", got)
	}
}

func TestMagnetNameRoundTrips(t *testing.T) {
	cases := []string{
		"A Release With Spaces",
		"Show.S01E02.1080p.WEB-DL",
		"Amélie (2001) [1080p]",
		"Batman 蝙蝠侠",
		"weird&name+with=reserved?chars",
	}

	for _, name := range cases {
		uri := Magnet([20]byte{0x01}, name, 1024)

		query, err := url.ParseQuery(strings.TrimPrefix(uri, "magnet:?"))
		if err != nil {
			t.Fatalf("Magnet(%q) is not a valid query: %v", name, err)
		}

		if got := query.Get("dn"); got != name {
			t.Errorf("dn for %q = %q", name, got)
		}

		if got := query.Get("xt"); got != "urn:btih:"+hex.EncodeToString([]byte{0x01, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
			t.Errorf("xt for %q = %q", name, got)
		}
	}
}

func TestMagnetUsesPercentEncodingForSpaces(t *testing.T) {
	got := Magnet([20]byte{}, "two words", 0)

	// A "+" in a URI is a literal plus, so spaces must be %20.
	if strings.Contains(got, "+") {
		t.Fatalf("Magnet() = %q, want spaces escaped as %%20", got)
	}
}
