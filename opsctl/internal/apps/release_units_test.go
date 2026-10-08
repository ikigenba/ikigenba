package apps_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestReleaseUnitsExactAtomicFiles(t *testing.T) {
	// R-A9FQ-DEEM R-AJ6X-FKC6 R-ALMQ-73TK R-AMUM-KVK9 R-AO2I-YNAY
	for _, core := range []bool{false, true} {
		t.Run(fmt.Sprint(core), func(t *testing.T) {
			root := t.TempDir()
			env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
				t.Fatal("unit writer executed command")
				return host.Result{}, nil
			}}
			m, err := apps.ParseManifest([]byte("app=\"notes\"\n"))
			if err != nil {
				t.Fatal(err)
			}
			lines := "Slice=ikigenba-apps.slice\nCPUWeight=100\nMemoryMax=134217728\nEnvironment=GOMEMLIMIT=100663296\n"
			if core {
				m.Resources = apps.Resources{Slice: "core", CPUWeight: 200, MemoryMax: 268435456, GoMemoryLimit: 201326592, Delegate: true, OOMPolicy: "continue"}
				lines = "Slice=ikigenba-core.slice\nCPUWeight=200\nMemoryMax=268435456\nMemoryLow=32M\nEnvironment=GOMEMLIMIT=201326592\nDelegate=yes\nOOMPolicy=continue\n"
			}
			before := snapshotTree(t, root)
			if apps.WriteReleaseUnits(env, "bad/name", m, apps.Timeouts{}) == nil || !reflect.DeepEqual(before, snapshotTree(t, root)) {
				t.Fatal("invalid app touched files")
			}
			if err := apps.WriteReleaseUnits(env, "notes", m, apps.Timeouts{StopSeconds: 13}); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "etc/systemd/system")
			socket := "[Unit]\nDescription=Ikigenba notes socket\n\n[Socket]\nListenStream=" + filepath.Join(root, "run/ikigenba/notes.sock") + "\nSocketUser=ikigenba\nSocketGroup=nginx\nSocketMode=0660\nRemoveOnStop=yes\nBacklog=4096\n\n[Install]\nWantedBy=sockets.target\n"
			service := "[Unit]\nDescription=Ikigenba notes app\nRequires=ikigenba-notes.socket\nAfter=ikigenba-notes.socket ikigenba-services.service\nWants=ikigenba-services.service\n\n[Service]\nType=notify\nExecStart=" + filepath.Join(root, "opt/ikigenba/current/notes/bin/notes") + "\nWorkingDirectory=" + filepath.Join(root, "var/opt/ikigenba/notes") + "\nEnvironmentFile=" + filepath.Join(root, "etc/opt/ikigenba/notes/env") + "\nUser=ikigenba\nRuntimeDirectory=ikigenba/notes\nPrivateTmp=yes\nRestart=on-failure\nTimeoutStopSec=13\n" + lines + "\n[Install]\nWantedBy=multi-user.target\n"
			assertFile(t, filepath.Join(dir, "ikigenba-notes.socket"), socket)
			assertFile(t, filepath.Join(dir, "ikigenba-notes.service"), service)
			old, err := os.Lstat(filepath.Join(dir, "ikigenba-notes.service"))
			if err != nil {
				t.Fatal(err)
			}
			if err := apps.WriteReleaseUnits(env, "notes", m, apps.Timeouts{StopSeconds: 13}); err != nil {
				t.Fatal(err)
			}
			next, err := os.Lstat(filepath.Join(dir, "ikigenba-notes.service"))
			if err != nil || os.SameFile(old, next) {
				t.Fatal("not replaced atomically")
			}
			if apps.ServicesUnit != "ikigenba-services.service" {
				t.Fatal(apps.ServicesUnit)
			}
			bootPath := filepath.Join(dir, apps.ServicesUnit)
			writeFixture(t, bootPath, []byte("stale"), 0600)
			bootOld, err := os.Lstat(bootPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := apps.WriteServicesUnit(env); err != nil {
				t.Fatal(err)
			}
			bootNew, err := os.Lstat(bootPath)
			if err != nil || os.SameFile(bootOld, bootNew) {
				t.Fatal("boot unit not replaced atomically")
			}
			want := "[Unit]\nDescription=Ikigenba services file\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=" + filepath.Join(root, "usr/local/bin/opsctl") + " services apply\n\n[Install]\nWantedBy=multi-user.target\n"
			assertFile(t, bootPath, want)
			assertMode(t, dir, 0755)
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 3 {
				t.Fatalf("unexpected files %v %v", entries, err)
			}
			for _, e := range entries {
				assertMode(t, filepath.Join(dir, e.Name()), 0644)
				if strings.HasPrefix(e.Name(), ".opsctl") {
					t.Fatal("temporary remains")
				}
			}
			for _, p := range []string{"opt", "run", "var"} {
				if _, err := os.Lstat(filepath.Join(root, p)); !os.IsNotExist(err) {
					t.Fatalf("extra path %s %v", p, err)
				}
			}
		})
	}
}

func TestServicesUnitCreatesItsOwnDirectoryWithoutCommands(t *testing.T) {
	// R-AMUM-KVK9 R-AO2I-YNAY
	root := t.TempDir()
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("unit writer executed command")
		return host.Result{}, nil
	}}
	if err := apps.WriteServicesUnit(env); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "etc/systemd/system")
	assertMode(t, directory, 0755)
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != apps.ServicesUnit {
		t.Fatalf("files %v %v", entries, err)
	}
	assertMode(t, filepath.Join(directory, apps.ServicesUnit), 0644)
}

func TestReleaseUnitsUseDirectManifestResourcesWithoutDefaults(t *testing.T) {
	// R-ALMQ-73TK
	root := t.TempDir()
	m := apps.Manifest{Resources: apps.Resources{CPUWeight: 37, MemoryMax: 256, GoMemoryLimit: 192, Delegate: true, OOMPolicy: "continue"}}
	if err := apps.WriteReleaseUnits(host.Env{Root: root}, "notes", m, apps.Timeouts{StopSeconds: 11}); err != nil {
		t.Fatal(err)
	}
	want := "[Unit]\nDescription=Ikigenba notes app\nRequires=ikigenba-notes.socket\nAfter=ikigenba-notes.socket ikigenba-services.service\nWants=ikigenba-services.service\n\n[Service]\nType=notify\nExecStart=" + filepath.Join(root, "opt/ikigenba/current/notes/bin/notes") + "\nWorkingDirectory=" + filepath.Join(root, "var/opt/ikigenba/notes") + "\nEnvironmentFile=" + filepath.Join(root, "etc/opt/ikigenba/notes/env") + "\nUser=ikigenba\nRuntimeDirectory=ikigenba/notes\nPrivateTmp=yes\nRestart=on-failure\nTimeoutStopSec=11\nSlice=ikigenba-.slice\nCPUWeight=37\nMemoryMax=256\nEnvironment=GOMEMLIMIT=192\nDelegate=yes\nOOMPolicy=continue\n\n[Install]\nWantedBy=multi-user.target\n"
	assertFile(t, filepath.Join(root, "etc/systemd/system/ikigenba-notes.service"), want)
}
