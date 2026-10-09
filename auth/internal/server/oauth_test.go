package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

const oauthVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
const oauthChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
const oauthCallback = "http://localhost:53682/callback"

type oauthFixture struct {
	st      *store.Store
	trail   *trailFixture
	now     time.Time
	session store.Session
	user    store.User
}

func newOAuthFixture(t *testing.T, publicURL string) *oauthFixture {
	t.Helper()
	f := &oauthFixture{now: time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)}
	f.st = openServerStore(t, filepath.Join(t.TempDir(), "auth.db"), &identityRand{}, func() time.Time { return f.now })
	var err error
	f.user, _, err = f.st.UpsertUserOnLogin("issuer", "subject", "person@work.example", f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.session, err = f.st.CreateSession(f.user.ID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.trail = newTrail(t, Config{Store: f.st, Now: func() time.Time { return f.now }, PublicURL: publicURL}, nil)
	return f
}
func (f *oauthFixture) request(t *testing.T, method, path, body, session string) (*httptest.ResponseRecorder, []telemetry.Event) {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), method, "https://auth.sbx.ikigenba.dev"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", ownOrigin(r.Host, f.trail.server.cfg.PublicURL))
	r.Header.Set("X-Request-Id", "0123456789abcdef0123456789abcdef")
	if session != "" {
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: session, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	return f.trail.request(t, r)
}
func oauthObject(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &obj); err != nil {
		t.Fatalf("JSON %q: %v", w.Body.String(), err)
	}
	return obj
}
func oauthAssertError(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	obj := oauthObject(t, w)
	if w.Code != 400 || w.Header().Get("Content-Type") != "application/json" || len(obj) != 2 || obj["error"] != code {
		t.Fatalf("error response: %d %v %v", w.Code, w.Header(), obj)
	}
	description, ok := obj["error_description"].(string)
	if !ok || strings.ContainsAny(description, "\r\n") {
		t.Fatalf("description %#v", obj)
	}
}
func oauthAssertPlain(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	body := w.Body.String()
	if w.Code != status || w.Header().Get("Location") != "" || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || !strings.HasSuffix(body, "\n") || len(body) < 2 || strings.ContainsAny(body[:len(body)-1], "\r\n") {
		t.Fatalf("plain response: %d %v %q", w.Code, w.Header(), body)
	}
}
func (f *oauthFixture) client(t *testing.T, name string, uris ...string) store.Client {
	t.Helper()
	c, err := f.st.RegisterClient(name, uris, f.now)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func oauthAuthParams(client store.Client) url.Values {
	return url.Values{"client_id": {client.ID}, "redirect_uri": {oauthCallback}, "response_type": {"code"}, "code_challenge": {oauthChallenge}, "code_challenge_method": {"S256"}, "state": {"a b&c=d"}}
}
func oauthCount(t *testing.T, st *store.Store, table string) int {
	t.Helper()
	var count int
	err := serverStoreDB(t, st).Read(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&count)
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}
func oauthDomain(t *testing.T, events []telemetry.Event, name, user, key, value string) {
	t.Helper()
	if len(events) != 3 || events[0].Name != "request.started" || events[2].Name != "request.finished" || events[1].Name != name || events[1].User != user || !reflect.DeepEqual(events[1].Attrs, telemetry.Attrs{key: value}) || events[1].RequestID != events[0].RequestID {
		t.Fatalf("events %#v", events)
	}
}
func oauthNoDomain(t *testing.T, events []telemetry.Event) {
	t.Helper()
	if len(events) != 2 || events[0].Name != "request.started" || events[1].Name != "request.finished" {
		t.Fatalf("events %#v", events)
	}
}

func TestOAuthMetadataAndExactRoutes(t *testing.T) {
	// R-51TQ-VE5Y R-549J-MXNC R-G6AC-62HG: public exact routes and store-independent metadata.
	for _, origin := range []string{"", "http://auth.wip.localhost:7400"} {
		f := newOAuthFixture(t, origin)
		serverStoreDB(t, f.st).SetFailing(true)
		w, events := f.request(t, "GET", "/.well-known/oauth-authorization-server", "", "")
		oauthNoDomain(t, events)
		o := origin
		if o == "" {
			o = "https://auth.sbx.ikigenba.dev"
		}
		want := map[string]any{"issuer": o, "authorization_endpoint": o + "/authorize", "token_endpoint": o + "/token", "registration_endpoint": o + "/register", "response_types_supported": []any{"code"}, "grant_types_supported": []any{"authorization_code"}, "code_challenge_methods_supported": []any{"S256"}, "token_endpoint_auth_methods_supported": []any{"none"}}
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || !reflect.DeepEqual(oauthObject(t, w), want) {
			t.Fatalf("metadata %d %q", w.Code, w.Body.String())
		}
		serverStoreDB(t, f.st).SetFailing(false)
		for _, route := range []struct{ method, path string }{{"GET", "/.well-known/oauth-authorization-server"}, {"POST", "/register"}, {"GET", "/authorize"}, {"POST", "/authorize"}, {"POST", "/token"}} {
			for _, suffix := range []string{"/extra", "x"} {
				w, _ := f.request(t, route.method, route.path+suffix, "", "")
				if w.Code != 404 {
					t.Fatalf("nonexact %s: %d", route.path+suffix, w.Code)
				}
			}
		}
	}
}

func TestOAuthRegistration(t *testing.T) {
	// R-J1ZW-5KG5 R-J37S-JC6U R-AGXA-RIWF R-4ZDY-3UOK: ordered metadata/URI validation and OAuth error shape.
	base := `{"redirect_uris":["http://localhost:53682/callback"]}`
	longURI := "https://app.example/" + strings.Repeat("a", 2048-len("https://app.example/"))
	cases := []struct{ body, code string }{{"[]", "invalid_client_metadata"}, {"null", "invalid_client_metadata"}, {base + " trailing", "invalid_client_metadata"}, {base + strings.Repeat(" ", 65537-len(base)), "invalid_client_metadata"}, {`{"client_name":null,"redirect_uris":[]}`, "invalid_client_metadata"}, {`{"client_name":"","redirect_uris":[]}`, "invalid_client_metadata"}, {`{"client_name":"a\u0000b","redirect_uris":[]}`, "invalid_client_metadata"}, {`{"redirect_uris":[9,"http://localhost/call\u0000back"]}`, "invalid_client_metadata"}, {fmt.Sprintf(`{"client_name":%q,"redirect_uris":[]}`, strings.Repeat("é", 201)), "invalid_client_metadata"}, {"{}", "invalid_redirect_uri"}, {`{"redirect_uris":[]}`, "invalid_redirect_uri"}, {`{"redirect_uris":"http://localhost/cb"}`, "invalid_redirect_uri"}, {`{"redirect_uris":[null]}`, "invalid_redirect_uri"}, {`{"redirect_uris":[9]}`, "invalid_redirect_uri"}}
	for _, uri := range []string{"http://example.com/callback", "http://LOCALHOST:1/cb", "http://localhost.example.com/cb", "http://u@localhost/cb", "myapp://callback", "http://localhost/cb#x", "http://localhost/cb#", "https:///cb", "not a url", "/callback", longURI + "a", "http://localhost:abc/cb"} {
		body, _ := json.Marshal(map[string]any{"redirect_uris": []string{uri}})
		cases = append(cases, struct{ body, code string }{string(body), "invalid_redirect_uri"})
	}
	eleven := make([]string, 11)
	for i := range eleven {
		eleven[i] = oauthCallback
	}
	body, _ := json.Marshal(map[string]any{"redirect_uris": eleven})
	cases = append(cases, struct{ body, code string }{string(body), "invalid_redirect_uri"})
	f := newOAuthFixture(t, "")
	for _, test := range cases {
		w, events := f.request(t, "POST", "/register", test.body, "")
		oauthAssertError(t, w, test.code)
		oauthNoDomain(t, events)
		if oauthCount(t, f.st, "clients") != 0 {
			t.Fatal("refused registration persisted")
		}
	}
	// R-IPOI-DPUH R-IQWE-RHL6 R-62EQ-CIES: exact registration object, one client, no token/session, no credentials/origin check.
	if DefaultClientName == "" {
		t.Fatal("empty default client name")
	}
	valid := []string{base, base + strings.Repeat(" ", 65536-len(base)), fmt.Sprintf(`{"client_name":%q,"redirect_uris":[%q],"scope":"\u0000","token_endpoint_auth_method":"ignored","grant_types":9}`, strings.Repeat("é", 200), longURI), `{"client_name":"a\\u0000b","redirect_uris":["http://127.0.0.1:9/"]}`}
	ten := make([]string, 10)
	for i := range ten {
		ten[i] = "https://app.example/cb"
	}
	body, _ = json.Marshal(map[string]any{"redirect_uris": ten})
	valid = append(valid, string(body))
	distinctURIs := []string{"https://z.example/callback", "http://localhost:61000/callback", "https://a.example/return"}
	body, _ = json.Marshal(map[string]any{"client_name": "Ordered client", "redirect_uris": distinctURIs})
	valid = append(valid, string(body))
	for _, body := range valid {
		before := oauthCount(t, f.st, "clients")
		sessions := oauthCount(t, f.st, "sessions")
		r := httptest.NewRequestWithContext(context.Background(), "POST", "https://auth.sbx.ikigenba.dev/register", strings.NewReader(body))
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Authorization", "Bearer invalid")
		r.Header.Set("Content-Type", "text/plain")
		w, events := f.trail.request(t, r)
		obj := oauthObject(t, w)
		if w.Code != 201 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" || len(obj) != 7 {
			t.Fatalf("registered: %d %v", w.Code, obj)
		}
		c, err := f.st.LookupClient(obj["client_id"].(string), f.now)
		if err != nil {
			t.Fatal(err)
		}
		var input map[string]any
		_ = json.Unmarshal([]byte(body), &input)
		name, ok := input["client_name"].(string)
		if !ok {
			name = DefaultClientName
		}
		want := map[string]any{"client_id": c.ID, "client_id_issued_at": float64(f.now.Unix()), "client_name": name, "redirect_uris": input["redirect_uris"], "grant_types": []any{"authorization_code"}, "response_types": []any{"code"}, "token_endpoint_auth_method": "none"}
		if !reflect.DeepEqual(obj, want) || c.Name != name || oauthCount(t, f.st, "clients") != before+1 || oauthCount(t, f.st, "tokens") != 0 || oauthCount(t, f.st, "sessions") != sessions {
			t.Fatalf("registration persisted %#v %#v", obj, c)
		}
		expectedURIs, _ := json.Marshal(c.RedirectURIs)
		actualURIs, _ := json.Marshal(obj["redirect_uris"])
		if !bytes.Equal(expectedURIs, actualURIs) {
			t.Fatal("URI order")
		}
		oauthDomain(t, events, "client.registered", "", "client", c.ID)
	}
}

func TestOAuthAuthorizationRefusalsAndFaultOrder(t *testing.T) {
	// R-ALSW-ALV7 R-AN0S-ODLW R-J5NL-AVO8 R-4UIC-KRPS R-4WY5-CB76: trust URI before faults, exact/loopback matching, escaped state.
	// R-4QUN-FGHP R-4S2J-T88E: resource definition and first/body-only parameters.
	f := newOAuthFixture(t, "")
	c := f.client(t, "Client", oauthCallback)
	two := f.client(t, "Two", oauthCallback, "https://app.example/cb")
	for _, change := range []func(url.Values){func(p url.Values) { p.Del("client_id") }, func(p url.Values) { p.Set("client_id", "unknown") }, func(p url.Values) { p.Set("client_id", c.ID+"\x00") }, func(p url.Values) { p.Set("client_id", two.ID); p.Del("redirect_uri") }, func(p url.Values) { p.Set("redirect_uri", oauthCallback+"\x00") }} {
		p := oauthAuthParams(c)
		change(p)
		for _, method := range []string{"GET", "POST"} {
			path, body := "/authorize?"+p.Encode(), ""
			if method == "POST" {
				path, body = "/authorize", p.Encode()
			}
			w, e := f.request(t, method, path, body, f.session.ID)
			oauthAssertPlain(t, w, 400)
			oauthNoDomain(t, e)
		}
	}
	for _, uri := range []string{"https://evil.example/callback", "http://127.0.0.1:53682/callback", "https://localhost:53682/callback", "http://localhost:53682/other", oauthCallback + "/"} {
		p := oauthAuthParams(c)
		p.Set("redirect_uri", uri)
		w, _ := f.request(t, "GET", "/authorize?"+p.Encode(), "", "")
		oauthAssertPlain(t, w, 400)
	}
	faults := []struct{ key, value, code string }{{"response_type", "", "invalid_request"}, {"response_type", "token", "unsupported_response_type"}, {"code_challenge", "", "invalid_request"}, {"code_challenge_method", "plain", "invalid_request"}, {"code_challenge_method", "", "invalid_request"}, {"state", "a\x00b", "invalid_request"}, {"scope", "\x00", "invalid_request"}, {"x\x00", "1", "invalid_request"}, {"resource", "https://repos.sbx.ikigenba.dev/mcp", "invalid_target"}}
	for _, test := range faults {
		for _, method := range []string{"GET", "POST"} {
			p := oauthAuthParams(c)
			p.Set(test.key, test.value)
			if test.key == "response_type" && test.value == "token" {
				p.Set("code_challenge_method", "plain")
			}
			if test.key == "scope" {
				p.Set("response_type", "token")
			}
			path, body := "/authorize?"+p.Encode(), ""
			if method == "POST" {
				path, body = "/authorize?client_id=ignored", p.Encode()
			}
			w, e := f.request(t, method, path, body, "")
			location := oauthCallback + "?error=" + test.code + "&state=" + url.QueryEscape(p.Get("state"))
			if w.Code != 302 || !reflect.DeepEqual(w.Header().Values("Location"), []string{location}) {
				t.Fatalf("fault %+v %d %q", test, w.Code, w.Header().Get("Location"))
			}
			oauthNoDomain(t, e)
		}
	}
	p := oauthAuthParams(c)
	p["state"] = []string{"ok", "a\x00b"}
	w, _ := f.request(t, "GET", "/authorize?"+p.Encode(), "", "")
	if w.Header().Get("Location") != oauthCallback+"?error=invalid_request&state=ok" {
		t.Fatal(w.Header())
	}
	p = oauthAuthParams(c)
	p["response_type"] = []string{"", "code"}
	w, _ = f.request(t, "GET", "/authorize?"+p.Encode(), "", "")
	if !strings.Contains(w.Header().Get("Location"), "error=invalid_request") {
		t.Fatal(w.Header())
	}
	for _, resource := range []string{"https://mcp.sbx.ikigenba.dev/mcp", "HTTPS://MCP.sbx.ikigenba.dev/other", "https://mcp.sbx.ikigenba.dev"} {
		p := oauthAuthParams(c)
		p.Set("resource", resource)
		w, _ := f.request(t, "GET", "/authorize?"+p.Encode(), "", f.session.ID)
		if w.Code != 200 {
			t.Fatal(resource, w.Code)
		}
	}
	for _, resource := range []string{"http://mcp.sbx.ikigenba.dev/mcp", "https://mcp.sbx.ikigenba.dev:8443/mcp", "https://u@mcp.sbx.ikigenba.dev/mcp", "https://mcp.sbx.ikigenba.dev/mcp#", "/mcp"} {
		p := oauthAuthParams(c)
		p.Set("resource", resource)
		w, _ := f.request(t, "GET", "/authorize?"+p.Encode(), "", f.session.ID)
		if !strings.Contains(w.Header().Get("Location"), "error=invalid_target") {
			t.Fatal(resource, w.Header())
		}
	}
	if oauthCount(t, f.st, "auth_codes") != 0 {
		t.Fatal("faults created code")
	}
}

func TestOAuthAuthorizationSessionAndDecisions(t *testing.T) {
	// R-BX8J-UD7Y R-FWJ5-3WJW R-50LU-HMF9: raw GET return, re-encoded POST without decision, cookie-only live session.
	// R-FXR1-HOAL R-FYYX-VG1A R-G06U-97RZ R-G52F-SAQR: decision behavior, code binding and domain events.
	f := newOAuthFixture(t, "")
	c := f.client(t, "Client", oauthCallback)
	p := oauthAuthParams(c)
	query := "state=a%20b&" + p.Encode()
	w, e := f.request(t, "GET", "/authorize?"+query, "", "")
	want := "/?return=" + url.QueryEscape("https://auth.sbx.ikigenba.dev/authorize?"+query)
	if w.Code != 302 || w.Header().Get("Location") != want {
		t.Fatal(w.Code, w.Header())
	}
	oauthNoDomain(t, e)
	p.Set("decision", "approve")
	body := p.Encode()
	p.Del("decision")
	w, e = f.request(t, "POST", "/authorize?state=ignored", body, "")
	want = "/?return=" + url.QueryEscape("https://auth.sbx.ikigenba.dev/authorize?"+p.Encode())
	if w.Code != 302 || w.Header().Get("Location") != want {
		t.Fatal(w.Code, w.Header())
	}
	oauthNoDomain(t, e)
	for _, decision := range []string{"", "unknown", "deny", "approve"} {
		p := oauthAuthParams(c)
		p.Set("decision", decision)
		p.Set("redirect_uri", "http://localhost:61000/callback")
		w, e := f.request(t, "POST", "/authorize", p.Encode(), f.session.ID)
		switch decision {
		case "", "unknown":
			oauthAssertPlain(t, w, 400)
			oauthNoDomain(t, e)
		case "deny":
			if w.Code != 302 || w.Header().Get("Location") != "http://localhost:61000/callback?error=access_denied&state=a+b%26c%3Dd" {
				t.Fatal(w.Code, w.Header())
			}
			oauthDomain(t, e, "client.denied", f.user.ID, "client", c.ID)
		case "approve":
			location, err := url.Parse(w.Header().Get("Location"))
			if err != nil || w.Code != 302 {
				t.Fatal(w.Code, w.Header())
			}
			code, err := f.st.ConsumeAuthCode(location.Query().Get("code"), f.now)
			if err != nil {
				t.Fatal(err)
			}
			want := store.AuthCode{Code: code.Code, ClientID: c.ID, UserID: f.user.ID, RedirectURI: p.Get("redirect_uri"), Challenge: oauthChallenge, Resource: "https://mcp.sbx.ikigenba.dev/mcp", IssuedAt: f.now}
			if !reflect.DeepEqual(code, want) || oauthCount(t, f.st, "auth_codes") != 0 {
				t.Fatalf("code %#v", code)
			}
			oauthDomain(t, e, "client.approved", f.user.ID, "client", c.ID)
		}
		if oauthCount(t, f.st, "tokens") != 0 {
			t.Fatal("authorization minted token")
		}
	}
	// Exact redirect URI containing query retains it; no state means no state pair.
	c = f.client(t, "HTTPS", "https://app.example/cb?x=1")
	p = oauthAuthParams(c)
	p.Del("redirect_uri")
	p.Del("state")
	p.Set("decision", "deny")
	w, _ = f.request(t, "POST", "/authorize", p.Encode(), f.session.ID)
	if w.Header().Get("Location") != "https://app.example/cb?x=1&error=access_denied" {
		t.Fatal(w.Header())
	}
	f.now = f.now.Add(store.SessionIdle + time.Nanosecond)
	p = oauthAuthParams(c)
	p.Set("redirect_uri", c.RedirectURIs[0])
	w, _ = f.request(t, "GET", "/authorize?"+p.Encode(), "", f.session.ID)
	if !strings.HasPrefix(w.Header().Get("Location"), "/?return=") {
		t.Fatal("expired session", w.Code)
	}
}

func TestOAuthAuthorizeOriginAndStoreFailures(t *testing.T) {
	// R-5Q7Q-ISZU R-GDOY-JEGC R-G6AC-62HG: origin first even during store failure, plain 500 for actual store faults, no domain events.
	f := newOAuthFixture(t, "")
	c := f.client(t, "Client", oauthCallback)
	p := oauthAuthParams(c)
	p.Set("decision", "approve")
	serverStoreDB(t, f.st).SetFailing(true)
	for _, origin := range []string{"", "https://AUTH.sbx.ikigenba.dev", "https://evil.example"} {
		r := httptest.NewRequestWithContext(context.Background(), "POST", "https://auth.sbx.ikigenba.dev/authorize", strings.NewReader(p.Encode()))
		r.Header.Set("Origin", origin)
		w, e := f.trail.request(t, r)
		oauthAssertPlain(t, w, 403)
		oauthNoDomain(t, e)
	}
	for _, test := range []struct{ method, path, body string }{{"POST", "/register", `{"redirect_uris":["http://localhost/cb"]}`}, {"GET", "/authorize?" + p.Encode(), ""}, {"POST", "/authorize", p.Encode()}, {"POST", "/token", "code=anything"}} {
		w, e := f.request(t, test.method, test.path, test.body, f.session.ID)
		oauthAssertPlain(t, w, 500)
		oauthNoDomain(t, e)
		if w.Header().Get(HeaderUserID) != "" || w.Header().Get(HeaderUserEmail) != "" {
			t.Fatal("identity leaked")
		}
	}
}

func TestOAuthApprovePage(t *testing.T) {
	// R-4PMR-1OR0 R-4VQ8-YJGH R-531N-95WN R-RYK1-WX4A: canonical template bytes and exact approve data, one injected banner call, no mutations.
	for _, origin := range []string{"", "http://auth.wip.localhost:7400"} {
		for _, name := range []string{`fixture<&"`, "a\tb", "  x", "client\x00<&name"} {
			for _, state := range []string{"", "a b&c=d"} {
				f := newOAuthFixture(t, origin)
				c := f.client(t, name, oauthCallback)
				p := oauthAuthParams(c)
				p.Set("state", state)
				p.Set("redirect_uri", "http://localhost:61000/callback")
				banner := testPageBanner(page.User{Email: f.user.Email, ProfileURL: "/", LogoutURL: "/logout"})
				banner.Trail = []page.Level{{Name: "approve supplied trail", URL: "/supplied-approve"}}
				wantBanner := banner
				wantBanner.Trail = nil
				calls := 0
				f.trail.server.cfg.Banner = func(user page.User) page.Banner {
					calls++
					if user.Email != f.user.Email || user.ProfileURL != "/" || user.LogoutURL != "/logout" {
						t.Fatalf("banner user %#v", user)
					}
					return banner
				}
				before := map[string][][]any{}
				for _, table := range []string{"clients", "tokens", "auth_codes"} {
					before[table] = oauthRows(t, f.st, table)
				}
				gateway := "mcp.sbx.ikigenba.dev"
				resource := "https://" + gateway + "/mcp"
				if origin != "" {
					gateway = "mcp.wip.localhost:7400"
					resource = "http://" + gateway + "/mcp"
				}
				if state != "" {
					resource = strings.TrimSuffix(resource, "/mcp") + "/other?client=custom"
					p.Set("resource", resource)
				}
				w, e := f.request(t, "GET", "/authorize?"+p.Encode(), "", f.session.ID)
				oauthNoDomain(t, e)
				data := map[string]any{"Banner": wantBanner, "ClientName": name, "Gateway": gateway, "Until": f.now.Add(store.ClientTokenTTL).UTC().Format("2006-01-02"), "ReturnHost": "localhost:61000", "ClientID": c.ID, "RedirectURI": p.Get("redirect_uri"), "Challenge": oauthChallenge, "State": state, "Resource": resource}
				expected := expectedAuthTemplate(t, "approve", data)
				if w.Code != 200 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Body.String() != expected || calls != 1 {
					t.Fatalf("approve response %d calls=%d %q", w.Code, calls, w.Body.String())
				}
				for _, table := range []string{"clients", "tokens", "auth_codes"} {
					if !reflect.DeepEqual(before[table], oauthRows(t, f.st, table)) {
						t.Fatalf("GET mutated %s", table)
					}
				}
			}
		}
	}
}

// oauthRows reads domain state through the store's public database seam.
func oauthRows(t *testing.T, st *store.Store, table string) [][]any {
	t.Helper()
	result := [][]any{}
	err := serverStoreDB(t, st).Read(context.Background(), func(tx *sql.Tx) error {
		queries := map[string]string{"clients": "SELECT * FROM clients ORDER BY 1", "tokens": "SELECT * FROM tokens ORDER BY 1", "auth_codes": "SELECT * FROM auth_codes ORDER BY 1"}
		rows, err := tx.QueryContext(context.Background(), queries[table])
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		columns, err := rows.Columns()
		if err != nil {
			return err
		}
		for rows.Next() {
			values := make([]any, len(columns))
			targets := make([]any, len(columns))
			for i := range targets {
				targets[i] = &values[i]
			}
			if err := rows.Scan(targets...); err != nil {
				return err
			}
			result = append(result, values)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestOAuthAuthorizationPreservesAndAddsOnlyCode(t *testing.T) {
	// R-G06U-97RZ R-RYK1-WX4A R-FYYX-VG1A R-FXR1-HOAL: table-level evidence for repeated rendering, denial, invalid decision and exact one code addition.
	f := newOAuthFixture(t, "")
	c := f.client(t, "Persistent client", oauthCallback)
	_, _, err := f.st.CreateToken(f.user.ID, "prior token", store.ExpiryNever, f.now)
	if err != nil {
		t.Fatal(err)
	}
	p := oauthAuthParams(c)
	p.Set("decision", "approve")
	for range 3 {
		w, _ := f.request(t, "POST", "/authorize", p.Encode(), f.session.ID)
		if w.Code != 302 {
			t.Fatal(w.Code)
		}
	}
	clientRows := oauthRows(t, f.st, "clients")
	tokenRows := oauthRows(t, f.st, "tokens")
	codeRows := oauthRows(t, f.st, "auth_codes")
	for _, decision := range []string{"GET", "GET", "GET", "deny", "invalid"} {
		method, path, body := "POST", "/authorize", ""
		if decision == "GET" {
			method, path, body = "GET", "/authorize?"+p.Encode(), ""
		} else {
			p.Set("decision", decision)
			body = p.Encode()
		}
		w, _ := f.request(t, method, path, body, f.session.ID)
		if decision == "GET" && w.Code != 200 || decision == "deny" && w.Code != 302 || decision == "invalid" && w.Code != 400 {
			t.Fatal(decision, w.Code)
		}
		if decision == "GET" {
			data := map[string]any{"Banner": testPageBanner(page.User{Email: f.user.Email, ProfileURL: "/", LogoutURL: "/logout"}), "ClientName": c.Name, "Gateway": "mcp.sbx.ikigenba.dev", "Until": f.now.Add(store.ClientTokenTTL).UTC().Format("2006-01-02"), "ReturnHost": "localhost:53682", "ClientID": c.ID, "RedirectURI": oauthCallback, "Challenge": oauthChallenge, "State": p.Get("state"), "Resource": "https://mcp.sbx.ikigenba.dev/mcp"}
			assertAuthTemplate(t, w.Body.String(), "approve", data)
		}
		if !reflect.DeepEqual(clientRows, oauthRows(t, f.st, "clients")) || !reflect.DeepEqual(tokenRows, oauthRows(t, f.st, "tokens")) || !reflect.DeepEqual(codeRows, oauthRows(t, f.st, "auth_codes")) {
			t.Fatal("nonapproval changed domain")
		}
	}
	p.Set("decision", "approve")
	p.Set("resource", "https://mcp.sbx.ikigenba.dev/other")
	w, _ := f.request(t, "POST", "/authorize", p.Encode(), f.session.ID)
	codesAfter := oauthRows(t, f.st, "auth_codes")
	if len(codesAfter) != len(codeRows)+1 || !reflect.DeepEqual(clientRows, oauthRows(t, f.st, "clients")) || !reflect.DeepEqual(tokenRows, oauthRows(t, f.st, "tokens")) {
		t.Fatal("approval domain changes")
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code, err := f.st.ConsumeAuthCode(location.Query().Get("code"), f.now)
	if err != nil || code.Resource != p.Get("resource") {
		t.Fatal(code, err)
	}
}

func TestOAuthSessionAndCodeWriteFailures(t *testing.T) {
	// R-GDOY-JEGC: authorization-code write faults are also plain 500, without domain events.
	f := newOAuthFixture(t, "")
	c := f.client(t, "Client", oauthCallback)
	p := oauthAuthParams(c)
	err := serverStoreDB(t, f.st).Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `CREATE TRIGGER refuse_code BEFORE INSERT ON auth_codes BEGIN SELECT RAISE(ABORT,'fixture refusal'); END`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	p.Set("decision", "approve")
	w, e := f.request(t, "POST", "/authorize", p.Encode(), f.session.ID)
	oauthAssertPlain(t, w, 500)
	oauthNoDomain(t, e)
	if oauthCount(t, f.st, "auth_codes") != 0 {
		t.Fatal("failed approval wrote code")
	}
}

func TestOAuthAuthorizationUsesFirstCookieAndBodyValues(t *testing.T) {
	// R-50LU-HMF9 R-4S2J-T88E: first cookie and first body value govern authorization; bearer credentials and URL query do not.
	f := newOAuthFixture(t, "")
	c := f.client(t, "Client", oauthCallback)
	p := oauthAuthParams(c)
	p.Set("decision", "deny")
	for _, firstCookie := range []string{f.session.ID, "unknown"} {
		r := httptest.NewRequestWithContext(context.Background(), "POST", "https://auth.sbx.ikigenba.dev/authorize?client_id=unknown&decision=approve&state=ignored", strings.NewReader(p.Encode()+"&decision=approve"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "https://auth.sbx.ikigenba.dev")
		r.Header.Set("Authorization", "Bearer unknown")
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: firstCookie, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: f.session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		w, e := f.trail.request(t, r)
		if firstCookie == f.session.ID {
			if w.Header().Get("Location") != oauthCallback+"?error=access_denied&state=a+b%26c%3Dd" {
				t.Fatal(w.Header())
			}
			oauthDomain(t, e, "client.denied", f.user.ID, "client", c.ID)
		} else {
			if !strings.HasPrefix(w.Header().Get("Location"), "/?return=") {
				t.Fatal(w.Header())
			}
			oauthNoDomain(t, e)
		}
	}
	_, secret, err := f.st.CreateToken(f.user.ID, "Live personal API token", store.ExpiryNever, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if identity, err := f.st.LookupTokenIdentity(secret, "mcp.sbx.ikigenba.dev", f.now); err != nil || identity.UserID != f.user.ID {
		t.Fatalf("fixture bearer is not live: %#v %v", identity, err)
	}
	for _, method := range []string{"GET", "POST"} {
		for _, sessionID := range []string{"", "unknown"} {
			path, body := "/authorize?"+p.Encode(), ""
			if method == "POST" {
				path, body = "/authorize", p.Encode()
			}
			r := httptest.NewRequestWithContext(context.Background(), method, "https://auth.sbx.ikigenba.dev"+path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", "https://auth.sbx.ikigenba.dev")
			r.Header.Set("Authorization", "Bearer "+secret)
			if sessionID != "" {
				r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessionID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			}
			w, events := f.trail.request(t, r)
			if w.Code != 302 || !strings.HasPrefix(w.Header().Get("Location"), "/?return=") {
				t.Fatalf("%s bearer with session %q: %d %v", method, sessionID, w.Code, w.Header())
			}
			oauthNoDomain(t, events)
		}
	}
	if oauthCount(t, f.st, "auth_codes") != 0 {
		t.Fatal("body/query precedence produced code")
	}
}
