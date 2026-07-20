package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"dashboard/internal/githubidp"
	"dashboard/internal/googleidp"
	"dashboard/internal/oauthstate"
)

func newGitHubCallbackServer(t *testing.T, githubIdentity githubidp.Identity) (*http.Server, serverDeps) {
	t.Helper()
	d := newServerDeps(t)
	github := githubidp.NewStub()
	github.Identity = githubIdentity
	opts := d.opts()
	opts.GithubProvider = github
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv, d
}

func activeGitHubIdentity() githubidp.Identity {
	return githubidp.Identity{
		Iss:           "https://github.com",
		Sub:           "424242",
		Login:         "member",
		Email:         "member@int.ikigenba.com",
		EmailVerified: true,
		Name:          "Active Member",
		Picture:       "https://avatars.example/member",
		OrgMembership: "active",
	}
}

func githubWebCallback(t *testing.T, srv *http.Server, d serverDeps, returnTo string) *httptest.ResponseRecorder {
	t.Helper()
	handshake, binding, err := d.handshakes.CreateWeb(context.Background(), oauthstate.ProviderGitHub, returnTo)
	if err != nil {
		t.Fatalf("CreateWeb: %v", err)
	}
	target := "https://int.ikigenba.com/oauth/github/callback?" + url.Values{
		"state": {handshake.ID}, "code": {"github-code"},
	}.Encode()
	return do(t, srv, http.MethodGet, target, map[string]string{"Cookie": bindingCookieName + "=" + binding})
}

func requireNoSessionCookie(t *testing.T, resp *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range resp.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			t.Errorf("response set %s cookie", sessionCookieName)
		}
	}
}

func requireAuditReason(t *testing.T, database *sql.DB, reason string) {
	t.Helper()
	var eventType, detailsJSON string
	if err := database.QueryRow(`SELECT event_type, details FROM audit_log ORDER BY occurred_at DESC LIMIT 1`).Scan(&eventType, &detailsJSON); err != nil {
		t.Fatalf("read audit record: %v", err)
	}
	if eventType != "federation.reject" {
		t.Errorf("audit event_type = %q, want federation.reject", eventType)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
		t.Fatalf("decode audit details: %v", err)
	}
	if got := details["reason"]; got != reason {
		t.Errorf("audit reason = %v, want %q", got, reason)
	}
}

// R-INT3-YCY6
func TestGitHubWebCallbackMintsSessionAndReturnsToHandshakeDestination(t *testing.T) {
	srv, d := newGitHubCallbackServer(t, activeGitHubIdentity())
	for _, returnTo := range []string{"/after-login", ""} {
		t.Run(returnTo, func(t *testing.T) {
			resp := githubWebCallback(t, srv, d, returnTo)
			if resp.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", resp.Code)
			}
			want := returnTo
			if want == "" {
				want = "/"
			}
			if got := resp.Header().Get("Location"); got != want {
				t.Errorf("Location = %q, want %q", got, want)
			}
			var session *http.Cookie
			for _, cookie := range resp.Result().Cookies() {
				if cookie.Name == sessionCookieName {
					session = cookie
				}
			}
			if session == nil || session.Value == "" {
				t.Fatal("callback did not mint a non-empty dashboard_session cookie")
			}
		})
	}
}

// R-IP10-C4OV
func TestGitHubAndGoogleSameEmailResolveToDifferentIssuerSubjectRows(t *testing.T) {
	d := newServerDeps(t)
	ghIdentity := activeGitHubIdentity()
	ghIdentity.Email = googleidp.StubIdentity.Email
	github := githubidp.NewStub()
	github.Identity = ghIdentity
	google := googleidp.NewStub()
	opts := d.opts()
	opts.GithubProvider = github
	opts.IDPProvider = google
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := githubWebCallback(t, srv, d, ""); got.Code != http.StatusFound {
		t.Fatalf("GitHub callback status = %d, want 302", got.Code)
	}
	googleHandshake, binding, err := d.handshakes.CreateWeb(context.Background(), oauthstate.ProviderGoogle, "")
	if err != nil {
		t.Fatalf("CreateWeb Google: %v", err)
	}
	googleResp := do(t, srv, http.MethodGet, "https://int.ikigenba.com/oauth/google/callback?"+url.Values{"state": {googleHandshake.ID}, "code": {"google-code"}}.Encode(), map[string]string{"Cookie": bindingCookieName + "=" + binding})
	if googleResp.Code != http.StatusFound {
		t.Fatalf("Google callback status = %d, want 302", googleResp.Code)
	}

	var githubID, githubIss, githubSub, googleID string
	if err := d.db.QueryRow(`SELECT id, iss, sub FROM identities WHERE iss = ? AND sub = ?`, ghIdentity.Iss, ghIdentity.Sub).Scan(&githubID, &githubIss, &githubSub); err != nil {
		t.Fatalf("read GitHub identity: %v", err)
	}
	if githubIss != "https://github.com" || githubSub != ghIdentity.Sub {
		t.Errorf("GitHub row (iss, sub) = (%q, %q), want (%q, %q)", githubIss, githubSub, "https://github.com", ghIdentity.Sub)
	}
	if err := d.db.QueryRow(`SELECT id FROM identities WHERE iss = ? AND sub = ?`, googleidp.StubIdentity.Iss, googleidp.StubIdentity.Sub).Scan(&googleID); err != nil {
		t.Fatalf("read Google identity: %v", err)
	}
	if githubID == googleID {
		t.Errorf("GitHub and Google same-email identities share id %q", githubID)
	}
	var distinctOwners int
	if err := d.db.QueryRow(`SELECT COUNT(DISTINCT owner_id) FROM web_sessions`).Scan(&distinctOwners); err != nil {
		t.Fatalf("count session owners: %v", err)
	}
	if distinctOwners != 2 {
		t.Errorf("distinct session owner_ids = %d, want 2", distinctOwners)
	}
}

// R-IQ8W-PWFK
func TestGitHubCallbackRejectsNonActiveOrganizationMembership(t *testing.T) {
	for _, membership := range []string{"", "pending"} {
		t.Run(membership, func(t *testing.T) {
			identity := activeGitHubIdentity()
			identity.OrgMembership = membership
			srv, d := newGitHubCallbackServer(t, identity)
			resp := githubWebCallback(t, srv, d, "")
			if resp.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.Code)
			}
			requireNoSessionCookie(t, resp)
			requireAuditReason(t, d.db, "org_membership")
			var identities int
			if err := d.db.QueryRow(`SELECT COUNT(*) FROM identities`).Scan(&identities); err != nil {
				t.Fatalf("count identities: %v", err)
			}
			if identities != 0 {
				t.Errorf("identities rows = %d, want 0", identities)
			}
		})
	}
}

// R-IRGT-3O69
func TestGitHubCallbackRejectsUnverifiedEmail(t *testing.T) {
	identity := activeGitHubIdentity()
	identity.EmailVerified = false
	srv, d := newGitHubCallbackServer(t, identity)
	resp := githubWebCallback(t, srv, d, "")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.Code)
	}
	requireNoSessionCookie(t, resp)
	requireAuditReason(t, d.db, "email_not_verified")
	var sessions int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM web_sessions`).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Errorf("web_sessions rows = %d, want 0", sessions)
	}
}

// R-ISOP-HFWY
func TestGitHubMCPCallbackIssuesOwnerBoundTokens(t *testing.T) {
	d := newServerDeps(t)
	github := githubidp.NewStub()
	github.Identity = activeGitHubIdentity()
	opts := d.opts()
	opts.GithubProvider = github
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	clientID := registerClient(t, ts, client)
	handshake, binding, err := d.handshakes.CreateMCP(context.Background(), oauthstate.ProviderGitHub, oauthstate.MCPContext{
		ClientID: clientID, RedirectURI: clientRedirectURI, CodeChallenge: pkceChallenge(),
		CodeChallengeMethod: "S256", ClientState: "github-client-state", Resource: testResource,
	})
	if err != nil {
		t.Fatalf("CreateMCP: %v", err)
	}
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/oauth/github/callback?"+url.Values{"state": {handshake.ID}, "code": {"github-code"}}.Encode(), nil)
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: bindingCookieName, Value: binding})
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("callback status = %d, want 303", resp.StatusCode)
	}
	back, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := back.Scheme + "://" + back.Host + back.Path; got != clientRedirectURI {
		t.Errorf("Location = %q, want %q", got, clientRedirectURI)
	}
	if got := back.Query().Get("state"); got != "github-client-state" {
		t.Errorf("returned state = %q, want github-client-state", got)
	}
	tokens := redeemCode(t, ts, client, clientID, back.Query().Get("code"))
	intro := introspect(t, ts, client, tokens.AccessToken, tokens.AccessToken)
	var ownerID string
	if err := d.db.QueryRow(`SELECT id FROM identities WHERE iss = ? AND sub = ?`, github.Identity.Iss, github.Identity.Sub).Scan(&ownerID); err != nil {
		t.Fatalf("read identity owner_id: %v", err)
	}
	if !intro.Active || intro.OwnerID != ownerID {
		t.Errorf("introspection = active:%v owner_id:%q, want active owner_id %q", intro.Active, intro.OwnerID, ownerID)
	}
}

// R-ITWL-V7NN
func TestGitHubCallbackUsesLoginOnlyWhenProfileNameIsEmpty(t *testing.T) {
	for _, tc := range []struct{ name, want string }{{"", "member"}, {"Profile Name", "Profile Name"}} {
		t.Run(tc.want, func(t *testing.T) {
			identity := activeGitHubIdentity()
			identity.Sub = "sub-" + tc.want
			identity.Name = tc.name
			srv, d := newGitHubCallbackServer(t, identity)
			if got := githubWebCallback(t, srv, d, ""); got.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", got.Code)
			}
			var stored string
			if err := d.db.QueryRow(`SELECT name FROM identities WHERE iss = ? AND sub = ?`, identity.Iss, identity.Sub).Scan(&stored); err != nil {
				t.Fatalf("read identity name: %v", err)
			}
			if stored != tc.want {
				t.Errorf("stored name = %q, want %q", stored, tc.want)
			}
		})
	}
}

// R-IF9T-9YRB
func TestGitHubCallbackRejectsGoogleHandshake(t *testing.T) {
	srv, d := newGitHubCallbackServer(t, activeGitHubIdentity())
	handshake, binding, err := d.handshakes.CreateWeb(context.Background(), oauthstate.ProviderGoogle, "")
	if err != nil {
		t.Fatalf("CreateWeb: %v", err)
	}
	resp := do(t, srv, http.MethodGet, "https://int.ikigenba.com/oauth/github/callback?"+url.Values{"state": {handshake.ID}, "code": {"github-code"}}.Encode(), map[string]string{"Cookie": bindingCookieName + "=" + binding})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Code)
	}
	requireNoSessionCookie(t, resp)
	requireAuditReason(t, d.db, "provider_mismatch")
}
