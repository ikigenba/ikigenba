package server

import (
	"testing"
)

func TestHostDerivedValues(t *testing.T) {
	// R-ILH9-35UU: remove only one leading auth. label, preserving the port.
	for _, tc := range []struct{ host, want string }{
		{"auth.green.example:443", "green.example:443"},
		{"auth.auth.green.example", "auth.green.example"},
		{"green.example:8443", "green.example:8443"},
		{"localhost:3001", "localhost:3001"},
		{"AUTH.green.example", "AUTH.green.example"},
	} {
		if got := space(tc.host); got != tc.want {
			t.Errorf("space(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestRedirectURI(t *testing.T) {
	// R-T8R1-7SBK
	for _, tc := range []struct{ host, want string }{
		{"auth.green.example", "https://auth.green.example/login/google/callback"},
		{"auth.green.example:8443", "https://auth.green.example:8443/login/google/callback"},
		{"localhost:3001", "https://auth.localhost:3001/login/google/callback"},
	} {
		if got := redirectURI(tc.host, ""); got != tc.want {
			t.Errorf("redirectURI(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestOwnOrigin(t *testing.T) {
	// R-TCEQ-D3JN
	for _, tc := range []struct{ host, want string }{
		{"auth.green.example", "https://auth.green.example"},
		{"green.example", "https://auth.green.example"},
		{"auth.green.example:8443", "https://auth.green.example:8443"},
		{"localhost:3001", "https://auth.localhost:3001"},
	} {
		if got := ownOrigin(tc.host, ""); got != tc.want {
			t.Errorf("ownOrigin(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestCookieDomain(t *testing.T) {
	// R-9Y8U-AAQ2
	for _, tc := range []struct{ host, want string }{
		{"auth.green.example:8443", "green.example"},
		{"auth.green.example:443", "green.example"},
		{"auth.green.example", "green.example"},
		{"auth.green.example:", "green.example:"},
		{"auth.green.example:abc", "green.example:abc"},
		{"auth.auth.green.example:123", "auth.green.example"},
	} {
		if got := cookieForHost(tc.host, "session", false).Domain; got != tc.want {
			t.Errorf("cookie domain for %q = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestOnSpaceOrigin(t *testing.T) {
	// R-GQZR-C3MP: space origins require HTTPS and an exact space host or
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
		{"https://.app.green.example", false},
		{"https://app..green.example", false},
		{"https://green.example/path", false},
		{"https://green.example?x=1", false},
		{"https://green.example#x", false},
		{"https://user@green.example", false},
		{"http://localhost", false},
		{"http://localhost:3001", false},
		{"null", false},
	} {
		if got := onSpaceOrigin(tc.origin, "auth.green.example", ""); got != tc.want {
			t.Errorf("onSpaceOrigin(%q, space) = %t, want %t", tc.origin, got, tc.want)
		}
	}

}

func TestOnSpaceOriginComparesEveryHostByte(t *testing.T) {
	// R-GQZR-C3MP, R-GS7N-PVDE: host comparison folds ASCII only;
	// identical UTF-8 text is allowed, but differing continuation bytes
	// cannot make different hosts equal.
	for _, publicURL := range []string{"", "http://configured.example:7400"} {
		scheme, port, host := "https", "", "auth.é.example"
		if publicURL != "" {
			scheme, port, host = "http", ":7400", "auth.é.example:9000"
		}
		for _, tc := range []struct {
			originHost string
			want       bool
		}{
			{"é.example", true},
			{"APP.é.EXAMPLE", true},
			{"ê.example", false},
			{"app.ê.example", false},
			{"É.example", false},
		} {
			origin := scheme + "://" + tc.originHost + port
			if got := onSpaceOrigin(origin, host, publicURL); got != tc.want {
				t.Errorf("onSpaceOrigin(%q, %q, %q) = %t, want %t", origin, host, publicURL, got, tc.want)
			}
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
