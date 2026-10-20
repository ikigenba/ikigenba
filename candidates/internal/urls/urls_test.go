package urls_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/urls"
)

// R-729R-C2EM R-1WYO-7MDJ R-1Y6K-LE48 R-1ZEG-Z5UX
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

// R-8ANX-A3CH
func TestPagePath(t *testing.T) {
	for _, tc := range []struct {
		id     string
		number int64
		want   string
	}{
		{"cand_5a5a5a5a5a5a5a5a", 7, "/candidates/cand_5a5a5a5a5a5a5a5a/pages/7"},
		{"arbitrary/path", 12345678901234, "/candidates/arbitrary/path/pages/12345678901234"},
		{"", -7, "/candidates//pages/-7"},
		{"anything", 0, "/candidates/anything/pages/0"},
	} {
		if got := urls.PagePath(tc.id, tc.number); got != tc.want {
			t.Fatalf("PagePath = %q, want %q", got, tc.want)
		}
	}
}

// R-73HN-PU5B R-0YFO-7SVP R-729R-C2EM
func TestCampaignAddresses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.json")
	for i, data := range []string{`{"services":[{"name":"campaigns","description":"","socket":"","enabled":true,"mcp":false,"url":"https://campaigns.example.test//"}]}`, `{"services":[{"name":"auth","description":"","socket":"","url":"https://auth.test","enabled":true,"mcp":false},{"name":"scrapes","description":"","socket":"","url":"https://scrapes.test","enabled":true,"mcp":false}]}`, `not-json`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		want := ""
		if i == 0 {
			want = "https://campaigns.example.test"
		}
		if got := urls.Campaigns(path); got != want {
			t.Fatal(got, want)
		}
	}
	for _, path := range []string{"", path + "absent"} {
		if urls.Campaigns(path) != "" {
			t.Fatal(path)
		}
	}
	for _, tc := range []struct{ base, id, want string }{
		{"https://chosen.test", "camp_chosen", "https://chosen.test/campaigns/camp_chosen"},
		{"base", "a/b?c#d", "base/campaigns/a%2Fb%3Fc%23d"}, {"base", "a b", "base/campaigns/a%20b"}, {"", "id", ""}, {"base", "", ""},
	} {
		if got := urls.CampaignPage(tc.base, tc.id); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}
