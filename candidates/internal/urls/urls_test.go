package urls_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/urls"
)

// R-1VQR-TUMU R-1WYO-7MDJ R-1Y6K-LE48 R-1ZEG-Z5UX
func TestSiblingAddresses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"auth","description":"","socket":"","enabled":true,"mcp":false,"url":"https://accounts.example.test///"},{"name":"scrapes","description":"","socket":"","enabled":true,"mcp":false,"url":"https://records.example.test//"}]} `), 0600); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "https://candidates.Example.test/", nil)
	r.Host = "CANDIDATES.Example.test:443"
	r.Header.Set("X-Forwarded-Proto", "HtTp")
	for _, tc := range []struct{ path, want string }{{path, "https://accounts.example.test"}, {"", "http://auth.Example.test"}, {path + "missing", "http://auth.Example.test"}} {
		if got := urls.Auth(r, tc.path); got != tc.want {
			t.Fatalf("Auth = %q want %q", got, tc.want)
		}
	}
	r.Header.Set("X-Forwarded-Proto", "anything")
	if urls.Auth(r, "") != "https://auth.Example.test" {
		t.Fatal("scheme fallback")
	}
	if urls.Base("a///") != "a" || urls.Scrapes(path) != "https://records.example.test" || urls.Scrapes("") != "" || urls.Scrapes(path+"missing") != "" {
		t.Fatal("base")
	}
	if got := urls.RowPage("base", "run", "a/b"); got != "base/runs/run/rows/a%2Fb" {
		t.Fatal(got)
	}
	if urls.RunPage("base", "run") != "base/runs/run" {
		t.Fatal("run")
	}
	for _, tc := range [][3]string{{"", "run", "id"}, {"base", "", "id"}, {"base", "run", ""}} {
		if urls.RowPage(tc[0], tc[1], tc[2]) != "" {
			t.Fatal(tc)
		}
	}
	if urls.RunPage("", "run") != "" || urls.RunPage("base", "") != "" {
		t.Fatal("empty run")
	}
}
