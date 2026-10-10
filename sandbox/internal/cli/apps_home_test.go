package cli

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

// R-4YRM-W3FD
func TestHomeGroupRefusals(t *testing.T) {
	for _, value := range []string{`"Core"`, `"apps"`, `""`, `1`, `true`, `[]`, `{}`} {
		t.Run(value, func(t *testing.T) {
			f := newAppFixture(t)
			f.manifest("dummy", appManifest("dummy")+"[home]\ngroup="+value+"\n")
			f.refuse(`dummy: etc/manifest.toml: 'home.group' must be "core" or "application"`)
		})
	}
}

// R-4ZZJ-9V62
func TestHomeUnknownKeys(t *testing.T) {
	for _, entry := range []struct{ key, value, printed string }{
		{"order", "1", "order"}, {"slice", `"core"`, "slice"},
		{"aa", "[]", "aa"}, {"extra", "{}", "extra"},
		{"bad\nkey\t\x7f", "false", `bad\x0akey\x09\x7f`},
	} {
		t.Run(entry.printed, func(t *testing.T) {
			f := newAppFixture(t)
			f.manifest("dummy", appManifest("dummy")+"[home]\n"+appTOMLString(entry.key)+"="+entry.value+"\n")
			f.refuse("dummy: etc/manifest.toml: 'home." + entry.printed + "' is not allowed; the only home key is group")
		})
	}
}

// R-4XJQ-IBOO
func TestHomeCheckOrder(t *testing.T) {
	for _, c := range []struct{ entries, want string }{
		{"aa=1\ngroup=\"apps\"\n", `'home.group' must be "core" or "application"`},
		{"zz=1\norder=1\n", "'home.order' is not allowed; the only home key is group"},
		{"group=\"core\"\nzz=1\norder=1\n", "'home.order' is not allowed; the only home key is group"},
	} {
		f := newAppFixture(t)
		f.manifest("dummy", appManifest("dummy")+"[home]\n"+c.entries)
		f.refuse("dummy: etc/manifest.toml: " + c.want)
	}
}

// R-517F-NMWR
func TestHomeGroupsInServices(t *testing.T) {
	for _, table := range []string{"", "[home]\n", "[home]\ngroup=\"application\"\n"} {
		f := newAppFixture(t)
		f.manifest("auth", appManifest("auth")+"[home]\ngroup=\"core\"\n")
		f.manifest("dummy", appManifest("dummy")+table)
		f.success()
		var services struct {
			Services []struct{ Name, Group string }
		}
		if err := json.Unmarshal([]byte(f.read("services.json")), &services); err != nil {
			t.Fatal(err)
		}
		if len(services.Services) != 2 || services.Services[0].Name != "auth" || services.Services[0].Group != "core" || services.Services[1].Name != "dummy" || services.Services[1].Group != "application" {
			t.Fatalf("home groups: %+v", services)
		}
	}
}

// R-53N8-F6E5
func TestHomeDoesNotChangeDeployment(t *testing.T) {
	f := newAppFixture(t)
	f.manifest("auth", appManifest("auth"))
	f.manifest("dummy", appManifest("dummy"))
	f.success()
	snapshot := func() map[string]string {
		files := upSnapshot(t, filepath.Join(f.state, "ikigenba/sandbox"))
		for name, value := range upSnapshot(t, filepath.Join(f.config, "systemd/user")) {
			files["units/"+name] = value
		}
		return files
	}
	code, beforeOut, stderr := f.run()
	if code != 0 || stderr != "" {
		t.Fatalf("baseline: %d %s", code, stderr)
	}
	beforeFiles := snapshot()
	beforeCommands := append([]seam.Cmd(nil), f.commands...)
	f.manifest("auth", appManifest("auth")+"[home]\ngroup=\"core\"\n")
	code, afterOut, stderr := f.run()
	if code != 0 || stderr != "" {
		t.Fatalf("with home: %d %s", code, stderr)
	}
	afterFiles := snapshot()
	const services = "wip/services.json"
	wantServices := strings.Replace(beforeFiles[services], `"group": "application"`, `"group": "core"`, 1)
	if beforeFiles[services] == "" || afterFiles[services] != wantServices {
		t.Fatalf("home changed services beyond auth's group: %q", afterFiles[services])
	}
	afterFiles[services] = beforeFiles[services]
	if beforeOut != afterOut || !reflect.DeepEqual(beforeCommands, f.commands) || !reflect.DeepEqual(beforeFiles, afterFiles) {
		t.Fatal("home changed deployment output, files or commands")
	}
}
