package google_test

import (
	"context"
	"crypto"
	"crypto/rand"
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
	"runtime"
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

type issuerHit struct {
	method string
	path   string
	form   url.Values
	user   string
	secret string
	basic  bool
	stack  string
}

type fakeIssuer struct {
	t             *testing.T
	server        *httptest.Server
	key           *rsa.PrivateKey
	mu            sync.Mutex
	discoveryHits int
	failDiscovery bool
	forms         []url.Values
	tokens        map[string]string
	authPath      string
	tokenPath     string
	jwksPath      string
	tokenEndpoint string
	clientID      string
	clientSecret  string
	recordStacks  bool
	hitsLog       []issuerHit
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

	fake := &fakeIssuer{
		t:            t,
		key:          key,
		tokens:       make(map[string]string),
		authPath:     "/authorize",
		tokenPath:    "/token",
		jwksPath:     "/jwks",
		clientID:     "client-id",
		clientSecret: "client-secret",
	}
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

func (f *fakeIssuer) config() (authPath, tokenPath, jwksPath, tokenEndpoint, clientID, clientSecret string, recordStacks bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authPath, f.tokenPath, f.jwksPath, f.tokenEndpoint, f.clientID, f.clientSecret, f.recordStacks
}

func (f *fakeIssuer) logHit(hit issuerHit, recordStacks bool) {
	if recordStacks {
		hit.stack = allStacks()
	}
	f.mu.Lock()
	f.hitsLog = append(f.hitsLog, hit)
	f.mu.Unlock()
}

func (f *fakeIssuer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	authPath, tokenPath, jwksPath, tokenEndpoint, clientID, clientSecret, recordStacks := f.config()
	hit := issuerHit{method: r.Method, path: r.URL.Path}
	if user, pass, ok := r.BasicAuth(); ok {
		hit.user, hit.secret, hit.basic = user, pass, true
	}

	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		f.logHit(hit, recordStacks)
		f.mu.Lock()
		f.discoveryHits++
		fail := f.failDiscovery
		f.mu.Unlock()
		if fail {
			http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
			return
		}
		tokenURL := f.server.URL + tokenPath
		if tokenEndpoint != "" {
			tokenURL = tokenEndpoint
		}
		writeJSON(f.t, w, map[string]any{
			"issuer":                                f.server.URL,
			"authorization_endpoint":                f.server.URL + authPath,
			"token_endpoint":                        tokenURL,
			"jwks_uri":                              f.server.URL + jwksPath,
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case jwksPath:
		f.logHit(hit, recordStacks)
		writeJSON(f.t, w, f.jwks())
	case tokenPath:
		if r.Method != http.MethodPost {
			f.logHit(hit, recordStacks)
			http.Error(w, "token exchange must use POST", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			f.logHit(hit, recordStacks)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		hit.form = cloneValues(r.PostForm)
		f.logHit(hit, recordStacks)
		if !hit.basic || hit.user != clientID || hit.secret != clientSecret {
			http.Error(w, "wrong OAuth client credentials", http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		f.forms = append(f.forms, cloneValues(r.PostForm))
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
		f.logHit(hit, recordStacks)
		http.NotFound(w, r)
	}
}

func (f *fakeIssuer) jwks() map[string]any {
	return map[string]any{"keys": []any{map[string]any{
		"kty": "RSA",
		"kid": "test-key",
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
	}}}
}

func (f *fakeIssuer) issue(code, algorithm string, claims map[string]any) {
	f.t.Helper()
	f.issueKey(code, algorithm, claims, f.key)
}

func (f *fakeIssuer) issueKey(code, algorithm string, claims map[string]any, key *rsa.PrivateKey) {
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
	signature, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, digest[:])
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

func (f *fakeIssuer) setClient(clientID, clientSecret string) {
	f.mu.Lock()
	f.clientID = clientID
	f.clientSecret = clientSecret
	f.mu.Unlock()
}

func (f *fakeIssuer) setPaths(authPath, tokenPath, jwksPath string) {
	f.mu.Lock()
	f.authPath = authPath
	f.tokenPath = tokenPath
	f.jwksPath = jwksPath
	f.mu.Unlock()
}

func (f *fakeIssuer) setTokenEndpoint(endpoint string) {
	f.mu.Lock()
	f.tokenEndpoint = endpoint
	f.mu.Unlock()
}

func (f *fakeIssuer) setRecordStacks(record bool) {
	f.mu.Lock()
	f.recordStacks = record
	f.mu.Unlock()
}

func (f *fakeIssuer) hits() []issuerHit {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]issuerHit, len(f.hitsLog))
	copy(out, f.hitsLog)
	return out
}

func (f *fakeIssuer) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hitsLog)
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for key, values := range v {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func allStacks() string {
	buf := make([]byte, 64*1024)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return string(buf[:n])
		}
		buf = make([]byte, len(buf)*2)
	}
}

func closedLoopbackURL(t *testing.T) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return "http://" + addr
}

func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func hitByPath(hits []issuerHit, path string) (issuerHit, bool) {
	for i := len(hits) - 1; i >= 0; i-- {
		if hits[i].path == path {
			return hits[i], true
		}
	}
	return issuerHit{}, false
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
	(func(func(*googleclient.Client, string, string, string) (string, error)) {})((*googleclient.Client).AuthCodeURL)
	(func(func(*googleclient.Client, context.Context, string, string, string) (googleclient.Claims, error)) {
	})((*googleclient.Client).Exchange)

	fake := newFakeIssuer(t)
	client := googleclient.NewClient("client-id", "client-secret", "example.test", fake.server.URL)

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

func TestClaimsCarryVerifiedIDTokenClaims(t *testing.T) {
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
		if field.Name != want.name || field.Type != want.typ || !field.IsExported() {
			t.Errorf("Claims field %d = %s %v exported %v, want %s %v exported", i, field.Name, field.Type, field.IsExported(), want.name, want.typ)
		}
	}

	fake := newFakeIssuer(t)
	const clientID = "client-i82c"
	fake.setClient(clientID, "secret-i82c")
	client := googleclient.NewClient(clientID, "secret-i82c", "workspace-i82c.example", fake.server.URL)

	withHost := validClaims("https://accounts.google.com")
	withHost["aud"] = clientID
	withHost["sub"] = "subject-alpha"
	withHost["email"] = "alpha@users.example"
	withHost["email_verified"] = false
	withHost["hd"] = "hosted-alpha.example"
	fake.issue("code-alpha", "RS256", withHost)
	got, err := client.Exchange(context.Background(), "code-alpha", "verifier-alpha", "https://auth.i82c.example/callback-alpha")
	if err != nil {
		t.Fatalf("Exchange with hosted domain: %v", err)
	}
	want := googleclient.Claims{
		Issuer:        "https://accounts.google.com",
		Subject:       "subject-alpha",
		Email:         "alpha@users.example",
		EmailVerified: false,
		HostedDomain:  "hosted-alpha.example",
	}
	if got != want {
		t.Fatalf("Claims = %#v, want %#v", got, want)
	}

	withoutHost := validClaims("accounts.google.com")
	withoutHost["aud"] = clientID
	withoutHost["sub"] = "subject-beta"
	withoutHost["email"] = "beta@users.example"
	withoutHost["email_verified"] = true
	delete(withoutHost, "hd")
	fake.issue("code-beta", "RS256", withoutHost)
	got, err = client.Exchange(context.Background(), "code-beta", "verifier-beta", "https://auth.i82c.example/callback-beta")
	if err != nil {
		t.Fatalf("Exchange without hosted domain: %v", err)
	}
	want = googleclient.Claims{
		Issuer:        "accounts.google.com",
		Subject:       "subject-beta",
		Email:         "beta@users.example",
		EmailVerified: true,
		HostedDomain:  "",
	}
	if got != want {
		t.Fatalf("Claims = %#v, want %#v", got, want)
	}
}

func TestAuthCodeURLReturnsRedirectForInputsOrError(t *testing.T) {
	// R-KVOL-GRQ2
	fake := newFakeIssuer(t)
	client := googleclient.NewClient("client-id", "client-secret", "example.test", fake.server.URL)

	state := "login-state-kvol"
	verifier := "verifier-kvol-unique"
	redirectURI := "https://auth.kvol.example/login/google/callback"
	rawURL, err := client.AuthCodeURL(state, verifier, redirectURI)
	if err != nil {
		t.Fatalf("AuthCodeURL: %v", err)
	}
	authURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if authURL.Scheme+"://"+authURL.Host+authURL.Path != fake.server.URL+"/authorize" {
		t.Errorf("authorization URL = %q, want %q", rawURL, fake.server.URL+"/authorize")
	}
	query := authURL.Query()
	if query.Get("state") != state {
		t.Errorf("state = %q, want %q", query.Get("state"), state)
	}
	if query.Get("redirect_uri") != redirectURI {
		t.Errorf("redirect_uri = %q, want %q", query.Get("redirect_uri"), redirectURI)
	}
	if got := query.Get("code_challenge"); got != s256Challenge(verifier) || got == verifier {
		t.Errorf("code_challenge = %q, want S256 of the given verifier", got)
	}
	if query.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", query.Get("code_challenge_method"))
	}

	otherVerifier := "verifier-kvol-other"
	otherURL, err := client.AuthCodeURL("other-state-kvol", otherVerifier, "https://auth.other-kvol.example/callback")
	if err != nil {
		t.Fatalf("second AuthCodeURL: %v", err)
	}
	parsedOther, err := url.Parse(otherURL)
	if err != nil {
		t.Fatalf("parse second authorization URL: %v", err)
	}
	if parsedOther.Query().Get("state") != "other-state-kvol" {
		t.Errorf("second state = %q", parsedOther.Query().Get("state"))
	}
	if parsedOther.Query().Get("redirect_uri") != "https://auth.other-kvol.example/callback" {
		t.Errorf("second redirect_uri = %q", parsedOther.Query().Get("redirect_uri"))
	}
	if parsedOther.Query().Get("code_challenge") != s256Challenge(otherVerifier) {
		t.Errorf("second code_challenge = %q, want S256 of the second verifier", parsedOther.Query().Get("code_challenge"))
	}

	unreachable := googleclient.NewClient("client-id", "client-secret", "example.test", closedLoopbackURL(t))
	if _, err := unreachable.AuthCodeURL(state, verifier, redirectURI); err == nil {
		t.Fatal("AuthCodeURL returned nil error when the authorization URL could not be built")
	}
}

func TestExchangeExchangesCodeVerifierAndRedirect(t *testing.T) {
	// R-FX2G-ZLVJ
	fake := newFakeIssuer(t)
	client := googleclient.NewClient("client-id", "client-secret", "example.test", fake.server.URL)

	code := "code-fx2g"
	verifier := "verifier-fx2g"
	redirectURI := "https://auth.fx2g.example/callback"
	tokenClaims := validClaims("https://accounts.google.com")
	tokenClaims["sub"] = "subject-fx2g"
	tokenClaims["email"] = "fx2g@users.example"
	fake.issue(code, "RS256", tokenClaims)

	claims, err := client.Exchange(context.Background(), code, verifier, redirectURI)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if claims.Subject != "subject-fx2g" || claims.Email != "fx2g@users.example" || claims.Issuer != "https://accounts.google.com" {
		t.Fatalf("Claims = %#v, want the verified token's identity", claims)
	}
	form := fake.lastForm()
	for key, want := range map[string]string{
		"code":          code,
		"code_verifier": verifier,
		"redirect_uri":  redirectURI,
	} {
		if got := form.Get(key); got != want {
			t.Errorf("exchange form %s = %q, want %q", key, got, want)
		}
	}

	tampered := fake.token(code)
	parts := strings.Split(tampered, ".")
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode JWT signature: %v", err)
	}
	signature[0] ^= 0xff
	fake.setToken("code-fx2g-bad", parts[0]+"."+parts[1]+"."+base64.RawURLEncoding.EncodeToString(signature))
	if _, err := client.Exchange(context.Background(), "code-fx2g-bad", verifier, redirectURI); err == nil {
		t.Fatal("Exchange returned claims for an unverified ID token")
	}
	if _, err := client.Exchange(context.Background(), "code-fx2g-missing", verifier, redirectURI); err == nil {
		t.Fatal("Exchange returned nil error when the code was rejected")
	}
}

func TestNewClientDefersDiscoveryAndUsesConstructorInputs(t *testing.T) {
	// R-KWWH-UJGR
	const (
		clientID     = "client-kwwh"
		clientSecret = "secret-kwwh"
		workspace    = "workspace-kwwh.example"
		authPath     = "/oauth2/v2/auth-discovered"
		tokenPath    = "/token-discovered"
		jwksPath     = "/certs-discovered"
	)

	fake := newFakeIssuer(t)
	fake.setClient(clientID, clientSecret)
	fake.setPaths(authPath, tokenPath, jwksPath)
	client := googleclient.NewClient(clientID, clientSecret, workspace, fake.server.URL)
	if fake.requestCount() != 0 || fake.discoveryRequests() != 0 {
		t.Fatalf("requests at construction = %d, discovery = %d, want no issuer contact", fake.requestCount(), fake.discoveryRequests())
	}

	firstRedirect := "https://auth.kwwh.example/callback-one"
	rawURL, err := client.AuthCodeURL("state-kwwh-1", "verifier-kwwh-1", firstRedirect)
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
	if authURL.Scheme+"://"+authURL.Host+authURL.Path != fake.server.URL+authPath {
		t.Errorf("authorization URL = %q, want discovered %s", rawURL, authPath)
	}
	if host, _, err := net.SplitHostPort(authURL.Host); err != nil || host != "127.0.0.1" {
		t.Errorf("authorization host = %q, want loopback issuer", authURL.Host)
	}
	if authURL.Query().Get("client_id") != clientID {
		t.Errorf("client_id = %q, want constructor client id", authURL.Query().Get("client_id"))
	}
	if authURL.Query().Get("hd") != workspace {
		t.Errorf("hd = %q, want constructor workspace domain", authURL.Query().Get("hd"))
	}
	if authURL.Query().Get("redirect_uri") != firstRedirect {
		t.Errorf("redirect_uri = %q, want %q", authURL.Query().Get("redirect_uri"), firstRedirect)
	}

	secondRedirect := "https://auth.kwwh.example/callback-two"
	secondURL, err := client.AuthCodeURL("state-kwwh-2", "verifier-kwwh-2", secondRedirect)
	if err != nil {
		t.Fatalf("second AuthCodeURL: %v", err)
	}
	parsedSecond, err := url.Parse(secondURL)
	if err != nil {
		t.Fatalf("parse second authorization URL: %v", err)
	}
	if parsedSecond.Query().Get("redirect_uri") != secondRedirect {
		t.Errorf("second redirect_uri = %q, want %q", parsedSecond.Query().Get("redirect_uri"), secondRedirect)
	}
	if strings.Contains(secondURL, url.QueryEscape(firstRedirect)) {
		t.Errorf("second authorization URL still carries the first redirect URI: %s", secondURL)
	}

	exchangeOnly := newFakeIssuer(t)
	exchangeOnly.setClient(clientID, clientSecret)
	exchangeOnly.setPaths(authPath, tokenPath, jwksPath)
	exchangeClient := googleclient.NewClient(clientID, clientSecret, workspace, exchangeOnly.server.URL)
	if exchangeOnly.requestCount() != 0 {
		t.Fatalf("requests at second construction = %d, want 0", exchangeOnly.requestCount())
	}
	redirectA := "https://auth.kwwh.example/exchange-a"
	redirectB := "https://auth.kwwh.example/exchange-b"
	claimsA := validClaims("https://accounts.google.com")
	claimsA["aud"] = clientID
	claimsA["sub"] = "subject-kwwh-a"
	exchangeOnly.issue("code-kwwh-a", "RS256", claimsA)
	if _, err := exchangeClient.Exchange(context.Background(), "code-kwwh-a", "verifier-kwwh-a", redirectA); err != nil {
		t.Fatalf("Exchange without a prior AuthCodeURL: %v", err)
	}
	formA := exchangeOnly.lastForm()
	if formA.Get("redirect_uri") != redirectA || formA.Get("code") != "code-kwwh-a" || formA.Get("code_verifier") != "verifier-kwwh-a" {
		t.Fatalf("first exchange form = %v", formA)
	}
	tokenHit, ok := hitByPath(exchangeOnly.hits(), tokenPath)
	if !ok || tokenHit.method != http.MethodPost {
		t.Fatalf("token request = %+v, want POST %s", tokenHit, tokenPath)
	}
	if tokenHit.user != clientID || tokenHit.secret != clientSecret {
		t.Fatalf("token credentials = %q %q, want constructor client id and secret", tokenHit.user, tokenHit.secret)
	}
	if _, ok := hitByPath(exchangeOnly.hits(), jwksPath); !ok {
		t.Fatal("Exchange did not fetch JWKS from the discovered jwks_uri")
	}
	if _, ok := hitByPath(exchangeOnly.hits(), "/jwks"); ok {
		t.Fatal("Exchange fetched a guessed /jwks path instead of the discovered jwks_uri")
	}
	if _, ok := hitByPath(exchangeOnly.hits(), "/token"); ok {
		t.Fatal("Exchange posted to a guessed /token path instead of the discovered token_endpoint")
	}

	claimsB := validClaims("accounts.google.com")
	claimsB["aud"] = clientID
	claimsB["sub"] = "subject-kwwh-b"
	exchangeOnly.issue("code-kwwh-b", "RS256", claimsB)
	if _, err := exchangeClient.Exchange(context.Background(), "code-kwwh-b", "verifier-kwwh-b", redirectB); err != nil {
		t.Fatalf("second Exchange: %v", err)
	}
	formB := exchangeOnly.lastForm()
	if formB.Get("redirect_uri") != redirectB {
		t.Errorf("second exchange redirect_uri = %q, want %q", formB.Get("redirect_uri"), redirectB)
	}

	retryingIssuer := newFakeIssuer(t)
	retryingIssuer.setPaths(authPath, tokenPath, jwksPath)
	retrying := googleclient.NewClient(clientID, clientSecret, workspace, retryingIssuer.server.URL)
	retryingIssuer.setDiscoveryFailure(true)
	if _, err := retrying.AuthCodeURL("state-kwwh", "verifier-kwwh", firstRedirect); err == nil {
		t.Fatal("AuthCodeURL cached nothing and still returned nil error for a failed discovery")
	}
	if retryingIssuer.discoveryRequests() != 1 {
		t.Fatalf("discovery requests after failed AuthCodeURL = %d, want 1", retryingIssuer.discoveryRequests())
	}
	if _, err := retrying.Exchange(context.Background(), "code-kwwh", "verifier-kwwh", firstRedirect); err == nil {
		t.Fatal("Exchange reused a failed discovery")
	}
	if retryingIssuer.discoveryRequests() != 2 {
		t.Fatalf("discovery requests after failed Exchange = %d, want 2", retryingIssuer.discoveryRequests())
	}
	retryingIssuer.setClient(clientID, clientSecret)
	retryingIssuer.setDiscoveryFailure(false)
	recovered, err := retrying.AuthCodeURL("state-kwwh", "verifier-kwwh", secondRedirect)
	if err != nil {
		t.Fatalf("AuthCodeURL after discovery recovery: %v", err)
	}
	parsedRecovered, err := url.Parse(recovered)
	if err != nil {
		t.Fatalf("parse recovered authorization URL: %v", err)
	}
	if parsedRecovered.Scheme+"://"+parsedRecovered.Host+parsedRecovered.Path != retryingIssuer.server.URL+authPath {
		t.Errorf("recovered authorization URL = %q, want discovered endpoint", recovered)
	}
	if retryingIssuer.discoveryRequests() != 3 {
		t.Fatalf("discovery requests after recovery = %d, want 3", retryingIssuer.discoveryRequests())
	}
}

func TestExchangeUsesOAuth2AndOIDCLibraries(t *testing.T) {
	// R-IFDR-6B5D
	fake := newFakeIssuer(t)
	fake.setRecordStacks(true)
	client := googleclient.NewClient("client-id", "client-secret", "example.test", fake.server.URL)
	fake.issue("code-ifdr", "RS256", validClaims("https://accounts.google.com"))
	if _, err := client.Exchange(context.Background(), "code-ifdr", "verifier-ifdr", "https://auth.ifdr.example/callback"); err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	tokenHit, ok := hitByPath(fake.hits(), "/token")
	if !ok {
		t.Fatal("token endpoint received no request")
	}
	if !strings.Contains(tokenHit.stack, "golang.org/x/oauth2") {
		t.Fatalf("token exchange stack does not include golang.org/x/oauth2\n%s", tokenHit.stack)
	}
	jwksHit, ok := hitByPath(fake.hits(), "/jwks")
	if !ok {
		t.Fatal("JWKS endpoint received no request")
	}
	if !strings.Contains(jwksHit.stack, "github.com/coreos/go-oidc/v3") {
		t.Fatalf("ID token verification stack does not include github.com/coreos/go-oidc/v3\n%s", jwksHit.stack)
	}
}

func TestExchangePostsToDiscoveredTokenEndpointAndVerifiesRS256(t *testing.T) {
	// R-G0Q6-4X3M
	const (
		clientID  = "client-g0q6"
		secret    = "secret-g0q6"
		tokenPath = "/token-g0q6"
		jwksPath  = "/certs-g0q6"
	)
	fake := newFakeIssuer(t)
	fake.setClient(clientID, secret)
	fake.setPaths("/authorize-g0q6", tokenPath, jwksPath)
	client := googleclient.NewClient(clientID, secret, "workspace-g0q6.example", fake.server.URL)

	tests := []struct {
		name         string
		code         string
		verifier     string
		redirectURI  string
		issuer       string
		hostedDomain string
		subject      string
		email        string
		verified     bool
	}{
		{
			name:         "https issuer",
			code:         "code-g0q6-https",
			verifier:     "verifier-g0q6-https",
			redirectURI:  "https://auth.g0q6.example/callback-https",
			issuer:       "https://accounts.google.com",
			hostedDomain: "hosted-g0q6.example",
			subject:      "subject-g0q6-https",
			email:        "https-user@g0q6.example",
			verified:     true,
		},
		{
			name:        "legacy issuer",
			code:        "code-g0q6-legacy",
			verifier:    "verifier-g0q6-legacy",
			redirectURI: "https://auth.g0q6.example/callback-legacy",
			issuer:      "accounts.google.com",
			subject:     "subject-g0q6-legacy",
			email:       "legacy-user@g0q6.example",
			verified:    false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokenClaims := validClaims(test.issuer)
			tokenClaims["aud"] = clientID
			tokenClaims["sub"] = test.subject
			tokenClaims["email"] = test.email
			tokenClaims["email_verified"] = test.verified
			if test.hostedDomain == "" {
				delete(tokenClaims, "hd")
			} else {
				tokenClaims["hd"] = test.hostedDomain
			}
			fake.issue(test.code, "RS256", tokenClaims)

			claims, err := client.Exchange(context.Background(), test.code, test.verifier, test.redirectURI)
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			want := googleclient.Claims{
				Issuer:        test.issuer,
				Subject:       test.subject,
				Email:         test.email,
				EmailVerified: test.verified,
				HostedDomain:  test.hostedDomain,
			}
			if claims != want {
				t.Fatalf("Claims = %#v, want %#v", claims, want)
			}
			hit, ok := hitByPath(fake.hits(), tokenPath)
			if !ok {
				t.Fatal("discovered token endpoint received no request")
			}
			if hit.method != http.MethodPost {
				t.Fatalf("token method = %s, want POST", hit.method)
			}
			for key, value := range map[string]string{
				"code":          test.code,
				"code_verifier": test.verifier,
				"redirect_uri":  test.redirectURI,
			} {
				if got := hit.form.Get(key); got != value {
					t.Errorf("token form %s = %q, want %q", key, got, value)
				}
			}
		})
	}
	if _, ok := hitByPath(fake.hits(), jwksPath); !ok {
		t.Fatal("Exchange did not verify against the discovered JWKS")
	}
	if _, ok := hitByPath(fake.hits(), "/jwks"); ok {
		t.Fatal("Exchange used a guessed JWKS path")
	}
	if _, ok := hitByPath(fake.hits(), "/token"); ok {
		t.Fatal("Exchange posted to a guessed token path")
	}

	badClaims := validClaims("https://accounts.google.com")
	badClaims["aud"] = clientID
	fake.issue("code-g0q6-bad-sig", "RS256", badClaims)
	token := fake.token("code-g0q6-bad-sig")
	parts := strings.Split(token, ".")
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode JWT signature: %v", err)
	}
	signature[0] ^= 0xff
	fake.setToken("code-g0q6-bad-sig", parts[0]+"."+parts[1]+"."+base64.RawURLEncoding.EncodeToString(signature))
	if _, err := client.Exchange(context.Background(), "code-g0q6-bad-sig", "verifier", "https://auth.g0q6.example/callback"); err == nil {
		t.Fatal("Exchange accepted an ID token whose RS256 signature does not match the discovered JWKS")
	}

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	fake.issueKey("code-g0q6-other-key", "RS256", badClaims, otherKey)
	if _, err := client.Exchange(context.Background(), "code-g0q6-other-key", "verifier", "https://auth.g0q6.example/callback"); err == nil {
		t.Fatal("Exchange accepted an ID token signed by a key that is not in the discovered JWKS")
	}

	fake.issue("code-g0q6-hs256", "HS256", badClaims)
	if _, err := client.Exchange(context.Background(), "code-g0q6-hs256", "verifier", "https://auth.g0q6.example/callback"); err == nil {
		t.Fatal("Exchange accepted an ID token that is not RS256")
	}

	if _, err := client.Exchange(context.Background(), "code-g0q6-rejected", "verifier", "https://auth.g0q6.example/callback"); err == nil {
		t.Fatal("Exchange returned nil error for a failed token exchange")
	}

	unreachable := newFakeIssuer(t)
	unreachable.setClient(clientID, secret)
	unreachable.setTokenEndpoint(closedLoopbackURL(t) + "/token")
	unreachableClaims := validClaims("https://accounts.google.com")
	unreachableClaims["aud"] = clientID
	unreachable.issue("code-g0q6-unreachable", "RS256", unreachableClaims)
	unreachableClient := googleclient.NewClient(clientID, secret, "workspace-g0q6.example", unreachable.server.URL)
	if _, err := unreachableClient.Exchange(context.Background(), "code-g0q6-unreachable", "verifier", "https://auth.g0q6.example/callback"); err == nil {
		t.Fatal("Exchange returned nil error when the discovered token endpoint was unreachable")
	}
	if unreachable.discoveryRequests() == 0 {
		t.Fatal("Exchange failed before discovering the token endpoint")
	}
	if _, ok := hitByPath(unreachable.hits(), "/token"); ok {
		t.Fatal("Exchange posted to the issuer origin instead of the discovered token endpoint")
	}
}
