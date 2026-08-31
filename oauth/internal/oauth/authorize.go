package oauth

import (
	"crypto/sha256"
	"encoding/base64"
)

// Challenge returns the unpadded base64url-encoded SHA-256 digest of verifier.
func Challenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))

	return base64.RawURLEncoding.EncodeToString(digest[:])
}
