package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"dashboard/internal/githubidp"
	"dashboard/internal/googleidp"
)

// loginServer builds a server backed by one shared temp SQLite file, returning
// the raw *sql.DB too so a test can inspect persisted rows.
func loginServer(t *testing.T) (*http.Server, *sql.DB) {
	t.Helper()
	d := newServerDeps(t)
	srv, err := New(d.opts())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv, d.db
}

func loginServerWithProviders(t *testing.T, google googleidp.Provider, github githubidp.Provider) (*http.Server, *sql.DB) {
	t.Helper()
	d := newServerDeps(t)
	opts := d.opts()
	opts.IDPProvider = google
	opts.GithubProvider = github
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv, d.db
}

func bindingCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == bindingCookieName {
			return cookie
		}
	}
	return nil
}

// R-IGHP-NQI0
func TestLoginChooserRendersProvidersWithoutMintingHandshake(t *testing.T) {
	srv, database := loginServer(t)

	assertChooser := func(target, googleHref, githubHref string) {
		t.Helper()
		rec := do(t, srv, "GET", target, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		googleAnchor := `<a href="` + googleHref + `" class="btn btn-primary btn-lg btn-accent-link">Sign in with Google</a>`
		githubAnchor := `<a href="` + githubHref + `" class="btn btn-primary btn-lg btn-accent-link">Sign in with GitHub</a>`
		if strings.Count(body, googleAnchor) != 1 || strings.Count(body, githubAnchor) != 1 || strings.Count(body, `class="btn btn-primary btn-lg btn-accent-link"`) != 2 {
			t.Errorf("chooser does not contain exactly the expected provider anchors:\n%s", body)
		}
		if bindingCookie(rec) != nil {
			t.Error("chooser set a binding cookie")
		}
	}

	assertChooser("https://int.ikigenba.com/login", "/login/google", "/login/github")
	const returnTo = "/srv/sites/private/test07/"
	encoded := url.QueryEscape(returnTo)
	assertChooser("https://int.ikigenba.com/login?return_to="+encoded, "/login/google?return_to="+encoded, "/login/github?return_to="+encoded)
	assertChooser("https://int.ikigenba.com/login?return_to="+url.QueryEscape("//evil.com"), "/login/google", "/login/github")

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM oauth_state`).Scan(&count); err != nil {
		t.Fatalf("count oauth_state: %v", err)
	}
	if count != 0 {
		t.Errorf("chooser minted %d handshakes, want 0", count)
	}
}

// R-IHPM-1I8P
func TestLoginGoogleMintsProviderBoundHandshake(t *testing.T) {
	google := googleidp.New(googleidp.Credentials{ClientID: "client", ClientSecret: "secret", WorkspaceDomain: testWorkspaceDomain})
	srv, database := loginServerWithProviders(t, google, githubidp.NewStub())
	const returnTo = "/srv/wiki/"
	rec := do(t, srv, "GET", "https://int.ikigenba.com/login/google?return_to="+url.QueryEscape(returnTo), nil)
	if rec.Code != http.StatusFound || bindingCookie(rec) == nil {
		t.Fatalf("status/cookie = %d/%v, want 302 and binding cookie", rec.Code, bindingCookie(rec) != nil)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := location.Scheme + "://" + location.Host + location.Path; got != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Errorf("authorize endpoint = %q", got)
	}
	var id, provider, storedReturnTo string
	if err := database.QueryRow(`SELECT id, provider, return_to FROM oauth_state`).Scan(&id, &provider, &storedReturnTo); err != nil {
		t.Fatalf("read handshake: %v", err)
	}
	if provider != "google" || storedReturnTo != returnTo || location.Query().Get("state") != id {
		t.Errorf("handshake = provider %q return_to %q id %q; redirect state %q", provider, storedReturnTo, id, location.Query().Get("state"))
	}
}

// R-IIXI-F9ZE
func TestLoginGitHubMintsProviderBoundHandshake(t *testing.T) {
	github := githubidp.New(githubidp.Credentials{ClientID: "client", ClientSecret: "secret", Org: "ikigenba"})
	srv, database := loginServerWithProviders(t, googleidp.NewStub(), github)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/login/github", nil)
	if rec.Code != http.StatusFound || bindingCookie(rec) == nil {
		t.Fatalf("status/cookie = %d/%v, want 302 and binding cookie", rec.Code, bindingCookie(rec) != nil)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := location.Scheme + "://" + location.Host + location.Path; got != "https://github.com/login/oauth/authorize" {
		t.Errorf("authorize endpoint = %q", got)
	}
	var id, provider string
	if err := database.QueryRow(`SELECT id, provider FROM oauth_state`).Scan(&id, &provider); err != nil {
		t.Fatalf("read handshake: %v", err)
	}
	if provider != "github" || location.Query().Get("state") != id {
		t.Errorf("handshake provider/state = %q/%q, redirect state %q", provider, id, location.Query().Get("state"))
	}
	if got := location.Query().Get("redirect_uri"); !strings.HasSuffix(got, "/oauth/github/callback") {
		t.Errorf("redirect_uri = %q, want GitHub callback", got)
	}
}

// R-IK5E-T1Q3
func TestLoginGitHubRejectsHostileReturnTo(t *testing.T) {
	for _, hostile := range []string{"//evil.com", "https://evil.com/x"} {
		t.Run(hostile, func(t *testing.T) {
			srv, database := loginServer(t)
			rec := do(t, srv, "GET", "https://int.ikigenba.com/login/github?return_to="+url.QueryEscape(hostile), nil)
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}
			var provider, returnTo string
			if err := database.QueryRow(`SELECT provider, return_to FROM oauth_state`).Scan(&provider, &returnTo); err != nil {
				t.Fatalf("read handshake: %v", err)
			}
			if provider != "github" || returnTo != "" {
				t.Errorf("provider/return_to = %q/%q, want github/empty", provider, returnTo)
			}
		})
	}
}

// TestLoginPersistsHandshakeAndBindsBrowser is the end-to-end contract for the
// start of the login flow: GET /login redirects to the IdP carrying a state, sets
// the plaintext binding cookie on the browser, and persists exactly one handshake
// whose stored id matches the state and whose stored hash is the SHA-256 of the
// cookie — never the cookie itself.
func TestLoginPersistsHandshakeAndBindsBrowser(t *testing.T) {
	srv, database := loginServer(t)
	rec := do(t, srv, "GET", "http://int.ikigenba.com/login/google", nil)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}

	// State sent to the IdP.
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location unparseable: %v", err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("redirect carries no state")
	}

	// Plaintext binding cookie handed to the browser.
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == bindingCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("no %s cookie set", bindingCookieName)
	}

	// Exactly one handshake persisted.
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM oauth_state`).Scan(&count); err != nil {
		t.Fatalf("count oauth_state: %v", err)
	}
	if count != 1 {
		t.Fatalf("oauth_state rows = %d, want 1", count)
	}

	// The stored id is the state; the stored hash is sha256(cookie), not the cookie.
	var storedID, storedHash string
	if err := database.QueryRow(`SELECT id, binding_cookie_hash FROM oauth_state`).Scan(&storedID, &storedHash); err != nil {
		t.Fatalf("read oauth_state: %v", err)
	}
	if storedID != state {
		t.Errorf("stored id = %q, want state %q", storedID, state)
	}
	sum := sha256.Sum256([]byte(cookie.Value))
	wantHash := hex.EncodeToString(sum[:])
	if storedHash != wantHash {
		t.Errorf("stored hash = %q, want sha256(cookie) %q", storedHash, wantHash)
	}
	if storedHash == cookie.Value {
		t.Error("plaintext cookie was stored — must store only the hash")
	}
}

// R-XO7E-R1H7
// TestLoginCapturesSameSiteReturnTo confirms a local path is persisted on the
// web handshake instead of being carried by the browser through OAuth.
func TestLoginCapturesSameSiteReturnTo(t *testing.T) {
	srv, database := loginServer(t)
	const returnTo = "/srv/sites/private/test07/"
	rec := do(t, srv, "GET", "http://int.ikigenba.com/login/google?return_to="+url.QueryEscape(returnTo), nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}

	var got string
	if err := database.QueryRow(`SELECT return_to FROM oauth_state`).Scan(&got); err != nil {
		t.Fatalf("read persisted return_to: %v", err)
	}
	if got != returnTo {
		t.Errorf("persisted return_to = %q, want %q", got, returnTo)
	}
}

// R-XPFB-4T7W
// TestLoginRejectsHostileReturnTo verifies off-site and browser-normalized
// targets are replaced with the empty default before the handshake is stored.
func TestLoginRejectsHostileReturnTo(t *testing.T) {
	for _, returnTo := range []string{
		"//evil.com",
		"https://evil.com/x",
		"/\\evil.com",
		"evil.com",
	} {
		t.Run(returnTo, func(t *testing.T) {
			srv, database := loginServer(t)
			rec := do(t, srv, "GET", "http://int.ikigenba.com/login/google?return_to="+url.QueryEscape(returnTo), nil)
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}

			var got string
			if err := database.QueryRow(`SELECT return_to FROM oauth_state`).Scan(&got); err != nil {
				t.Fatalf("read persisted return_to: %v", err)
			}
			if got != "" {
				t.Errorf("persisted return_to = %q, want empty for hostile %q", got, returnTo)
			}
		})
	}
}

// TestSetBindingCookieAttributes pins the cookie's hardening attributes, including
// the Secure gate: off on plain HTTP (localhost), on behind an HTTPS proxy.
func TestSetBindingCookieAttributes(t *testing.T) {
	read := func(r *http.Request) *http.Cookie {
		rec := httptest.NewRecorder()
		setBindingCookie(rec, r, "secret-value")
		for _, c := range rec.Result().Cookies() {
			if c.Name == bindingCookieName {
				return c
			}
		}
		t.Fatalf("no %s cookie set", bindingCookieName)
		return nil
	}

	plain := read(httptest.NewRequest("GET", "http://int.ikigenba.com/login", nil))
	if plain.Value != "secret-value" {
		t.Errorf("value = %q, want secret-value", plain.Value)
	}
	if plain.Path != "/" {
		t.Errorf("path = %q, want /", plain.Path)
	}
	if !plain.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if plain.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", plain.SameSite)
	}
	if plain.Secure {
		t.Error("Secure must be off on plain HTTP")
	}

	fwd := httptest.NewRequest("GET", "http://int.ikigenba.com/login", nil)
	fwd.Header.Set("X-Forwarded-Proto", "https")
	if !read(fwd).Secure {
		t.Error("Secure must be on when X-Forwarded-Proto is https")
	}
}
