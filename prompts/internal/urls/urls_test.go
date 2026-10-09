package urls_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/prompts/internal/urls"
)

// R-AU5G-JA8M R-AXT5-OLGP
func TestScheme(t *testing.T) {
	for _, value := range []string{"http", "https", "HTTPS", "HTTP", "Http", "https, http", "ftp", ""} {
		r := httptest.NewRequest("GET", "https://prompts.example.test/", nil)
		r.Header.Set("X-Forwarded-Proto", value)
		want := "https"
		if value == "http" {
			want = "http"
		}
		if got := urls.Scheme(r); got != want {
			t.Fatalf("%q: %q != %q", value, got, want)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	if urls.Scheme(r) != "https" {
		t.Fatal("absent header")
	}
}

// R-AVDC-X1ZB R-AWL9-ATQ0 R-AZ12-2D7E R-B1GU-TWOS R-B2OR-7OFH
func TestAuthAddresses(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "services.json")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{"", `not-json`, `{"services":[]}`, `{"services":[{"name":"auth","url":"","description":"","socket":"","enabled":true,"mcp":false}]}`, `{"services":[{"name":"dummy","url":"https://dummy.example.test","description":"","socket":"","enabled":true,"mcp":false}]}`} {
		write(body)
		for _, c := range []struct{ host, proto, space, scheme string }{
			{"prompts.sbx.ikigenba.dev:443", "https", "sbx.ikigenba.dev", "https"},
			{"PROMPTS.sbx.ikigenba.dev:", "", "sbx.ikigenba.dev", "https"},
			{"prompts.sbx.ikigenba.dev", "HTTPS", "sbx.ikigenba.dev", "https"},
			{"sbx.ikigenba.dev", "http", "sbx.ikigenba.dev", "http"},
			{"prompts.", "ftp", "prompts.", "https"},
			{"prompts.foo:abc", "", "foo:abc", "https"},
			{"prompts.prompts.foo:80", "", "prompts.foo", "https"},
		} {
			r := httptest.NewRequest("GET", "/", nil)
			r.Host = c.host
			r.Header.Set("X-Forwarded-Proto", c.proto)
			base := c.scheme + "://auth." + c.space
			for _, p := range []string{path, "", filepath.Join(root, "absent"), root} {
				if got := urls.AuthProfile(r, p); got != base+"/" {
					t.Fatalf("%q %q: profile %q", c.host, p, got)
				}
				if got := urls.AuthLogout(r, p); got != base+"/logout" {
					t.Fatalf("%q %q: logout %q", c.host, p, got)
				}
			}
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "prompts.foo"
	r.Header.Set("X-Forwarded-Proto", "http")
	for _, base := range []string{"https://auth.first.example.test", "https://auth.second.example.test/"} {
		write(`{"services":[{"name":"auth","url":"` + base + `","description":"","socket":"","enabled":true,"mcp":false}]}`)
		if got := urls.AuthProfile(r, path); got != base+"/" {
			t.Fatalf("rewrite profile %q", got)
		}
		if got := urls.AuthLogout(r, path); got != base+"/logout" {
			t.Fatalf("rewrite logout %q", got)
		}
	}
}
