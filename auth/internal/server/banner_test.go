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
	data := testPageBanner(page.User{Email: email, ProfileURL: "/", LogoutURL: "/logout"})
	return pageWrittenMarkup(body, renderTestBanner(t, data), renderTestFooter(t, data))
}

func TestWrittenMarkupRemovesOnlyExactBoundaryTemplates(t *testing.T) {
	data := testPageBanner(page.User{Email: "one@example", ProfileURL: "/", LogoutURL: "/logout"})
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
	// R-4ZBA-OGOE R-50J7-28F3 R-51R3-G05S R-06EF-SAT3 R-52YZ-TRWH
	for _, services := range [][]page.Service{nil, {
		{Name: "auth", URL: "https://auth.example/", Icon: template.HTML(`<svg><path d="M1 1h2v2H1z"/></svg>`), Enabled: true, Current: true},
	}, {
		{Name: "Outside & secret", URL: "https://other.example/", Icon: template.HTML(`<svg><path d="x"/></svg>`), Enabled: true},
		{Name: "auth", URL: "https://auth.example/", Enabled: true, Current: true},
	}} {
		st := openTokenTestStore(t)
		user, session := tokenTestIdentity(t, st, "banner")
		calls := []page.User{}
		returned := page.Banner{Service: "returned service", Version: "fixture<& version", Email: "returned <& email", ProfileURL: "/returned-profile", LogoutURL: "/returned-logout", Services: services}
		for _, service := range services {
			if service.Current {
				returned.Icon = service.Icon
			}
		}
		srv := newTestServer(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }, Banner: func(u page.User) page.Banner { calls = append(calls, u); return returned }})
		requests := []*http.Request{tokenProfileRequest(session.ID), tokenRequest("/tokens", session.ID, url.Values{"name": {""}, "expires": {"bad"}}), tokenRequest("/tokens", session.ID, url.Values{"name": {"banner token"}, "expires": {"never"}}), tokenProfileRequest(session.ID)}
		for i, req := range requests {
			returned.Service = []string{"profile", "rejected", "created", "refreshed"}[i] + " <& service"
			returned.Version = []string{"first", "second", "third", "fourth"}[i] + " <& version"
			calls = nil
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			wantUser := page.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"}
			if len(calls) != 1 || calls[0] != wantUser {
				t.Fatalf("request %d calls=%v want=%v", i, calls, wantUser)
			}
			body := w.Body.String()
			assertAuthPageFavicon(t, body)
			banner := renderTestBanner(t, returned)
			footer := renderTestFooter(t, returned)
			bodies := pageElements(body, "body")
			if len(bodies) != 1 || !strings.HasPrefix(body[bodies[0].end:], banner) || !strings.HasSuffix(body[:strings.LastIndex(body, "</body>")], footer) {
				t.Fatalf("request %d missing boundary templates", i)
			}
			if len(services) != 0 {
				assertBannerLauncher(t, body[bodies[0].end:bodies[0].end+len(banner)])
			}
			written := pageWrittenMarkup(body, banner, footer)
			if written == body {
				t.Fatalf("request %d missing exact returned banner", i)
			}
			inside := pageContent(written, pageOne(t, written, "body"))
			pageSequence(t, inside, "main")
			assertAuthPage(t, written)
		}
	}
}

func assertBannerLauncher(t *testing.T, banner string) {
	t.Helper()
	// R-F7MK-697Z: inspect the banner actually emitted in the response.
	header := pageOne(t, banner, "header")
	var launchers, marks []pageTag
	for _, tag := range pageElements(banner, "button") {
		if values := pageAttrs(tag)["class"]; len(values) == 1 && values[0] == "launcher" {
			launchers = append(launchers, tag)
		}
	}
	for _, tag := range pageElements(banner, "strong") {
		if values := pageAttrs(tag)["class"]; len(values) == 1 && values[0] == "mark" {
			marks = append(marks, tag)
		}
	}
	if len(launchers) != 1 || len(marks) != 1 {
		t.Fatalf("banner launchers=%d marks=%d", len(launchers), len(marks))
	}
	button, mark := launchers[0], marks[0]
	if !strings.HasPrefix(strings.TrimLeft(pageContent(banner, header), " \t\r\n\f"), mark.raw) {
		t.Fatalf("banner header does not begin with mark: %q", pageContent(banner, header))
	}
	markEnd := mark.end + len(pageContent(banner, mark)) + len("</strong>")
	if button.start < markEnd || strings.Trim(banner[markEnd:button.start], " \t\r\n\f") != "" {
		t.Fatal("banner launcher does not immediately follow mark")
	}
}

func TestApproveBannerMarkBeforeLauncher(t *testing.T) {
	f := newOAuthFixture(t, "")
	client := f.client(t, "Banner fixture", oauthCallback)
	returned := testPageBanner(page.User{Email: f.user.Email, ProfileURL: "/", LogoutURL: "/logout"})
	returned.Icon = template.HTML(`<svg><path d="M1 1h2v2H1z"/></svg>`)
	returned.Services = []page.Service{{Name: "auth", URL: "/", Icon: returned.Icon, Enabled: true, Current: true}}
	f.trail.server.cfg.Banner = func(page.User) page.Banner { return returned }
	w, _ := f.request(t, http.MethodGet, "/authorize?"+oauthAuthParams(client).Encode(), "", f.session.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("approve status=%d", w.Code)
	}
	body := w.Body.String()
	banner := renderTestBanner(t, returned)
	bodyTag := pageOne(t, body, "body")
	if !strings.HasPrefix(body[bodyTag.end:], banner) {
		t.Fatal("approve page does not begin its body with the returned banner")
	}
	assertBannerLauncher(t, body[bodyTag.end:bodyTag.end+len(banner)])
}

func TestOtherResponsesNeverCallBanner(t *testing.T) {
	// R-52YZ-TRWH: only a response drawn with the banner consults its source.
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
