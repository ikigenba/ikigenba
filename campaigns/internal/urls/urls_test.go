package urls_test

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/campaigns/internal/urls"
)

// R-BDR4-6CM6 R-BEZ0-K4CV R-BG6W-XW3K R-BHET-BNU9
func TestAuthenticationAndSiblingAddresses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.json")
	body := `{"services":[{"name":"auth","url":"https://identity.example.test///","description":"","socket":"/unused","enabled":true,"mcp":false},{"name":"candidates","url":"https://business.example.test//","description":"","socket":"/unused","enabled":true,"mcp":true}]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ host, proto, base string }{
		{"campaigns.sbx.ikigenba.dev:443", "https", "https://auth.sbx.ikigenba.dev"},
		{"CAMPAIGNS.sbx.ikigenba.dev:80", "HTTP", "http://auth.sbx.ikigenba.dev"},
		{"example.test:abc", "", "https://auth.example.test:abc"},
		{"campaigns.example.test", "ftp", "https://auth.example.test"},
	} {
		r := httptest.NewRequest("GET", "/campaigns/record?page=last", nil)
		r.Host = tc.host
		r.Header.Set("X-Forwarded-Proto", tc.proto)
		for _, p := range []string{"", filepath.Join(t.TempDir(), "missing")} {
			if got := urls.Auth(r, p); got != tc.base {
				t.Fatalf("auth: %q", got)
			}
			scheme := "https"
			if tc.proto == "HTTP" {
				scheme = "http"
			}
			want := tc.base + "/?return=" + url.QueryEscape(scheme+"://"+r.Host+r.RequestURI)
			if got := urls.SignIn(r, p); got != want {
				t.Fatalf("signin: %q, want %q", got, want)
			}
			if urls.Sibling(p, "candidates") != "" {
				t.Fatal("missing sibling")
			}
		}
		if urls.Auth(r, path) != "https://identity.example.test" {
			t.Fatal("configured auth")
		}
		if urls.Sibling(path, "candidates") != "https://business.example.test" || urls.Sibling(path, "artifacts") != "" {
			t.Fatal("configured siblings")
		}
	}
	if err := os.WriteFile(path, []byte(`{bad`), 0600); err != nil {
		t.Fatal(err)
	}
	if urls.Sibling(path, "auth") != "" {
		t.Fatal("invalid services")
	}
}

// R-BIMP-PFKY
func TestSiblingPages(t *testing.T) {
	for _, tc := range []struct {
		build func(string, string) string
		path  string
	}{{urls.CandidatePage, "/candidates/"}, {urls.CohortPage, "/cohorts/"}, {urls.ArtifactPage, "/"}} {
		if tc.build("", "a") != "" || tc.build("https://example.test", "") != "" {
			t.Fatal("empty inputs")
		}
		if got := tc.build("https://example.test", "a/b ?"); got != "https://example.test"+tc.path+url.PathEscape("a/b ?") {
			t.Fatal(got)
		}
	}
}
