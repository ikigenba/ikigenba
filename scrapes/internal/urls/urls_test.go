package urls_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/scrapes/internal/urls"
)

// R-ZAXX-4YNC R-ZC5T-IQE1
func TestAuth(t *testing.T) {
	r := httptest.NewRequest("GET", "https://local.test", nil)
	r.Host = "SCRAPES.space.test:443"
	r.Header.Set("X-Forwarded-Proto", "HTTP")
	if got := urls.Auth(r, ""); got != "http://auth.space.test" {
		t.Fatal(got)
	}
	r.Header.Set("X-Forwarded-Proto", "invalid")
	if got := urls.Auth(r, filepath.Join(t.TempDir(), "missing")); got != "https://auth.space.test" {
		t.Fatal(got)
	}
	r.Host = "scrapes.space.test:nondigit"
	if got := urls.Auth(r, ""); got != "https://auth.space.test:nondigit" {
		t.Fatal(got)
	}
	p := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(p, []byte(`{"services":[{"name":"auth","url":"https://chosen.test///","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := urls.Auth(r, p); got != "https://chosen.test" {
		t.Fatal(got)
	}
	if err := os.WriteFile(p, []byte(`{"services":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := urls.Auth(r, p); got != "https://auth.space.test:nondigit" {
		t.Fatal(got)
	}
}
