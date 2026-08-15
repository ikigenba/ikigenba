// Package correlation owns the suite-wide correlation identifier carried on
// request contexts and over HTTP.
package correlation

import (
	"context"
	"crypto/rand"
	"time"
)

// Header is the HTTP header used to propagate a correlation identifier.
const Header = "X-Correlation-Id"

const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

type contextKey struct{}

// New returns a 26-character Crockford-base32 ULID.
func New() string {
	now := uint64(time.Now().UnixMilli())
	var randomness [10]byte
	if _, err := rand.Read(randomness[:]); err != nil {
		panic("correlation: crypto/rand failed: " + err.Error())
	}

	var id [26]byte
	id[0] = alphabet[(now>>45)&7]
	for i, shift := range [...]uint{40, 35, 30, 25, 20, 15, 10, 5, 0} {
		id[i+1] = alphabet[(now>>shift)&31]
	}

	var bits uint32
	var bitCount uint
	out := 10
	for _, b := range randomness {
		bits = bits<<8 | uint32(b)
		bitCount += 8
		for bitCount >= 5 {
			bitCount -= 5
			id[out] = alphabet[(bits>>bitCount)&31]
			out++
		}
	}

	return string(id[:])
}

// Valid reports whether s has the suite correlation identifier shape.
func Valid(s string) bool {
	if len(s) != 26 {
		return false
	}
	for i := range s {
		if !crockfordByte(s[i]) {
			return false
		}
	}
	return true
}

func crockfordByte(b byte) bool {
	if b >= '0' && b <= '9' {
		return true
	}
	if b < 'A' || b > 'Z' {
		return false
	}
	switch b {
	case 'I', 'L', 'O', 'U':
		return false
	default:
		return true
	}
}

// WithContext returns a context carrying id. Invalid identifiers are ignored.
func WithContext(ctx context.Context, id string) context.Context {
	if !Valid(id) {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the correlation identifier on ctx, or an empty string.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

// Ensure returns the existing context identifier or installs a newly minted one.
func Ensure(ctx context.Context) (ensured context.Context, id string) {
	if id = FromContext(ctx); id != "" {
		return ctx, id
	}
	id = New()
	return WithContext(ctx, id), id
}
