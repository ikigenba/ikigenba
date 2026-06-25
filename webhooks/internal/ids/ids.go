// Package ids generates opaque, unguessable identifiers for the webhooks
// service. Each id is a 128-bit cryptographically-random value, base32-encoded
// (StdEncoding, no padding) into 26 chars over [A-Z2-7]. Like the rest of the
// suite it carries no embedded timestamp, so it leaks nothing about when it was
// minted. The suite deliberately shares no crypto package — this is a
// copied-pattern helper local to the module.
package ids

import (
	"crypto/rand"
	"encoding/base32"
)

var enc = base32.StdEncoding.WithPadding(base32.NoPadding)

// New returns a fresh 128-bit cryptographically-random opaque identifier. It
// panics if the system CSPRNG fails — an unusable entropy source is not a
// condition the caller can sensibly recover from.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("ids: crypto/rand failed: " + err.Error())
	}
	return enc.EncodeToString(b[:])
}
