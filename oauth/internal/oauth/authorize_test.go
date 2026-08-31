package oauth_test

import (
	"math/rand"
	"net/url"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
)

// R-J54Q-DJJP
func TestAuthorizeURLIncludesRequiredParameters(t *testing.T) {
	authURL, err := url.Parse("https://accounts.example.com/oauth/authorize")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	client := oauth.Client{
		AuthURL:     authURL,
		ClientID:    "client-123",
		RedirectURI: "https://app.example.com/oauth/callback",
	}
	session := oauth.Session{
		State:        "state-456",
		CodeVerifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
	}
	expectedChallenge := oauth.Challenge(session.CodeVerifier)
	if expectedChallenge != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("Challenge(%q) = %q, want RFC 7636 test vector", session.CodeVerifier, expectedChallenge)
	}

	gotURL, err := url.Parse(client.AuthorizeURL(session, nil))
	if err != nil {
		t.Fatalf("url.Parse(AuthorizeURL()) error = %v", err)
	}
	query := gotURL.Query()
	want := map[string]string{
		"response_type":         "code",
		"client_id":             client.ClientID,
		"redirect_uri":          client.RedirectURI,
		"state":                 session.State,
		"code_challenge":        expectedChallenge,
		"code_challenge_method": "S256",
	}
	for key, wantValue := range want {
		if gotValue := query.Get(key); gotValue != wantValue {
			t.Errorf("query.Get(%q) = %q, want %q", key, gotValue, wantValue)
		}
	}
}

// R-J8SF-IURS
func TestAuthorizeURLIncludesScopeOnlyWhenNonEmpty(t *testing.T) {
	authURL, err := url.Parse("https://accounts.example.com/oauth/authorize")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	client := oauth.Client{
		AuthURL:     authURL,
		ClientID:    "client-123",
		RedirectURI: "https://app.example.com/oauth/callback",
		Scope:       "openid profile email",
	}
	session := oauth.Session{
		State:        "state-456",
		CodeVerifier: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc",
	}

	withScope, err := url.Parse(client.AuthorizeURL(session, nil))
	if err != nil {
		t.Fatalf("url.Parse(AuthorizeURL() with scope) error = %v", err)
	}
	if got := withScope.Query().Get("scope"); got != client.Scope {
		t.Errorf("scope = %q, want %q", got, client.Scope)
	}

	client.Scope = ""
	withoutScope, err := url.Parse(client.AuthorizeURL(session, nil))
	if err != nil {
		t.Fatalf("url.Parse(AuthorizeURL() without scope) error = %v", err)
	}
	if withoutScope.Query().Has("scope") {
		t.Errorf("scope is present for empty Client.Scope: %q", withoutScope.Query()["scope"])
	}
}

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
