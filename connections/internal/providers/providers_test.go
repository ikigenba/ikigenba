package providers_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/connections/internal/providers"
)

func equal(t *testing.T, a, b any) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %#v want %#v", a, b)
	}
}
func jwt(payload string) string {
	return "unchecked." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".unchecked"
}

// R-9LGQ-33NF R-9MOM-GVE4 R-9NWI-UN4T R-9P4F-8EVI R-BS74-J6E0 R-9RK7-ZYCW R-9SS4-DQ3L R-9WFT-J1BO R-9XNP-WT2D R-KLQG-D6VR R-XCFS-0AUJ
func TestCatalog(t *testing.T) {
	table := providers.Table()
	if len(table) != 1 {
		t.Fatal(table)
	}
	p := table[0]
	if p.Name != "google" || p.DisplayName == "" || p.AuthorizeURL != "https://accounts.google.com/o/oauth2/v2/auth" || p.TokenURL != "https://oauth2.googleapis.com/token" || p.RevokeURL != "https://oauth2.googleapis.com/revoke" || p.ClientIDVar != "GOOGLE_CLIENT_ID" || p.ClientSecretVar != "GOOGLE_CLIENT_SECRET" || p.Account == nil {
		t.Fatal(p)
	}
	equal(t, p.Scopes, []string{"openid", "email", "https://www.googleapis.com/auth/drive", "https://www.googleapis.com/auth/gmail.readonly", "https://www.googleapis.com/auth/gmail.compose"})
	equal(t, p.Granted, map[string]string{"email": "https://www.googleapis.com/auth/userinfo.email"})
	equal(t, p.Extra, url.Values{"access_type": {"offline"}, "prompt": {"consent"}})
	token := jwt(`{"email":"account@example.test","sub":"subject"}`)
	a, e := p.Account(token)
	b, f := providers.IDTokenAccount(token)
	equal(t, a, b)
	equal(t, e, f)
	if p.GrantedName("email") != p.Granted["email"] || p.GrantedName("openid") != "openid" {
		t.Fatal("mapping")
	}
	c := providers.Client{Provider: p, ID: "client"}
	parsed, e := url.Parse(c.AuthorizeURL("state", "verifier", "https://callback.test/google/callback"))
	if e != nil {
		t.Fatal(e)
	}
	equal(t, parsed.Query(), url.Values{"response_type": {"code"}, "client_id": {"client"}, "redirect_uri": {"https://callback.test/google/callback"}, "scope": {strings.Join(p.Scopes, " ")}, "state": {"state"}, "code_challenge": {providers.Challenge("verifier")}, "code_challenge_method": {"S256"}, "access_type": {"offline"}, "prompt": {"consent"}})
	if providers.Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk") != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatal("challenge")
	}
	p.Scopes[0] = "changed"
	p.Granted["email"] = "changed"
	p.Extra["prompt"][0] = "changed"
	table[0].Name = "changed"
	fresh := providers.Table()[0]
	if fresh.Name != "google" || fresh.Scopes[0] != "openid" || fresh.Granted["email"] == "changed" || fresh.Extra.Get("prompt") != "consent" {
		t.Fatal(fresh)
	}
	errs := []error{providers.ErrRefused, providers.ErrFailed, providers.ErrAccount}
	for i, a := range errs {
		if a == nil {
			t.Fatal("nil")
		}
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Fatal("overlap")
			}
		}
	}
	for _, e := range []error{nil, errors.New("ordinary"), providers.ErrAccount} {
		if providers.HTTPStatus(e) != 0 {
			t.Fatal(e)
		}
	}
}

// R-A1BF-24AG R-A2JB-FW15 R-A3R7-TNRU R-A8MT-CQQM R-AH64-14XH
func TestRequestsAndGrants(t *testing.T) {
	now := time.Date(2032, 3, 4, 5, 6, 7, 890, time.FixedZone("test", 3600))
	p := providers.Table()[0]
	calls := 0
	var expected url.Values
	var response string
	var endpoint string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.URL.Path != endpoint {
			t.Error(r.Method, r.Header, r.URL)
		}
		if e := r.ParseForm(); e != nil {
			t.Error(e)
		}
		equal(t, r.PostForm, expected)
		_, _ = w.Write([]byte(response))
	}))
	defer fake.Close()
	p.TokenURL = fake.URL + "/token"
	p.RevokeURL = fake.URL + "/revoke"
	c := providers.Client{Provider: p, ID: "client", Secret: "secret", HTTP: fake.Client(), Now: func() time.Time { return now }}
	account := providers.Account{Email: "account@example.test", Subject: "subject"}
	id := jwt(`{"email":"account@example.test","sub":"subject"}`)
	scope := "extra " + p.GrantedName("email") + " " + p.Scopes[4] + " openid extra " + p.Scopes[3] + " " + p.Scopes[2]
	for _, sc := range []string{scope, "", "   "} {
		endpoint = "/token"
		expected = url.Values{"grant_type": {"authorization_code"}, "code": {"code"}, "code_verifier": {"verifier"}, "redirect_uri": {"https://callback.test/google/callback"}, "client_id": {"client"}, "client_secret": {"secret"}}
		body := map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600, "id_token": id}
		if sc != "" {
			body["scope"] = sc
		}
		b, _ := json.Marshal(body)
		response = string(b)
		before := calls
		g, e := c.Exchange(context.Background(), "code", "verifier", "https://callback.test/google/callback")
		if e != nil || calls != before+1 {
			t.Fatal(g, e, calls)
		}
		wantScopes := []string{}
		for _, s := range p.Scopes {
			wantScopes = append(wantScopes, p.GrantedName(s))
		}
		if sc == scope {
			wantScopes = append(wantScopes, "extra")
		}
		equal(t, g, providers.Grant{AccessToken: "access", RefreshToken: "refresh", Expires: now.UTC().Truncate(time.Second).Add(time.Hour), Scopes: wantScopes, Account: account})
	}
	expected = url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"stored"}, "client_id": {"client"}, "client_secret": {"secret"}}
	for _, body := range []string{`{"access_token":"new","expires_in":1}`, `{"access_token":"new","expires_in":1,"scope":"   "}`, `{"access_token":"new","expires_in":1,"refresh_token":"rotated","scope":"extra openid extra","id_token":"ignored"}`} {
		response = body
		before := calls
		g, e := c.Refresh(context.Background(), "stored")
		if e != nil || calls != before+1 {
			t.Fatal(g, e)
		}
		want := providers.Grant{AccessToken: "new", Expires: now.UTC().Truncate(time.Second).Add(time.Second)}
		if strings.Contains(body, "rotated") {
			want.RefreshToken = "rotated"
			want.Scopes = []string{"openid", "extra"}
		}
		equal(t, g, want)
		if g.Expires.Location() != time.UTC || g.Expires.Nanosecond() != 0 {
			t.Fatal(g.Expires)
		}
	}
	endpoint = "/revoke"
	expected = url.Values{"token": {"revoked"}}
	response = "not json"
	before := calls
	if e := c.Revoke(context.Background(), "revoked"); e != nil || calls != before+1 {
		t.Fatal(e, calls)
	}
}

// R-X9ZZ-8RD5 R-AB2M-4A80 R-AFY7-ND6S
func TestRefusalsMalformedAndRedirects(t *testing.T) {
	secret := "SecretMarkerZZZZ123456"
	code := "CodeMarkerZZZZ123456"
	verifier := "VerifierMarkerZZZZ123456"
	refresh := "RefreshMarkerZZZZ123456"
	access := "AccessMarkerZZZZ123456"
	token := jwt(`{"email":"account@example.test","sub":"subject"}`)
	status := 200
	body := ""
	calls := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if status == 307 {
			w.Header().Set("Location", "/redirected")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer fake.Close()
	p := providers.Table()[0]
	p.TokenURL = fake.URL
	p.RevokeURL = fake.URL
	c := providers.Client{Provider: p, ID: "client", Secret: secret, HTTP: fake.Client(), Now: func() time.Time { return time.Unix(1800000000, 0) }}
	check := func(e error, kind error, wantStatus int) {
		t.Helper()
		if !errors.Is(e, kind) || providers.HTTPStatus(e) != wantStatus {
			t.Fatal(e, providers.HTTPStatus(e))
		}
		for _, value := range []string{secret, code, verifier, refresh, access, token} {
			if strings.Contains(e.Error(), value) {
				t.Fatal("credential in error")
			}
		}
	}
	for _, st := range []int{400, 401, 403, 500, 307} {
		status = st
		body = secret + code + verifier + refresh + access + token
		kind := providers.ErrFailed
		if st == 400 {
			kind = providers.ErrRefused
		}
		before := calls
		g, e := c.Exchange(context.Background(), code, verifier, "https://callback.test")
		equal(t, g, providers.Grant{})
		check(e, kind, st)
		g, e = c.Refresh(context.Background(), refresh)
		equal(t, g, providers.Grant{})
		check(e, kind, st)
		check(c.Revoke(context.Background(), refresh), kind, st)
		if calls != before+3 {
			t.Fatal(calls, before)
		}
	}
	status = 200
	for _, bad := range []string{`[]`, `null`, `{}`, `{"access_token":"a","expires_in":0}`, `{"access_token":"a","expires_in":-1}`, `{"access_token":"a","expires_in":1.5}`, `{"access_token":"a","expires_in":"1"}`, `{"access_token":3,"expires_in":1}`, `{"access_token":"a","expires_in":1,"scope":null}`, `{"access_token":"a","expires_in":1,"id_token":2}`} {
		body = bad
		g, e := c.Refresh(context.Background(), refresh)
		equal(t, g, providers.Grant{})
		check(e, providers.ErrFailed, 200)
	}
	body = `{"access_token":"a","expires_in":1}`
	_, e := c.Exchange(context.Background(), code, verifier, "https://callback.test")
	check(e, providers.ErrFailed, 200)
	for _, id := range []string{"", jwt(`{"sub":"subject"}`)} {
		body = fmt.Sprintf(`{"access_token":%q,"refresh_token":%q,"expires_in":1,"id_token":%q}`, access, refresh, id)
		g, e := c.Exchange(context.Background(), code, verifier, "https://callback.test")
		equal(t, g, providers.Grant{})
		check(e, providers.ErrAccount, 0)
	}
	body = fmt.Sprintf(`{"access_token":%q,"refresh_token":%q,"expires_in":1}`, access, refresh)
	_, e = c.Exchange(context.Background(), code, verifier, "https://callback.test")
	check(e, providers.ErrAccount, 0)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	g, e := c.Refresh(canceled, refresh)
	equal(t, g, providers.Grant{})
	check(e, providers.ErrFailed, 0)
	check(c.Revoke(canceled, refresh), providers.ErrFailed, 0)
	fake.Close()
	g, e = c.Exchange(context.Background(), code, verifier, "https://callback.test")
	equal(t, g, providers.Grant{})
	check(e, providers.ErrFailed, 0)
}

// R-ACAI-I1YP
func TestIDTokenAccount(t *testing.T) {
	for _, payload := range []string{`{"email":"e","sub":"s"}`, `{"email":"e","sub":"s","email_verified":false,"exp":0}`} {
		a, e := providers.IDTokenAccount(jwt(payload))
		if e != nil {
			t.Fatal(e)
		}
		equal(t, a, providers.Account{Email: "e", Subject: "s"})
	}
	for _, token := range []string{"", "one.two", "one.!.three", "one.e30=.three", jwt(`[]`), jwt(`null`), jwt(`{}`), jwt(`{"email":3,"sub":"s"}`), jwt(`{"email":"","sub":"s"}`), jwt(`{"email":"e","sub":null}`), jwt(`{"email":"e","sub":""}`)} {
		a, e := providers.IDTokenAccount(token)
		equal(t, a, providers.Account{})
		if !errors.Is(e, providers.ErrAccount) || providers.HTTPStatus(e) != 0 {
			t.Fatal(e)
		}
	}
}

// R-A3R7-TNRU R-AH64-14XH
func TestLargeExpiryAndRevocationBody(t *testing.T) {
	now := time.Unix(1800000000, 123)
	id := jwt(`{"email":"e","sub":"s"}`)
	response := fmt.Sprintf(`{"access_token":"access","refresh_token":"refresh","expires_in":10000000000,"id_token":%q}`, id)
	c := providers.Client{Provider: providers.Table()[0], Now: func() time.Time { return now }, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header), Request: r}, nil
	})}}
	for _, call := range []func() (providers.Grant, error){func() (providers.Grant, error) {
		return c.Exchange(context.Background(), "code", "verifier", "https://callback.test")
	}, func() (providers.Grant, error) { return c.Refresh(context.Background(), "refresh") }} {
		g, e := call()
		if e != nil {
			t.Fatal(e)
		}
		if g.Expires.Unix() != now.Unix()+10000000000 || g.Expires.Location() != time.UTC || g.Expires.Nanosecond() != 0 {
			t.Fatal(g.Expires)
		}
	}
	c.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: errorBody{}, Header: make(http.Header), Request: r}, nil
	})}
	if e := c.Revoke(context.Background(), "token"); e != nil {
		t.Fatal(e)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type errorBody struct{}

func (errorBody) Read([]byte) (int, error) { return 0, errors.New("unreadable body") }
func (errorBody) Close() error             { return nil }
