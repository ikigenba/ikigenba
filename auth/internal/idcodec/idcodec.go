package idcodec

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"io"
)

const (
	Alphabet     = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	SecretPrefix = "ikp_"
)

const (
	idBytes     = 16
	secretBytes = 32
)

var encoding = base32.NewEncoding(Alphabet).WithPadding(base32.NoPadding)

func Encode(b []byte) string {
	return encoding.EncodeToString(b)
}

func NewID(rand io.Reader) (string, error) {
	b := make([]byte, idBytes)
	if _, err := io.ReadFull(rand, b); err != nil {
		return "", err
	}

	return Encode(b), nil
}

func NewSecret(rand io.Reader) (string, error) {
	b := make([]byte, secretBytes)
	if _, err := io.ReadFull(rand, b); err != nil {
		return "", err
	}

	return SecretPrefix + Encode(b), nil
}

func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))

	return hex.EncodeToString(sum[:])
}
