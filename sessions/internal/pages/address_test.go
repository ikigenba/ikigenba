package pages_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/sessions/internal/pages"
)

func serviceFile(t *testing.T, entries ...services.Entry) string {
	t.Helper()
	values := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		values = append(values, map[string]any{"name": e.Name, "url": e.URL, "description": e.Description, "socket": e.Socket, "enabled": e.Enabled, "mcp": e.MCP})
	}
	b, err := json.Marshal(map[string]any{"services": values})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// R-173Y-D2H3 R-19JR-4LYH R-1ARN-IDP6 R-1BZJ-W5FV
func TestAddresses(t *testing.T) {
	auth := pages.Auth
	signin := pages.SignIn
	for _, tc := range []struct{ host, proto, want string }{
		{"sessions.sbx.example.test:443", "https", "https://auth.sbx.example.test"},
		{"SESSIONS.sbx.example.test", "HTTP", "http://auth.sbx.example.test"},
		{"sessions.sbx.example.test:987", "", "https://auth.sbx.example.test"},
		{"example.test", "http", "http://auth.example.test"},
		{"sessions.example.test:abc", "http, https", "https://auth.example.test:abc"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/about?x=1&next=%2Ftwo", nil)
		r.Host = tc.host
		r.Header["X-Forwarded-Proto"] = []string{tc.proto, "http"}
		for _, path := range []string{"", filepath.Join(t.TempDir(), "absent"), serviceFile(t), serviceFile(t, services.Entry{Name: "auth"})} {
			if got := auth(r, path); got != tc.want {
				t.Fatalf("auth %q: got %q want %q", path, got, tc.want)
			}
			scheme := "https"
			if tc.proto == "HTTP" || tc.proto == "http" {
				scheme = "http"
			}
			want := tc.want + "/?return=" + url.QueryEscape(scheme+"://"+tc.host+r.RequestURI)
			if got := signin(r, path); got != want {
				t.Fatalf("sign in: %q != %q", got, want)
			}
		}
		path := serviceFile(t, services.Entry{Name: "auth", URL: "https://auth.chosen.test///"})
		if got := auth(r, path); got != "https://auth.chosen.test" {
			t.Fatal(got)
		}
		scheme := "https"
		if tc.proto == "HTTP" || tc.proto == "http" {
			scheme = "http"
		}
		if got, want := signin(r, path), "https://auth.chosen.test/?return="+url.QueryEscape(scheme+"://"+tc.host+r.RequestURI); got != want {
			t.Fatalf("%q != %q", got, want)
		}
		if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
			t.Fatal(err)
		}
		if got := auth(r, path); got != tc.want {
			t.Fatal(got)
		}
	}
}
