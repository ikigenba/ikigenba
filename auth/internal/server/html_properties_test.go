package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func assertAuthPageReferences(t *testing.T, body string, banner *page.Banner) htmlPageReferences {
	t.Helper()
	renderedBanner := ""
	if banner != nil {
		renderedBanner = renderTestBanner(t, *banner)
	}
	refs := readPageReferences(t, body, renderedBanner)
	if len(refs.bases) != 0 || len(refs.automatic) != 0 {
		t.Fatalf("page sets base or automatic navigation: %v %v", refs.bases, refs.automatic)
	}
	for _, target := range refs.resources {
		if !strings.HasPrefix(target, page.StaticPrefix) {
			t.Fatalf("resource target %q outside shared prefix", target)
		}
	}
	check := func(target string) {
		if strings.ContainsAny(target, "\t\r\n") || target != "/" && (len(target) <= 1 || target[0] != '/' || target[1] == '/' || target[1] == '\\') {
			t.Fatalf("non-banner navigation target %q is not an own-origin path", target)
		}
	}
	for _, target := range refs.outsideLinks {
		check(target)
	}
	for _, form := range refs.outsideForms {
		check(form.target)
	}
	return refs
}

func requirePageLink(t *testing.T, refs htmlPageReferences, target string) {
	t.Helper()
	for _, found := range refs.links {
		if found == target {
			return
		}
	}
	t.Fatalf("missing hyperlink target %q in %v", target, refs.links)
}
func requirePageForm(t *testing.T, refs htmlPageReferences, target string) {
	t.Helper()
	for _, found := range refs.forms {
		if found.method == http.MethodPost && found.target == target {
			return
		}
	}
	t.Fatalf("missing POST form target %q in %v", target, refs.forms)
}
func referenceResponse(t *testing.T, server *Server, request *http.Request, status int) string {
	t.Helper()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != status || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("HTML response %d %v", response.Code, response.Header())
	}
	return response.Body.String()
}

func TestSignInPageReferenceProperties(t *testing.T) {
	// R-3XX5-S23D R-7ZXA-Q4XF: anonymous page states have only shared resources and own-origin navigation.
	// R-40CY-JLKR: login targets are built from the sign-in data's encoded Return.
	st := openSignInStore(t)
	issuer := newSignInIssuer(t)
	server := signInServer(t, st, issuer, func() time.Time { return signInNow })
	for _, raw := range []string{"", "https://app.green.example/path?a=b&c=d#fragment", "https://outside.test/a", "//outside.test/a", "a\t\r\nb", "\x00<&\"\\", string([]byte{0xff})} {
		target := "/"
		if raw != "" {
			target += "?return=" + url.QueryEscape(raw)
		}
		response := serveSignIn(server, "GET", target, "auth.green.example", nil, "")
		assertHTMLStatus(t, response, 200)
		refs := assertAuthPageReferences(t, response.Body.String(), nil)
		// percentEncode's definition is independently expressed using QueryEscape
		// plus the two differences between query escaping and percent encoding.
		encoded := strings.ReplaceAll(strings.ReplaceAll(url.QueryEscape(raw), "+", "%20"), "%7E", "~")
		login := "/login/google"
		if raw != "" {
			login += "?return=" + encoded
		}
		requirePageLink(t, refs, login)
	}
	cancelled := serveSignIn(server, "GET", "/login/google/callback?error=access_denied&return=ignored", "auth.green.example", nil, "")
	assertHTMLStatus(t, cancelled, 200)
	requirePageLink(t, assertAuthPageReferences(t, cancelled.Body.String(), nil), "/login/google")
	issuer.issueClaims("property-code", map[string]any{"iss": "https://accounts.google.com", "sub": "property-subject", "aud": "client-id", "exp": 4102444800, "iat": 1700000000, "email": "outside@other.test", "email_verified": true, "hd": "other.test"})
	state, err := st.CreateLoginState("property-verifier", "https://app.green.example/carried")
	if err != nil {
		t.Fatal(err)
	}
	refused := serveSignIn(server, "GET", "/login/google/callback?state="+state.State+"&code=property-code", "auth.green.example", nil, "")
	assertHTMLStatus(t, refused, 403)
	requirePageLink(t, assertAuthPageReferences(t, refused.Body.String(), nil), "/login/google")
}

func TestTokenPageReferenceProperties(t *testing.T) {
	// R-3XX5-S23D R-7ZXA-Q4XF: profile, rejected and created pages obey reference properties.
	// R-41KU-XDBG: both profile and rejected-create offer POST /tokens.
	// R-8157-3WO4: every personal and client row offers the action paths built from its ID and Enabled.
	st := openTokenTestStore(t)
	user, session := tokenTestIdentity(t, st, "property")
	banner := page.Banner{Service: "auth", Email: user.Email, ProfileURL: "/", LogoutURL: "/logout", Home: "https://outside.test/home", Tools: true}
	server := newTestServer(t, Config{Store: st, Now: func() time.Time { return tokenTestNow }, Banner: func(page.User) page.Banner { return banner }})
	// Empty lists exercise their distinct template branches before populated rows.
	body := referenceResponse(t, server, tokenProfileRequest(session.ID), 200)
	requirePageForm(t, assertAuthPageReferences(t, body, &banner), "/tokens")
	var rows []tokenRowData
	for i, expiry := range []store.Expiry{store.ExpiryNever, store.Expiry30d, store.Expiry90d, store.Expiry365d} {
		token, secret, err := st.CreateToken(user.ID, "fixture <& token", expiry, tokenTestNow)
		if err != nil {
			t.Fatal(err)
		}
		enabled := i%2 == 0
		if err := st.SetTokenEnabled(user.ID, token.ID, enabled); err != nil {
			t.Fatal(err)
		}
		if i != 0 {
			if _, err := st.LookupTokenIdentity(secret, "", tokenTestNow.Add(-time.Duration(i)*time.Hour)); err != nil && enabled {
				t.Fatal(err)
			}
		}
		rows = append(rows, tokenRowData{ID: token.ID, Enabled: enabled})
	}
	var clients []mcpClientData
	for _, issued := range []time.Time{tokenTestNow, tokenTestNow.Add(-100 * 24 * time.Hour)} {
		client, err := st.RegisterClient("fixture <& client", []string{oauthCallback}, issued)
		if err != nil {
			t.Fatal(err)
		}
		token, secret, err := st.CreateClientToken(store.AuthCode{ClientID: client.ID, UserID: user.ID, IssuedAt: issued}, "mcp.green.example", issued)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.LookupTokenIdentity(secret, "mcp.green.example", issued.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, mcpClientData{ID: token.ID})
	}
	body = referenceResponse(t, server, tokenProfileRequest(session.ID), 200)
	refs := assertAuthPageReferences(t, body, &banner)
	requirePageForm(t, refs, "/tokens")
	for _, row := range rows {
		action := "enable"
		if row.Enabled {
			action = "disable"
		}
		requirePageForm(t, refs, "/tokens/"+row.ID+"/"+action)
		requirePageForm(t, refs, "/tokens/"+row.ID+"/delete")
	}
	for _, client := range clients {
		requirePageForm(t, refs, "/tokens/"+client.ID+"/revoke")
	}
	for _, form := range []url.Values{{"name": {""}, "expires": {"90d"}}, {"name": {"valid"}, "expires": {"bad"}}, {"name": {"\t"}, "expires": {"bad"}}, {"name": {strings.Repeat("z", 65)}, "expires": {"never"}}} {
		body = referenceResponse(t, server, tokenRequest("/tokens", session.ID, form), 400)
		requirePageForm(t, assertAuthPageReferences(t, body, &banner), "/tokens")
	}
	body = referenceResponse(t, server, tokenRequest("/tokens", session.ID, url.Values{"name": {"created <& fixture"}, "expires": {"never"}}), 200)
	assertAuthPageReferences(t, body, &banner)
}

func TestApprovePageReferenceProperties(t *testing.T) {
	// R-3XX5-S23D R-7ZXA-Q4XF: the D09 approve page also has only shared resource and own-origin navigation targets.
	for _, publicURL := range []string{"", "http://auth.wip.localhost:7400"} {
		fixture := newOAuthFixture(t, publicURL)
		banner := page.Banner{Service: "auth", Email: fixture.user.Email, ProfileURL: "/", LogoutURL: "/logout", Home: "https://outside.test/home", Tools: true}
		fixture.trail.server.cfg.Banner = func(page.User) page.Banner { return banner }
		for _, name := range []string{"fixture <&\" client", "https://outside.test/not-a-resource"} {
			client := fixture.client(t, name, oauthCallback)
			params := oauthAuthParams(client)
			response, _ := fixture.request(t, "GET", "/authorize?"+params.Encode(), "", fixture.session.ID)
			assertHTMLStatus(t, response, 200)
			assertAuthPageReferences(t, response.Body.String(), &banner)
		}
	}
}

func TestAuthReferencesAcrossDeclaredDataBranches(t *testing.T) {
	// R-3XX5-S23D R-7ZXA-Q4XF R-8157-3WO4: named-template data covers all relative-time and row branches.
	banner := testPageBanner(page.User{Email: "fixture@example.test", ProfileURL: "/", LogoutURL: "/logout"})
	for _, elapsed := range []elapsedData{{Unit: "now"}, {Unit: "minute", Count: 1}, {Unit: "minute", Count: 2}, {Unit: "hour", Count: 1}, {Unit: "hour", Count: 2}, {Unit: "day", Count: 1}, {Unit: "day", Count: 2}} {
		for _, enabled := range []bool{false, true} {
			row := tokenRowData{ID: "tok_0123456789ABCDEFGHJKMNPQRS", Name: "fixture <& token", Created: tokenTimeData{Datetime: "2026-10-07T00:00:00Z", Text: "fixture date"}, LastUsed: &tokenLastUsedData{Datetime: "2026-10-07T00:00:00Z", Title: "fixture time", Elapsed: elapsed}, Enabled: enabled}
			client := mcpClientData{ID: "tok_0123456789ABCDEFGHJKMNPQRT", Name: "fixture <& client", LastUsed: "2026-10-07T00:00:00Z", LastUsedElapsed: elapsed, Expired: enabled}
			data := authPageData{Banner: banner, Profile: &profilePageData{Apex: "example.test", Email: "fixture@example.test", Workspace: "example.test", Rows: []tokenRowData{row}, Create: tokenCreateData{Expiry: "90d"}, Clients: mcpClientsData{Clients: []mcpClientData{client}}}}
			refs := assertAuthPageReferences(t, expectedAuthTemplate(t, "page", data), &banner)
			action := "enable"
			if row.Enabled {
				action = "disable"
			}
			requirePageForm(t, refs, "/tokens/"+row.ID+"/"+action)
			requirePageForm(t, refs, "/tokens/"+row.ID+"/delete")
			requirePageForm(t, refs, "/tokens/"+client.ID+"/revoke")
		}
	}
}

func TestHTMLReferenceReader(t *testing.T) {
	// Reader fixtures exercise HTML vocabulary, not auth markup or hooks.
	remote := "https://outside.test/resource"
	cases := []struct {
		document         string
		resources, links []string
		forms            []htmlFormTarget
	}{
		{document: `<style>/* url(ignored) */ p { content:"url(ignored)"; background-image:image-set("` + remote + `" 1x) }</style>`, resources: []string{remote}},
		{document: `<svg><script href="` + remote + `"></script><rect fill="url(` + remote + `)"/><a xlink:href="` + remote + `"></a></svg>`, resources: []string{remote, remote}, links: []string{remote}},
		{document: `<form method=post action=/one><button formaction=/two></button></form>`, forms: []htmlFormTarget{{"POST", "/one"}, {"POST", "/two"}}},
		{document: `<div src="` + remote + `"></div>`, resources: nil},
		{document: `<image src="` + remote + `">`, resources: []string{remote}},
		{document: `<button formaction=/tokens formmethod=post></button><input type=text formaction=/tokens>`, forms: nil},
		{document: `<form method=get action=/tokens><button formmethod=post></button></form>`, forms: []htmlFormTarget{{"GET", "/tokens"}, {"POST", "/tokens"}}},
		{document: `<svg><rect fill="url(#paint)"/></svg>`, resources: nil},
		{document: `<!-- <img src=ignored> --><img SRC='` + remote + `?a=1&amp;b=2' src=ignored>`, resources: []string{remote + "?a=1&b=2"}},
		{document: `<textarea>&lt;img src=ignored&gt;</textarea><source srcset="` + remote + ` 1x, /_appkit/other 2x">`, resources: []string{remote, "/_appkit/other"}},
		{document: `<base href=/base><meta http-equiv=refresh content="0;url=/other">`, resources: nil},
	}
	for i, tc := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			refs := readPageReferences(t, tc.document, "")
			if !equalReferenceStrings(refs.resources, tc.resources) || !equalReferenceStrings(refs.links, tc.links) || len(refs.forms) != len(tc.forms) {
				t.Fatalf("references %+v, expected %+v", refs, tc)
			}
			for index, form := range tc.forms {
				if refs.forms[index] != form {
					t.Fatalf("form %+v expected %+v", refs.forms[index], form)
				}
			}
			if i == len(cases)-1 && (len(refs.bases) != 1 || len(refs.automatic) != 1) {
				t.Fatal("base/refresh references omitted")
			}
		})
	}
}
func equalReferenceStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
