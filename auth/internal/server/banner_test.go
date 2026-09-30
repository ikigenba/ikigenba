package server

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit"
)

func testPageBanner(u appkit.User) appkit.Banner {
	return appkit.Banner{Service: "auth", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
}

func renderTestBanner(t *testing.T, data appkit.Banner) string {
	t.Helper()
	var out strings.Builder
	if err := appkit.Templates().ExecuteTemplate(&out, "banner", data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func renderTestFooter(t *testing.T, data appkit.Banner) string {
	t.Helper()
	var out strings.Builder
	if err := appkit.Templates().ExecuteTemplate(&out, "footer", data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func pageWrittenMarkup(body, banner, footer string) string {
	// R-056J-EJ2E: remove only exact boundary occurrences, independently.
	bodies := pageElements(body, "body")
	if len(bodies) > 0 && strings.HasPrefix(body[bodies[0].end:], banner) {
		at := bodies[0].end
		body = body[:at] + body[at+len(banner):]
	}
	at := strings.LastIndex(body, "</body>")
	if at >= 0 && strings.HasSuffix(body[:at], footer) {
		body = body[:at-len(footer)] + body[at:]
	}
	return body
}

func fixtureWrittenMarkup(t *testing.T, body string) string {
	t.Helper()
	bodies := pageElements(body, "body")
	if len(bodies) == 0 || !strings.HasPrefix(body[bodies[0].end:], "<header>") {
		return body
	}
	header := pageContent(body, pageElements(body, "header")[0])
	links := pageElements(header, "a")
	if len(links) == 0 {
		return body
	}
	email := pageAttrs(links[0])["title"][0]
	data := testPageBanner(appkit.User{Email: email, ProfileURL: "/", LogoutURL: "/logout"})
	return pageWrittenMarkup(body, renderTestBanner(t, data), renderTestFooter(t, data))
}

func TestWrittenMarkupRemovesOnlyExactBoundaryTemplates(t *testing.T) {
	data := testPageBanner(appkit.User{Email: "one@example", ProfileURL: "/", LogoutURL: "/logout"})
	banner, footer := renderTestBanner(t, data), renderTestFooter(t, data)
	for _, tc := range []struct{ body, want string }{
		{"<body>" + banner + "<main>" + banner + footer + "</main>" + footer + "</body>", "<body><main>" + banner + footer + "</main></body>"},
		{"<body> " + banner + "<main></main>" + footer + " </body>", "<body> " + banner + "<main></main>" + footer + " </body>"},
		{"<body><main>" + banner + "</main>" + footer + "</body>", "<body><main>" + banner + "</main></body>"},
		{"<body>" + banner + "<main>" + footer + "</main></body>", "<body><main>" + footer + "</main></body>"},
		{"<body><main></main></body>", "<body><main></main></body>"},
	} {
		if got := pageWrittenMarkup(tc.body, banner, footer); got != tc.want {
			t.Fatalf("written=%q want=%q", got, tc.want)
		}
	}
}

func TestPagesUseReturnedBannerOnce(t *testing.T) {
	// R-02QQ-MZL0 R-03YN-0RBP R-1MU4-8FOY R-06EF-SAT3 R-07MC-62JS
	for _, services := range [][]appkit.Service{nil, {
		{Name: "Outside & secret", URL: "https://other.example/", Icon: template.HTML(`<svg><path d="x"/></svg>`), Enabled: true},
		{Name: "auth", URL: "https://auth.example/", Enabled: true, Current: true},
	}} {
		st := openTokenTestStore(t)
		user, session := tokenTestIdentity(t, st, "banner")
		calls := []appkit.User{}
		returned := appkit.Banner{Service: "returned service", Version: "fixture<& version", Email: "returned <& email", ProfileURL: "/returned-profile", LogoutURL: "/returned-logout", Services: services}
		srv := New(Config{Store: st, Now: func() time.Time { return tokenTestNow }, Banner: func(u appkit.User) appkit.Banner { calls = append(calls, u); return returned }})
		requests := []*http.Request{tokenProfileRequest(session.ID), tokenRequest("/tokens", session.ID, url.Values{"name": {""}, "expires": {"bad"}}), tokenRequest("/tokens", session.ID, url.Values{"name": {"banner token"}, "expires": {"never"}}), tokenProfileRequest(session.ID)}
		for i, req := range requests {
			returned.Service = []string{"profile", "rejected", "created", "refreshed"}[i] + " <& service"
			returned.Version = []string{"first", "second", "third", "fourth"}[i] + " <& version"
			calls = nil
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			wantUser := appkit.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"}
			if len(calls) != 1 || calls[0] != wantUser {
				t.Fatalf("request %d calls=%v want=%v", i, calls, wantUser)
			}
			body := w.Body.String()
			banner := renderTestBanner(t, returned)
			footer := renderTestFooter(t, returned)
			bodies := pageElements(body, "body")
			if len(bodies) != 1 || !strings.HasPrefix(body[bodies[0].end:], banner) || !strings.HasSuffix(body[:strings.LastIndex(body, "</body>")], footer) {
				t.Fatalf("request %d missing boundary templates", i)
			}
			written := pageWrittenMarkup(body, banner, footer)
			if written == body {
				t.Fatalf("request %d missing exact returned banner", i)
			}
			inside := pageContent(written, pageOne(t, written, "body"))
			pageSequence(t, inside, "main")
			assertAuthPage(t, written, i == 2)
		}
	}
}

func TestOtherResponsesNeverCallBanner(t *testing.T) {
	// R-07MC-62JS: only a response drawn with the banner consults its source.
	st := openTokenTestStore(t)
	_, session := tokenTestIdentity(t, st, "no-banner")
	calls := 0
	issuer := newSignInIssuer(t)
	issuer.issue("banner-member", "banner-member-subject", "member@green.example")
	issuer.issueClaims("banner-nonmember", map[string]any{"iss": "https://accounts.google.com", "sub": "banner-nonmember-subject", "aud": "client-id", "exp": 4102444800, "iat": 1700000000, "email": "other@example", "email_verified": true, "hd": "other"})
	srv := signInServer(t, st, issuer, func() time.Time { return tokenTestNow })
	srv.cfg.Banner = func(appkit.User) appkit.Banner { calls++; return appkit.Banner{} }
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
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/login/google/callback?error=access_denied", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/login/google", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/login/google/callback", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/check", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/me", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/_appkit/theme.css", nil),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing", nil),
		tokenRequest("/tokens", "", url.Values{"name": {"valid"}, "expires": {"never"}}),
		tokenActionRequest(session.ID, "00000000000000000000000000", "delete"),
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
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	srv.ServeHTTP(httptest.NewRecorder(), tokenProfileRequest(session.ID))
	if calls != 0 {
		t.Fatal("store failure called banner")
	}
}
