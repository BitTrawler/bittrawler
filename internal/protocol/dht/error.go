package dht

import (
	"fmt"

	"github.com/anacrolix/torrent/bencode"
)

const (
	// These are documented in BEP 5.
	ErrorCodeGenericError  = 201
	ErrorCodeServerError   = 202
	ErrorCodeProtocolError = 203
	ErrorCodeMethodUnknown = 204
	// BEP 44
	ErrorCodeMessageValueFieldTooBig       = 205
	ErrorCodeInvalidSignature              = 206
	ErrorCodeSaltFieldTooBig               = 207
	ErrorCodeCasHashMismatched             = 301
	ErrorCodeSequenceNumberLessThanCurrent = 302
)

// Represented as a string or list in bencode.
type Error struct {
	Code int
	Msg  string
}

var (
	_ bencode.Unmarshaler = (*Error)(nil)
	_ bencode.Marshaler   = (*Error)(nil)
	_ error               = Error{}
)

func (e *Error) UnmarshalBencode(_b []byte) (err error) {
	var _v interface{}

	err = bencode.Unmarshal(_b, &_v)
	if err != nil {
		return
	}

	switch v := _v.(type) {
	case []interface{}:
		if len(v) < 2 {
			return fmt.Errorf("dht: krpc error list has %d elements, want 2", len(v))
		}

		code, ok := v[0].(int64)
		if !ok {
			return fmt.Errorf("dht: krpc error code is %T, want integer", v[0])
		}

		msg, ok := v[1].(string)
		if !ok {
			return fmt.Errorf("dht: krpc error message is %T, want string", v[1])
		}

		e.Code = int(code)
		e.Msg = msg
	case string:
		e.Msg = v
	default:
		err = fmt.Errorf(`KRPC error bencode value has unexpected type: %T`, _v)
	}

	return
}

func (e Error) MarshalBencode() (ret []byte, err error) {
	return bencode.Marshal([]interface{}{e.Code, e.Msg})
}

func (e Error) Error() string {
	return fmt.Sprintf("KRPC error %d: %s", e.Code, e.Msg)
}
