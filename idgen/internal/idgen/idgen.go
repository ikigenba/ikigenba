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

// ErrInvalidID wraps the error TimeOf returns for a malformed id.
//
//nolint:revive // The exported variable's static error type is part of the API contract.
var ErrInvalidID error = errors.New("invalid id")

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

// MintAt returns "<prefix>-XXXX-XXXX" for the given instant. Instants before
// Epoch() are clamped to Epoch(). The caller guarantees prefix satisfies
// ValidPrefix (cli validates at the flag boundary; D5); MintAt does not
// re-validate.
//
// The representable range is the half-open window [Epoch, Epoch+36^8 ms),
// approximately 89 years. Instants at or beyond the ceiling wrap modulo 36^8
// and collide with an earlier instant in the window; offsets beyond
// time.Duration's range first saturate according to time.Sub. Callers that
// need to distinguish later instants must range-check before calling MintAt.
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

// TimeOf inverts the body of any "<prefix>-XXXX-XXXX" id to the instant it
// was minted from, at millisecond precision, in UTC. Ids of any prefix
// decode. Returns an error wrapping ErrInvalidID when id is not canonical.
func TimeOf(id string) (time.Time, error) {
	parts := strings.Split(id, "-")
	if len(parts) != 3 || !ValidPrefix(parts[0]) || !validBodyPart(parts[1]) || !validBodyPart(parts[2]) {
		return time.Time{}, fmt.Errorf("%w: non-canonical format", ErrInvalidID)
	}

	// parts[1] and parts[2] each passed validBodyPart above, so both are exactly
	// groupWidth valid base36 digits: decodeBase36 cannot fail on their
	// concatenation, and bodyDigits (=8) base36 digits max out at 36^8 - 1, one
	// below modulus (36^8). Neither a decode failure nor n >= modulus is reachable.
	n, _ := decodeBase36(parts[1] + parts[2])

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
