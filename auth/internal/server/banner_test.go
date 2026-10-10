package server

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
)

func testPageBanner(u page.User) page.Banner {
	return page.Banner{Service: "auth", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
}

func renderTestBanner(t *testing.T, data page.Banner) string {
	t.Helper()
	var out strings.Builder
	if err := page.Templates().ExecuteTemplate(&out, "banner", data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func renderTestFooter(t *testing.T, data page.Banner) string {
	t.Helper()
	var out strings.Builder
	if err := page.Templates().ExecuteTemplate(&out, "footer", data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func assertPageBanner(t *testing.T, body string, data page.Banner) {
	t.Helper()
	banner, footer := renderTestBanner(t, data), renderTestFooter(t, data)
	start := strings.Index(body, banner)
	end := strings.Index(body, footer)
	if start < 0 || end < start+len(banner) {
		t.Fatal("page omits or reorders returned banner and footer")
	}
}

func TestPagesUseReturnedBannerOnce(t *testing.T) {
	// R-4ZBA-OGOE R-RL55-PFYN R-RNKY-GZG1 R-51R3-G05S R-VBN1-UF6L R-PT80-EZ08
	for _, icon := range []template.HTML{"", "fixture-icon-one", "fixture-icon-two"} {
		st := openTokenTestStore(t)
		user, session := tokenTestIdentity(t, st, "banner")
		calls := []page.User{}
		returned := page.Banner{Service: "returned service", Icon: icon, Release: "fixture<& release", Commit: "fixture<& commit", Email: "returned <& email", ProfileURL: "/returned-profile", LogoutURL: "/returned-logout", Home: "https://home.example/", Tools: true, Trail: []page.Level{{Name: "incoming", URL: "/incoming"}}}
		srv := newTestServer(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }, Banner: func(u page.User) page.Banner { calls = append(calls, u); return returned }})
		requests := []*http.Request{tokenProfileRequest(session.ID), tokenRequest("/tokens", session.ID, url.Values{"name": {""}, "expires": {"bad"}}), tokenRequest("/tokens", session.ID, url.Values{"name": {"banner token"}, "expires": {"never"}}), tokenProfileRequest(session.ID)}
		for i, req := range requests {
			returned.Service = []string{"profile", "rejected", "created", "refreshed"}[i] + " <& service"
			returned.Release = []string{"first", "second", "third", "fourth"}[i] + " <& release"
			returned.Commit = []string{"one", "two", "three", "four"}[i] + " <& commit"
			calls = nil
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			wantUser := page.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"}
			if len(calls) != 1 || calls[0] != wantUser {
				t.Fatalf("request %d calls=%v want=%v", i, calls, wantUser)
			}
			body := w.Body.String()
			wantBanner := returned
			wantBanner.Trail = []page.Level{}
			assertPageBanner(t, body, wantBanner)
			if returned.Trail[0].Name != "incoming" || returned.Trail[0].URL != "/incoming" {
				t.Fatal("returned banner trail mutated")
			}
		}
	}
}

func TestOtherResponsesNeverCallBanner(t *testing.T) {
	// R-PT80-EZ08: only a response drawn with the banner consults its source.
	st := openTokenTestStore(t)
	_, session := tokenTestIdentity(t, st, "no-banner")
	calls := 0
	issuer := newSignInIssuer(t)
	issuer.issue("banner-member", "banner-member-subject", "member@green.example")
	issuer.issueClaims("banner-nonmember", map[string]any{"iss": "https://accounts.google.com", "sub": "banner-nonmember-subject", "aud": "client-id", "exp": 4102444800, "iat": 1700000000, "email": "other@example", "email_verified": true, "hd": "other"})
	srv := signInServer(t, st, issuer, func() time.Time { return tokenTestNow })
	srv.cfg.Banner = func(page.User) page.Banner { calls++; return page.Banner{} }
	for _, code := range []string{"banner-member", "banner-nonmember"} {
		state, err := st.CreateLoginState("verifier", "")
		if err != nil {
			t.Fatal(err)
		}
		serveSignIn(srv, http.MethodGet, "/login/google/callback?state="+state.State+"&code="+code, "auth.green.example", nil, "")
		if calls != 0 {
			t.Fatalf("%s called banner", code)
		}
	}
	requests := []*http.Request{
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/about", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/about", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/about", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/login/google/callback?error=access_denied", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/login/google", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/login/google/callback", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/check", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/me", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, page.StaticPrefix+"theme.css", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, page.StaticPrefix+"missing", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing", nil),
		tokenRequest("/tokens", "", url.Values{"name": {"valid"}, "expires": {"never"}}),
		tokenActionRequest(session.ID, "00000000000000000000000000", "delete"),
	}
	for _, target := range []string{"/check", "/me"} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		requests = append(requests, r)
	}
	for _, r := range requests {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		if calls != 0 {
			t.Fatalf("%s %s called banner", r.Method, r.URL)
		}
	}
	logout := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/logout", nil)
	logout.Host = "auth.green.example"
	logout.Header.Set("Origin", "https://auth.green.example")
	logout.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	srv.ServeHTTP(httptest.NewRecorder(), logout)
	if calls != 0 {
		t.Fatal("redirect called banner")
	}
	failServerStore(t, st)
	srv.ServeHTTP(httptest.NewRecorder(), tokenProfileRequest(session.ID))
	if calls != 0 {
		t.Fatal("store failure called banner")
	}
}
