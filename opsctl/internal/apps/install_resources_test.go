package apps_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func expectedResourceService(root string, stop int64, resourceLines string) string {
	const app = "notes"
	appRoot := filepath.Join(root, "opt", app)
	return "[Unit]\nDescription=Ikigenba " + app + " app\nRequires=ikigenba-" + app + ".socket\nAfter=ikigenba-" + app + ".socket\n\n" +
		"[Service]\nType=notify\nExecStart=" + filepath.Join(appRoot, "bin", app) + "\n" +
		"WorkingDirectory=" + appRoot + "\nEnvironmentFile=" + filepath.Join(appRoot, "etc", "env") + "\n" +
		"User=ikigenba\nRestart=on-failure\nTimeoutStopSec=" + strconv.FormatInt(stop, 10) + "\n" + resourceLines + "\n" +
		"[Install]\nWantedBy=multi-user.target\n"
}

// R-81CI-RJCX R-83SB-J2UB
func TestInstallPublishesExactResourceUnitsOnEveryInstall(t *testing.T) {
	for _, test := range []struct{ name, table, lines string }{
		{"absent", "", ""},
		{"empty", "[resources]\n", ""},
		{"memory", "[resources]\nmemory_max = '512M'\n", "MemoryMax=536870912\n"},
		{"cpu", "[resources]\ncpu_weight = 1\n", "CPUWeight=1\n"},
		{"io", "[resources]\nio_weight = 10000\n", "IOWeight=10000\n"},
		{"all", "[resources]\nio_weight = 200\nmemory_max = '512M'\ncpu_weight = 100\n", "CPUWeight=100\nMemoryMax=536870912\nIOWeight=200\n"},
	} {
		for _, mode := range []string{"fresh", "installed", "disabled"} {
			t.Run(test.name+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				fixture := newCompletedInstallFixture(t, root, mode == "installed")
				fixture.disabled = mode == "disabled"
				fixture.archive = validInstallTar(t, "app = 'notes'\n"+test.table)
				installStoreAt(t, root, map[string]string{"apps.drain_seconds": "0007", "apps.stop_seconds": "00019"})
				servicePath := filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")
				socketPath := filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.socket")
				if mode != "fresh" {
					writeFixture(t, servicePath, []byte("stale service\nCPUWeight=999\nKillMode=process\n"), 0o644)
					writeFixture(t, socketPath, []byte("stale socket\nMemoryMax=1\n"), 0o644)
					writeFixture(t, filepath.Join(root, "opt", "notes", "etc", "manifest.toml"), []byte("app = 'notes'\n"), 0o640)
					writeFixture(t, filepath.Join(root, "opt", "notes", "bin", "notes"), []byte("binary"), 0o750)
				}
				if err := fixture.run(); err != nil {
					t.Fatal(err)
				}
				wantSocket := "[Unit]\nDescription=Ikigenba notes socket\n\n[Socket]\nListenStream=" + filepath.Join(root, "run", "ikigenba", "notes.sock") +
					"\nSocketUser=ikigenba\nSocketGroup=nginx\nSocketMode=0660\nRemoveOnStop=yes\nBacklog=4096\n\n[Install]\nWantedBy=sockets.target\n"
				assertFile(t, socketPath, wantSocket)
				assertFile(t, servicePath, expectedResourceService(root, 19, test.lines))
				// Allowlisting every published directive also rejects resource controls
				// outside the three optional limits and cgroup default overrides.
				for _, unit := range []struct {
					path    string
					allowed map[string]bool
				}{
					{servicePath, map[string]bool{"Description": true, "Requires": true, "After": true, "Type": true, "ExecStart": true, "WorkingDirectory": true, "EnvironmentFile": true, "User": true, "Restart": true, "TimeoutStopSec": true, "CPUWeight": true, "MemoryMax": true, "IOWeight": true, "WantedBy": true}},
					{socketPath, map[string]bool{"Description": true, "ListenStream": true, "SocketUser": true, "SocketGroup": true, "SocketMode": true, "RemoveOnStop": true, "Backlog": true, "WantedBy": true}},
				} {
					data, err := os.ReadFile(unit.path)
					if err != nil {
						t.Fatal(err)
					}
					for line := range strings.SplitSeq(string(data), "\n") {
						if key, _, directive := strings.Cut(line, "="); directive && !unit.allowed[key] {
							t.Errorf("unit contains forbidden directive %q", key)
						}
					}
				}
			})
		}
	}
}

// R-82KF-5B3M
func TestSetupTimeoutsPreservesAndCorrectsManifestResources(t *testing.T) {
	for _, manifest := range []string{
		"app = 'notes'\n[resources]\ncpu_weight = 100\nmemory_max = '512M'\nio_weight = 200\n",
		"app = 'notes'\n[resources]\nmemory_max = '512M'\n",
		"app = 'notes'\n", "",
	} {
		t.Run(fmt.Sprintf("manifest %q", manifest), func(t *testing.T) {
			root := t.TempDir()
			appRoot := filepath.Join(root, "opt", "notes")
			if manifest != "" {
				writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte(manifest), 0o640)
			}
			writeFixture(t, filepath.Join(appRoot, "bin", "notes"), []byte("binary"), 0o750)
			envPath := filepath.Join(appRoot, "etc", "env")
			writeFixture(t, envPath, []byte("# keep\nDRAIN_SECONDS=05\nKEEP='literal'\nIKIGENBA_SERVICES=old\nTAIL=x"), 0o640)
			unitPath := filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")
			writeFixture(t, unitPath, []byte(expectedResourceService(root, 10, "CPUWeight=999\nIOWeight=300\n")), 0o644)
			resourceLines := ""
			if strings.Contains(manifest, "cpu_weight") {
				resourceLines = "CPUWeight=100\nMemoryMax=536870912\nIOWeight=200\n"
			} else if strings.Contains(manifest, "memory_max") {
				resourceLines = "MemoryMax=536870912\n"
			}
			store := installStoreAt(t, root, map[string]string{"apps.drain_seconds": "0008", "apps.stop_seconds": "00020"})
			env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				if command.Args[0] == "show" {
					return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
				}
				return host.Result{}, nil
			}}
			if err := apps.SetupTimeouts(t.Context(), env, store); err != nil {
				t.Fatal(err)
			}
			assertFile(t, envPath, "# keep\nDRAIN_SECONDS=8\nKEEP='literal'\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\nTAIL=x")
			assertMode(t, envPath, 0o600)
			assertFile(t, unitPath, expectedResourceService(root, 20, resourceLines))
			setTimeoutFileTimes(t, envPath, unitPath)
			beforeEnv, err := os.Stat(envPath)
			if err != nil {
				t.Fatal(err)
			}
			beforeUnit, err := os.Stat(unitPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := apps.SetupTimeouts(t.Context(), env, store); err != nil {
				t.Fatal(err)
			}
			afterEnv, err := os.Stat(envPath)
			if err != nil || !os.SameFile(beforeEnv, afterEnv) || !beforeEnv.ModTime().Equal(afterEnv.ModTime()) {
				t.Fatalf("unchanged env rewritten: %v", err)
			}
			afterUnit, err := os.Stat(unitPath)
			if err != nil || !os.SameFile(beforeUnit, afterUnit) || !beforeUnit.ModTime().Equal(afterUnit.ModTime()) {
				t.Fatalf("unchanged resource unit rewritten: %v", err)
			}
			if err := store.Set("apps.stop_seconds", "21"); err != nil {
				t.Fatal(err)
			}
			if err := apps.SetupTimeouts(t.Context(), env, store); err != nil {
				t.Fatal(err)
			}
			assertFile(t, unitPath, expectedResourceService(root, 21, resourceLines))
		})
	}
}

// R-82KF-5B3M
func TestSetupTimeoutsRejectsInstalledManifestFailureBeforeAnyWrite(t *testing.T) {
	for _, manifest := range []string{"invalid = [", "app = 'other'", "app = 'zeta'\n[resources]\nmemory_max = '512MB'"} {
		root := t.TempDir()
		store := installStoreAt(t, root, nil)
		for _, name := range []string{"alpha", "zeta"} {
			appRoot := filepath.Join(root, "opt", name)
			writeFixture(t, filepath.Join(appRoot, "bin", name), []byte("binary"), 0o750)
			writeFixture(t, filepath.Join(appRoot, "etc", "env"), []byte("DRAIN_SECONDS=1\n"), 0o600)
			writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte("app = '"+name+"'"), 0o640)
		}
		writeFixture(t, filepath.Join(root, "opt", "zeta", "etc", "manifest.toml"), []byte(manifest), 0o640)
		before := snapshotTree(t, root)
		err := apps.SetupTimeouts(t.Context(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
			t.Fatal("command after manifest error")
			return host.Result{}, nil
		}}, store)
		if err == nil || !strings.Contains(err.Error(), "zeta") {
			t.Fatalf("manifest failure = %v; want zeta identified", err)
		}
		if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
			t.Fatal("manifest failure changed files")
		}
	}
}

// R-82KF-5B3M
func TestSetupTimeoutsRejectsUnreadableEnvironment(t *testing.T) {
	root := t.TempDir()
	store := installStoreAt(t, root, nil)
	writeFixture(t, filepath.Join(root, "opt", "notes", "bin", "notes"), []byte("binary"), 0o750)
	if err := os.MkdirAll(filepath.Join(root, "opt", "notes", "etc", "env"), 0o750); err != nil {
		t.Fatal(err)
	}
	err := apps.SetupTimeouts(t.Context(), host.Env{Root: root}, store)
	if err == nil || !strings.Contains(err.Error(), "notes") {
		t.Fatalf("unreadable env failure = %v; want notes identified", err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")); !os.IsNotExist(err) {
		t.Fatalf("unit written with unreadable env: %v", err)
	}
}
