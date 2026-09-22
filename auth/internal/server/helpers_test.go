package server

import (
	"net/http"
	"testing"
)

func TestHostDerivedValues(t *testing.T) {
	// R-ILH9-35UU: the space is the host with one leading auth. label removed.
	// R-IMP5-GXLJ: the callback redirect_uri is https on a space and the local
	// http URL on localhost.
	// R-INX1-UPC8: auth's own origin is https on a space and the local http
	// origin on 127.0.0.1.
	if got := space("auth.green.example:443"); got != "green.example" {
		t.Fatalf("space() = %q, want green.example", got)
	}
	if got := redirectURI("auth.green.example"); got != "https://auth.green.example/login/google/callback" {
		t.Fatalf("redirectURI() = %q", got)
	}
	if got := ownOrigin("auth.green.example"); got != "https://auth.green.example" {
		t.Fatalf("ownOrigin() = %q", got)
	}
	if got := redirectURI("localhost:3001"); got != "http://localhost:3001/login/google/callback" {
		t.Fatalf("local redirectURI() = %q", got)
	}
	if got := ownOrigin("127.0.0.1:3001"); got != "http://127.0.0.1:3001" {
		t.Fatalf("local ownOrigin() = %q", got)
	}
}

func TestCookieAndReturnURLHelpers(t *testing.T) {
	// R-IQCU-M8TM: a return URL is in-space only when its host is the space
	// or a subdomain of the space.
	for _, test := range []struct {
		name, returnURL string
		want            bool
	}{
		{name: "space", returnURL: "https://green.example/path", want: true},
		{name: "subdomain", returnURL: "https://app.green.example/path", want: true},
		{name: "lookalike", returnURL: "https://notgreen.example/path", want: false},
		{name: "other", returnURL: "https://example.net/path", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := inSpace(test.returnURL, "auth.green.example"); got != test.want {
				t.Fatalf("inSpace(%q) = %t, want %t", test.returnURL, got, test.want)
			}
		})
	}

	cookie := cookieForHost("auth.green.example", "session", false)
	if cookie.Name != SessionCookieName || cookie.Path != "/" || cookie.Domain != "green.example" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("space cookie = %#v", cookie)
	}
	cleared := cookieForHost("localhost:3001", "", true)
	if cleared.Value != "" || cleared.Domain != "" || cleared.Path != "/" || cleared.MaxAge != -1 || !cleared.Secure || !cleared.HttpOnly || cleared.SameSite != http.SameSiteLaxMode {
		t.Fatalf("local cleared cookie = %#v", cleared)
	}
}
