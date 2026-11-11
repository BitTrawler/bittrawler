package metainfo_test

import (
	"testing"

	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo"
	"github.com/anacrolix/torrent/bencode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadTorrentFile(t *testing.T) {
	t.Parallel()

	input, err := bencode.Marshal(metainfo.TorrentFile{
		Info:         metainfo.Info{Name: "example", PieceLength: 16384, Length: 1024},
		Announce:     "https://tracker.example/announce",
		AnnounceList: [][]string{{"https://tracker.example/announce"}},
		Comment:      "synthetic fixture",
	})
	require.NoError(t, err)

	torrentFile, err := metainfo.ReadTorrentFileBytes(input)
	require.NoError(t, err)

	assert.Equal(t, "example", torrentFile.Info.BestName())
	assert.Equal(t, "https://tracker.example/announce", torrentFile.Announce)
	assert.Equal(t, "synthetic fixture", torrentFile.Comment)
}
