package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"dashboard/internal/googleidp"
	"dashboard/internal/oauthstate"
)

// startLogin runs the GET /login/google leg and returns the state the handler sent to
// the IdP and the plaintext binding cookie it set on the browser — the two
// values the callback leg needs to prove it is the same round-trip.
func startLogin(t *testing.T, srv *http.Server) (state string, binding *http.Cookie) {
	t.Helper()
	rec := do(t, srv, "GET", "https://int.ikigenba.com/login/google", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("login Location unparseable: %v", err)
	}
	state = loc.Query().Get("state")
	if state == "" {
		t.Fatal("login redirect carries no state")
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == bindingCookieName {
			binding = c
		}
	}
	if binding == nil {
		t.Fatalf("login set no %s cookie", bindingCookieName)
	}
	return state, binding
}

// liveSession drives the full login -> callback flow and returns the resulting
// dashboard_session cookie, so identity-aware tests can present a genuine,
// store-backed session rather than fabricating one.
func liveSession(t *testing.T, srv *http.Server) *http.Cookie {
	t.Helper()
	state, binding := startLogin(t, srv)
	target := "https://int.ikigenba.com/oauth/google/callback?state=" + url.QueryEscape(state) + "&code=auth-code"
	rec := do(t, srv, "GET", target, map[string]string{"Cookie": binding.Name + "=" + binding.Value})
	if rec.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatalf("callback set no %s cookie", sessionCookieName)
	return nil
}

// R-VQY2-GZ3F
// TestCallbackSuccessMintsSession is the end-to-end contract for the callback
// leg: a valid handshake + binding cookie + a verified, in-Workspace identity
// yields a redirect to /, a session cookie on the browser, and exactly one
// web_sessions row whose stored hash is sha256(session cookie) — never the
// plaintext — bound to the identity's email.
func TestCallbackSuccessMintsSession(t *testing.T) {
	// loginServer gates federation on testWorkspaceDomain, which matches the
	// stub's canned StubIdentity.HostedDomain — keep them aligned or the gate
	// rejects the stub identity and this test exercises the wrong path.
	if testWorkspaceDomain != googleidp.StubIdentity.HostedDomain {
		t.Fatalf("test setup drift: workspace domain %q != stub hd %q",
			testWorkspaceDomain, googleidp.StubIdentity.HostedDomain)
	}
	srv, database := loginServer(t)
	state, binding := startLogin(t, srv)

	target := "https://int.ikigenba.com/oauth/google/callback?state=" + url.QueryEscape(state) + "&code=auth-code"
	rec := do(t, srv, "GET", target, map[string]string{"Cookie": binding.Name + "=" + binding.Value})

	if rec.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("redirect Location = %q, want /", loc)
	}

	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			session = c
		}
	}
	if session == nil {
		t.Fatalf("callback set no %s cookie", sessionCookieName)
	}
	if session.Value == "" {
		t.Fatal("session cookie has empty value")
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM web_sessions`).Scan(&count); err != nil {
		t.Fatalf("count web_sessions: %v", err)
	}
	if count != 1 {
		t.Fatalf("web_sessions rows = %d, want 1", count)
	}

	var ownerEmail, ownerID, storedHash string
	if err := database.QueryRow(`SELECT owner_email, owner_id, cookie_hash FROM web_sessions`).Scan(&ownerEmail, &ownerID, &storedHash); err != nil {
		t.Fatalf("read web_sessions: %v", err)
	}
	if ownerEmail != googleidp.StubIdentity.Email {
		t.Errorf("owner_email = %q, want %q", ownerEmail, googleidp.StubIdentity.Email)
	}
	var resolvedID string
	if err := database.QueryRow(`SELECT id FROM identities WHERE iss = ? AND sub = ?`, googleidp.StubIdentity.Iss, googleidp.StubIdentity.Sub).Scan(&resolvedID); err != nil {
		t.Fatalf("read resolved identity: %v", err)
	}
	if ownerID != resolvedID {
		t.Errorf("owner_id = %q, want resolved identity handle %q", ownerID, resolvedID)
	}
	sum := sha256.Sum256([]byte(session.Value))
	wantHash := hex.EncodeToString(sum[:])
	if storedHash != wantHash {
		t.Errorf("stored hash = %q, want sha256(session cookie) %q", storedHash, wantHash)
	}
	if storedHash == session.Value {
		t.Error("plaintext session cookie was stored — must store only the hash")
	}
}

// R-XQN7-IKYL
// TestCallbackRedirectsToHandshakeReturnTo proves the web callback replays the
// validated same-site destination persisted by the real /login route.
func TestCallbackRedirectsToHandshakeReturnTo(t *testing.T) {
	srv, _ := loginServer(t)
	const returnTo = "/srv/sites/private/test07/"

	login := do(t, srv, "GET", "https://int.ikigenba.com/login/google?return_to="+url.QueryEscape(returnTo), nil)
	if login.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", login.Code)
	}
	idpURL, err := url.Parse(login.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse login Location: %v", err)
	}
	state := idpURL.Query().Get("state")
	if state == "" {
		t.Fatal("login redirect carries no state")
	}
	var binding *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == bindingCookieName {
			binding = cookie
			break
		}
	}
	if binding == nil {
		t.Fatalf("login set no %s cookie", bindingCookieName)
	}

	callback := do(t, srv, "GET", "https://int.ikigenba.com/oauth/google/callback?state="+url.QueryEscape(state)+"&code=auth-code", map[string]string{"Cookie": binding.Name + "=" + binding.Value})
	if callback.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", callback.Code)
	}
	if got := callback.Header().Get("Location"); got != returnTo {
		t.Errorf("callback Location = %q, want %q", got, returnTo)
	}
}

// R-ICU0-IF9X
// TestGoogleCallbackRejectsGitHubHandshake proves a state minted for GitHub
// cannot cross the Google callback boundary, even with the correct binding
// cookie, and that the rejected state remains single-use.
func TestGoogleCallbackRejectsGitHubHandshake(t *testing.T) {
	srv, database := loginServer(t)
	store := oauthstate.NewHandshakeStore(database, 5*time.Minute)
	handshake, binding, err := store.CreateWeb(context.Background(), oauthstate.ProviderGitHub, "")
	if err != nil {
		t.Fatalf("CreateWeb: %v", err)
	}

	target := "https://int.ikigenba.com/oauth/google/callback?state=" + url.QueryEscape(handshake.ID) + "&code=auth-code"
	rec := do(t, srv, "GET", target, map[string]string{"Cookie": bindingCookieName + "=" + binding})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("callback status = %d, want 400", rec.Code)
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			t.Errorf("provider mismatch set %s cookie", sessionCookieName)
		}
	}

	var eventType, detailsJSON string
	if err := database.QueryRow(`
		SELECT event_type, details FROM audit_log ORDER BY occurred_at DESC LIMIT 1
	`).Scan(&eventType, &detailsJSON); err != nil {
		t.Fatalf("read audit record: %v", err)
	}
	if eventType != "federation.reject" {
		t.Errorf("audit event_type = %q, want federation.reject", eventType)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
		t.Fatalf("decode audit details: %v", err)
	}
	if got := details["reason"]; got != "provider_mismatch" {
		t.Errorf("audit reason = %v, want provider_mismatch", got)
	}

	_, err = store.Consume(context.Background(), handshake.ID, binding)
	if !errors.Is(err, oauthstate.ErrHandshakeNotFound) {
		t.Errorf("replay Consume err = %v, want ErrHandshakeNotFound", err)
	}
	var sessions int
	if err := database.QueryRow(`SELECT COUNT(*) FROM web_sessions`).Scan(&sessions); err != nil {
		t.Fatalf("count web_sessions: %v", err)
	}
	if sessions != 0 {
		t.Errorf("web_sessions rows = %d, want 0", sessions)
	}
}

// R-XRV3-WCPA
// TestCallbackDefaultAndMCPRedirects confirms ordinary web logins retain the
// apex default and that the separate MCP callback resume target is unchanged.
func TestCallbackDefaultAndMCPRedirects(t *testing.T) {
	t.Run("web default", func(t *testing.T) {
		srv, _ := loginServer(t)
		state, binding := startLogin(t, srv)
		callback := do(t, srv, "GET", "https://int.ikigenba.com/oauth/google/callback?state="+url.QueryEscape(state)+"&code=auth-code", map[string]string{"Cookie": binding.Name + "=" + binding.Value})
		if callback.Code != http.StatusFound {
			t.Fatalf("callback status = %d, want 302", callback.Code)
		}
		if got := callback.Header().Get("Location"); got != "/" {
			t.Errorf("callback Location = %q, want /", got)
		}
	})

	t.Run("MCP redirect", func(t *testing.T) {
		ts, _, client := newOAuthTest(t)
		clientID := registerClient(t, ts, client)
		resp, err := client.Get(authorizeURL(ts, clientID, map[string]string{"provider": "google"}))
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("authorize status = %d, want 303", resp.StatusCode)
		}
		idpURL, err := url.Parse(resp.Header.Get("Location"))
		if err != nil {
			t.Fatalf("parse authorize Location: %v", err)
		}
		state := idpURL.Query().Get("state")
		if state == "" {
			t.Fatal("authorize redirect carries no state")
		}

		resp, err = client.Get(ts.URL + "/oauth/google/callback?" + url.Values{"state": {state}, "code": {"auth-code"}}.Encode())
		if err != nil {
			t.Fatalf("callback: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("callback status = %d, want 303", resp.StatusCode)
		}
		location, err := url.Parse(resp.Header.Get("Location"))
		if err != nil {
			t.Fatalf("parse callback Location: %v", err)
		}
		if got := location.Scheme + "://" + location.Host + location.Path; got != clientRedirectURI {
			t.Errorf("callback Location = %q, want %q", got, clientRedirectURI)
		}
	})
}
