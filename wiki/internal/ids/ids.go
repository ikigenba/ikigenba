// Package ids centralizes stable identifier types for wiki entities.
package ids

import (
	"crypto/rand"
	"time"
)

// ID is a stable wiki entity identifier.
type ID string

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New mints a time-sortable ULID using the current millisecond and crypto/rand.
func New() string {
	var raw [16]byte
	millis := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		raw[i] = byte(millis)
		millis >>= 8
	}
	if _, err := rand.Read(raw[6:]); err != nil {
		panic("ids: crypto/rand failed: " + err.Error())
	}

	var encoded [26]byte
	for i := range encoded {
		var value byte
		for bit := 0; bit < 5; bit++ {
			value <<= 1
			position := i*5 + bit - 2 // ULID has two leading zero padding bits.
			if position >= 0 && position < 128 {
				value |= (raw[position/8] >> (7 - position%8)) & 1
			}
		}
		encoded[i] = crockford[value]
	}
	return string(encoded[:])
}
