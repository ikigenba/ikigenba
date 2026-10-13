// Package paging supplies opaque continuation tokens and list windows.
package paging

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
)

// Limits and copy shared by paged tools.
const (
	DefaultLimit            = 25
	MaxLimit                = 500
	BadPage          string = "page is not one this tool issued; leave it out to start from the first page"
	BadLimit         string = "limit must be between %d and %d, got %d"
	PageDescription  string = "Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."
	LimitDescription string = "Most entries to return, 1 to 500; 25 when left out."
	NextDescription  string = "Present when more entries follow; pass it back as page to get them."
)

// ErrToken marks a continuation token this tool cannot accept.
var ErrToken = errors.New(BadPage)

// Token binds continuation keys to the tool that issues them.
type Token struct {
	Tool string
}

// Encode frames a nonempty key of int64 and string values.
// Invalid field types panic because they indicate a programming error.
func (t Token) Encode(fields ...any) string {
	if len(fields) == 0 {
		panic("paging: empty key")
	}
	var payload []byte
	for _, field := range fields {
		switch value := field.(type) {
		case int64:
			payload = append(payload, 1)
			var err error
			payload, err = binary.Append(payload, binary.BigEndian, value)
			if err != nil {
				panic(err)
			}
		case string:
			length := uint64(len(value))
			if length > math.MaxUint32 {
				panic("paging: string too long")
			}
			payload = append(payload, 2)
			payload = binary.BigEndian.AppendUint32(payload, uint32(length))
			payload = append(payload, value...)
		default:
			panic("paging: invalid key field")
		}
	}
	payload = binary.BigEndian.AppendUint32(payload, t.checksum(payload))
	return base64.RawURLEncoding.EncodeToString(payload)
}

func (t Token) checksum(payload []byte) uint32 {
	length := uint64(len(t.Tool))
	if length > math.MaxUint32 {
		panic("paging: tool name too long")
	}
	hash := crc32.NewIEEE()
	_, _ = hash.Write(binary.BigEndian.AppendUint32(nil, uint32(length)))
	_, _ = hash.Write([]byte(t.Tool))
	_, _ = hash.Write(payload)
	return hash.Sum32()
}

// Decode reads a canonical continuation key into non-nil *int64 and *string
// destinations. Invalid destinations panic; invalid tokens leave them unchanged.
func (t Token) Decode(token string, into ...any) error {
	if len(into) == 0 {
		panic("paging: empty destination")
	}
	for _, destination := range into {
		switch value := destination.(type) {
		case *int64:
			if value == nil {
				panic("paging: nil destination")
			}
		case *string:
			if value == nil {
				panic("paging: nil destination")
			}
		default:
			panic("paging: invalid destination")
		}
	}
	if uint64(len(t.Tool)) > math.MaxUint32 {
		return ErrToken
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) < 4 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return ErrToken
	}
	payload := raw[:len(raw)-4]
	if binary.BigEndian.Uint32(raw[len(raw)-4:]) != t.checksum(payload) {
		return ErrToken
	}
	values := make([]any, len(into))
	remaining := payload
	for i, destination := range into {
		if len(remaining) == 0 {
			return ErrToken
		}
		switch destination.(type) {
		case *int64:
			if remaining[0] != 1 || len(remaining) < 9 {
				return ErrToken
			}
			var value int64
			if _, err := binary.Decode(remaining[1:9], binary.BigEndian, &value); err != nil {
				return ErrToken
			}
			values[i] = value
			remaining = remaining[9:]
		case *string:
			if remaining[0] != 2 || len(remaining) < 5 {
				return ErrToken
			}
			length := uint64(binary.BigEndian.Uint32(remaining[1:5]))
			remaining = remaining[5:]
			if length > uint64(len(remaining)) {
				return ErrToken
			}
			values[i] = string(remaining[:length])
			remaining = remaining[length:]
		}
	}
	if len(remaining) != 0 || t.Encode(values...) != token {
		return ErrToken
	}
	for i, destination := range into {
		switch value := destination.(type) {
		case *int64:
			*value = values[i].(int64)
		case *string:
			*value = values[i].(string)
		}
	}
	return nil
}

// Window resolves the optional limit and refuses values outside the bounds.
func Window(limit *int) (int, error) {
	if limit == nil {
		return DefaultLimit, nil
	}
	if *limit < 1 || *limit > MaxLimit {
		return 0, fmt.Errorf(BadLimit, 1, MaxLimit, *limit)
	}
	return *limit, nil
}
