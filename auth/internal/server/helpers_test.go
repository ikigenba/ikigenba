package server

import (
	"net/http"
	"testing"
)

func TestHostDerivedValues(t *testing.T) {
	// R-ILH9-35UU: the space is the host with one leading auth. label removed.
	// R-U4SD-S667: only the exact localhost:3001 host is local.
	// R-U8G2-XHEA: callback redirect_uri follows the exact host classification.
	// R-UC3S-2SMD: own origin follows the same host classification.
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
	if got := ownOrigin("localhost:3001"); got != "http://localhost:3001" {
		t.Fatalf("local ownOrigin() = %q", got)
	}
	for _, host := range []string{"localhost", "localhost:3002", "127.0.0.1:3001", "127.0.0.1", "LOCALHOST:3001"} {
		if isLocalRequest(host) {
			t.Fatalf("host %q was treated as local", host)
		}
		if got := redirectURI(host); got != "https://auth."+space(host)+"/login/google/callback" {
			t.Fatalf("redirectURI(%q) = %q", host, got)
		}
		if got := ownOrigin(host); got != "https://auth."+space(host) {
			t.Fatalf("ownOrigin(%q) = %q", host, got)
		}
		if got := cookieForHost(host, "session", false).Domain; got != space(host) {
			t.Fatalf("cookie domain for %q = %q", host, got)
		}
	}
	if !isLocalRequest("localhost:3001") {
		t.Fatal("localhost:3001 was not treated as local")
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
