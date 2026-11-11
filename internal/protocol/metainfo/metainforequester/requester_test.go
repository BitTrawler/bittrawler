package metainforequester

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/anacrolix/torrent/bencode"
)

// extFrame builds a BEP 10 message: 4-byte length, then the extension id and
// the ut_metadata message id, then the payload.
func extFrame(extID byte, payload []byte) []byte {
	body := make([]byte, 0, 2+len(payload))
	body = append(body, 20, extID)
	body = append(body, payload...)

	frame := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(body)))
	copy(frame[4:], body)

	return frame
}

func dataMessage(t *testing.T, piece int, data []byte) []byte {
	t.Helper()

	dict, err := bencode.Marshal(extDict{MsgType: 1, Piece: piece})
	if err != nil {
		t.Fatal(err)
	}

	return extFrame(1, append(dict, data...))
}

func requireError(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestReadAllPiecesHappyPath(t *testing.T) {
	first := bytes.Repeat([]byte("a"), metadataPieceSize)
	last := bytes.Repeat([]byte("b"), 4096)
	metadataSize := uint(len(first) + len(last))

	var stream bytes.Buffer

	stream.Write(dataMessage(t, 0, first))
	stream.Write(dataMessage(t, 1, last))

	got, err := readAllPieces(&stream, metadataSize)
	if err != nil {
		t.Fatalf("readAllPieces: %v", err)
	}

	if !bytes.Equal(got, append(append([]byte{}, first...), last...)) {
		t.Fatal("metadata bytes differ")
	}
}

// The piece index is chosen by the peer. Before the fix it was used as an
// unchecked offset and this case panicked.
func TestReadAllPiecesRejectsOutOfRangePiece(t *testing.T) {
	metadataSize := uint(4096)

	stream := bytes.NewBuffer(dataMessage(t, 99, bytes.Repeat([]byte("a"), int(metadataSize))))

	_, err := readAllPieces(stream, metadataSize)
	requireError(t, err)
}

func TestReadAllPiecesRejectsNegativePiece(t *testing.T) {
	metadataSize := uint(4096)

	stream := bytes.NewBuffer(dataMessage(t, -1, bytes.Repeat([]byte("a"), int(metadataSize))))

	_, err := readAllPieces(stream, metadataSize)
	requireError(t, err)
}

func TestReadAllPiecesRejectsDuplicatePiece(t *testing.T) {
	piece := bytes.Repeat([]byte("a"), metadataPieceSize)
	last := bytes.Repeat([]byte("b"), 4096)
	metadataSize := uint(len(piece) + len(last))

	var stream bytes.Buffer

	stream.Write(dataMessage(t, 0, piece))
	stream.Write(dataMessage(t, 0, piece))

	_, err := readAllPieces(&stream, metadataSize)
	requireError(t, err)
}

func TestReadAllPiecesRejectsShortPiece(t *testing.T) {
	metadataSize := uint(2 * metadataPieceSize)

	stream := bytes.NewBuffer(dataMessage(t, 0, bytes.Repeat([]byte("a"), 100)))

	_, err := readAllPieces(stream, metadataSize)
	requireError(t, err)
}

func TestReadAllPiecesRejectsZeroSize(t *testing.T) {
	_, err := readAllPieces(bytes.NewReader(nil), 0)
	requireError(t, err)
}

func TestReadMessageRejectsOversizedMessage(t *testing.T) {
	var stream bytes.Buffer

	var length [4]byte
	binary.BigEndian.PutUint32(length[:], maxMessageSize+1)
	stream.Write(length[:])

	_, err := readMessage(&stream)
	requireError(t, err)
}

func FuzzReadAllPieces(f *testing.F) {
	f.Add(extFrame(1, append(mustMarshal(extDict{MsgType: 1, Piece: 0}), bytes.Repeat([]byte("a"), 60)...)), uint(60))
	f.Add(extFrame(1, append(mustMarshal(extDict{MsgType: 1, Piece: 7}), []byte("x")...)), uint(60))
	f.Add(extFrame(2, mustMarshal(extDict{MsgType: 2, Piece: 0})), uint(60))
	f.Add([]byte{}, uint(60))
	f.Add([]byte("garbage"), uint(1))

	f.Fuzz(func(t *testing.T, data []byte, size uint) {
		if size > 1<<16 {
			return
		}

		_, _ = readAllPieces(bytes.NewReader(data), size)
	})
}

func FuzzReadMessage(f *testing.F) {
	f.Add([]byte{0, 0, 0, 2, 20, 0})
	f.Add([]byte{0, 0, 0, 1, 20})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = readMessage(bytes.NewReader(data))
	})
}

func mustMarshal(v any) []byte {
	b, err := bencode.Marshal(v)
	if err != nil {
		panic(err)
	}

	return b
}
