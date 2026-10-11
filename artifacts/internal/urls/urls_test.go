package urls_test

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/artifacts/internal/urls"
)

// R-C56X-WD7F R-C6EU-A4Y4 R-C7MQ-NWOT R-C8UN-1OFI R-CA2J-FG67
func TestAddresses(t *testing.T) {
	if urls.Service != "artifacts" || urls.FilesPrefix != "/files/" {
		t.Fatal("address constants")
	}
	for _, tc := range []struct{ proto, scheme string }{{"http", "http"}, {"HTTP", "http"}, {"https", "https"}, {"HtTpS", "https"}, {"", "https"}, {"ftp", "https"}, {" http", "https"}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = "ArTiFaCtS.sbx.example:7401"
		r.Header.Set("X-Forwarded-Proto", tc.proto)
		if got := urls.Scheme(r); got != tc.scheme {
			t.Fatalf("scheme %q: %q", tc.proto, got)
		}
		if got := urls.Base(r, ""); got != tc.scheme+"://"+r.Host {
			t.Fatal(got)
		}
		if got := urls.Auth(r, ""); got != tc.scheme+"://auth.sbx.example" {
			t.Fatal(got)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "other.example:abc"
	if got := urls.Auth(r, ""); got != "https://auth.other.example:abc" {
		t.Fatal(got)
	}
	if got := urls.File("https://artifacts.example", "a/b"); got != "https://artifacts.example/files/a/b" {
		t.Fatal(got)
	}
	file := filepath.Join(t.TempDir(), "services.json")
	data := `{"services":[{"name":"artifacts","url":"http://published.example/","description":"","socket":"","enabled":true,"mcp":true},{"name":"auth","url":"https://identity.example/","description":"","socket":"","enabled":true,"mcp":false}]}`
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if got := urls.Base(r, file); got != "http://published.example" {
		t.Fatal(got)
	}
	if got := urls.Auth(r, file); got != "https://identity.example" {
		t.Fatal(got)
	}
	if err := os.WriteFile(file, []byte(`{"services":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := urls.Base(r, file); got != "https://"+r.Host {
		t.Fatal(got)
	}
	if got := urls.Auth(r, file); got != "https://auth.other.example:abc" {
		t.Fatal(got)
	}
	if err := os.WriteFile(file, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := urls.Base(r, file); got != "https://"+r.Host {
		t.Fatal(got)
	}
	ctx := context.Background()
	if base, ok := urls.FromContext(ctx); base != "" || ok {
		t.Fatal(base, ok)
	}
	for _, base := range []string{"http://chosen.example", ""} {
		if got, ok := urls.FromContext(urls.NewContext(ctx, base)); got != base || !ok {
			t.Fatal(got, ok)
		}
	}
}

// R-05Q2-1GZY R-06XY-F8QN
func TestConfigured(t *testing.T) {
	file := filepath.Join(t.TempDir(), "services.json")
	for _, tc := range []struct{ data, want string }{
		{`{"services":[{"name":"artifacts","url":"https://published.example.test","description":"","socket":"","enabled":true,"mcp":true}]}`, "https://published.example.test"},
		{`{"services":[{"name":"artifacts","url":"https://published.example.test/","description":"","socket":"","enabled":true,"mcp":true}]}`, "https://published.example.test"},
		{`{"services":[{"name":"auth","url":"https://identity.example.test/","description":"","socket":"","enabled":true,"mcp":false}]}`, ""},
		{"broken", ""},
	} {
		if err := os.WriteFile(file, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		if got := urls.Configured(file); got != tc.want {
			t.Fatalf("Configured(%q) = %q, want %q", tc.data, got, tc.want)
		}
		if tc.want != "" {
			for _, host := range []string{"gateway.example.test", "artifacts.example.test:8123"} {
				for _, scheme := range []string{"http", "https"} {
					r := httptest.NewRequest("GET", "/", nil)
					r.Host = host
					r.Header.Set("X-Forwarded-Proto", scheme)
					if got := urls.Base(r, file); got != urls.Configured(file) {
						t.Fatalf("request changed configured URL: %q", got)
					}
				}
			}
		}
	}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing")} {
		if got := urls.Configured(path); got != "" {
			t.Fatalf("unreadable services: %q", got)
		}
	}
}
