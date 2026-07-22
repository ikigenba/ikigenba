package sites

import "crypto/rand"

const tokenAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

// NewToken returns a 150-bit unlisted-site name encoded as 30 lowercase base32
// characters. A failed system CSPRNG is fatal because the token is a credential.
func NewToken() string {
	random := make([]byte, 30)
	if _, err := rand.Read(random); err != nil {
		panic(err)
	}
	for i, b := range random {
		random[i] = tokenAlphabet[int(b)&31]
	}
	return string(random)
}
