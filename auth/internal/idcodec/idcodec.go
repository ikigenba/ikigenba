// Package idcodec encodes opaque ids and secrets: Crockford base32, the id and
// secret minters, and the secret hash.
package idcodec

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"io"
)

const (
	// Alphabet is the Crockford base32 alphabet.
	Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	// SecretPrefix is the prefix of a minted secret.
	SecretPrefix = "ikp_"
)

const (
	idBytes     = 16
	secretBytes = 32
)

var encoding = base32.NewEncoding(Alphabet).WithPadding(base32.NoPadding)

// Encode maps b to unpadded Crockford base32 using Alphabet.
func Encode(b []byte) string {
	return encoding.EncodeToString(b)
}

// NewID reads 16 bytes from rand and returns their Encode.
func NewID(rand io.Reader) (string, error) {
	b := make([]byte, idBytes)
	if _, err := io.ReadFull(rand, b); err != nil {
		return "", err
	}

	return Encode(b), nil
}

// NewSecret reads 32 bytes from rand and returns SecretPrefix plus their Encode.
func NewSecret(rand io.Reader) (string, error) {
	b := make([]byte, secretBytes)
	if _, err := io.ReadFull(rand, b); err != nil {
		return "", err
	}

	return SecretPrefix + Encode(b), nil
}

// HashSecret returns the lowercase-hex SHA-256 of secret.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))

	return hex.EncodeToString(sum[:])
}
