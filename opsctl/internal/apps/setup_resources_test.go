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
		"WorkingDirectory=" + filepath.Join(root, "var", "opt", "ikigenba", "notes") + "\nEnvironmentFile=" + filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env") + "\n" +
		"User=ikigenba\nRestart=on-failure\nTimeoutStopSec=" + strconv.FormatInt(stop, 10) + "\n" + resourceLines + "\n" +
		"[Install]\nWantedBy=multi-user.target\n"
}

func TestSetupTimeoutsPreservesAndCorrectsManifestResources(t *testing.T) {
	// R-CM1B-RL1J R-CZG7-Z276
	for _, manifest := range []string{
		"app = 'notes'\n[resources]\ncpu_weight = 100\nmemory_max = '512M'\nslice = 'core'\ndelegate = true\noom_policy = 'continue'\n",
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
			if err := os.MkdirAll(filepath.Join(appRoot, "etc"), 0o750); err != nil {
				t.Fatal(err)
			}
			envPath := filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env")
			writeFixture(t, envPath, []byte("# keep\nDRAIN_SECONDS=05\nKEEP='literal'\nIKIGENBA_SERVICES=old\nTAIL=x"), 0o640)
			unitPath := filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")
			writeFixture(t, unitPath, []byte(expectedResourceService(root, 10, "CPUWeight=999\nIOWeight=300\n")), 0o644)
			resourceLines := "Slice=ikigenba-apps.slice\nCPUWeight=100\nMemoryMax=134217728\nEnvironment=GOMEMLIMIT=100663296\n"
			if strings.Contains(manifest, "cpu_weight") {
				resourceLines = "Slice=ikigenba-core.slice\nCPUWeight=100\nMemoryMax=536870912\nMemoryLow=32M\nEnvironment=GOMEMLIMIT=402653184\nDelegate=yes\nOOMPolicy=continue\n"
			} else if strings.Contains(manifest, "memory_max") {
				resourceLines = "Slice=ikigenba-apps.slice\nCPUWeight=100\nMemoryMax=536870912\nEnvironment=GOMEMLIMIT=402653184\n"
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

func TestSetupTimeoutsRejectsInstalledManifestFailureBeforeAnyWrite(t *testing.T) {
	// R-CM1B-RL1J
	for _, manifest := range []string{"invalid = [", "app = 'other'", "app = 'zeta'\n[resources]\nio_weight = 50", "app = 'zeta'\n[resources]\nmemory_max = '512MB'"} {
		root := t.TempDir()
		store := installStoreAt(t, root, nil)
		for _, name := range []string{"alpha", "zeta"} {
			appRoot := filepath.Join(root, "opt", name)
			writeFixture(t, filepath.Join(appRoot, "bin", name), []byte("binary"), 0o750)
			writeFixture(t, filepath.Join(root, "etc", "opt", "ikigenba", name, "env"), []byte("DRAIN_SECONDS=1\n"), 0o600)
			writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte("app = '"+name+"'"), 0o640)
		}
		writeFixture(t, filepath.Join(root, "opt", "zeta", "etc", "manifest.toml"), []byte(manifest), 0o640)
		before := snapshotTree(t, root)
		err := apps.SetupTimeouts(t.Context(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
			t.Fatal("command after manifest error")
			return host.Result{}, nil
		}}, store)
		if _, parseErr := apps.ParseManifest([]byte(manifest)); parseErr != nil && (err == nil || err.Error() != "zeta: etc/manifest.toml: "+parseErr.Error()) {
			t.Fatalf("manifest error = %v; want exact parser reason %v", err, parseErr)
		}
		if err == nil || !strings.Contains(err.Error(), "zeta") {
			t.Fatalf("manifest failure = %v; want zeta identified", err)
		}
		if after := snapshotTree(t, root); !reflect.DeepEqual(before, after) {
			t.Fatal("manifest failure changed files")
		}
	}
}

func TestSetupTimeoutsRejectsUnreadableEnvironment(t *testing.T) {
	// R-CM1B-RL1J
	root := t.TempDir()
	store := installStoreAt(t, root, nil)
	writeFixture(t, filepath.Join(root, "opt", "notes", "bin", "notes"), []byte("binary"), 0o750)
	if err := os.MkdirAll(filepath.Join(root, "opt", "notes", "etc"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env"), 0o750); err != nil {
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
