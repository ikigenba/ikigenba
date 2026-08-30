// Package idgen encodes instants as short, reversible identifiers.
package idgen

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	base       = int64(36)
	bodyDigits = 8
	modulus    = base * base * base * base * base * base * base * base
	multiplier = int64(0x9E3779B1)
	// multiplierInverse was computed with the extended Euclidean algorithm as
	// multiplier^-1 modulo modulus. validateDerivedConstants enforces the result.
	multiplierInverse = int64(2032036425553)
	offset            = int64(0xC0FFEE)
)

// Epoch is the zero point for encoded timestamps.
var Epoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// ErrInvalidID identifies malformed identifiers passed to TimeOf.
var ErrInvalidID = errors.New("invalid id")

func init() {
	validateDerivedConstants()
	validateAffineMap(multiplier, modulus)
}

func validateDerivedConstants() {
	derivedModulus := int64(1)
	for range bodyDigits {
		derivedModulus *= base
	}
	if modulus != derivedModulus {
		panic("idgen: modulus does not match base^bodyDigits")
	}
	if multiplyMod(multiplier, multiplierInverse) != 1 {
		panic("idgen: multiplierInverse is not the modular inverse of multiplier")
	}
}

// MintAt returns an identifier for t using prefix. Instants before Epoch are
// represented as Epoch.
func MintAt(prefix string, t time.Time) string {
	if t.Before(Epoch) {
		t = Epoch
	}

	ms := int64(t.Sub(Epoch) / time.Millisecond)
	n := (multiplyMod(ms, multiplier) + offset) % modulus
	body := encodeBase36(n)

	return prefix + "-" + body[:4] + "-" + body[4:]
}

// TimeOf recovers the UTC, millisecond-precision instant encoded in id.
func TimeOf(id string) (time.Time, error) {
	parts := strings.Split(id, "-")
	if len(parts) != 3 || !validPrefix(parts[0]) || !validBodyPart(parts[1]) || !validBodyPart(parts[2]) {
		return time.Time{}, fmt.Errorf("%w: non-canonical format", ErrInvalidID)
	}

	n, ok := decodeBase36(parts[1] + parts[2])
	if !ok || n >= modulus {
		return time.Time{}, fmt.Errorf("%w: body out of range", ErrInvalidID)
	}

	difference := (n + modulus - offset) % modulus
	ms := multiplyMod(difference, multiplierInverse)
	return Epoch.Add(time.Duration(ms) * time.Millisecond).UTC(), nil
}

func multiplyMod(value, factor int64) int64 {
	value %= modulus
	var product int64
	for factor > 0 {
		if factor&1 != 0 {
			product = (product + value) % modulus
		}
		value = (value * 2) % modulus
		factor >>= 1
	}
	return product
}

func encodeBase36(value int64) string {
	encoded := [bodyDigits]byte{}
	for i := len(encoded) - 1; i >= 0; i-- {
		digit := value % base
		if digit < 10 {
			encoded[i] = byte('0' + digit)
		} else {
			encoded[i] = byte('A' + digit - 10)
		}
		value /= base
	}
	return string(encoded[:])
}

func decodeBase36(body string) (int64, bool) {
	var value int64
	for i := 0; i < len(body); i++ {
		var digit int64
		switch {
		case body[i] >= '0' && body[i] <= '9':
			digit = int64(body[i] - '0')
		case body[i] >= 'A' && body[i] <= 'Z':
			digit = int64(body[i]-'A') + 10
		default:
			return 0, false
		}
		value = value*base + digit
	}
	return value, true
}

func validPrefix(prefix string) bool {
	if prefix == "" {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if (prefix[i] < 'A' || prefix[i] > 'Z') &&
			(prefix[i] < 'a' || prefix[i] > 'z') &&
			(prefix[i] < '0' || prefix[i] > '9') {
			return false
		}
	}
	return true
}

func validBodyPart(part string) bool {
	if len(part) != 4 {
		return false
	}
	for i := 0; i < len(part); i++ {
		if (part[i] < '0' || part[i] > '9') && (part[i] < 'A' || part[i] > 'Z') {
			return false
		}
	}
	return true
}

func validateAffineMap(mapMultiplier, mapModulus int64) {
	if mapModulus <= 0 {
		panic("idgen: affine modulus must be positive")
	}

	a, b := mapMultiplier, mapModulus
	if a < 0 {
		a = -a
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a != 1 {
		panic("idgen: affine multiplier is not invertible")
	}
}
