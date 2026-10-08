package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-ZT46-0YK2 R-TVM7-45J1 R-TY1Z-VP0F
func TestActivateResourcesMemoryGrammarAndRendering(t *testing.T) {
	for _, test := range []struct{ unit, maximum, manifest, want string }{
		{"ikigenba.slice", "MemoryMax=1G\nMemoryMax=4096M\n", "128M", "resources: ok (1 app)\n"},
		{"ikigenba-apps.slice", "MemoryMax=4096M\nMemoryMax=infinity\n", "128M", "/etc/systemd/system/ikigenba-apps.slice has no MemoryMax; run 'opsctl init'"},
		{"ikigenba.slice", "MemoryMax=\n", "128M", "/etc/systemd/system/ikigenba.slice has no MemoryMax; run 'opsctl init'"},
		{"ikigenba.slice", "MemoryMax=1.5G\n", "128M", "/etc/systemd/system/ikigenba.slice has no MemoryMax; run 'opsctl init'"},
		{"ikigenba.slice", "MemoryMax=1024MB\n", "128M", "/etc/systemd/system/ikigenba.slice has no MemoryMax; run 'opsctl init'"},
		{"ikigenba-apps.slice", "MemoryMax=1024M\n", "2G", "dummy: etc/manifest.toml: memory_max 2048M is more than ikigenba-apps.slice's MemoryMax 1024M"},
		{"ikigenba-apps.slice", "MemoryMax=1024K\n", "1500K", "dummy: etc/manifest.toml: memory_max 1500K is more than ikigenba-apps.slice's MemoryMax 1M"},
		{"ikigenba-apps.slice", "MemoryMax=999\n", "1000", "dummy: etc/manifest.toml: memory_max 1000 is more than ikigenba-apps.slice's MemoryMax 999"},
		{"ikigenba-apps.slice", "MemoryMax=128M\n", "128M", "resources: ok (1 app)\n"},
	} {
		t.Run(test.unit+test.maximum+test.manifest, func(t *testing.T) {
			f := newTransitionFixture(t)
			f.write("etc/systemd/system/"+test.unit, test.maximum, 0644)
			f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/etc/manifest.toml", "app='dummy'\n[resources]\nmemory_max="+strconv.Quote(test.manifest), 0644)
			code, out, err := f.run("activate", transitionSHA)
			if strings.HasPrefix(test.want, "resources: ok") {
				if code != 0 || err != "" || !strings.Contains(out, test.want) {
					t.Fatalf("exit %d output %q error %q", code, out, err)
				}
			} else if code != 1 || err != "opsctl: activate failed\n" || !strings.Contains(out, "resources: failed: "+test.want+"\n") {
				t.Fatalf("exit %d output %q error %q", code, out, err)
			}
		})
	}

}

// R-TVM7-45J1 R-TY1Z-VP0F
func TestActivateResourcesWarningsIncludeDisabledAppsAndExactSums(t *testing.T) {
	for _, test := range []struct {
		count               int
		memory, suite, want string
	}{
		{2, "128M", "256M", "resources: ok (2 apps)\n"},
		{3, "128M", "256M", "resources: ok (3 apps; warning: ikigenba-apps.slice memory_max adds up to 384M, more than twice its MemoryMax 128M)\n"},
		{3, "128M", "128M", "resources: ok (3 apps; warning: ikigenba-apps.slice memory_max adds up to 384M, more than twice its MemoryMax 128M; warning: ikigenba.slice memory_max adds up to 512M, more than twice its MemoryMax 128M)\n"},
		{3, "9223372036854775807", "9223372036854775807", "resources: ok (3 apps; warning: ikigenba-apps.slice memory_max adds up to 27670116110564327421, more than twice its MemoryMax 9223372036854775807; warning: ikigenba.slice memory_max adds up to 27670116110698545149, more than twice its MemoryMax 9223372036854775807)\n"},
	} {
		f := newTransitionFixture(t)
		names := []string{"dummy", "notes", "tasks"}[:test.count]
		f.addRelease(transitionSHA, names...)
		for _, name := range names {
			f.write("opt/ikigenba/releases/"+transitionSHA+"/"+name+"/etc/manifest.toml", "app="+strconv.Quote(name)+"\n[resources]\nmemory_max="+strconv.Quote(test.memory), 0644)
		}
		f.write("etc/systemd/system/ikigenba.slice", "MemoryMax="+test.suite+"\n", 0644)
		f.write("etc/systemd/system/ikigenba-apps.slice", "MemoryMax="+test.memory+"\n", 0644)
		// A released host supplies a disabled installed app at invocation.
		f.addRelease(transitionOld, "dummy")
		f.link("current", transitionOld)
		f.disabled["dummy"] = true
		code, out, err := f.run("activate", transitionSHA)
		if code != 0 || err != "" || !strings.Contains(out, test.want) {
			t.Fatalf("exit %d output %q error %q", code, out, err)
		}
	}
}

// R-TVM7-45J1
func TestActivateResourcesCoreCeilingAndFirstFailure(t *testing.T) {
	for _, test := range []struct{ name, suite, core, want string }{
		{"core uses suite", "MemoryMax=128M\n", "MemoryMax=4096M\n", "dummy: etc/manifest.toml: memory_max 256M is more than ikigenba.slice's MemoryMax 128M"},
		{"core needs no MemoryMax", "MemoryMax=256M\n", "[Slice]\nCPUWeight=300\n", ""},
		{"suite first", "MemoryMax=infinity\n", "", "/etc/systemd/system/ikigenba.slice has no MemoryMax; run 'opsctl init'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newTransitionFixture(t)
			f.write("etc/systemd/system/ikigenba.slice", test.suite, 0644)
			f.write("etc/systemd/system/ikigenba-core.slice", test.core, 0644)
			f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/etc/manifest.toml", "app='dummy'\n[resources]\nslice='core'\nmemory_max='256M'", 0644)
			before, err := preflightTree(f.root, "opt/ikigenba/releases/"+transitionSHA, false)
			if err != nil {
				t.Fatal(err)
			}
			code, out, stderr := f.run("activate", transitionSHA)
			if test.want == "" {
				if code != 0 || stderr != "" || !strings.Contains(out, "resources: ok (1 app)\n") {
					t.Fatalf("exit %d output %q error %q", code, out, stderr)
				}
			} else {
				if code != 1 || stderr != "opsctl: activate failed\n" || !strings.HasSuffix(out, "resources: failed: "+test.want+"\n") {
					t.Fatalf("exit %d output %q error %q", code, out, stderr)
				}
				if err := checkPreflightTree(f, before, false); err != nil {
					t.Fatal(err)
				}
				for _, command := range f.commands {
					if !allowedPreflightCommand(f.root, command, false) {
						t.Fatalf("side effect before resource refusal: %#v", command)
					}
				}
			}
		})
	}
	// Both apps fail, so only the first name in ascending order must be reported.
	f := newTransitionFixture(t)
	f.addRelease(transitionSHA, "alpha", "zeta")
	for _, name := range []string{"alpha", "dummy", "zeta"} {
		f.write("opt/ikigenba/releases/"+transitionSHA+"/"+name+"/etc/manifest.toml", "app="+strconv.Quote(name)+"\n[resources]\nmemory_max='8G'", 0644)
	}
	before, err := preflightTree(f.root, "opt/ikigenba/releases/"+transitionSHA, false)
	if err != nil {
		t.Fatal(err)
	}
	code, out, stderr := f.run("activate", transitionSHA)
	if code != 1 || stderr != "opsctl: activate failed\n" || !strings.HasSuffix(out, "resources: failed: alpha: etc/manifest.toml: memory_max 8192M is more than ikigenba-apps.slice's MemoryMax 4096M\n") {
		t.Fatalf("exit %d output %q error %q", code, out, stderr)
	}
	if err := checkPreflightTree(f, before, false); err != nil {
		t.Fatal(err)
	}
}

// R-TVM7-45J1
func TestReleaseResourceMissingUnitsHaveExactErrorsWithoutEffects(t *testing.T) {
	for _, unit := range []string{"ikigenba.slice", "ikigenba-core.slice", "ikigenba-apps.slice"} {
		t.Run(unit, func(t *testing.T) {
			f := newTransitionFixture(t)
			slice := "apps"
			if unit == "ikigenba-core.slice" {
				slice = "core"
			}
			name := filepath.Join(f.root, "etc/systemd/system", unit)
			if err := os.Remove(name); err != nil {
				t.Fatal(err)
			}
			before, err := preflightTree(f.root, "", false)
			if err != nil {
				t.Fatal(err)
			}
			warnings, err := apps.CheckReleaseResources(f.root, []apps.Manifest{{App: "dummy", Resources: apps.Resources{Slice: slice, MemoryMax: 134217728}}})
			want := "/etc/systemd/system/" + unit + " is missing; run 'opsctl init'"
			if err == nil || err.Error() != want || warnings != "" {
				t.Fatalf("warnings %q error %v want %q", warnings, err, want)
			}
			after, err := preflightTree(f.root, "", false)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("resource error changed files: %v", err)
			}
			if len(f.commands) != 0 {
				t.Fatalf("resource check executed commands: %#v", f.commands)
			}
		})
	}
}
