package urls_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/gdrive/internal/urls"
)

// R-YJI4-MFJN R-YKQ1-07AC R-70D0-XASE R-71KX-B2J3
func TestAddresses(t *testing.T) {
	if urls.Base("https://broker.test///") != "https://broker.test" || urls.Connect(urls.Base("https://broker.test///")) != "https://broker.test/google/connect" || urls.Connect("") != "/google/connect" {
		t.Fatal("connect")
	}
	if urls.ArtifactPage("https://files.test", "object") != "https://files.test/object" || urls.ArtifactPage("", "object") != "" || urls.ArtifactPage("https://files.test", "") != "" {
		t.Fatal("artifact")
	}
	for _, tc := range []struct{ host, proto, want string }{{"gdrive.space.test:443", "", "https://auth.space.test"}, {"GDRIVE.space.test:8080", "HTTP", "http://auth.space.test"}, {"space.test", "ftp", "https://auth.space.test"}, {"gdrive.space.test:foo", "https", "https://auth.space.test:foo"}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = tc.host
		r.Header.Set("X-Forwarded-Proto", tc.proto)
		if got := urls.Auth(r, ""); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	file := filepath.Join(t.TempDir(), "services.json")
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "gdrive.space.test"
	for _, tc := range []struct{ body, want string }{{`{"services":[{"name":"auth","url":"https://login.test///","socket":"","description":"","enabled":true,"mcp":false}]}`, "https://login.test"}, {`broken`, "https://auth.space.test"}, {`{"services":[]}`, "https://auth.space.test"}, {`{"services":[{"name":"auth","url":"","socket":"","description":"","enabled":true,"mcp":false}]}`, "https://auth.space.test"}} {
		if err := os.WriteFile(file, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		if got := urls.Auth(r, file); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}
