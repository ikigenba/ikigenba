package page_test

import (
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
)

func writeEntries(t *testing.T, path string, entries []map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func entry(name, url string, enabled bool) map[string]any {
	return map[string]any{
		"name": name, "url": url, "enabled": enabled,
		"description": "", "socket": "", "mcp": false,
	}
}

func iconEntry(name, url, icon string, enabled bool) map[string]any {
	value := entry(name, url, enabled)
	value["icon"] = icon
	return value
}

func TestPublicSurface(t *testing.T) {
	// R-HLTX-LS8Q R-HT5B-WEOW R-HUD8-A6FL R-SGAN-GQS4 R-HY0X-FHNO
	t.Setenv(services.Variable, "")
	user := page.User{"email", "/profile", "/logout"}
	if user.Email != "email" || user.ProfileURL != "/profile" || user.LogoutURL != "/logout" {
		t.Fatal("User fields or order")
	}
	icon := template.HTML("<svg/>")
	service := page.Service{"app", "/app", icon, true, false}
	if service.Name != "app" || service.URL != "/app" || service.Icon != icon || !service.Enabled || service.Current {
		t.Fatal("Service fields or order")
	}
	banner := page.Banner{"app", icon, "build", user.Email, user.ProfileURL, user.LogoutURL, []page.Service{service}}
	if banner.Service != "app" || banner.Icon != icon || banner.Version != "build" || banner.Email != user.Email || banner.ProfileURL != user.ProfileURL || banner.LogoutURL != user.LogoutURL || banner.Services[0] != service {
		t.Fatal("Banner fields or order")
	}
	useBanner := func(bannerOf func(page.User) page.Banner) { _ = bannerOf(user) }
	useKit := func(newKit func(string, string) *page.Kit) { useBanner(newKit("app", "build").Banner) }
	useKit(page.New)
}

func TestBannerUnalteredValues(t *testing.T) {
	// R-I0GQ-7152
	t.Setenv(services.Variable, "")
	u := page.User{Email: "  person+tag@example.test ", ProfileURL: "?profile=<>&", LogoutURL: "../exit?x=1"}
	version := " release+build/<>& \n"
	got := page.New(" App ", version).Banner(u)
	if got.Service != " App " || got.Version != version || got.Email != u.Email || got.ProfileURL != u.ProfileURL || got.LogoutURL != u.LogoutURL {
		t.Fatalf("values altered: %+v", got)
	}
}

func TestEnvironmentSnapshot(t *testing.T) {
	// R-HZ8T-T9ED
	first, second := filepath.Join(t.TempDir(), "first"), filepath.Join(t.TempDir(), "second")
	writeEntries(t, first, []map[string]any{iconEntry("first", "/", "", true)})
	writeEntries(t, second, []map[string]any{iconEntry("second", "/", "", true)})
	t.Setenv(services.Variable, first)
	kit := page.New("app", "build")
	for _, path := range []string{second, ""} {
		t.Setenv(services.Variable, path)
		if got := kit.Banner(page.User{}).Services; len(got) != 1 || got[0].Name != "first" {
			t.Fatalf("snapshot changed: %+v", got)
		}
	}
	if err := os.Unsetenv(services.Variable); err != nil {
		t.Fatal(err)
	}
	if got := kit.Banner(page.User{}).Services; len(got) != 1 || got[0].Name != "first" {
		t.Fatalf("unset changed snapshot: %+v", got)
	}
	unset := page.New("app", "build")
	t.Setenv(services.Variable, "")
	empty := page.New("app", "build")
	t.Setenv(services.Variable, second)
	for _, noPath := range []*page.Kit{unset, empty} {
		if len(noPath.Banner(page.User{}).Services) != 0 {
			t.Fatal("later setting changed captured empty path")
		}
	}
}

func TestBannerReadsEachCall(t *testing.T) {
	// R-I1OM-KSVR
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(services.Variable, "services.json")
	kit := page.New("app", "build")
	if len(kit.Banner(page.User{}).Services) != 0 {
		t.Fatal("missing file yielded services")
	}
	path := filepath.Join(dir, "services.json")
	for _, name := range []string{"appeared", "changed"} {
		writeEntries(t, path, []map[string]any{iconEntry(name, "/", "", true)})
		if got := kit.Banner(page.User{}).Services; len(got) != 1 || got[0].Name != name {
			t.Fatalf("file change not reflected: %+v", got)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if len(kit.Banner(page.User{}).Services) != 0 {
		t.Fatal("removed file retained services")
	}
}

func TestBannerFiltersAndMapsReaderEntries(t *testing.T) {
	// R-6XQZ-7ZFT
	path := filepath.Join(t.TempDir(), "services.json")
	badIcon := entry("bad-icon", "/", true)
	badIcon["icon"] = false
	legacy := map[string]any{"name": "old", "url": "/", "icon": "old", "enabled": true}
	writeEntries(t, path, []map[string]any{
		iconEntry("app", "/a?x=1&y=2", "<svg>\n&\"é</svg>", false),
		entry("without-icon", "/", true), badIcon, legacy,
		iconEntry("APP", "", "", true),
		iconEntry("app", "different", "other", true),
		iconEntry(" ", "", "", false),
	})
	t.Setenv(services.Variable, path)
	got := page.New("app", "build").Banner(page.User{}).Services
	want := []page.Service{
		{Name: "app", URL: "/a?x=1&y=2", Icon: template.HTML("<svg>\n&\"é</svg>"), Current: true},
		{Name: "APP", Enabled: true},
		{Name: "app", URL: "different", Icon: "other", Enabled: true, Current: true},
		{Name: " "},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("services = %+v; want %+v", got, want)
	}
}

func TestBannerFallbackAndNoPanic(t *testing.T) {
	// R-I44F-CCD5 R-I5CB-Q43U
	path := filepath.Join(t.TempDir(), "services.json")
	for _, content := range []string{
		"", "{", "\xff", "\xef\xbb\xbf{}", "{} {}", "[]", "null",
		"true", "42", "{}", `{"services":null}`, `{"services":[]}`,
		`{"services":[{"name":"app","url":"/","description":"","socket":"","enabled":true,"mcp":false}]}`,
		`{"services":[{"name":"app","url":"/","description":"","socket":"","enabled":true,"mcp":false,"icon":null}]}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(services.Variable, path)
		if got := page.New("app", "build").Banner(page.User{}).Services; len(got) != 0 {
			t.Fatalf("content %q yielded %+v", content, got)
		}
	}
	for _, unreadable := range []string{"", filepath.Join(t.TempDir(), "absent"), t.TempDir(), path + "/"} {
		t.Setenv(services.Variable, unreadable)
		if len(page.New("app", "build").Banner(page.User{}).Services) != 0 {
			t.Fatalf("path %q yielded services", unreadable)
		}
	}
	if err := os.Unsetenv(services.Variable); err != nil {
		t.Fatal(err)
	}
	if len(page.New("app", "build").Banner(page.User{}).Services) != 0 {
		t.Fatal("unset variable yielded services")
	}
}

func TestZeroKit(t *testing.T) {
	// R-I6K8-3VUJ
	t.Setenv(services.Variable, "")
	if err := os.Unsetenv(services.Variable); err != nil {
		t.Fatal(err)
	}
	u := page.User{Email: "email", ProfileURL: "profile", LogoutURL: "logout"}
	var zero page.Kit
	got, want := zero.Banner(u), page.New("", "").Banner(u)
	if !reflect.DeepEqual(got, want) || got.Service != "" || got.Version != "" || len(got.Services) != 0 {
		t.Fatalf("zero Kit: %+v versus %+v", got, want)
	}
}

func TestConcurrentBanner(t *testing.T) {
	// R-I7S4-HNL8
	path := filepath.Join(t.TempDir(), "services.json")
	writeEntries(t, path, []map[string]any{iconEntry("app", "/", "<svg/>", true)})
	t.Setenv(services.Variable, path)
	kit := page.New("app", "build")
	want := kit.Banner(page.User{Email: "person"})
	results := make(chan page.Banner, 64)
	var workers sync.WaitGroup
	for range 64 {
		workers.Go(func() { results <- kit.Banner(page.User{Email: "person"}) })
	}
	workers.Wait()
	close(results)
	for got := range results {
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("concurrent result: %+v", got)
		}
	}
}

func TestBannerCurrentIcon(t *testing.T) {
	// R-SHIJ-UIIT
	path := filepath.Join(t.TempDir(), "services.json")
	t.Setenv(services.Variable, path)
	kit := page.New("app", "build")
	for _, test := range []struct {
		name    string
		entries []map[string]any
		want    template.HTML
	}{
		{"first current after other services", []map[string]any{iconEntry("other", "/", "other", true), entry("app", "/", true), iconEntry("app", "/", "<svg>\n&amp;é</svg>", false), iconEntry("app", "/", "second", true)}, "<svg>\n&amp;é</svg>"},
		{"empty first current icon", []map[string]any{iconEntry("app", "/", "", true), iconEntry("app", "/", "second", true)}, ""},
		{"name matching is exact", []map[string]any{iconEntry("APP", "/", "other", true)}, ""},
		{"current lacks icon", []map[string]any{entry("app", "/", true), iconEntry("other", "/", "other", true)}, ""},
		{"no entries", nil, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			writeEntries(t, path, test.entries)
			if got := kit.Banner(page.User{}).Icon; got != test.want {
				t.Fatalf("Icon = %q, want %q", got, test.want)
			}
		})
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := kit.Banner(page.User{}).Icon; got != "" {
		t.Fatalf("missing file Icon = %q", got)
	}
	t.Setenv(services.Variable, "")
	if got := page.New("app", "build").Banner(page.User{}).Icon; got != "" {
		t.Fatalf("empty path Icon = %q", got)
	}
	if err := os.Unsetenv(services.Variable); err != nil {
		t.Fatal(err)
	}
	if got := page.New("app", "build").Banner(page.User{}).Icon; got != "" {
		t.Fatalf("unset path Icon = %q", got)
	}
}
