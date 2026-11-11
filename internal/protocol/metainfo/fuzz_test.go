package metainfo

import (
	"testing"

	"github.com/BitTrawler/bittrawler/internal/protocol"
)

// Both parsers are fed peer-supplied bytes; neither may panic.
func FuzzParseMetaInfoBytes(f *testing.F) {
	f.Add([]byte("d4:name4:test6:lengthi100eee"))
	f.Add([]byte("de"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, infoBytes []byte) {
		if _, err := ParseMetaInfoBytes(protocol.ID{}, infoBytes); err != nil {
			return
		}
	})
}

func FuzzReadTorrentFileBytes(f *testing.F) {
	f.Add([]byte("d4:infod4:name4:test6:lengthi100eeee"))
	f.Add([]byte("de"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ReadTorrentFileBytes(data)
	})
}
