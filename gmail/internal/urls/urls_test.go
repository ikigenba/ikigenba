package urls_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/gmail/internal/urls"
)

// R-W82Y-SZ7A R-W9AV-6QXZ R-WBQN-YAFD R-WCYK-C262
func TestAddresses(t *testing.T) {
	for _, tc := range []struct{ got, want string }{{urls.Base("https://service.test///"), "https://service.test"}, {urls.Base(""), ""}, {urls.Connect(""), "/google/connect"}, {urls.Connect("base"), "base/google/connect"}, {urls.ArtifactPage("base", "id"), "base/id"}, {urls.ArtifactFile("base", "a/b"), "base/files/a/b"}, {urls.ArtifactPage("", "id"), ""}, {urls.ArtifactPage("base", ""), ""}, {urls.ArtifactFile("", "path"), ""}, {urls.ArtifactFile("base", ""), ""}, {urls.MessageLink("message"), "https://mail.google.com/mail/u/0/#all/message"}, {urls.DraftLink("message"), "https://mail.google.com/mail/u/0/#drafts/message"}, {urls.MessageLink(""), ""}, {urls.DraftLink(""), ""}, {urls.DraftPage("id"), "/drafts/id"}, {urls.TransferPage("id"), "/transfers/id"}, {urls.DraftPage(""), "/drafts/"}, {urls.TransferPage(""), "/transfers/"}} {
		if tc.got != tc.want {
			t.Fatalf("%q != %q", tc.got, tc.want)
		}
	}
}

// R-WAIR-KIOO
func TestAuth(t *testing.T) {
	r := httptest.NewRequest("GET", "https://local.test", nil)
	r.Host = "GMAIL.space.test:443"
	r.Header.Set("X-Forwarded-Proto", "HTTP")
	if got := urls.Auth(r, ""); got != "http://auth.space.test" {
		t.Fatal(got)
	}
	r.Header.Set("X-Forwarded-Proto", "invalid")
	if got := urls.Auth(r, filepath.Join(t.TempDir(), "missing")); got != "https://auth.space.test" {
		t.Fatal(got)
	}
	r.Host = "gmail.space.test:nondigit"
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
