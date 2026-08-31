// Package idgen encodes instants as short, reversible identifiers.
package idgen

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	base           = int64(36)
	base36Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	bodyDigits     = 8
	groupWidth     = bodyDigits / 2
	modulus        = base * base * base * base * base * base * base * base
	multiplier     = int64(0x9E3779B1)
	// multiplierInverse was computed with the extended Euclidean algorithm as
	// multiplier^-1 modulo modulus. validateDerivedConstants enforces the result.
	multiplierInverse = int64(2032036425553)
	offset            = int64(0xC0FFEE)
)

// Epoch returns the zero point for encoded timestamps: 2026-01-01 00:00:00 UTC.
// It is an accessor so importers cannot reassign the epoch and change the id
// encoding for the process.
func Epoch() time.Time {
	return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
}

// ErrInvalidID identifies malformed identifiers passed to TimeOf.
var ErrInvalidID = errors.New("invalid id")

func init() {
	validateDerivedConstants()
	validateAffineMap()
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

// MintAt returns an identifier for t using prefix.
//
// The representable range is the half-open window [Epoch, Epoch+modulus ms),
// where modulus is 36^8 milliseconds (≈89 years). The encoded body is the
// affine map (multiplier*ms+offset) taken modulo modulus, so t is carried into
// that ring before encoding.
//
// Outside the window the result is silently aliased rather than reported:
//   - Instants before Epoch are represented as Epoch (the floor).
//   - Instants at or beyond Epoch+modulus ms wrap through the modulus and
//     collide with an earlier instant inside the window; the id no longer
//     round-trips through TimeOf, which recovers that earlier aliased instant.
//
// Callers that must distinguish instants past the ceiling should range-check t
// against Epoch+modulus ms before minting.
func MintAt(prefix string, t time.Time) string {
	epoch := Epoch()
	if t.Before(epoch) {
		t = epoch
	}

	ms := int64(t.Sub(epoch) / time.Millisecond)
	n := (multiplyMod(ms, multiplier) + offset) % modulus
	body := encodeBase36(n)

	return prefix + "-" + body[:groupWidth] + "-" + body[groupWidth:]
}

// TimeOf recovers the UTC, millisecond-precision instant encoded in id.
func TimeOf(id string) (time.Time, error) {
	parts := strings.Split(id, "-")
	if len(parts) != 3 || !ValidPrefix(parts[0]) || !validBodyPart(parts[1]) || !validBodyPart(parts[2]) {
		return time.Time{}, fmt.Errorf("%w: non-canonical format", ErrInvalidID)
	}

	n, ok := decodeBase36(parts[1] + parts[2])
	if !ok || n >= modulus {
		return time.Time{}, fmt.Errorf("%w: body out of range", ErrInvalidID)
	}

	// Add modulus before subtracting offset to keep the dividend non-negative:
	// Go's % takes the dividend's sign, so a plain (n-offset)%modulus would yield
	// a negative remainder whenever n < offset and break the round-trip. Since
	// 0 <= n < modulus and 0 <= offset < modulus, (n+modulus-offset) stays in
	// [1, 2*modulus) and the final % maps it back into [0, modulus).
	difference := (n + modulus - offset) % modulus
	ms := multiplyMod(difference, multiplierInverse)
	return Epoch().Add(time.Duration(ms) * time.Millisecond).UTC(), nil
}

// multiplyMod returns (value*factor) mod modulus using multiply-by-doubling
// specifically to avoid int64 overflow. A direct value*factor overflows: the
// largest operands here are ms≈modulus-1 (≈2.82e12) times multiplier
// (0x9E3779B1 ≈ 2.65e9), whose product reaches ≈7.5e21 and blows past int64's
// max (≈9.22e18). The doubling loop keeps every intermediate inside int64: each
// step reduces mod modulus, so value peaks at 2*(modulus-1) ≈ 5.64e12 and
// product stays below modulus. Do not "simplify" this to value*factor%modulus —
// that reintroduces the overflow the doubling exists to prevent.
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
		encoded[i] = base36Alphabet[digit]
		value /= base
	}
	return string(encoded[:])
}

func decodeBase36(body string) (int64, bool) {
	var value int64
	for i := 0; i < len(body); i++ {
		digit, ok := base36Digit(body[i])
		if !ok {
			return 0, false
		}
		value = value*base + digit
	}
	return value, true
}

func base36Digit(character byte) (int64, bool) {
	digit := strings.IndexByte(base36Alphabet, character)
	if digit < 0 {
		return 0, false
	}
	return int64(digit), true
}

// ValidPrefix reports whether prefix is a well-formed id prefix: a non-empty
// run of ASCII letters and digits.
func ValidPrefix(prefix string) bool {
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
	if len(part) != groupWidth {
		return false
	}
	for i := 0; i < len(part); i++ {
		if _, ok := base36Digit(part[i]); !ok {
			return false
		}
	}
	return true
}

// validateAffineMap confirms that the shipped affine map (multiplier over
// modulus) is invertible: modulus is positive and multiplier is coprime with
// it, so MintAt/TimeOf form a bijection. It reads the package constants
// directly because the process only ever ships those values.
func validateAffineMap() {
	if modulus <= 0 {
		panic("idgen: affine modulus must be positive")
	}

	a, b := multiplier, modulus
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
