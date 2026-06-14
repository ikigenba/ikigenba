package integrate

import (
	"crypto/rand"
	"encoding/base32"
	"time"
)

var ulidEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

// newULID returns a 26-char lexicographically-time-ordered identifier (48 bits of
// millisecond time + 80 bits of randomness, the suite's standard ULID shape). The
// assembler mints it for a create/no_match subject — the durable subject_id the
// page registry keys on. Time-ordering matters for the dup-pair canonical order
// (smaller ULID first), so a fresh id sorts after older ids minted earlier.
func newULID() string {
	var b [16]byte
	now := uint64(time.Now().UnixMilli())
	b[0] = byte(now >> 40)
	b[1] = byte(now >> 32)
	b[2] = byte(now >> 24)
	b[3] = byte(now >> 16)
	b[4] = byte(now >> 8)
	b[5] = byte(now)
	if _, err := rand.Read(b[6:]); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return ulidEnc.EncodeToString(b[:])
}
