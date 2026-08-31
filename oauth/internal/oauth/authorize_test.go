package oauth_test

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
)

// R-J6CM-RBAE
func TestChallengeMatchesRFC7636AppendixB(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	if got := oauth.Challenge(verifier); got != want {
		t.Fatalf("Challenge(%q) = %q, want %q", verifier, got, want)
	}
}

// R-J7KJ-5313
func TestChallengeUsesUnpaddedBase64URLAlphabet(t *testing.T) {
	const seed int64 = 7636
	const samples = 64
	const verifierAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

	rng := rand.New(rand.NewSource(seed)) // #nosec G404 -- fixed test entropy must be reproducible.
	seen := make(map[string]struct{}, samples)
	for sample := range samples {
		length := 43 + rng.Intn(128-43+1)
		verifierBytes := make([]byte, length)
		for index := range verifierBytes {
			verifierBytes[index] = verifierAlphabet[rng.Intn(len(verifierAlphabet))]
		}
		verifier := string(verifierBytes)
		if _, duplicate := seen[verifier]; duplicate {
			t.Fatalf("seed %d sample %d generated duplicate verifier %q", seed, sample, verifier)
		}
		seen[verifier] = struct{}{}

		challenge := oauth.Challenge(verifier)
		if strings.ContainsRune(challenge, '=') {
			t.Errorf("seed %d sample %d: Challenge returned padded value %q", seed, sample, challenge)
		}
		for _, character := range challenge {
			if !isChallengeBase64URLCharacter(character) {
				t.Errorf("seed %d sample %d: Challenge returned invalid character %q in %q", seed, sample, character, challenge)
			}
		}
	}
}

func isChallengeBase64URLCharacter(character rune) bool {
	return character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9' ||
		character == '-' || character == '_'
}
