package oauth_test

import (
	"math/rand"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/oauth"
)

// R-VFWC-VKW0
func TestClientExportedFieldContract(t *testing.T) {
	authURL := &url.URL{Scheme: "https", Host: "identity.example", Path: "/authorize"}
	tokenURL := &url.URL{Scheme: "https", Host: "identity.example", Path: "/token"}
	client := oauth.Client{
		AuthURL:      authURL,
		TokenURL:     tokenURL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURI:  "http://127.0.0.1:8391/callback",
		Scope:        "openid",
	}
	hasType[*url.URL](client.AuthURL)
	hasType[*url.URL](client.TokenURL)
	hasType[string](client.ClientID)
	hasType[string](client.ClientSecret)
	hasType[string](client.RedirectURI)
	hasType[string](client.Scope)
	if client.AuthURL != authURL || client.TokenURL != tokenURL || client.ClientID != "client-id" ||
		client.ClientSecret != "client-secret" || client.RedirectURI != "http://127.0.0.1:8391/callback" || client.Scope != "openid" {
		t.Errorf("oauth.Client fields read back %+v, want the constructed values", client)
	}
}

// R-VH49-9CMP
func TestParamExportedFieldContract(t *testing.T) {
	param := oauth.Param{Key: "audience", Value: "api"}
	hasType[string](param.Key)
	hasType[string](param.Value)
	if param.Key != "audience" || param.Value != "api" {
		t.Errorf("oauth.Param{Key: %q, Value: %q} read back %+v", "audience", "api", param)
	}
}

// R-LBR3-Q4A0
func TestChallengeExportedFunctionSignature(t *testing.T) {
	hasType[func(verifier string) string](oauth.Challenge)
	if oauth.Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk") == "" {
		t.Error("oauth.Challenge returned an empty challenge")
	}
}

// R-LCZ0-3W0P
func TestAuthorizeURLExportedMethodSignature(t *testing.T) {
	hasType[func(oauth.Client, oauth.Session, []oauth.Param) string](oauth.Client.AuthorizeURL)
	client := oauth.Client{AuthURL: &url.URL{Scheme: "https", Host: "identity.example", Path: "/authorize"}}
	if got := client.AuthorizeURL(oauth.Session{State: "state", CodeVerifier: "verifier"}, nil); !strings.HasPrefix(got, "https://identity.example/authorize?") {
		t.Errorf("Client.AuthorizeURL() = %q, want the authorize endpoint with a query", got)
	}
}

// R-LE6W-HNRE
func TestReservedAuthorizeParamExportedFunctionSignature(t *testing.T) {
	hasType[func(key string) bool](oauth.ReservedAuthorizeParam)
	if !oauth.ReservedAuthorizeParam("state") {
		t.Error("oauth.ReservedAuthorizeParam(\"state\") = false, want true")
	}
}

// hasType compiles only when its argument is assignable to T.
func hasType[T any](T) {}

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

// R-JA0B-WMIH
func TestAuthorizeURLFormEncodesScope(t *testing.T) {
	authURL, err := url.Parse("https://accounts.example.com/oauth/authorize")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	client := oauth.Client{
		AuthURL: authURL,
		Scope:   "openid user+profile",
	}

	gotURL, err := url.Parse(client.AuthorizeURL(oauth.Session{}, nil))
	if err != nil {
		t.Fatalf("url.Parse(AuthorizeURL()) error = %v", err)
	}
	if !strings.Contains(gotURL.RawQuery, "scope=openid+user%2Bprofile") {
		t.Errorf("RawQuery = %q, want form-encoded scope", gotURL.RawQuery)
	}
	if strings.Contains(gotURL.RawQuery, "scope=openid%20") {
		t.Errorf("RawQuery = %q, scope space encoded as %%20 instead of +", gotURL.RawQuery)
	}
	if got := gotURL.Query().Get("scope"); got != client.Scope {
		t.Errorf("decoded scope = %q, want %q", got, client.Scope)
	}
}

// R-JB88-AE96
func TestAuthorizeURLRetainsEndpointQueryParameters(t *testing.T) {
	authURL, err := url.Parse("https://accounts.example.com/oauth/authorize?audience=api&prompt=login&audience=profile")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	originalRawQuery := authURL.RawQuery
	client := oauth.Client{AuthURL: authURL, ClientID: "client-123"}

	gotURL, err := url.Parse(client.AuthorizeURL(oauth.Session{}, nil))
	if err != nil {
		t.Fatalf("url.Parse(AuthorizeURL()) error = %v", err)
	}
	query := gotURL.Query()
	if got, want := query["audience"], []string{"api", "profile"}; !slices.Equal(got, want) {
		t.Errorf("audience values = %q, want %q", got, want)
	}
	if got := query.Get("prompt"); got != "login" {
		t.Errorf("prompt = %q, want login", got)
	}
	if got := query.Get("client_id"); got != client.ClientID {
		t.Errorf("client_id = %q, want %q", got, client.ClientID)
	}
	if authURL.RawQuery != originalRawQuery {
		t.Errorf("Client.AuthURL.RawQuery = %q after AuthorizeURL, want unchanged %q", authURL.RawQuery, originalRawQuery)
	}
}

// R-JCG4-O5ZV
func TestAuthorizeURLAppendsExtrasInCallerOrderWithRepeatedKeys(t *testing.T) {
	authURL, err := url.Parse("https://accounts.example.com/oauth/authorize?endpoint=value")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	extra := []oauth.Param{
		{Key: "audience", Value: "user profile"},
		{Key: "prompt", Value: "select+account"},
		{Key: "audience", Value: "admin"},
	}

	gotURL, err := url.Parse((oauth.Client{AuthURL: authURL}).AuthorizeURL(oauth.Session{}, extra))
	if err != nil {
		t.Fatalf("url.Parse(AuthorizeURL()) error = %v", err)
	}
	const extraSuffix = "audience=user+profile&prompt=select%2Baccount&audience=admin"
	if !strings.HasSuffix(gotURL.RawQuery, "&"+extraSuffix) {
		t.Errorf("RawQuery = %q, want extras appended in caller order as suffix %q", gotURL.RawQuery, extraSuffix)
	}
	if got, want := gotURL.Query()["audience"], []string{"user profile", "admin"}; !slices.Equal(got, want) {
		t.Errorf("audience values = %q, want %q", got, want)
	}
}

// R-JDO1-1XQK
func TestReservedAuthorizeParamDefinedKeysAndRepresentativeOthers(t *testing.T) {
	tests := []struct {
		key      string
		reserved bool
	}{
		{key: "response_type", reserved: true},
		{key: "client_id", reserved: true},
		{key: "redirect_uri", reserved: true},
		{key: "state", reserved: true},
		{key: "code_challenge", reserved: true},
		{key: "code_challenge_method", reserved: true},
		{key: "scope", reserved: true},
		{key: "", reserved: false},
		{key: "response-type", reserved: false},
		{key: "clientid", reserved: false},
		{key: "Scope", reserved: false},
		{key: "scope ", reserved: false},
		{key: "prompt", reserved: false},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			if got := oauth.ReservedAuthorizeParam(test.key); got != test.reserved {
				t.Errorf("ReservedAuthorizeParam(%q) = %t, want %t", test.key, got, test.reserved)
			}
		})
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
