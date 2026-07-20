package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"dashboard/internal/oauth"
	"dashboard/internal/oauthstate"
)

// ── harness ─────────────────────────────────────────────────────────────────

// clientRedirectURI is the registered redirect target the MCP client is sent
// back to after federation; the host is unroutable so a stray real request fails.
const clientRedirectURI = "https://client.example/callback"

// newOAuthTest builds a live httptest server over a fresh shared-db deps set and
// a redirect-halting client with a cookie jar (so the authorize→callback binding
// cookie round-trips like a browser's). It returns the deps too so tests can
// inspect persisted state directly.
func newOAuthTest(t *testing.T) (*httptest.Server, serverDeps, *http.Client) {
	t.Helper()
	d := newServerDeps(t)
	srv, err := New(d.opts())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return ts, d, client
}

// s256 is the PKCE S256 transform: base64url(sha256(verifier)).
func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// pkceVerifier is a fixed, spec-valid (43-char, unreserved) verifier; pkceChallenge
// is its S256 challenge, recomputed so the pair can never drift.
const pkceVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

func pkceChallenge() string { return s256(pkceVerifier) }

// oauthErr decodes an RFC 6749 error body and returns its "error" code.
func oauthErr(t *testing.T, resp *http.Response) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode oauth error: %v", err)
	}
	return body.Error
}

// registerClient drives DCR for clientRedirectURI and returns the issued client_id.
func registerClient(t *testing.T, ts *httptest.Server, client *http.Client) string {
	t.Helper()
	body, _ := json.Marshal(dcrRequest{RedirectURIs: []string{clientRedirectURI}, ClientName: "Test Client"})
	resp, err := client.Post(ts.URL+"/oauth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want 201", resp.StatusCode)
	}
	var out dcrResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode dcr response: %v", err)
	}
	if out.ClientID == "" {
		t.Fatal("register returned empty client_id")
	}
	return out.ClientID
}

// authorizeURL builds an /oauth/authorize query with the given overrides applied
// to an otherwise-valid parameter set.
func authorizeURL(ts *httptest.Server, clientID string, override map[string]string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {clientRedirectURI},
		"code_challenge":        {pkceChallenge()},
		"code_challenge_method": {"S256"},
		"state":                 {"client-state-xyz"},
		"resource":              {testResource},
	}
	for k, v := range override {
		if v == "" {
			q.Del(k)
		} else {
			q.Set(k, v)
		}
	}
	return ts.URL + "/oauth/authorize?" + q.Encode()
}

// obtainCode runs register → authorize → federation callback and returns the
// authorization code handed back to the client (plus the client_id). The cookie
// jar carries the binding cookie from authorize into the callback.
func obtainCode(t *testing.T, ts *httptest.Server, client *http.Client) (clientID, code string) {
	t.Helper()
	clientID = registerClient(t, ts, client)

	resp, err := client.Get(authorizeURL(ts, clientID, map[string]string{"provider": "google"}))
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("authorize status = %d, want 303", resp.StatusCode)
	}
	idpLoc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("authorize Location unparseable: %v", err)
	}
	handshakeID := idpLoc.Query().Get("state")
	if handshakeID == "" {
		t.Fatal("authorize redirect carries no state")
	}

	// Simulate the IdP returning the browser to our callback (the stub ignores the
	// code and returns a canned verified Workspace identity).
	cbURL := ts.URL + "/oauth/google/callback?" + url.Values{"state": {handshakeID}, "code": {"stub-auth-code"}}.Encode()
	resp, err = client.Get(cbURL)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("callback status = %d, want 303", resp.StatusCode)
	}
	back, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("callback Location unparseable: %v", err)
	}
	if got := back.Scheme + "://" + back.Host + back.Path; got != clientRedirectURI {
		t.Fatalf("callback redirected to %q, want %q", got, clientRedirectURI)
	}
	if st := back.Query().Get("state"); st != "client-state-xyz" {
		t.Errorf("returned state = %q, want client-state-xyz", st)
	}
	code = back.Query().Get("code")
	if code == "" {
		t.Fatal("callback returned no authorization code")
	}
	return clientID, code
}

// redeemCode exchanges an authorization code for a token pair via /oauth/token.
func redeemCode(t *testing.T, ts *httptest.Server, client *http.Client, clientID, code string) tokenResponse {
	t.Helper()
	resp, err := client.PostForm(ts.URL+"/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {clientRedirectURI},
		"code_verifier": {pkceVerifier},
	})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d, want 200 (err=%q)", resp.StatusCode, oauthErr(t, resp))
	}
	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	return tok
}

// ── metadata ────────────────────────────────────────────────────────────────

func TestASMetadata(t *testing.T) {
	ts, _, client := newOAuthTest(t)
	resp, err := client.Get(ts.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc["issuer"] != "https://int.ikigenba.com" {
		t.Errorf("issuer = %v, want https://int.ikigenba.com", doc["issuer"])
	}
	if doc["token_endpoint"] != "https://int.ikigenba.com/oauth/token" {
		t.Errorf("token_endpoint = %v", doc["token_endpoint"])
	}
	if methods, ok := doc["code_challenge_methods_supported"].([]any); !ok || len(methods) != 1 || methods[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v, want [S256]", doc["code_challenge_methods_supported"])
	}
}

// ── dynamic client registration ─────────────────────────────────────────────

func TestDCRRegisterSuccess(t *testing.T) {
	ts, _, client := newOAuthTest(t)
	body, _ := json.Marshal(dcrRequest{RedirectURIs: []string{clientRedirectURI}, ClientName: "Test Client"})
	resp, err := client.Post(ts.URL+"/oauth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var out dcrResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ClientID == "" {
		t.Error("empty client_id")
	}
	if out.TokenEndpointAuthMethod != "none" {
		t.Errorf("token_endpoint_auth_method = %q, want none", out.TokenEndpointAuthMethod)
	}
	if len(out.RedirectURIs) != 1 || out.RedirectURIs[0] != clientRedirectURI {
		t.Errorf("redirect_uris = %v, want [%s]", out.RedirectURIs, clientRedirectURI)
	}
}

func TestDCRRegisterRejections(t *testing.T) {
	ts, _, client := newOAuthTest(t)
	cases := []struct {
		name     string
		body     string
		wantCode string
	}{
		{"malformed json", `{`, "invalid_client_metadata"},
		{"no redirect uris", `{"redirect_uris":[]}`, "invalid_redirect_uri"},
		{"relative redirect", `{"redirect_uris":["/cb"]}`, "invalid_redirect_uri"},
		{"redirect with fragment", `{"redirect_uris":["https://x.example/cb#frag"]}`, "invalid_redirect_uri"},
		{"non-http scheme", `{"redirect_uris":["ftp://x.example/cb"]}`, "invalid_redirect_uri"},
		{"confidential client", `{"redirect_uris":["https://x.example/cb"],"token_endpoint_auth_method":"client_secret_basic"}`, "invalid_client_metadata"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("register: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			if got := oauthErr(t, resp); got != tc.wantCode {
				t.Errorf("error = %q, want %q", got, tc.wantCode)
			}
		})
	}
}

// ── authorize ───────────────────────────────────────────────────────────────

func TestAuthorizeRejections(t *testing.T) {
	ts, _, client := newOAuthTest(t)
	clientID := registerClient(t, ts, client)
	cases := []struct {
		name     string
		override map[string]string
		wantCode string
	}{
		{"bad response_type", map[string]string{"response_type": "token"}, "unsupported_response_type"},
		{"missing challenge", map[string]string{"code_challenge": ""}, "invalid_request"},
		{"bad challenge method", map[string]string{"code_challenge_method": "plain"}, "invalid_request"},
		{"missing resource", map[string]string{"resource": ""}, "invalid_target"},
		{"unknown resource", map[string]string{"resource": "https://other.example/mcp"}, "invalid_target"},
		{"unknown client", map[string]string{"client_id": "ms_dcr_nope"}, "invalid_client"},
		{"redirect mismatch", map[string]string{"redirect_uri": "https://client.example/evil"}, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.Get(authorizeURL(ts, clientID, tc.override))
			if err != nil {
				t.Fatalf("authorize: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			if got := oauthErr(t, resp); got != tc.wantCode {
				t.Errorf("error = %q, want %q", got, tc.wantCode)
			}
		})
	}
}

func TestAuthorizeHappyRedirectsAndBindsBrowser(t *testing.T) {
	ts, _, client := newOAuthTest(t)
	clientID := registerClient(t, ts, client)
	resp, err := client.Get(authorizeURL(ts, clientID, map[string]string{"provider": "google"}))
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("Location unparseable: %v", err)
	}
	if loc.Host != "idp.stub.invalid" {
		t.Errorf("redirected to %q, want the stub IdP", loc.Host)
	}
	var bound bool
	for _, c := range resp.Cookies() {
		if c.Name == bindingCookieName {
			bound = true
		}
	}
	if !bound {
		t.Errorf("no %s binding cookie set", bindingCookieName)
	}
}

// R-IWCE-MR51
func TestAuthorizeChooserPreservesRequestWithoutMintingState(t *testing.T) {
	ts, d, client := newOAuthTest(t)
	clientID := registerClient(t, ts, client)
	target := authorizeURL(ts, clientID, map[string]string{"state": "client state & résumé"})

	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", got)
	}
	if got := resp.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("Set-Cookie = %v, want none", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read chooser: %v", err)
	}
	anchors := regexp.MustCompile(`<a\s+[^>]*href="([^"]+)"[^>]*>`).FindAllStringSubmatch(string(body), -1)
	if len(anchors) != 2 {
		t.Fatalf("chooser anchors = %d, want exactly 2; body:\n%s", len(anchors), body)
	}
	original, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	for i, provider := range []string{oauthstate.ProviderGoogle, oauthstate.ProviderGitHub} {
		wantQuery := original.Query()
		wantQuery.Set("provider", provider)
		want := "/oauth/authorize?" + wantQuery.Encode()
		if got := html.UnescapeString(anchors[i][1]); got != want {
			t.Errorf("%s chooser href = %q, want %q", provider, got, want)
		}
	}
	var count int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM oauth_state`).Scan(&count); err != nil {
		t.Fatalf("count handshakes: %v", err)
	}
	if count != 0 {
		t.Errorf("handshake rows = %d, want 0", count)
	}
}

// R-IXKB-0IVQ
func TestAuthorizeGoogleMintsBoundHandshakeAndRedirects(t *testing.T) {
	ts, d, client := newOAuthTest(t)
	clientID := registerClient(t, ts, client)
	resp, err := client.Get(authorizeURL(ts, clientID, map[string]string{"provider": "google"}))
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if bindingCookieFromResponse(resp) == nil {
		t.Fatal("authorize set no binding cookie")
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if location.Host != "idp.stub.invalid" {
		t.Errorf("Location host = %q, want Google stub", location.Host)
	}
	id, provider := onlyMCPHandshake(t, d)
	if provider != oauthstate.ProviderGoogle {
		t.Errorf("handshake provider = %q, want google", provider)
	}
	if got := location.Query().Get("state"); got != id {
		t.Errorf("redirect state = %q, want handshake id %q", got, id)
	}
}

// R-IYS7-EAMF
func TestAuthorizeGitHubMintsBoundHandshakeAndRedirects(t *testing.T) {
	ts, d, client := newOAuthTest(t)
	clientID := registerClient(t, ts, client)
	resp, err := client.Get(authorizeURL(ts, clientID, map[string]string{"provider": "github"}))
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if bindingCookieFromResponse(resp) == nil {
		t.Fatal("authorize set no binding cookie")
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if location.Host != "github.stub.invalid" {
		t.Errorf("Location host = %q, want GitHub stub", location.Host)
	}
	id, provider := onlyMCPHandshake(t, d)
	if provider != oauthstate.ProviderGitHub {
		t.Errorf("handshake provider = %q, want github", provider)
	}
	if got := location.Query().Get("state"); got != id {
		t.Errorf("redirect state = %q, want handshake id %q", got, id)
	}
	if got := location.Query().Get("redirect_uri"); !strings.HasSuffix(got, "/oauth/github/callback") {
		t.Errorf("redirect_uri = %q, want GitHub callback", got)
	}
}

// R-J003-S2D4
func TestAuthorizeProviderValidationFollowsRequestValidation(t *testing.T) {
	ts, _, client := newOAuthTest(t)
	clientID := registerClient(t, ts, client)
	tests := []struct {
		name     string
		override map[string]string
	}{
		{name: "unknown provider", override: map[string]string{"provider": "gitlab"}},
		{name: "request error precedes chooser", override: map[string]string{"code_challenge": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Get(authorizeURL(ts, clientID, tt.override))
			if err != nil {
				t.Fatalf("authorize: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			if got := oauthErr(t, resp); got != "invalid_request" {
				t.Errorf("error = %q, want invalid_request", got)
			}
		})
	}
}

func bindingCookieFromResponse(resp *http.Response) *http.Cookie {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == bindingCookieName {
			return cookie
		}
	}
	return nil
}

func onlyMCPHandshake(t *testing.T, d serverDeps) (id, provider string) {
	t.Helper()
	var count int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM oauth_state`).Scan(&count); err != nil {
		t.Fatalf("count handshakes: %v", err)
	}
	if count != 1 {
		t.Fatalf("handshake rows = %d, want 1", count)
	}
	if err := d.db.QueryRow(`SELECT id, provider FROM oauth_state`).Scan(&id, &provider); err != nil {
		t.Fatalf("read handshake: %v", err)
	}
	return id, provider
}

// ── full flow ───────────────────────────────────────────────────────────────

// TestFullFlow exercises register → authorize → callback → token → introspect →
// refresh → refresh-replay, asserting the replay cascade-revokes the whole chain.
func TestFullFlow(t *testing.T) {
	ts, d, client := newOAuthTest(t)
	ctx := context.Background()

	clientID, code := obtainCode(t, ts, client)
	tok := redeemCode(t, ts, client, clientID, code)

	if !strings.HasPrefix(tok.AccessToken, oauth.AccessPrefix) {
		t.Errorf("access token %q lacks prefix %q", tok.AccessToken, oauth.AccessPrefix)
	}
	if !strings.HasPrefix(tok.RefreshToken, oauth.RefreshPrefix) {
		t.Errorf("refresh token %q lacks prefix %q", tok.RefreshToken, oauth.RefreshPrefix)
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", tok.TokenType)
	}

	// Introspect the access token, authenticated with itself.
	intro := introspect(t, ts, client, tok.AccessToken, tok.AccessToken)
	if !intro.Active {
		t.Fatal("freshly issued token introspects as inactive")
	}
	if intro.Username != "owner@int.ikigenba.com" {
		t.Errorf("introspect username = %q, want owner@int.ikigenba.com", intro.Username)
	}
	if intro.Resource != testResource {
		t.Errorf("introspect resource = %q, want %q", intro.Resource, testResource)
	}

	// Rotate the refresh token.
	resp, err := client.PostForm(ts.URL+"/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
		"client_id":     {clientID},
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200 (err=%q)", resp.StatusCode, oauthErr(t, resp))
	}
	var rotated tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&rotated); err != nil {
		t.Fatalf("decode rotated: %v", err)
	}
	resp.Body.Close()
	if rotated.RefreshToken == tok.RefreshToken {
		t.Error("refresh token was not rotated")
	}

	// Replaying the now-used refresh token detects reuse and revokes the chain.
	resp, err = client.PostForm(ts.URL+"/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
		"client_id":     {clientID},
	})
	if err != nil {
		t.Fatalf("refresh replay: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("replay status = %d, want 400", resp.StatusCode)
	}
	if got := oauthErr(t, resp); got != "invalid_grant" {
		t.Errorf("replay error = %q, want invalid_grant", got)
	}
	resp.Body.Close()

	// The chain is revoked, so the rotated access token no longer validates.
	if _, err := d.tokens.ValidateAccess(ctx, rotated.AccessToken); err == nil {
		t.Error("rotated access token still valid after reuse-triggered chain revocation")
	}
}

// introspect calls /oauth/introspect with callerToken as the bearer and target as
// the inspected token, returning the decoded response.
func introspect(t *testing.T, ts *httptest.Server, client *http.Client, callerToken, target string) introspectResponse {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+"/oauth/introspect", strings.NewReader(url.Values{"token": {target}}.Encode()))
	if err != nil {
		t.Fatalf("introspect req: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+callerToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("introspect status = %d, want 200", resp.StatusCode)
	}
	var out introspectResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode introspect: %v", err)
	}
	return out
}

func TestIntrospectRequiresCallerBearer(t *testing.T) {
	ts, _, client := newOAuthTest(t)
	resp, err := client.PostForm(ts.URL+"/oauth/introspect", url.Values{"token": {"ms_oat_whatever"}})
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (no bearer)", resp.StatusCode)
	}
}

// ── token-grant edge cases ──────────────────────────────────────────────────

func TestTokenPKCEMismatch(t *testing.T) {
	ts, d, client := newOAuthTest(t)
	ctx := context.Background()
	ensureTestIdentity(t, d, "owner-test", "owner@int.ikigenba.com")
	plaintext, _, err := d.codes.Issue(ctx, oauth.IssueParams{
		ClientID:            "client-x",
		OwnerEmail:          "owner@int.ikigenba.com",
		OwnerID:             "owner-test",
		CodeChallenge:       pkceChallenge(),
		CodeChallengeMethod: "S256",
		RedirectURI:         clientRedirectURI,
		Resource:            testResource,
	})
	if err != nil {
		t.Fatalf("seed authcode: %v", err)
	}
	resp, err := client.PostForm(ts.URL+"/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {plaintext},
		"client_id":     {"client-x"},
		"redirect_uri":  {clientRedirectURI},
		"code_verifier": {"a-different-verifier-that-will-not-match-000"},
	})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if got := oauthErr(t, resp); got != "invalid_grant" {
		t.Errorf("error = %q, want invalid_grant", got)
	}
}

func TestTokenExpiredCode(t *testing.T) {
	ts, d, client := newOAuthTest(t)
	ctx := context.Background()
	ensureTestIdentity(t, d, "owner-test", "owner@int.ikigenba.com")
	// A code store with a negative TTL persists an already-expired code; the row
	// lives in the shared db, so the server's token handler looks it up and rejects.
	expired := oauth.NewAuthCodeStore(d.db, -time.Minute)
	plaintext, _, err := expired.Issue(ctx, oauth.IssueParams{
		ClientID:            "client-x",
		OwnerEmail:          "owner@int.ikigenba.com",
		OwnerID:             "owner-test",
		CodeChallenge:       pkceChallenge(),
		CodeChallengeMethod: "S256",
		RedirectURI:         clientRedirectURI,
		Resource:            testResource,
	})
	if err != nil {
		t.Fatalf("seed expired authcode: %v", err)
	}
	resp, err := client.PostForm(ts.URL+"/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {plaintext},
		"client_id":     {"client-x"},
		"redirect_uri":  {clientRedirectURI},
		"code_verifier": {pkceVerifier},
	})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if got := oauthErr(t, resp); got != "invalid_grant" {
		t.Errorf("error = %q, want invalid_grant", got)
	}
}

func TestTokenUnsupportedGrant(t *testing.T) {
	ts, _, client := newOAuthTest(t)
	resp, err := client.PostForm(ts.URL+"/oauth/token", url.Values{"grant_type": {"password"}})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if got := oauthErr(t, resp); got != "unsupported_grant_type" {
		t.Errorf("error = %q, want unsupported_grant_type", got)
	}
}

// ── revoke ──────────────────────────────────────────────────────────────────

func TestRevoke(t *testing.T) {
	ts, d, client := newOAuthTest(t)
	ctx := context.Background()
	clientID, code := obtainCode(t, ts, client)
	tok := redeemCode(t, ts, client, clientID, code)

	if _, err := d.tokens.ValidateAccess(ctx, tok.AccessToken); err != nil {
		t.Fatalf("token invalid before revoke: %v", err)
	}

	resp, err := client.PostForm(ts.URL+"/oauth/revoke", url.Values{"token": {tok.AccessToken}})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200", resp.StatusCode)
	}
	if _, err := d.tokens.ValidateAccess(ctx, tok.AccessToken); err == nil {
		t.Error("access token still valid after revoke")
	}
}

func TestRevokeUnknownTokenIs200(t *testing.T) {
	ts, _, client := newOAuthTest(t)
	resp, err := client.PostForm(ts.URL+"/oauth/revoke", url.Values{"token": {"ms_oat_does-not-exist"}})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 for unknown token", resp.StatusCode)
	}
}
