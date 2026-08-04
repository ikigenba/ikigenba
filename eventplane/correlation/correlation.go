// Package correlation provides correlation identifiers and request-scoped
// context accessors shared by eventplane users.
package correlation

import (
	"context"
	"crypto/rand"
	"time"
)

// Header is the HTTP header carrying a correlation id between suite processes.
const Header = "X-Correlation-Id"

const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

type contextKey struct{}

// New mints a 26-character Crockford base32 ULID.
func New() string {
	var data [16]byte
	milliseconds := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		data[i] = byte(milliseconds)
		milliseconds >>= 8
	}
	if _, err := rand.Read(data[6:]); err != nil {
		panic("correlation: crypto/rand failed: " + err.Error())
	}

	var id [26]byte
	// A ULID has 128 significant bits encoded in 130 base32 bits. Treating the
	// input as if prefixed by two zero bits makes each output character a
	// straightforward five-bit window, most significant first.
	for output := range id {
		value := byte(0)
		for offset := 0; offset < 5; offset++ {
			bit := output*5 + offset - 2
			value <<= 1
			if bit >= 0 && data[bit/8]&(1<<uint(7-bit%8)) != 0 {
				value |= 1
			}
		}
		id[output] = alphabet[value]
	}
	return string(id[:])
}

// Valid reports whether s is a well-formed correlation id.
func Valid(s string) bool {
	if len(s) != 26 {
		return false
	}
	for i := range s {
		if !validCharacter(s[i]) {
			return false
		}
	}
	return true
}

func validCharacter(character byte) bool {
	for i := range alphabet {
		if character == alphabet[i] {
			return true
		}
	}
	return false
}

// WithContext returns a child of ctx carrying id. Invalid ids are ignored.
func WithContext(ctx context.Context, id string) context.Context {
	if !Valid(id) {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the correlation id carried by ctx, or an empty string.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}

// Ensure returns an existing correlation id or mints and attaches a new one.
func Ensure(ctx context.Context) (context.Context, string) {
	if id := FromContext(ctx); id != "" {
		return ctx, id
	}
	id := New()
	return WithContext(ctx, id), id
}
