package artifacts

import (
	"crypto/rand"
	"encoding/base32"
)

var tokenEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewToken returns a 150-bit, lowercase base32 identifier.
func NewToken() string {
	random := make([]byte, 19)
	if _, err := rand.Read(random); err != nil {
		panic("generate artifact token: " + err.Error())
	}
	return tokenEncoding.EncodeToString(random)[:30]
}
