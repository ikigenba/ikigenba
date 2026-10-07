package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func oauthStoredCode(t *testing.T, f *oauthFixture, c store.Client) store.AuthCode {
	t.Helper()
	code, err := f.st.CreateAuthCode(c.ID, f.user.ID, oauthCallback, oauthChallenge, "https://mcp.sbx.ikigenba.dev/mcp", f.now)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func oauthTokenParams(code store.AuthCode) url.Values {
	return url.Values{"grant_type": {"authorization_code"}, "code": {code.Code}, "redirect_uri": {code.RedirectURI}, "client_id": {code.ClientID}, "code_verifier": {oauthVerifier}}
}

func oauthTokenRequest(t *testing.T, f *oauthFixture, p url.Values) (*httptest.ResponseRecorder, []telemetry.Event) {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), "POST", "https://auth.sbx.ikigenba.dev/token", strings.NewReader(p.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://untrusted.example")
	r.Header.Set("Authorization", "Bearer irrelevant-credential")
	r.Header.Set("X-Request-Id", "fedcba9876543210fedcba9876543210")
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "irrelevant-cookie-value", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return f.trail.request(t, r)
}

func TestOAuthTokenOrderedErrorsAndConsumption(t *testing.T) {
	// R-B2VH-NE8X R-G1EQ-MZIO R-G2MN-0R9D: all error classes consume a presented stored code, with ordered validation, no-store, and replay refusal.
	cases := []struct {
		name, want string
		change     func(*oauthFixture, url.Values)
	}{
		{"refresh grant first", "unsupported_grant_type", func(_ *oauthFixture, p url.Values) {
			p.Set("grant_type", "refresh_token")
			p.Del("code_verifier")
			p.Set("resource", "/wrong")
		}},
		{"client credentials", "unsupported_grant_type", func(_ *oauthFixture, p url.Values) { p.Set("grant_type", "client_credentials") }},
		{"case sensitive grant", "unsupported_grant_type", func(_ *oauthFixture, p url.Values) { p.Set("grant_type", "AUTHORIZATION_CODE") }},
		{"wrong client before resource", "invalid_grant", func(_ *oauthFixture, p url.Values) { p.Set("client_id", "unknown-client"); p.Set("resource", "/wrong") }},
		{"redirect byte equality", "invalid_grant", func(_ *oauthFixture, p url.Values) { p.Set("redirect_uri", "http://localhost:61000/callback") }},
		{"wrong verifier", "invalid_grant", func(_ *oauthFixture, p url.Values) { p.Set("code_verifier", oauthVerifier+"x") }},
		{"challenge is not a verifier", "invalid_grant", func(_ *oauthFixture, p url.Values) { p.Set("code_verifier", oauthChallenge) }},
		{"expired code", "invalid_grant", func(f *oauthFixture, _ url.Values) { f.now = f.now.Add(store.AuthCodeTTL + time.Nanosecond) }},
	}
	for _, key := range []string{"grant_type", "redirect_uri", "client_id", "code_verifier"} {
		for _, empty := range []bool{false, true} {
			cases = append(cases, struct {
				name, want string
				change     func(*oauthFixture, url.Values)
			}{fmt.Sprintf("missing %s empty=%v", key, empty), "invalid_request", func(_ *oauthFixture, p url.Values) {
				if empty {
					p.Set(key, "")
				} else {
					p.Del(key)
				}
				p.Set("resource", "/wrong")
			}})
		}
	}
	for _, resource := range []string{"https://repos.sbx.ikigenba.dev/mcp", "http://mcp.sbx.ikigenba.dev/mcp", "https://mcp.sbx.ikigenba.dev:8443/mcp", "https://u@mcp.sbx.ikigenba.dev/mcp", "https://mcp.sbx.ikigenba.dev/mcp#", "/mcp", "https://mcp.sbx.ikigenba.dev:abc/mcp"} {
		cases = append(cases, struct {
			name, want string
			change     func(*oauthFixture, url.Values)
		}{"resource " + resource, "invalid_target", func(_ *oauthFixture, p url.Values) { p.Set("resource", resource) }})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOAuthFixture(t, "")
			c := f.client(t, "Token client", oauthCallback)
			code := oauthStoredCode(t, f, c)
			p := oauthTokenParams(code)
			tc.change(f, p)
			w, e := oauthTokenRequest(t, f, p)
			oauthAssertError(t, w, tc.want)
			oauthNoDomain(t, e)
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Header())
			}
			if _, err := f.st.ConsumeAuthCode(code.Code, f.now); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("code not consumed: %v", err)
			}
			w, _ = oauthTokenRequest(t, f, oauthTokenParams(code))
			oauthAssertError(t, w, "invalid_grant")
			if oauthCount(t, f.st, "tokens") != 0 {
				t.Fatal("failure minted token")
			}
		})
	}
	// S256 compares the unpadded encoding exactly, rather than accepting a padded stored challenge.
	for _, challenge := range []string{oauthChallenge + "=", "different-challenge"} {
		f := newOAuthFixture(t, "")
		c := f.client(t, "Challenge client", oauthCallback)
		code, err := f.st.CreateAuthCode(c.ID, f.user.ID, oauthCallback, challenge, "https://mcp.sbx.ikigenba.dev/mcp", f.now)
		if err != nil {
			t.Fatal(err)
		}
		w, _ := oauthTokenRequest(t, f, oauthTokenParams(code))
		oauthAssertError(t, w, "invalid_grant")
		if _, err := f.st.ConsumeAuthCode(code.Code, f.now); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("mismatched challenge did not consume code")
		}
	}
	// Missing code must leave a different stored code untouched; both absent and empty count as missing.
	for _, empty := range []bool{false, true} {
		f := newOAuthFixture(t, "")
		c := f.client(t, "Client", oauthCallback)
		code := oauthStoredCode(t, f, c)
		p := oauthTokenParams(code)
		if empty {
			p.Set("code", "")
		} else {
			p.Del("code")
		}
		w, _ := oauthTokenRequest(t, f, p)
		oauthAssertError(t, w, "invalid_request")
		if got, err := f.st.ConsumeAuthCode(code.Code, f.now); err != nil || !reflect.DeepEqual(got, code) {
			t.Fatalf("missing code consumed another: %#v %v", got, err)
		}
	}
	f := newOAuthFixture(t, "")
	p := oauthTokenParams(store.AuthCode{Code: "unknown-code", ClientID: "unknown-client", RedirectURI: oauthCallback})
	w, _ := oauthTokenRequest(t, f, p)
	oauthAssertError(t, w, "invalid_grant")
	// A stored code for a disappeared client reaches resource validation before client lookup.
	c := f.client(t, "Stale", oauthCallback)
	f.now = f.now.Add(store.UnusedClientTTL + time.Nanosecond)
	code := oauthStoredCode(t, f, c)
	p = oauthTokenParams(code)
	p.Set("resource", "/wrong")
	w, _ = oauthTokenRequest(t, f, p)
	oauthAssertError(t, w, "invalid_target")
	code = oauthStoredCode(t, f, c)
	w, _ = oauthTokenRequest(t, f, oauthTokenParams(code))
	oauthAssertError(t, w, "invalid_grant")
	if _, err := f.st.ConsumeAuthCode(code.Code, f.now); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	// A handled store error is exempt from the consumption rule and the code can then be redeemed.
	c = f.client(t, "Live", oauthCallback)
	code = oauthStoredCode(t, f, c)
	serverStoreDB(t, f.st).SetFailing(true)
	w, e := oauthTokenRequest(t, f, oauthTokenParams(code))
	oauthAssertPlain(t, w, 500)
	oauthNoDomain(t, e)
	serverStoreDB(t, f.st).SetFailing(false)
	w, _ = oauthTokenRequest(t, f, oauthTokenParams(code))
	if w.Code != 200 {
		t.Fatalf("store error consumed code: %d %q", w.Code, w.Body.String())
	}
}

func TestOAuthTokenMintResponseAndPersistence(t *testing.T) {
	// R-G3UJ-EJ02 R-5YR1-776P R-64UJ-41W6: successful exchanges return the exact bearer object, add one unchanged-history token, and record one correctly attributed mint.
	for _, publicURL := range []string{"", "http://auth.wip.localhost:7400"} {
		for _, resource := range []string{"", "https://mcp.sbx.ikigenba.dev/other", "HTTPS://MCP.sbx.ikigenba.dev", "https://mcp.sbx.ikigenba.dev"} {
			f := newOAuthFixture(t, publicURL)
			c := f.client(t, "My MCP client", oauthCallback)
			if _, _, err := f.st.CreateToken(f.user.ID, "Existing", store.ExpiryNever, f.now); err != nil {
				t.Fatal(err)
			}
			before, err := f.st.ListTokens(f.user.ID)
			if err != nil {
				t.Fatal(err)
			}
			approval := f.now
			p := oauthAuthParams(c)
			p.Set("decision", "approve")
			p.Del("state")
			w, _ := f.request(t, "POST", "/authorize", p.Encode(), f.session.ID)
			if w.Code != 302 {
				t.Fatalf("approval %d %q", w.Code, w.Body.String())
			}
			u, err := url.Parse(w.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			code := store.AuthCode{Code: u.Query().Get("code"), ClientID: c.ID, RedirectURI: oauthCallback}
			f.now = f.now.Add(9 * time.Minute)
			p = oauthTokenParams(code)
			if resource != "" {
				if publicURL != "" {
					resource = strings.ReplaceAll(strings.ReplaceAll(resource, "https://mcp.sbx.ikigenba.dev", "http://mcp.wip.localhost:7400"), "HTTPS://MCP.sbx.ikigenba.dev", "HTTP://MCP.wip.localhost:7400")
				}
				p.Set("resource", resource)
			}
			w, e := oauthTokenRequest(t, f, p)
			obj := oauthObject(t, w)
			secret, ok := obj["access_token"].(string)
			if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" || len(obj) != 3 || obj["token_type"] != "Bearer" || obj["expires_in"] != float64(7776000) || !ok || !strings.HasPrefix(secret, "ikp_") || len(secret) != 56 {
				t.Fatalf("mint %d %v %v", w.Code, w.Header(), obj)
			}
			for _, ch := range secret[4:] {
				if !strings.ContainsRune(idcodec.Alphabet, ch) {
					t.Fatalf("secret character %q", ch)
				}
			}
			after, err := f.st.ListTokens(f.user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(before)+1 {
				t.Fatalf("token count %d", len(after))
			}
			old := map[string]store.Token{}
			for _, tok := range before {
				old[tok.ID] = tok
			}
			var minted store.Token
			for _, tok := range after {
				if prior, ok := old[tok.ID]; ok {
					if !reflect.DeepEqual(prior, tok) {
						t.Fatal("existing token changed")
					}
					delete(old, tok.ID)
				} else {
					minted = tok
				}
			}
			host := "mcp.sbx.ikigenba.dev"
			if publicURL != "" {
				host = "mcp.wip.localhost"
			}
			if len(old) != 0 || minted.Kind != store.TokenClient || minted.UserID != f.user.ID || minted.Name != c.Name || minted.Host != host || !minted.Enabled || minted.LastUsedAt != nil || !minted.CreatedAt.Equal(f.now) || minted.ExpiresAt == nil || !minted.ExpiresAt.Equal(approval.Add(store.ClientTokenTTL)) {
				t.Fatalf("minted %#v", minted)
			}
			identity, err := f.st.LookupTokenIdentity(secret, host, f.now)
			if err != nil || identity.TokenID != minted.ID || identity.UserID != f.user.ID {
				t.Fatalf("secret identity %#v %v", identity, err)
			}
			oauthDomain(t, e, "token.minted", f.user.ID, "token", minted.ID)
			if e[1].RequestID != "fedcba9876543210fedcba9876543210" {
				t.Fatal("wrong request id")
			}
			if _, err := f.st.ConsumeAuthCode(code.Code, f.now); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("success did not consume code")
			}
			w, _ = oauthTokenRequest(t, f, p)
			oauthAssertError(t, w, "invalid_grant")
		}
	}
	// At exactly the code TTL, the code is still accepted.
	f := newOAuthFixture(t, "")
	c := f.client(t, "Boundary", oauthCallback)
	code := oauthStoredCode(t, f, c)
	f.now = f.now.Add(store.AuthCodeTTL)
	w, _ := oauthTokenRequest(t, f, oauthTokenParams(code))
	if w.Code != 200 {
		t.Fatalf("TTL boundary %d %q", w.Code, w.Body.String())
	}
}

func TestOAuthClientRegistrationRetention(t *testing.T) {
	// R-5F8N-2VBL: registration through HTTP expires only when unused, and an HTTP token exchange makes it survive later authorization.
	for _, used := range []bool{false, true} {
		f := newOAuthFixture(t, "")
		w, _ := f.request(t, "POST", "/register", `{"client_name":"Retained client","redirect_uris":["http://localhost:53682/callback"]}`, "")
		if w.Code != 201 {
			t.Fatal(w.Code)
		}
		clientID := oauthObject(t, w)["client_id"].(string)
		c, err := f.st.LookupClient(clientID, f.now)
		if err != nil {
			t.Fatal(err)
		}
		if used {
			code := oauthStoredCode(t, f, c)
			w, _ = oauthTokenRequest(t, f, oauthTokenParams(code))
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
		}
		for _, elapsed := range []time.Duration{store.UnusedClientTTL, store.UnusedClientTTL + time.Nanosecond, 2 * store.ClientTokenTTL} {
			f.now = c.CreatedAt.Add(elapsed)
			session, err := f.st.CreateSession(f.user.ID, f.now)
			if err != nil {
				t.Fatal(err)
			}
			w, _ = f.request(t, "GET", "/authorize?"+oauthAuthParams(c).Encode(), "", session.ID)
			if used || elapsed == store.UnusedClientTTL {
				if w.Code != 200 {
					t.Fatalf("used=%v age=%v got %d", used, elapsed, w.Code)
				}
			} else {
				oauthAssertPlain(t, w, 400)
			}
		}
	}
}

func oauthPrivateTrail(t *testing.T, events []telemetry.Event, forbidden ...string) {
	t.Helper()
	for _, e := range events {
		values := []string{e.RequestID, e.User}
		for _, v := range e.Attrs {
			values = append(values, fmt.Sprint(v))
		}
		for _, value := range values {
			for _, secret := range forbidden {
				if secret != "" && strings.Contains(value, secret) {
					t.Fatalf("private value %q in event %#v", secret, e)
				}
			}
		}
	}
}

func TestOAuthSecretAndTrailPrivacy(t *testing.T) {
	// R-616T-YQO3 R-67AB-VLDK: the minted secret appears only in access_token, later responses never repeat it, and all OAuth request event fields exclude private inputs.
	f := newOAuthFixture(t, "")
	name := "Private client name"
	redirect := "http://localhost:53682/callback"
	w, e := f.request(t, "POST", "/register", `{"client_name":"Private client name","redirect_uris":["http://localhost:53682/callback"]}`, "")
	if w.Code != 201 {
		t.Fatal(w.Code)
	}
	oauthPrivateTrail(t, e, name, redirect)
	c, err := f.st.LookupClient(oauthObject(t, w)["client_id"].(string), f.now)
	if err != nil {
		t.Fatal(err)
	}
	p := oauthAuthParams(c)
	p.Set("state", "private-state-for-client")
	p.Set("resource", "https://mcp.sbx.ikigenba.dev/private-resource")
	p.Set("decision", "approve")
	w, e = f.request(t, "POST", "/authorize?query-private-marker=private-query-value", p.Encode(), f.session.ID)
	if w.Code != 302 {
		t.Fatal(w.Code)
	}
	oauthPrivateTrail(t, e, name, redirect, oauthChallenge, p.Get("state"), p.Get("resource"), f.session.ID, "query-private-marker=private-query-value")
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := store.AuthCode{Code: u.Query().Get("code"), ClientID: c.ID, RedirectURI: redirect}
	p = oauthTokenParams(code)
	p.Set("state", "private-state-for-client")
	p.Set("resource", "https://mcp.sbx.ikigenba.dev/private-resource")
	w, e = oauthTokenRequest(t, f, p)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	secret := oauthObject(t, w)["access_token"].(string)
	if strings.Count(w.Body.String(), secret) != 1 {
		t.Fatal("secret repeated")
	}
	for key, values := range w.Header() {
		for _, v := range values {
			if strings.Contains(v, secret) {
				t.Fatalf("secret header %s", key)
			}
		}
	}
	oauthPrivateTrail(t, e, name, redirect, code.Code, oauthChallenge, oauthVerifier, p.Get("state"), p.Get("resource"), secret, "irrelevant-cookie-value")
	// Exercise subsequent profile, approve, metadata, register, replay and store-error responses.
	for _, tc := range []struct{ method, path, body, session string }{
		{"GET", "/", "", f.session.ID}, {"GET", "/authorize?" + oauthAuthParams(c).Encode(), "", f.session.ID},
		{"GET", "/.well-known/oauth-authorization-server", "", ""},
		{"POST", "/register", `{"redirect_uris":["http://localhost/other"]}`, ""},
		{"POST", "/token", p.Encode(), ""},
	} {
		w, e = f.request(t, tc.method, tc.path, tc.body, tc.session)
		if strings.Contains(w.Body.String(), secret) || strings.Contains(fmt.Sprint(w.Header()), secret) {
			t.Fatalf("secret in later %s", tc.path)
		}
		oauthPrivateTrail(t, e, name, redirect, code.Code, oauthChallenge, oauthVerifier, "a b&c=d", p.Get("state"), p.Get("resource"), secret, f.session.ID)
	}
	serverStoreDB(t, f.st).SetFailing(true)
	w, e = f.request(t, "POST", "/token", p.Encode(), "")
	oauthAssertPlain(t, w, 500)
	if strings.Contains(w.Body.String(), secret) {
		t.Fatal("secret in error")
	}
	oauthPrivateTrail(t, e, name, redirect, code.Code, oauthChallenge, oauthVerifier, p.Get("state"), p.Get("resource"), secret)
}
