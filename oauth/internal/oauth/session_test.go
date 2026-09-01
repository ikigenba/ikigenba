package oauth_test

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
)

const sampleSize = 64

// R-L4FP-FHTU
func TestNewSessionExposesExactSessionFields(t *testing.T) {
	sessionType := reflect.TypeOf(oauth.Session{})
	wantFields := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "State", typeOf: reflect.TypeOf("")},
		{name: "CodeVerifier", typeOf: reflect.TypeOf("")},
	}

	if sessionType.NumField() != len(wantFields) {
		t.Fatalf("oauth.Session has %d fields, want exactly %d", sessionType.NumField(), len(wantFields))
	}
	for index, want := range wantFields {
		got := sessionType.Field(index)
		if got.Name != want.name {
			t.Errorf("oauth.Session field %d name = %q, want %q", index, got.Name, want.name)
		}
		if got.Type != want.typeOf {
			t.Errorf("oauth.Session field %d (%s) type = %v, want %v", index, got.Name, got.Type, want.typeOf)
		}
		if !got.IsExported() {
			t.Errorf("oauth.Session field %d (%s) is not exported", index, got.Name)
		}
	}
}

// R-L5NL-T9KJ
func TestNewSessionHasExactFunctionSignature(t *testing.T) {
	got := reflect.TypeOf(oauth.NewSession)
	want := reflect.TypeOf(func(io.Reader) (oauth.Session, error) { return oauth.Session{}, nil })
	if got != want {
		t.Errorf("oauth.NewSession type = %v, want %v", got, want)
	}
}

// R-ISXQ-JU4R
func TestNewSessionRendersUnpaddedBase64URLSecrets(t *testing.T) {
	rng := rand.New(rand.NewSource(108_942)) // #nosec G404 -- fixed test entropy must be reproducible.
	for range sampleSize {
		session := mustNewSession(t, rng)
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
		verifier := mustNewSession(t, rng).CodeVerifier
		if len(verifier) != 86 {
			t.Errorf("CodeVerifier %q has length %d, want 86", verifier, len(verifier))
		}
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

	session := mustNewSession(t, bytes.NewReader(entropy))
	wantVerifier := "Hx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHw"
	wantState := "4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uI"
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
	for index := range entropy {
		entropy[index] = byte(index)
	}
	want := oauth.Session{
		CodeVerifier: "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8gISIjJCUmJygpKissLS4vMDEyMzQ1Njc4OTo7PD0-Pw",
		State:        "QEFCQ0RFRkdISUpLTE1OT1BRUlNUVVZXWFlaW1xdXl8",
	}

	first := mustNewSession(t, bytes.NewReader(entropy))
	second := mustNewSession(t, bytes.NewReader(entropy))
	if first != want {
		t.Errorf("first session = %#v, want %#v", first, want)
	}
	if second != want {
		t.Errorf("second session = %#v, want %#v", second, want)
	}
}

// R-J1H1-88BM
func TestNewSessionSecretsAreIndependentAndChangeWithEntropy(t *testing.T) {
	const seed = 292_811
	rng := rand.New(rand.NewSource(seed)) // #nosec G404 -- fixed test entropy must be reproducible.
	var previous oauth.Session
	for sample := range sampleSize {
		session := mustNewSession(t, rng)
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

// R-IXTC-2X3J
func TestNewSessionReportsCodeVerifierEntropyFailure(t *testing.T) {
	sentinel := errors.New("verifier entropy failed")
	entropy := &failingReader{
		data: bytes.Repeat([]byte{0x37}, 63),
		err:  sentinel,
	}

	_, gotErr := oauth.NewSession(entropy)
	if gotErr == nil {
		t.Fatal("NewSession() error = nil, want code verifier entropy failure")
	}
	if !strings.Contains(gotErr.Error(), "code verifier") {
		t.Errorf("NewSession() error = %q, want diagnostic naming code verifier", gotErr)
	}
	if !errors.Is(gotErr, sentinel) {
		t.Errorf("errors.Is(NewSession() error, sentinel) = false, error = %v", gotErr)
	}
	if entropy.offset != 63 {
		t.Errorf("entropy bytes consumed = %d, want 63", entropy.offset)
	}
}

// R-IZ18-GOU8
func TestNewSessionReportsStateEntropyFailureAfterVerifierDraw(t *testing.T) {
	sentinel := errors.New("state entropy failed")
	const statePrefixLength = 31
	entropy := &failingReader{
		data: bytes.Repeat([]byte{0x84}, 64+statePrefixLength),
		err:  sentinel,
	}

	_, gotErr := oauth.NewSession(entropy)
	if gotErr == nil {
		t.Fatal("NewSession() error = nil, want state entropy failure")
	}
	if !strings.Contains(gotErr.Error(), "state") {
		t.Errorf("NewSession() error = %q, want diagnostic naming state", gotErr)
	}
	if !errors.Is(gotErr, sentinel) {
		t.Errorf("errors.Is(NewSession() error, sentinel) = false, error = %v", gotErr)
	}
	if entropy.offset != 64+statePrefixLength {
		t.Errorf("entropy bytes consumed = %d, want %d after complete verifier draw", entropy.offset, 64+statePrefixLength)
	}
}

type failingReader struct {
	data   []byte
	err    error
	offset int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.offset == len(r.data) {
		return 0, r.err
	}

	n := copy(p, r.data[r.offset:])
	r.offset += n
	if r.offset == len(r.data) {
		return n, r.err
	}
	return n, nil
}

func mustNewSession(t *testing.T, entropy interface{ Read([]byte) (int, error) }) oauth.Session {
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
