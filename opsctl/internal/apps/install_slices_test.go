package apps_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

func installSliceFixtures(t *testing.T, root string) {
	t.Helper()
	for _, unit := range []string{"ikigenba.slice", "ikigenba-apps.slice", "ikigenba-core.slice"} {
		name := filepath.Join(root, "etc/systemd/system", unit)
		if _, err := os.Lstat(name); os.IsNotExist(err) {
			writeFixture(t, name, []byte("[Slice]\nMemoryMax=4096M\n"), 0o644)
		}
	}
}

// R-ZRW9-N6TD R-ZT46-0YK2 R-3DZE-E68V R-3CRI-0EI6
func TestInstallSliceChecksPrecedeDiscoveryAndMutation(t *testing.T) {
	for _, test := range []struct {
		name, slice, unit, contents, memory, want string
		missing                                   bool
		code                                      int
	}{
		{name: "suite absent", unit: "ikigenba.slice", missing: true, want: "/etc/systemd/system/ikigenba.slice is missing; run 'opsctl init'", code: 1},
		{name: "apps absent", unit: "ikigenba-apps.slice", missing: true, want: "/etc/systemd/system/ikigenba-apps.slice is missing; run 'opsctl init'", code: 1},
		{name: "core absent", slice: "core", unit: "ikigenba-core.slice", missing: true, want: "/etc/systemd/system/ikigenba-core.slice is missing; run 'opsctl init'", code: 1},
		{name: "suite no memory", unit: "ikigenba.slice", contents: "MemoryMax=1024M\nMemoryMax=infinity\n", want: "/etc/systemd/system/ikigenba.slice has no MemoryMax; run 'opsctl init'", code: 1},
		{name: "apps no memory", unit: "ikigenba-apps.slice", contents: " MemoryMax=1024M\n", want: "/etc/systemd/system/ikigenba-apps.slice has no MemoryMax; run 'opsctl init'", code: 1},
		{name: "apps limit M", unit: "ikigenba-apps.slice", contents: "MemoryMax=1024M\n", memory: "2G", want: "notes-from-uri-v0.tar.xz: etc/manifest.toml: memory_max 2048M is more than ikigenba-apps.slice's MemoryMax 1024M", code: 2},
		{name: "core limit K", slice: "core", unit: "ikigenba.slice", contents: "MemoryMax=1500K\n", memory: "1536001", want: "notes-from-uri-v0.tar.xz: etc/manifest.toml: memory_max 1536001 is more than ikigenba.slice's MemoryMax 1500K", code: 2},
		{name: "core limit bytes", slice: "core", unit: "ikigenba.slice", contents: "MemoryMax=1000\n", memory: "1001", want: "notes-from-uri-v0.tar.xz: etc/manifest.toml: memory_max 1001 is more than ikigenba.slice's MemoryMax 1000", code: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			fixture := newCompletedInstallFixture(t, root, false)
			if test.slice == "" {
				test.slice = "apps"
			}
			if test.memory == "" {
				test.memory = "128M"
			}
			fixture.archive = validInstallTar(t, fmt.Sprintf("app='notes'\n[resources]\nslice='%s'\nmemory_max='%s'\n", test.slice, test.memory))
			unitPath := filepath.Join(root, "etc/systemd/system", test.unit)
			if test.missing {
				if err := os.Remove(unitPath); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFixture(t, unitPath, []byte(test.contents), 0o644)
			}
			// This later discovery error must not replace the slice failure.
			writeFixture(t, filepath.Join(root, "opt/old/etc/manifest.toml"), []byte("app=[\n"), 0o600)
			installStoreAt(t, root, map[string]string{"host.name": "host.example", "aws.region": "us-east-1"})
			before := snapshotTree(t, root)
			err := fixture.run()
			var failure *apps.InstallError
			if !errors.As(err, &failure) || failure.Code != test.code || failure.Cause == nil {
				t.Fatalf("failure %#v", err)
			}
			want := []installReport{{"fetch", "notes-from-uri-v0.tar.xz, 0.0 MiB", true}, {"file", test.want, false}}
			if !reflect.DeepEqual(fixture.reports, want) || fixture.configureCalls != 0 {
				t.Fatalf("reports %#v configure %d", fixture.reports, fixture.configureCalls)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, root)) {
				t.Fatal("slice failure changed host state")
			}
			if len(fixture.commands) != 1 || fixture.commands[0].name != "xz" {
				t.Fatalf("later commands %#v", fixture.commands)
			}
		})
	}
}

// R-ZT46-0YK2 R-3DZE-E68V
func TestInstallAcceptsLastValidMemoryMaxAndEquality(t *testing.T) {
	for _, slice := range []string{"apps", "core"} {
		for _, ceiling := range []string{"1024M", "1536000", "1000", "00001024K"} {
			t.Run(slice+"/"+ceiling, func(t *testing.T) {
				fixture := newCompletedInstallFixture(t, t.TempDir(), false)
				fixture.archive = validInstallTar(t, "app='notes'\n[resources]\nslice='"+slice+"'\nmemory_max='"+ceiling+"'\n")
				writeFixture(t, filepath.Join(fixture.root, "etc/systemd/system/ikigenba.slice"), []byte("MemoryMax=infinity\nMemoryMax="+ceiling+"\n"), 0o644)
				if slice == "apps" {
					writeFixture(t, filepath.Join(fixture.root, "etc/systemd/system/ikigenba-apps.slice"), []byte("MemoryMax="+ceiling+"\n"), 0o644)
				} else {
					writeFixture(t, filepath.Join(fixture.root, "etc/systemd/system/ikigenba-core.slice"), []byte("[Slice]\n"), 0o644)
				}
				if err := fixture.run(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	for _, invalid := range []string{"infinity", "", "1.5G", "1024MB", "0", "9223372036854775808", "1m", " 1G", "1G "} {
		t.Run("invalid/"+invalid, func(t *testing.T) {
			fixture := newCompletedInstallFixture(t, t.TempDir(), false)
			writeFixture(t, filepath.Join(fixture.root, "etc/systemd/system/ikigenba.slice"), []byte("MemoryMax=4096M\nMemoryMax="+invalid+"\n"), 0o644)
			err := fixture.run()
			var failure *apps.InstallError
			if !errors.As(err, &failure) || failure.Code != 1 || fixture.reports[len(fixture.reports)-1].detail != "/etc/systemd/system/ikigenba.slice has no MemoryMax; run 'opsctl init'" {
				t.Fatalf("invalid value %q: %v %#v", invalid, err, fixture.reports)
			}
		})
	}
}

// R-ZPGG-VNBZ
func TestInstallReplacesItsOwnRejectedManifest(t *testing.T) {
	fixture := newCompletedInstallFixture(t, t.TempDir(), false)
	writeFixture(t, filepath.Join(fixture.root, "opt/notes/etc/manifest.toml"), []byte("app='notes'\n[resources]\nio_weight=50\n"), 0o644)
	if err := fixture.run(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(fixture.root, "opt/notes/etc/manifest.toml"))
	if err != nil || strings.Contains(string(data), "io_weight") {
		t.Fatalf("manifest %q: %v", data, err)
	}
}

// R-3F7A-RXZK R-3CRI-0EI6
func TestInstallReportsExactMemoryWarningsWithoutStopping(t *testing.T) {
	type installed struct{ name, slice, memory, kind string }
	for _, test := range []struct {
		name, slice, memory, suite, apps, want string
		installed                              []installed
	}{
		{name: "sample apps warning", memory: "640M", suite: "1536M", apps: "1024M", installed: []installed{{"auth", "core", "128M", ""}, {"telemetry", "core", "256M", ""}, {"mcp", "apps", "128M", ""}, {"sites", "apps", "128M", ""}, {"dummy", "apps", "64M", "disabled"}, {"repos", "apps", "256M", ""}, {"scripts", "apps", "896M", ""}}, want: "; warning: ikigenba-apps.slice memory_max adds up to 2112M, more than twice its MemoryMax 1024M"},
		{name: "equal apps boundary", memory: "512M", suite: "1536M", apps: "1024M", installed: []installed{{"other", "apps", "1536M", ""}}},
		{name: "replacement excludes old allocation", memory: "128M", suite: "512M", apps: "256M", installed: []installed{{"notes", "apps", "512M", ""}, {"other", "apps", "128M", ""}}},
		{name: "default manifestless includes disabled", memory: "128M", suite: "256M", apps: "128M", installed: []installed{{"bare", "", "", "manifestless"}, {"disabled", "apps", "128M", "disabled"}}, want: "; warning: ikigenba-apps.slice memory_max adds up to 384M, more than twice its MemoryMax 128M"},
		{name: "not installed excluded", memory: "128M", suite: "256M", apps: "128M", installed: []installed{{"absent", "apps", "512M", "absent"}, {"directory", "apps", "512M", "directory"}, {"link", "apps", "512M", "symlink"}, {"_invalid", "apps", "512M", "invalid"}}},
		{name: "core only suite warning", slice: "core", memory: "128M", suite: "128M", apps: "128M", installed: []installed{{"other", "apps", "128M", ""}}, want: "; warning: ikigenba.slice memory_max adds up to 384M, more than twice its MemoryMax 128M"},
		{name: "both warnings order and K", memory: "1000", suite: "1024", apps: "1000", installed: []installed{{"other", "apps", "2072", ""}}, want: "; warning: ikigenba-apps.slice memory_max adds up to 3K, more than twice its MemoryMax 1000; warning: ikigenba.slice memory_max adds up to 131075K, more than twice its MemoryMax 1K"},
		{name: "exact suite boundary", memory: "128M", suite: "192M", apps: "128M", installed: []installed{{"other", "core", "128M", ""}}},
		{name: "overflow safe", memory: "9223372036854775807", suite: "9223372036854775807", apps: "9223372036854775807", installed: []installed{{"other", "apps", "9223372036854775807", ""}, {"third", "apps", "9223372036854775807", ""}}, want: "; warning: ikigenba-apps.slice memory_max adds up to 27670116110564327421, more than twice its MemoryMax 9223372036854775807; warning: ikigenba.slice memory_max adds up to 27670116110698545149, more than twice its MemoryMax 9223372036854775807"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCompletedInstallFixture(t, t.TempDir(), false)
			if test.slice == "" {
				test.slice = "apps"
			}
			fixture.archive = validInstallTar(t, "app='notes'\n[resources]\nslice='"+test.slice+"'\nmemory_max='"+test.memory+"'\n")
			writeFixture(t, filepath.Join(fixture.root, "etc/systemd/system/ikigenba.slice"), []byte("MemoryMax="+test.suite+"\n"), 0o644)
			writeFixture(t, filepath.Join(fixture.root, "etc/systemd/system/ikigenba-apps.slice"), []byte("MemoryMax="+test.apps+"\n"), 0o644)
			for _, existing := range test.installed {
				manifest := "app='" + existing.name + "'\n[resources]\nslice='" + existing.slice + "'\nmemory_max='" + existing.memory + "'\n"
				if existing.kind == "invalid" {
					manifest = "[resources]\nmemory_max='" + existing.memory + "'\n"
				}
				if existing.kind == "manifestless" {
					if err := os.MkdirAll(filepath.Join(fixture.root, "opt", existing.name, "etc"), 0o750); err != nil {
						t.Fatal(err)
					}
				}
				if existing.kind != "manifestless" {
					writeFixture(t, filepath.Join(fixture.root, "opt", existing.name, "etc/manifest.toml"), []byte(manifest), 0o644)
				}
				binary := filepath.Join(fixture.root, "opt", existing.name, "bin", existing.name)
				switch existing.kind {
				case "absent":
				case "directory":
					if err := os.MkdirAll(binary, 0o750); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					writeFixture(t, binary+"-target", []byte("binary"), 0o755)
					if err := os.Symlink(existing.name+"-target", binary); err != nil {
						t.Fatal(err)
					}
				default:
					writeFixture(t, binary, []byte("binary"), 0o755)
				}
				if existing.kind == "disabled" {
					fixture.disabled = true
					writeFixture(t, filepath.Join(fixture.root, "etc/systemd/system", "ikigenba-"+existing.name+".socket"), []byte("disabled socket"), 0o644)
				}
			}
			for range 2 {
				fixture.reports = nil
				if err := fixture.run(); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, report := range fixture.reports {
					if report.step == "unit" {
						found = true
						if !report.success || report.detail != "ikigenba-notes.socket, ikigenba-notes.service"+test.want {
							t.Fatalf("unit outcome %#v want warning %q", report, test.want)
						}
					}
				}
				if !found || fixture.reports[len(fixture.reports)-1].step != "service" || !fixture.reports[len(fixture.reports)-1].success {
					t.Fatalf("install did not complete %#v", fixture.reports)
				}
			}
		})
	}
}

// R-ZRW9-N6TD R-3GF7-5PQ9
func TestInstallSliceReadFailureAndReportFailureRetainCauses(t *testing.T) {
	fixture := newCompletedInstallFixture(t, t.TempDir(), false)
	unit := filepath.Join(fixture.root, "etc/systemd/system/ikigenba.slice")
	if err := os.Remove(unit); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(unit, 0o750); err != nil {
		t.Fatal(err)
	}
	reportFailure := errors.New("file report unavailable")
	fixture.reportFailureStage = "file"
	fixture.reportFailure = reportFailure
	err := fixture.run()
	var failure *apps.InstallError
	if !errors.As(err, &failure) || failure.Code != 1 || !errors.Is(failure.Cause, reportFailure) {
		t.Fatalf("failure %#v", err)
	}
	var pathError *os.PathError
	if !errors.As(failure.Cause, &pathError) {
		t.Fatalf("slice read cause missing: %v", failure.Cause)
	}
	if len(fixture.reports) != 2 || fixture.reports[1].step != "file" || fixture.reports[1].success || !strings.HasPrefix(fixture.reports[1].detail, "/etc/systemd/system/ikigenba.slice:") || fixture.configureCalls != 0 {
		t.Fatalf("reports %#v configure %d", fixture.reports, fixture.configureCalls)
	}
}
