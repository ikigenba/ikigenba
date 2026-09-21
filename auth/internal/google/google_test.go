package google_test

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	googleclient "github.com/ikigenba/ikigenba/auth/internal/google"
)

const testPrivateKey = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDWfhAt9Sh/EOsg
ZV5xUBqBWF26EO2GIvjU4luxmWt/KYk01c2FsOgQ1/g/XFO9whTj2+QHrd4MD7dF
YwC7fPIbtCprY5JjR489o2iGFapU3zQURk6x4mALSrRpCzhcupU6kpqcIF48lxHA
EydkshyjaJZBORExD5W9tuINaZxKizMchGh6aavkJuQ8IoWEZyboZeFI23BZRB6G
3kSIiDfx+d5LR6NQONv4+s+XLYVCe4H7Xie22tM0LVedFNXBItgoARXuXYF1PNdj
OPaDfSCnAFANjXlsBW7snvm2GzhLIrKEXASfUtFwLwh9uTw7NKG8nZ7ImQ3bOCwa
apLathBVAgMBAAECggEABMOn31ioZ1FV/HiHJWdjZCEbpkDiNCfXTn85DMcjUWx3
3QhGUheXKAMFqbHTm424KHfsM9Sp76s/smP1eOfYrUKn68FT8WGX1GvEEn2TtK8a
hiLDp7h0BeKWAq+0A+QSR+R7i64KYDhcAnl9pAM/lv+a0TF2NZh3hoKNhdrx+xPb
1WYb10hSVTJPHYjTg7dcUY0sBj2ZSIO7OG5Pk6BFQucm32Mo0RfZfgQtMQMQx4Gy
MC7owE16SiO9QC2K8UNx804p7CGmpgyn9rq157PJf7adbrGMfSBqamBOTsNpgp4Q
Mi+HKoFgU/pL+OuuK/IXAWqizbqqmbPu6VQY9/z0wQKBgQD2twBa8dynWvqgEqJh
DYENsPrXtfFtq01IBHzhJ0jHn+8JplRxF24m4brhXa92/0cBIzUi0PgbdEuExkao
QarEHKu0K9o6HHfcguAM5LMXqOYp/ATNIgKbOYMlZ6zME7vVE2uKPXRbIEP1GM9W
YvsPk7YnL5RHw++2Z5CFGOJB9QKBgQDekJydEnwY5bQLxgtPS+xD3En2WaHeSqu9
R6w3KX3jlYSDRE8eIRv+HTMFYy3LZ8DSvwe9g6CYeoAHgZonloJLGOhO8Y9mI8yp
qqTg0zJs+BpViB6vXvkKrM8jsfcAJR3zbsWlntmVvxxFz3DNPLfh6U5FiVHQI1LV
QBVsuWq44QKBgHoJt/FEpmNaS5MW5J+hcG73Vn2RkGUxUT8IiUVOi1/DqxhY4Hg4
oNI24pxMHVl9mP/lDIm2WKQr+JcrBRSBtxjfHcg30PDh2CCJ1I5MKpLPh1rJQQ6/
fg6OemLsT7t7H3Sc8JsnHwFcioEYzqbqu0nPRVFI5c5CC7dsrz5HOtRpAoGAIKcY
wxamLETvEFci66RY6m/UThdCX0mXPrLYOlOVC6GxCk0oSmRTJgoEpUKywkfbi0/J
g+1ez2ARjoheJHa5cOkblBFul26jJTVK8U0q9b/EpU3OKq6FXSKTVUpT0aqgZUmY
J79Rbt3V+QwBIatJ0xQWjq8h2KaGDZFUU1o0pSECgYEApNmfouMah2gz1OiFtuRD
jW4QQG9lykBgExPAhUG13/J90DcHwW6cGTBip5+YrHkrh8eyb3i9g6Ly3TpfH3IL
CsMja5JQnGfH59dWtbvZ+OAPcbAJLXE9bw2Hb5sA0xxnGAygpGa7rPtcUDdFWIzs
X5mIakmJ0FlxKJFSMQC2VxM=
-----END PRIVATE KEY-----`

type fakeIssuer struct {
	t             *testing.T
	server        *httptest.Server
	key           *rsa.PrivateKey
	mu            sync.Mutex
	discoveryHits int
	failDiscovery bool
	forms         []url.Values
	tokens        map[string]string
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()

	block, _ := pem.Decode([]byte(testPrivateKey))
	if block == nil {
		t.Fatal("decode test private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse test private key: %v", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("test private key has type %T", parsed)
	}

	fake := &fakeIssuer{t: t, key: key, tokens: make(map[string]string)}
	fake.server = httptest.NewUnstartedServer(http.HandlerFunc(fake.serveHTTP))
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for fake issuer: %v", err)
	}
	fake.server.Listener = listener
	fake.server.Start()
	t.Cleanup(fake.server.Close)

	return fake
}

func (f *fakeIssuer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		f.mu.Lock()
		f.discoveryHits++
		fail := f.failDiscovery
		f.mu.Unlock()
		if fail {
			http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
			return
		}
		writeJSON(f.t, w, map[string]any{
			"issuer":                                f.server.URL,
			"authorization_endpoint":                f.server.URL + "/authorize",
			"token_endpoint":                        f.server.URL + "/token",
			"jwks_uri":                              f.server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case "/jwks":
		writeJSON(f.t, w, map[string]any{"keys": []any{map[string]any{
			"kty": "RSA",
			"kid": "test-key",
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
		}}})
	case "/token":
		if r.Method != http.MethodPost {
			http.Error(w, "token exchange must use POST", http.StatusMethodNotAllowed)
			return
		}
		clientID, clientSecret, ok := r.BasicAuth()
		if !ok || clientID != "client-id" || clientSecret != "client-secret" {
			http.Error(w, "wrong OAuth client credentials", http.StatusUnauthorized)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.forms = append(f.forms, r.PostForm)
		token, ok := f.tokens[r.PostForm.Get("code")]
		f.mu.Unlock()
		if !ok {
			http.Error(w, "exchange rejected", http.StatusBadRequest)
			return
		}
		writeJSON(f.t, w, map[string]any{
			"access_token": "access-token",
			"token_type":   "Bearer",
			"id_token":     token,
		})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeIssuer) issue(code, algorithm string, claims map[string]any) {
	f.t.Helper()

	header := map[string]any{"alg": algorithm, "kid": "test-key", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		f.t.Fatalf("marshal JWT header: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		f.t.Fatalf("marshal JWT claims: %v", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(claimsJSON)
	digest := sha256.Sum256([]byte(encoded))
	signature, err := rsa.SignPKCS1v15(nil, f.key, crypto.SHA256, digest[:])
	if err != nil {
		f.t.Fatalf("sign JWT: %v", err)
	}

	f.mu.Lock()
	f.tokens[code] = encoded + "." + base64.RawURLEncoding.EncodeToString(signature)
	f.mu.Unlock()
}

func (f *fakeIssuer) token(code string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens[code]
}

func (f *fakeIssuer) setToken(code, token string) {
	f.mu.Lock()
	f.tokens[code] = token
	f.mu.Unlock()
}

func (f *fakeIssuer) lastForm() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.forms) == 0 {
		f.t.Fatal("token endpoint received no form")
	}
	return f.forms[len(f.forms)-1]
}

func (f *fakeIssuer) discoveryRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.discoveryHits
}

func (f *fakeIssuer) setDiscoveryFailure(fail bool) {
	f.mu.Lock()
	f.failDiscovery = fail
	f.mu.Unlock()
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("write fake issuer response: %v", err)
	}
}

func validClaims(issuer string) map[string]any {
	return map[string]any{
		"iss":            issuer,
		"sub":            "google-subject",
		"aud":            "client-id",
		"exp":            4102444800,
		"iat":            1700000000,
		"email":          "member@example.test",
		"email_verified": true,
		"hd":             "example.test",
	}
}

func TestExportedAPIAndAuthorizationURL(t *testing.T) {
	// R-I82C-VOP7
	claimsType := reflect.TypeFor[googleclient.Claims]()
	wantFields := []struct {
		name string
		typ  reflect.Type
	}{
		{"Issuer", reflect.TypeFor[string]()},
		{"Subject", reflect.TypeFor[string]()},
		{"Email", reflect.TypeFor[string]()},
		{"EmailVerified", reflect.TypeFor[bool]()},
		{"HostedDomain", reflect.TypeFor[string]()},
	}
	if claimsType.NumField() != len(wantFields) {
		t.Fatalf("Claims has %d fields, want %d", claimsType.NumField(), len(wantFields))
	}
	for i, want := range wantFields {
		field := claimsType.Field(i)
		if field.Name != want.name || field.Type != want.typ {
			t.Errorf("Claims field %d = %s %v, want %s %v", i, field.Name, field.Type, want.name, want.typ)
		}
	}

	// R-KUGP-2ZZD
	(func(func(string, string, string, string) *googleclient.Client) {})(googleclient.NewClient)
	// R-KVOL-GRQ2
	(func(func(*googleclient.Client, string, string, string) (string, error)) {})((*googleclient.Client).AuthCodeURL)
	// R-FX2G-ZLVJ
	(func(func(*googleclient.Client, context.Context, string, string, string) (googleclient.Claims, error)) {
	})((*googleclient.Client).Exchange)

	fake := newFakeIssuer(t)
	client := googleclient.NewClient("client-id", "client-secret", "example.test", fake.server.URL)

	// R-KWWH-UJGR
	if fake.discoveryRequests() != 0 {
		t.Fatalf("discovery requests at construction = %d, want 0", fake.discoveryRequests())
	}

	verifier := "fixed-pkce-verifier"
	redirectURI := "https://auth.example.test/login/google/callback"
	rawURL, err := client.AuthCodeURL("login-state", verifier, redirectURI)
	if err != nil {
		t.Fatalf("AuthCodeURL: %v", err)
	}
	if fake.discoveryRequests() != 1 {
		t.Fatalf("discovery requests after AuthCodeURL = %d, want 1", fake.discoveryRequests())
	}
	authURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	// R-IGLN-K2W2
	if authURL.Scheme+"://"+authURL.Host+authURL.Path != fake.server.URL+"/authorize" {
		t.Errorf("authorization endpoint = %q, want %q", authURL.String(), fake.server.URL+"/authorize")
	}
	wantChallenge := sha256.Sum256([]byte(verifier))
	wantQuery := map[string]string{
		"client_id":             "client-id",
		"hd":                    "example.test",
		"redirect_uri":          redirectURI,
		"state":                 "login-state",
		"code_challenge":        base64.RawURLEncoding.EncodeToString(wantChallenge[:]),
		"code_challenge_method": "S256",
	}
	for key, want := range wantQuery {
		if got := authURL.Query().Get(key); got != want {
			t.Errorf("authorization query %s = %q, want %q", key, got, want)
		}
	}
	second, err := client.AuthCodeURL("other-state", "other-verifier", "https://auth.other.test/callback")
	if err != nil {
		t.Fatalf("second AuthCodeURL: %v", err)
	}
	if !strings.Contains(second, url.QueryEscape("https://auth.other.test/callback")) || strings.Contains(second, url.QueryEscape(redirectURI)) {
		t.Errorf("second authorization URL did not use its per-call redirect URI: %s", second)
	}
	if fake.discoveryRequests() != 1 {
		t.Errorf("discovery requests after second AuthCodeURL = %d, want 1", fake.discoveryRequests())
	}
}

func TestExchangeVerifiesAndReturnsClaims(t *testing.T) {
	fake := newFakeIssuer(t)
	client := googleclient.NewClient("client-id", "client-secret", "example.test", fake.server.URL)

	// R-IFDR-6B5D
	// R-G0Q6-4X3M
	tests := []struct {
		name         string
		issuer       string
		hostedDomain bool
	}{
		{"https issuer", "https://accounts.google.com", true},
		{"legacy issuer and absent hosted domain", "accounts.google.com", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code := strings.ReplaceAll(test.name, " ", "-")
			tokenClaims := validClaims(test.issuer)
			if !test.hostedDomain {
				delete(tokenClaims, "hd")
			}
			fake.issue(code, "RS256", tokenClaims)
			redirectURI := "https://auth.example.test/callback/" + code
			claims, err := client.Exchange(context.Background(), code, "verifier-"+code, redirectURI)
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			want := googleclient.Claims{
				Issuer:        test.issuer,
				Subject:       "google-subject",
				Email:         "member@example.test",
				EmailVerified: true,
			}
			if test.hostedDomain {
				want.HostedDomain = "example.test"
			}
			if claims != want {
				t.Errorf("Claims = %#v, want %#v", claims, want)
			}

			form := fake.lastForm()
			for key, value := range map[string]string{
				"grant_type":    "authorization_code",
				"code":          code,
				"code_verifier": "verifier-" + code,
				"redirect_uri":  redirectURI,
			} {
				if got := form.Get(key); got != value {
					t.Errorf("exchange form %s = %q, want %q", key, got, value)
				}
			}
		})
	}
}

func TestExchangeRejectsEndpointAndVerificationFailures(t *testing.T) {
	fake := newFakeIssuer(t)
	client := googleclient.NewClient("client-id", "client-secret", "example.test", fake.server.URL)

	tests := []struct {
		name  string
		code  string
		issue func()
	}{
		{"failed exchange", "exchange-failure", func() {}},
		{"missing ID token", "missing-token", func() { fake.setToken("missing-token", "") }},
		{"bad signature", "bad-signature", func() {
			fake.issue("bad-signature", "RS256", validClaims("https://accounts.google.com"))
			token := fake.token("bad-signature")
			parts := strings.Split(token, ".")
			signature, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				t.Fatalf("decode JWT signature: %v", err)
			}
			signature[0] ^= 0xff
			fake.setToken("bad-signature", parts[0]+"."+parts[1]+"."+base64.RawURLEncoding.EncodeToString(signature))
		}},
		{"bad algorithm", "bad-algorithm", func() {
			fake.issue("bad-algorithm", "HS256", validClaims("https://accounts.google.com"))
		}},
		{"bad issuer", "bad-issuer", func() {
			fake.issue("bad-issuer", "RS256", validClaims("https://issuer.invalid"))
		}},
		{"bad audience", "bad-audience", func() {
			claims := validClaims("https://accounts.google.com")
			claims["aud"] = "different-client"
			fake.issue("bad-audience", "RS256", claims)
		}},
		{"expired", "expired", func() {
			claims := validClaims("https://accounts.google.com")
			claims["exp"] = 1
			fake.issue("expired", "RS256", claims)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.issue()
			if _, err := client.Exchange(context.Background(), test.code, "verifier", "https://auth.example.test/callback"); err == nil {
				t.Fatal("Exchange returned nil error")
			}
		})
	}

	unreachable := newFakeIssuer(t)
	unreachableClient := googleclient.NewClient("client-id", "client-secret", "example.test", unreachable.server.URL)
	unreachable.server.Close()
	if _, err := unreachableClient.Exchange(context.Background(), "code", "verifier", "https://auth.example.test/callback"); err == nil {
		t.Fatal("Exchange with unreachable token endpoint returned nil error")
	}
}

func TestAuthCodeURLDiscoveryFailureIsRetried(t *testing.T) {
	// R-KZCA-M2Y5
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	issuer := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	client := googleclient.NewClient("client-id", "client-secret", "example.test", issuer)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	got, err := client.AuthCodeURL("login-state", "verifier", "https://auth.example.test/callback")
	if err == nil {
		t.Fatal("AuthCodeURL with unreachable issuer returned nil error")
	}
	if got != "" {
		t.Fatalf("AuthCodeURL URL = %q, want empty", got)
	}

	// R-KWWH-UJGR
	fake := newFakeIssuer(t)
	retrying := googleclient.NewClient("client-id", "client-secret", "example.test", fake.server.URL)
	if fake.discoveryRequests() != 0 {
		t.Fatalf("discovery requests at construction = %d, want 0", fake.discoveryRequests())
	}
	fake.setDiscoveryFailure(true)
	failedURL, err := retrying.AuthCodeURL("login-state", "verifier", "https://auth.example.test/callback")
	if err == nil {
		t.Fatal("AuthCodeURL against failing discovery returned nil error")
	}
	if failedURL != "" {
		t.Fatalf("AuthCodeURL URL = %q, want empty", failedURL)
	}
	if fake.discoveryRequests() != 1 {
		t.Fatalf("discovery requests after failed AuthCodeURL = %d, want 1", fake.discoveryRequests())
	}

	if _, err := retrying.Exchange(context.Background(), "code", "verifier", "https://auth.example.test/callback"); err == nil {
		t.Fatal("Exchange reused a failed discovery instead of retrying")
	}
	if fake.discoveryRequests() != 2 {
		t.Fatalf("discovery requests after failed Exchange = %d, want 2", fake.discoveryRequests())
	}

	fake.setDiscoveryFailure(false)
	rawURL, err := retrying.AuthCodeURL("state-2", "verifier", "https://auth.example.test/callback")
	if err != nil {
		t.Fatalf("AuthCodeURL after discovery recovery: %v", err)
	}
	if rawURL == "" {
		t.Fatal("AuthCodeURL after discovery recovery returned an empty URL")
	}
	if fake.discoveryRequests() != 3 {
		t.Fatalf("discovery requests after recovery = %d, want 3", fake.discoveryRequests())
	}
}

func TestFakeIssuerUsesOnlyLoopback(t *testing.T) {
	fake := newFakeIssuer(t)
	parsed, err := url.Parse(fake.server.URL)
	if err != nil {
		t.Fatalf("parse fake issuer URL: %v", err)
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split fake issuer host: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("fake issuer host = %q, want 127.0.0.1", host)
	}
}
