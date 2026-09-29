package server

import (
	"net/http"
	"testing"
)

func TestHostDerivedValues(t *testing.T) {
	// R-ILH9-35UU: the space is the host with one leading auth. label removed.
	// R-U4SD-S667: only the exact localhost:3001 host is local.
	// R-U8G2-XHEA: callback redirect_uri follows the exact host classification.
	if got := space("auth.green.example:443"); got != "green.example" {
		t.Fatalf("space() = %q, want green.example", got)
	}
	if got := redirectURI("auth.green.example"); got != "https://auth.green.example/login/google/callback" {
		t.Fatalf("redirectURI() = %q", got)
	}
	if got := redirectURI("localhost:3001"); got != "http://localhost:3001/login/google/callback" {
		t.Fatalf("local redirectURI() = %q", got)
	}
	for _, host := range []string{"localhost", "localhost:3002", "127.0.0.1:3001", "127.0.0.1", "LOCALHOST:3001"} {
		if isLocalRequest(host) {
			t.Fatalf("host %q was treated as local", host)
		}
		if got := redirectURI(host); got != "https://auth."+space(host)+"/login/google/callback" {
			t.Fatalf("redirectURI(%q) = %q", host, got)
		}
		if got := cookieForHost(host, "session", false).Domain; got != space(host) {
			t.Fatalf("cookie domain for %q = %q", host, got)
		}
	}
	if !isLocalRequest("localhost:3001") {
		t.Fatal("localhost:3001 was not treated as local")
	}
}

func TestOwnOrigin(t *testing.T) {
	// R-7AQD-QSNL: token routes use auth's own origin for the exact local
	// host and the derived auth host on a space.
	for _, tc := range []struct{ host, want string }{
		{"localhost:3001", "http://localhost:3001"},
		{"auth.green.example", "https://auth.green.example"},
		{"green.example", "https://auth.green.example"},
		{"localhost:3002", "https://auth.localhost"},
	} {
		if got := ownOrigin(tc.host); got != tc.want {
			t.Errorf("ownOrigin(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestOnSpaceOrigin(t *testing.T) {
	// R-60RY-2THD: space origins require HTTPS and an exact space host or
	// a label-boundary subdomain, without a port or URL suffix.
	for _, tc := range []struct {
		origin string
		want   bool
	}{
		{"https://green.example", true},
		{"HTTPS://GREEN.EXAMPLE", true},
		{"https://auth.green.example", true},
		{"https://nested.app.green.example", true},
		{"https://evilgreen.example", false},
		{"https://green.example.evil.com", false},
		{"http://green.example", false},
		{"https://green.example:443", false},
		{"https://green.example.", false},
		{"https://green.example/path", false},
		{"https://green.example?x=1", false},
		{"https://green.example#x", false},
		{"https://user@green.example", false},
		{"http://localhost", false},
		{"http://localhost:3001", false},
		{"null", false},
	} {
		if got := onSpaceOrigin(tc.origin, "auth.green.example"); got != tc.want {
			t.Errorf("onSpaceOrigin(%q, space) = %t, want %t", tc.origin, got, tc.want)
		}
	}

	// R-7D66-IC4Z: local origins accept localhost on HTTP, with no port
	// or a canonical decimal port in the valid range.
	for _, tc := range []struct {
		origin string
		want   bool
	}{
		{"http://localhost", true},
		{"HTTP://LOCALHOST", true},
		{"http://localhost:1", true},
		{"http://localhost:65535", true},
		{"http://localhost:3001", true},
		{"https://localhost", false},
		{"http://localhost.", false},
		{"http://localhost:", false},
		{"http://localhost:0", false},
		{"http://localhost:01", false},
		{"http://localhost:65536", false},
		{"http://localhost:999999999999999999999999", false},
		{"http://127.0.0.1:3001", false},
		{"https://auth.green.example", false},
		{"http://localhost/path", false},
		{"http://localhost?x=1", false},
		{"http://localhost#x", false},
		{"http://user@localhost", false},
		{"null", false},
	} {
		if got := onSpaceOrigin(tc.origin, "localhost:3001"); got != tc.want {
			t.Errorf("onSpaceOrigin(%q, local) = %t, want %t", tc.origin, got, tc.want)
		}
	}
}

func TestCookieAndReturnURLHelpers(t *testing.T) {
	// R-N2LW-9AQJ: a return URL is in-space only when its host is the space
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

func TestReturnURLUsesUnambiguousTextPolicy(t *testing.T) {
	// R-N2LW-9AQJ
	for _, tc := range []struct {
		raw, host string
		want      bool
	}{
		{"https://GREEN.EXAMPLE/a", "auth.green.example", true}, {"HtTp://App.Green.Example:0080/a", "auth.green.example:443", true}, {"https://green.example:/a", "auth.green.example", true}, {"https://.green.example/a", "auth.green.example", true}, {"http://LOCALHOST:3000/a", "localhost:3001", true},
		{"https://green.example:9999999999999/a", "auth.green.example", true}, {"https://green.example/a?q=é", "auth.green.example", true},
		{"https://green.example@evil.test/a", "auth.green.example", false}, {"https://evil.test@green.example/a", "auth.green.example", false}, {"https://%67reen.example/a", "auth.green.example", false}, {"https://Ｇreen.example/a", "auth.green.example", false}, {"https://[green.example]/a", "auth.green.example", false}, {"https://green.example:abc/a", "auth.green.example", false}, {"https://green.example:1:2/a", "auth.green.example", false},
		{"https:green.example/a", "auth.green.example", false}, {"https:/green.example/a", "auth.green.example", false}, {"https:///green.example/a", "auth.green.example", false}, {"//green.example/a", "auth.green.example", false}, {"ftp://green.example/a", "auth.green.example", false}, {"https://evilgreen.example/a", "auth.green.example", false}, {"https://green.example.evil/a", "auth.green.example", false}, {"https://green.example./a", "auth.green.example", false}, {"https://green.example\\@evil.test/a", "auth.green.example", false}, {" https://green.example/a", "auth.green.example", false}, {"https://green.example/a b", "auth.green.example", false}, {"https://green.example/a", "auth.:443", false}, {"not a URL", "auth.green.example", false},
	} {
		if got := inSpace(tc.raw, tc.host); got != tc.want {
			t.Errorf("inSpace(%q,%q) = %t want %t", tc.raw, tc.host, got, tc.want)
		}
	}
	for c := byte(0); c <= 0x20; c++ {
		if inSpace("https://green.example/a"+string([]byte{c}), "auth.green.example") {
			t.Errorf("accepted forbidden byte %x", c)
		}
	}
	if inSpace("https://green.example/a\x7f", "auth.green.example") {
		t.Error("accepted DEL")
	}
}
