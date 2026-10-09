package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
)

func assertPageValues(t *testing.T, body string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(body, value) {
			t.Fatalf("page omits supplied value %q", value)
		}
	}
}

func TestSignInPageValues(t *testing.T) {
	// R-3GUK-F9PN R-3I2G-T1GC R-3JAD-6T71 R-3MY2-C4F4 R-VCUY-86XA R-QI40-VGBM
	workspace := "fixture-workspace.example"
	for _, tc := range []struct{ host, apex, returnURL, display string }{
		{"auth.sbx.ikigenba.dev:443", "ikigenba.dev", "", ""},
		{"localhost:3001", "localhost", "", ""},
		{"auth.green.example", "green.example", "https://outside.test/path?a=b", ""},
		{"auth.sbx.ikigenba.dev", "ikigenba.dev", "HTTPS://App.SBX.Ikigenba.Dev:0080/path?x=1", "App.SBX.Ikigenba.Dev:0080"},
		{"localhost:3001", "localhost", "http://LOCALHOST:3000/path", "LOCALHOST:3000"},
		{"auth.green.example", "green.example", "https://app.green.example:/", "app.green.example"},
		{"a.b:port", "a.b:port", "", ""}, {"name", "name", "", ""}, {"name:", "name:", "", ""},
		{"auth.A.B:001", "A.B", "", ""}, {"auth.a.b.", "b.", "", ""},
	} {
		t.Run(tc.host+tc.returnURL, func(t *testing.T) {
			st := openSignInStore(t)
			s := newTestServer(t, Config{Banner: testPageBanner, Store: st, Now: func() time.Time { return signInNow }, WorkspaceDomain: workspace})
			before := profileStateSnapshot(t, st)
			w := serveSignIn(s, http.MethodGet, "/?return="+percentEncode(tc.returnURL), tc.host, nil, "")
			assertHTMLStatus(t, w, 200)
			assertAuthTemplate(t, w.Body.String(), "page", authPageData{SignIn: &signInPageData{Apex: tc.apex, Return: percentEncode(tc.returnURL), Destination: tc.display, Host: tc.host, Workspace: workspace}})
			if before != profileStateSnapshot(t, st) {
				t.Fatal("sign-in changed state")
			}
		})
	}
}

func TestCancelledAndNonmemberPageValues(t *testing.T) {
	// R-3O5Y-PW5T R-3PDV-3NWI
	host, workspace, email := "auth.sbx.ikigenba.dev:443", "fixture-workspace.test", "visitor@other.test"
	issuer := newSignInIssuer(t)
	issuer.issueClaims("refused", map[string]any{"iss": "https://accounts.google.com", "sub": "visitor", "aud": "client-id", "exp": 4102444800, "iat": 1700000000, "email": email, "email_verified": true, "hd": "other"})
	st := openSignInStore(t)
	s := signInServer(t, st, issuer, func() time.Time { return signInNow })
	s.cfg.WorkspaceDomain = workspace
	cancelled := serveSignIn(s, http.MethodGet, "/login/google/callback?error=access_denied&return=ignored", host, nil, "")
	assertHTMLStatus(t, cancelled, 200)
	assertAuthTemplate(t, cancelled.Body.String(), "page", authPageData{SignIn: &signInPageData{Apex: "ikigenba.dev", Refused: "cancelled", Host: host, Workspace: workspace}})
	state, err := st.CreateLoginState("verifier", "https://app.sbx.ikigenba.dev/ignored")
	if err != nil {
		t.Fatal(err)
	}
	refused := serveSignIn(s, http.MethodGet, "/login/google/callback?state="+state.State+"&code=refused", host, nil, "")
	assertHTMLStatus(t, refused, 403)
	assertAuthTemplate(t, refused.Body.String(), "page", authPageData{SignIn: &signInPageData{Apex: "ikigenba.dev", Refused: "not_member", Host: host, Workspace: workspace, Email: email}})
}

func TestProfilePageValues(t *testing.T) {
	// R-3LQ5-YCOF R-ROSU-UR6Q R-VK6C-ITDG
	st := openSignInStore(t)
	user, session := tokenTestIdentity(t, st, "profile-values")
	workspace, host := "fixture-workspace.test", "auth.sbx.ikigenba.dev"
	banner := testPageBanner(page.User{Email: user.Email, ProfileURL: "/", LogoutURL: "/logout"})
	banner.Trail = []page.Level{{Name: "profile supplied trail", URL: "/supplied-profile"}}
	wantBanner := banner
	wantBanner.Trail = nil
	s := newTestServer(t, Config{Banner: func(page.User) page.Banner { return banner }, Store: st, Now: func() time.Time { return tokenTestNow }, WorkspaceDomain: workspace})
	w := serveSignIn(s, http.MethodGet, "/", host, &http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}, "")
	assertHTMLStatus(t, w, 200)
	assertAuthTemplate(t, w.Body.String(), "page", authPageData{Banner: wantBanner, Profile: &profilePageData{Apex: "ikigenba.dev", Email: user.Email, Workspace: workspace, Create: tokenCreateData{Expiry: "90d"}}})
}

func TestReturnQueryDecodeAndByteEncoding(t *testing.T) {
	// R-PNB9-4VJK R-PPR1-WF0Y R-ED79-D60K R-3QLR-HFN7
	issuer := newSignInIssuer(t)
	for _, tc := range []struct{ query, value, encoded string }{
		{"", "", ""}, {"return", "", ""}, {"return=&return=later", "", ""}, {"return=first&return=second", "first", "first"}, {"x=1&ret%75rn=a+b%20c", "a b c", "a%20b%20c"}, {"return=%FF", "\xff", "%FF"}, {"return=%C3%A9", "é", "%C3%A9"},
		{"return=%&return=good", "good", "good"}, {"return=%G0&return=good", "good", "good"}, {"ret%urn=x&return=good", "good", "good"}, {"return=a;b&return=good", "good", "good"}, {"return=a%3Bb", "a;b", "a%3Bb"}, {"return=%00%7F%5C%3F%26%3D%2B%2F%3A", "\x00\x7f\\?&=+/:", "%00%7F%5C%3F%26%3D%2B%2F%3A"}, {"return=AZaz09-._~", "AZaz09-._~", "AZaz09-._~"}} {
		t.Run(tc.query, func(t *testing.T) {
			if returnQuery(tc.query) != tc.value || percentEncode(tc.value) != tc.encoded {
				t.Fatalf("decode/encode %q %q", returnQuery(tc.query), percentEncode(tc.value))
			}
			st := openSignInStore(t)
			s := signInServer(t, st, issuer, func() time.Time { return signInNow })
			page := serveSignIn(s, http.MethodGet, "/?"+tc.query, "auth.green.example", nil, "")
			if tc.encoded != "" {
				assertPageValues(t, page.Body.String(), tc.encoded)
			}
			assertEmptySignInTables(t, st)
			start := serveSignIn(s, http.MethodGet, "/login/google?"+tc.query, "auth.green.example", nil, "")
			if start.Code != 302 {
				t.Fatalf("start %d %s", start.Code, start.Body.String())
			}
			location, err := url.Parse(start.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			state, err := st.ConsumeLoginState(location.Query().Get("state"))
			if err != nil || state.ReturnURL != tc.value {
				t.Fatalf("stored return = %q %v", state.ReturnURL, err)
			}
		})
	}
}

func TestCallbackUsesTextPolicyForCarriedReturn(t *testing.T) {
	// R-N3TS-N2H8
	issuer := newSignInIssuer(t)
	issuer.issue("member", "return-subject", "member@green.example")
	for _, tc := range []struct{ value, want string }{
		{"HtTp://APP.GREEN.EXAMPLE:0080/a", "HtTp://APP.GREEN.EXAMPLE:0080/a"}, {"https://green.example:/a", "https://green.example:/a"}, {"https://green.example/a?x=%3Cscript%3E", "https://green.example/a?x=%3Cscript%3E"},
		{"https://green.example\\@evil.test/", "/"}, {"https:///green.example/", "/"}, {"https://green.example@evil.test/", "/"}, {"https://%67reen.example/", "/"}, {"https://green.example/\n", "/"}, {"https://green.example:abc/", "/"}, {"https://green.example.evil/", "/"}, {"", "/"},
	} {
		st := openSignInStore(t)
		s := signInServer(t, st, issuer, func() time.Time { return signInNow })
		state, err := st.CreateLoginState("verifier", tc.value)
		if err != nil {
			t.Fatal(err)
		}
		w := serveSignIn(s, http.MethodGet, "/login/google/callback?state="+state.State+"&code=member", "auth.green.example", nil, "")
		if w.Code != 302 || w.Header().Get("Location") != tc.want {
			t.Errorf("return %q -> %d %q want %q", tc.value, w.Code, w.Header().Get("Location"), tc.want)
		}
	}
}

func TestExternalPageValuesContributeNoAngleBrackets(t *testing.T) {
	// R-VAF5-GNFW
	plain, hostile := "probe-fixture", "<probe-fixture>"
	render := func(value, source string) string {
		st := openSignInStore(t)
		user, session := tokenTestIdentity(t, st, "escape")
		workspace, host := "workspace.test", "auth.green.example"
		issuer := newSignInIssuer(t)
		s := signInServer(t, st, issuer, func() time.Time { return tokenTestNow })
		target := "/"
		var cookie *http.Cookie
		switch source {
		case "workspace":
			workspace = value
		case "host":
			host = "auth." + value + ".test"
		case "return":
			target = "/?return=" + percentEncode("https://app."+value+".test/path")
		case "email":
			if _, _, err := st.UpsertUserOnLogin("issuer", "escape", value+"@example.com", tokenTestNow); err != nil {
				t.Fatal(err)
			}
			cookie = &http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
		case "token":
			if _, _, err := st.CreateToken(user.ID, value, "never", tokenTestNow); err != nil {
				t.Fatal(err)
			}
			cookie = &http.Cookie{Name: SessionCookieName, Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
		case "google":
			issuer.issueClaims("refused", map[string]any{"iss": "https://accounts.google.com", "sub": "visitor", "aud": "client-id", "exp": 4102444800, "iat": 1700000000, "email": value + "@other.test", "email_verified": true, "hd": "other"})
			state, err := st.CreateLoginState("verifier", "")
			if err != nil {
				t.Fatal(err)
			}
			target = "/login/google/callback?state=" + state.State + "&code=refused"
		case "submitted":
			s.cfg.WorkspaceDomain = workspace
			w := httptest.NewRecorder()
			s.ServeHTTP(w, tokenRequest("/tokens", session.ID, url.Values{"name": {value}, "expires": {"bad"}}))
			if w.Code != 400 {
				t.Fatal(w.Code)
			}
			return w.Body.String()
		}
		s.cfg.WorkspaceDomain = workspace
		return serveSignIn(s, http.MethodGet, target, host, cookie, "").Body.String()
	}
	for _, source := range []string{"workspace", "host", "return", "email", "token", "google", "submitted"} {
		t.Run(source, func(t *testing.T) {
			baseline, body := render(plain, source), render(hostile, source)
			for _, bracket := range []string{"<", ">"} {
				if strings.Count(body, bracket) != strings.Count(baseline, bracket) {
					t.Fatal("external value contributed an angle bracket")
				}
			}
			if strings.Contains(body, hostile) {
				t.Fatal("unescaped external value")
			}
		})
	}
}

func TestSignInCarriedReturnEveryByte(t *testing.T) {
	// R-3JAD-6T71 R-3QLR-HFN7: include every possible byte in one carried return.
	raw := make([]byte, 256)
	var encoded strings.Builder
	const hexDigits = "0123456789ABCDEF"
	for i := range raw {
		b := byte(i)
		raw[i] = b
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-._~", rune(b)) {
			encoded.WriteByte(b)
		} else {
			encoded.WriteByte('%')
			encoded.WriteByte(hexDigits[b>>4])
			encoded.WriteByte(hexDigits[b&15])
		}
	}
	issuer := newSignInIssuer(t)
	st := openSignInStore(t)
	s := signInServer(t, st, issuer, func() time.Time { return signInNow })
	s.cfg.WorkspaceDomain = "workspace.test"
	w := serveSignIn(s, http.MethodGet, "/?return="+url.QueryEscape(string(raw)), "auth.green.example", nil, "")
	assertHTMLStatus(t, w, http.StatusOK)
	assertAuthTemplate(t, w.Body.String(), "page", authPageData{SignIn: &signInPageData{Apex: "green.example", Return: encoded.String(), Host: "auth.green.example", Workspace: "workspace.test"}})
	start := serveSignIn(s, http.MethodGet, "/login/google?return="+encoded.String(), "auth.green.example", nil, "")
	if start.Code != http.StatusFound {
		t.Fatal(start.Code)
	}
	location, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := st.ConsumeLoginState(location.Query().Get("state"))
	if err != nil || state.ReturnURL != string(raw) {
		t.Fatalf("return bytes differ: %q, error %v", state.ReturnURL, err)
	}
}
