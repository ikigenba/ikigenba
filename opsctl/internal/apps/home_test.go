package apps_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-U766-75SS
func TestHomeFields(t *testing.T) {
	home := apps.Home{"core"}
	groups := []string{home.Group}
	group := groups[0]
	if group != "core" {
		t.Fatalf("Home = %#v", home)
	}
}

// R-U9LY-YPA6 R-UC1R-Q8RK
func TestParseManifestHomeDefaultsAndGroups(t *testing.T) {
	for _, data := range []string{"", "[home]", "home = {}"} {
		manifest, err := apps.ParseManifest([]byte(data))
		if err != nil || manifest.Home.Group != "application" {
			t.Fatalf("ParseManifest(%q) = %#v, %v", data, manifest, err)
		}
	}
	for _, group := range []string{"core", "application"} {
		for _, format := range []string{"[home]\ngroup = %q", "home.group = %q", "home = {group = %q}"} {
			data := fmt.Sprintf(format, group)
			manifest, err := apps.ParseManifest([]byte(data))
			if err != nil || manifest.Home.Group != group {
				t.Fatalf("ParseManifest(%q) = %#v, %v", data, manifest, err)
			}
		}
	}
	for _, value := range []string{`"apps"`, `"Core"`, `""`, `" core "`, "1", "true", "[]", "{}", "1.0", "1979-05-27"} {
		for _, format := range []string{"[home]\ngroup = %s", "home.group = %s", "home = {group = %s}"} {
			assertHomeError(t, fmt.Sprintf(format, value), `'home.group' must be "core" or "application"`)
		}
	}
}

// R-UD9O-40I9
func TestParseManifestRejectsUnknownHomeKeys(t *testing.T) {
	for _, key := range []string{"title", "order", "Group", "Unknown Key"} {
		for _, value := range []string{`"Mail"`, "1", "true", "[]", "{}", "{nested = 1}"} {
			for _, format := range []string{"[home]\n%q = %s", "home.%q = %s", "home = {%q = %s}"} {
				assertHomeError(t, fmt.Sprintf(format, key, value), fmt.Sprintf("'home.%s' is not allowed; the only home key is group", key))
			}
		}
	}
	for _, data := range []string{"[home.title]", "home.title.nested = 1", "[home]\ntitle.nested = 1"} {
		assertHomeError(t, data, "'home.title' is not allowed; the only home key is group")
	}
}

// R-7HVJ-BY7R
func TestHomeErrorOrder(t *testing.T) {
	assertHomeError(t, "[home]\ngroup = 'apps'\ntitle = 'Mail'", `'home.group' must be "core" or "application"`)
	assertHomeError(t, "[home]\ntitle = 1\norder = 1", "'home.order' is not allowed; the only home key is group")
	assertHomeError(t, "[home]\nzz = 1\n'a' = 1\n'Z' = 1", "'home.Z' is not allowed; the only home key is group")
	for _, other := range []string{
		"port = 1", "app = 'host'", "app = 3", "description = 3", `description = "\n"`,
		"default = 'yes'", "mcp = 3", "guests = 3", "secrets = [3]", "env = {MODE = 3}",
		"database = {engine = 'postgres', path = 'state/db'}", "database = {engine = 'sqlite', path = '../db'}",
		"resources = {slice = 'edge'}", "resources = {memory_max = 'bad'}", "resources = {go_memory_limit = 'bad'}",
		"resources = {go_memory_limit = '129M'}", "resources = {cpu_weight = 0}", "resources = {delegate = 1}",
		"resources = {oom_policy = 'stop'}", "resources = {unknown = 1}", "resources = 1", "database = 1",
	} {
		_, baseline := apps.ParseManifest([]byte(other))
		if baseline == nil {
			t.Fatalf("fault fixture %q accepted", other)
		}
		for _, home := range []string{"home = {group = 'apps'}", "home = {title = 1}"} {
			for _, data := range []string{other + "\n" + home, home + "\n" + other} {
				manifest, combined := apps.ParseManifest([]byte(data))
				assertEmptyResourceFailure(t, manifest)
				if combined == nil || manifestErrorDetail(combined.Error()) != manifestErrorDetail(baseline.Error()) {
					t.Errorf("ParseManifest(%q) = %v; want %v", data, combined, baseline)
				}
			}
		}
	}
	for _, home := range []string{"group = 'apps'", "title = 1"} {
		_, baseline := apps.ParseManifest([]byte("[home]\n" + home))
		assertHomeError(t, "mcp = true\n[home]\n"+home, baseline.Error())
	}
}

// R-UFPG-VJZN
func TestResourceErrorsPrecedeHomeErrors(t *testing.T) {
	for _, resources := range []string{
		"slice = 'edge'", "memory_max = 'bad'", "go_memory_limit = 'bad'", "go_memory_limit = '129M'",
		"cpu_weight = 0", "delegate = 1", "oom_policy = 'stop'", "io_weight = 1",
	} {
		_, baseline := apps.ParseManifest([]byte("[resources]\n" + resources))
		if baseline == nil {
			t.Fatalf("resource fault accepted: %s", resources)
		}
		for _, home := range []string{"group = 'apps'", "title = 1"} {
			assertHomeError(t, "[resources]\n"+resources+"\n[home]\n"+home, baseline.Error())
		}
	}
}

func manifestErrorDetail(message string) string {
	if strings.HasPrefix(message, "invalid manifest: line ") {
		return strings.SplitN(message, ": ", 3)[2]
	}
	return message
}

func assertHomeError(t *testing.T, data, want string) {
	t.Helper()
	manifest, err := apps.ParseManifest([]byte(data))
	assertEmptyResourceFailure(t, manifest)
	if err == nil || err.Error() != want {
		t.Fatalf("ParseManifest(%q) = %v; want %q", data, err, want)
	}
}
