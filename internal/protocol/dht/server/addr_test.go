package server

import (
	"net/netip"
	"testing"
)

func TestUnmapAddrPort(t *testing.T) {
	mapped := netip.AddrPortFrom(netip.MustParseAddr("::ffff:192.0.2.1"), 6881)
	unmapped := netip.AddrPortFrom(netip.MustParseAddr("192.0.2.1"), 6881)

	if got := unmapAddrPort(mapped); got != unmapped {
		t.Fatalf("mapped address = %s, want %s", got, unmapped)
	}

	if got := unmapAddrPort(unmapped); got != unmapped {
		t.Fatalf("plain address changed to %s", got)
	}

	ipv6 := netip.AddrPortFrom(netip.MustParseAddr("2001:db8::1"), 6881)
	if got := unmapAddrPort(ipv6); got != ipv6 {
		t.Fatalf("IPv6 address changed to %s", got)
	}
}
