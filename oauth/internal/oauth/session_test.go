package oauth_test

import (
	"bytes"
	"encoding/base64"
	"math/rand"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
)

const sampleSize = 64

// R-ISXQ-JU4R
func TestNewSessionRendersUnpaddedBase64URLSecrets(t *testing.T) {
	rng := rand.New(rand.NewSource(108_942)) // #nosec G404 -- fixed test entropy must be reproducible.
	for range sampleSize {
		session := newSession(t, rng)
		for field, value := range map[string]string{
			"State": session.State, "CodeVerifier": session.CodeVerifier,
		} {
			if strings.Contains(value, "=") {
				t.Errorf("%s %q contains base64 padding", field, value)
			}
			for _, character := range value {
				if !isBase64URLCharacter(character) {
					t.Errorf("%s %q contains non-base64url character %q", field, value, character)
				}
			}
		}
	}
}

// R-IU5M-XLVG
func TestNewSessionCodeVerifierSatisfiesRFC7636Grammar(t *testing.T) {
	const seed = 7636
	rng := rand.New(rand.NewSource(seed)) // #nosec G404 -- fixed test entropy must be reproducible.
	for range sampleSize {
		verifier := newSession(t, rng).CodeVerifier
		if len(verifier) < 43 || len(verifier) > 128 {
			t.Errorf("seed %d produced CodeVerifier %q with length %d, want between 43 and 128", seed, verifier, len(verifier))
		}
		for _, character := range verifier {
			if !isUnreserved(character) {
				t.Errorf("CodeVerifier %q contains reserved character %q", verifier, character)
			}
		}
	}
}

// R-IVDJ-BDM5
func TestNewSessionDrawsVerifierBeforeStateAtExactSizes(t *testing.T) {
	verifierBytes := bytes.Repeat([]byte{0x1f}, 64)
	stateBytes := bytes.Repeat([]byte{0xe2}, 32)
	entropy := append(append([]byte(nil), verifierBytes...), stateBytes...)

	session := newSession(t, bytes.NewReader(entropy))
	wantVerifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	wantState := base64.RawURLEncoding.EncodeToString(stateBytes)
	if session.CodeVerifier != wantVerifier {
		t.Errorf("CodeVerifier = %q, want %q", session.CodeVerifier, wantVerifier)
	}
	if session.State != wantState {
		t.Errorf("State = %q, want %q", session.State, wantState)
	}
	if len(session.CodeVerifier) != 86 {
		t.Errorf("CodeVerifier length = %d, want 86", len(session.CodeVerifier))
	}
	if len(session.State) != 43 {
		t.Errorf("State length = %d, want 43", len(session.State))
	}
}

// R-IWLF-P5CU
func TestNewSessionIsReproducibleFromSuppliedEntropy(t *testing.T) {
	entropy := make([]byte, 96)
	// #nosec G404 -- fixed test entropy must be reproducible.
	if _, err := rand.New(rand.NewSource(51_061)).Read(entropy); err != nil {
		t.Fatalf("generate fixed entropy: %v", err)
	}

	first := newSession(t, bytes.NewReader(entropy))
	second := newSession(t, bytes.NewReader(entropy))
	if first != second {
		t.Errorf("sessions from identical entropy differ: first = %#v, second = %#v", first, second)
	}
}

// R-J1H1-88BM
func TestNewSessionSecretsAreIndependentAndChangeWithEntropy(t *testing.T) {
	const seed = 292_811
	rng := rand.New(rand.NewSource(seed)) // #nosec G404 -- fixed test entropy must be reproducible.
	var previous oauth.Session
	for sample := range sampleSize {
		session := newSession(t, rng)
		if session.State == session.CodeVerifier {
			t.Errorf("sample %d has equal State and CodeVerifier %q", sample, session.State)
		}
		if sample > 0 {
			if session.State == previous.State {
				t.Errorf("seed %d sample %d State %q equals previous State", seed, sample, session.State)
			}
			if session.CodeVerifier == previous.CodeVerifier {
				t.Errorf("seed %d sample %d CodeVerifier %q equals previous CodeVerifier", seed, sample, session.CodeVerifier)
			}
		}
		previous = session
	}
}

func newSession(t *testing.T, entropy interface{ Read([]byte) (int, error) }) oauth.Session {
	t.Helper()
	session, err := oauth.NewSession(entropy)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	return session
}

func isBase64URLCharacter(character rune) bool {
	return character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9' ||
		character == '-' || character == '_'
}

func isUnreserved(character rune) bool {
	return isBase64URLCharacter(character) || character == '.' || character == '~'
}
