package urls_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/scripts/internal/urls"
)

// R-VBY6-TBZ2 R-VD63-73PR R-VEDZ-KVGG R-VFLV-YN75 R-VGTS-CEXU R-VI1O-Q6OJ R-VJ9L-3YF8
func TestAuthAddresses(t *testing.T) {
	var schemeContract func(*http.Request) string
	var profileContract func(*http.Request, string) string
	var logoutContract func(*http.Request, string) string
	schemeContract = urls.Scheme
	profileContract = urls.AuthProfile
	logoutContract = urls.AuthLogout
	_ = schemeContract
	_ = profileContract
	_ = logoutContract
	for _, proto := range []string{"http", "https", "HTTPS", "HTTP", "Http", "https, http", "ftp", ""} {
		for _, host := range []string{"scripts.sbx.example:443", "SCRIPTS.sbx.example", "sbx.example", "scripts.sbx.example:", "scripts.sbx.example:xyz", "scripts.", "scripts.scripts.sbx.example"} {
			r := httptest.NewRequest("GET", "/", nil)
			r.Host = host
			r.Header.Set("X-Forwarded-Proto", proto)
			scheme := "https"
			if proto == "http" {
				scheme = "http"
			}
			if urls.Scheme(r) != scheme {
				t.Fatal(proto)
			}
			spaces := map[string]string{"scripts.sbx.example:443": "sbx.example", "SCRIPTS.sbx.example": "sbx.example", "sbx.example": "sbx.example", "scripts.sbx.example:": "sbx.example", "scripts.sbx.example:xyz": "sbx.example:xyz", "scripts.": "scripts.", "scripts.scripts.sbx.example": "scripts.sbx.example"}
			prefix := scheme + "://auth." + spaces[host]
			for _, path := range []string{"", filepath.Join(t.TempDir(), "missing")} {
				if got := urls.AuthProfile(r, path); got != prefix+"/" {
					t.Fatalf("%q != %q", got, prefix+"/")
				}
				if got := urls.AuthLogout(r, path); got != prefix+"/logout" {
					t.Fatal(got)
				}
			}
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "scripts.sbx.example"
	p := filepath.Join(t.TempDir(), "services.json")
	for _, body := range []string{`{"services":[{"name":"auth","url":"https://alternate.example","socket":"/run/auth.sock","enabled":false,"mcp":false,"description":"Auth"}]}`, `{"services":[{"name":"auth","url":"http://next.example/","socket":"/run/auth.sock","enabled":true,"mcp":false,"description":"Auth"}]}`, `{"services":[]}`, `{"services":[{"name":"auth","url":"","socket":"/run/auth.sock","enabled":true,"mcp":false,"description":"Auth"}]}`, `not services`} {
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		want := "https://auth.sbx.example"
		// The expected URLs are fixture values, independent of services.Read.
		switch body {
		case `{"services":[{"name":"auth","url":"https://alternate.example","socket":"/run/auth.sock","enabled":false,"mcp":false,"description":"Auth"}]}`:
			want = "https://alternate.example"
		case `{"services":[{"name":"auth","url":"http://next.example/","socket":"/run/auth.sock","enabled":true,"mcp":false,"description":"Auth"}]}`:
			want = "http://next.example/"
		}
		if got := urls.AuthProfile(r, p); got != want+"/" {
			t.Fatalf("profile %q want %q", got, want+"/")
		}
		if got := urls.AuthLogout(r, p); got != want+"/logout" {
			t.Fatal(got)
		}
	}
}
