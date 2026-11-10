package dht

import (
	"bytes"
	"testing"

	"github.com/anacrolix/torrent/bencode"
)

// The decoders here are fed bytes from any host on the internet. These targets
// exist to keep them from panicking: a panic in the DHT listener kills the
// process, and that is reachable before any peer is authenticated.

func FuzzNodeAddrUnmarshalBinary(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 0, 80})
	f.Add([]byte{0, 0, 0, 0, 0, 0})
	f.Add(make([]byte, 18))
	f.Add([]byte{})
	f.Add([]byte{1, 2, 3, 4, 5})
	f.Add([]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 80})

	f.Fuzz(func(t *testing.T, b []byte) {
		var a NodeAddr

		if err := a.UnmarshalBinary(b); err != nil {
			if len(b) == 6 || len(b) == 18 {
				t.Fatalf("valid length %d rejected: %v", len(b), err)
			}

			return
		}

		if len(b) != 6 && len(b) != 18 {
			t.Fatalf("accepted %d bytes", len(b))
		}

		enc, err := a.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal after decode: %v", err)
		}

		if !bytes.Equal(enc, b) {
			t.Fatalf("round trip: %x -> %x", b, enc)
		}
	})
}

func FuzzNodeInfoUnmarshalBinary(f *testing.F) {
	f.Add(make([]byte, 26))
	f.Add(make([]byte, 38))
	f.Add(make([]byte, 25))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, b []byte) {
		var ni NodeInfo

		if err := ni.UnmarshalBinary(b); err != nil {
			return
		}

		if len(b) != 26 && len(b) != 38 {
			t.Fatalf("accepted %d bytes", len(b))
		}

		enc, err := ni.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal after decode: %v", err)
		}

		if !bytes.Equal(enc, b) {
			t.Fatalf("round trip: %x -> %x", b, enc)
		}
	})
}

func FuzzCompactIPv4NodeInfoUnmarshalBinary(f *testing.F) {
	f.Add(make([]byte, 26))
	f.Add(make([]byte, 52))
	f.Add(make([]byte, 25))
	f.Add([]byte("d26:x"))

	f.Fuzz(func(t *testing.T, b []byte) {
		var ni CompactIPv4NodeInfo

		if err := ni.UnmarshalBencode(b); err != nil {
			return
		}

		if len(b) == 0 {
			return
		}

		// UnmarshalBencode wraps the bytes in a bencode string, so anything
		// it accepts must be a whole number of elements.
		enc, err := ni.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal after decode: %v", err)
		}

		if len(enc) != len(ni)*26 {
			t.Fatalf("serialised %d bytes for %d elements", len(enc), len(ni))
		}
	})
}

func FuzzMsgUnmarshal(f *testing.F) {
	f.Add([]byte("d1:ad2:id20:abcdefghij0123456789e1:q9:get_peers1:t2:aa1:y1:qe"))
	f.Add([]byte("d1:rd2:id20:01234567890123456789e1:t1:c1:y1:re"))
	f.Add([]byte("d1:eli201e4:fail1:t1:a1:y1:ee"))
	f.Add([]byte("d2:ip6:1234561:rd2:id20:01234567890123456789e1:t1:a1:y1:re"))
	f.Add([]byte("de"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, b []byte) {
		var m Msg

		if err := bencode.Unmarshal(b, &m); err != nil {
			return
		}

		// Re-encoding what was decoded must not panic either.
		if _, err := bencode.Marshal(m); err != nil {
			return
		}
	})
}
