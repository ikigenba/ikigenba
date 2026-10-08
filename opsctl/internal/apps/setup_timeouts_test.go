package apps_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestSetupTimeoutsUpdatesRunningAppsAndRerunIsInert(t *testing.T) {
	// R-UUUF-JC2O  R-Y1GZ-HRRY
	root := t.TempDir()
	store := installStoreAt(t, root, map[string]string{
		"apps.drain_seconds": "7", "apps.stop_seconds": "19",
	})
	appRoot := filepath.Join(root, "opt", "notes")
	writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte("app = \"notes\"\n"), 0o640)
	writeFixture(t, filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env"), []byte("TOKEN=\"value\"\nDRAIN_SECONDS=5\nMODE=\"prod\"\n"), 0o600)
	writeFixture(t, filepath.Join(appRoot, "bin", "notes"), []byte("binary"), 0o750)
	unit := filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")
	writeFixture(t, unit, []byte("old service"), 0o644)
	var calls []commandCall
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		calls = append(calls, commandCall{command.Name, append([]string(nil), command.Args...)})
		if command.Name == "systemctl" && len(command.Args) > 0 {
			switch command.Args[0] {
			case "show":
				return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
			case "is-active":
				return host.Result{Stdout: []byte("active\n")}, nil
			}
		}
		return host.Result{}, nil
	}}
	if err := apps.SetupTimeouts(t.Context(), env, store); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env"), "TOKEN=\"value\"\nDRAIN_SECONDS=7\nMODE=\"prod\"\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n")
	unitData, err := readFixturePath(root, "etc/systemd/system/ikigenba-notes.service")
	if err != nil || !strings.Contains(string(unitData), "TimeoutStopSec=19\n") ||
		!strings.Contains(string(unitData), "Requires=ikigenba-notes.socket\n") {
		t.Fatalf("unit = %q, error = %v", unitData, err)
	}
	want := []commandCall{
		{"systemctl", []string{"daemon-reload"}},
		{"systemctl", []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}},
		{"systemctl", []string{"is-active", "ikigenba-notes.socket"}},
		{"systemctl", []string{"restart", "ikigenba-notes.service"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("commands = %#v, want %#v", calls, want)
	}
	setTimeoutFileTimes(t, filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env"), unit)
	beforeEnv, err := os.Stat(filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env"))
	if err != nil {
		t.Fatal(err)
	}
	beforeUnit, err := os.Stat(unit)
	if err != nil {
		t.Fatal(err)
	}
	calls = nil
	if err := apps.SetupTimeouts(t.Context(), env, store); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("unchanged rerun commands = %#v", calls)
	}
	afterEnv, err := os.Stat(filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env"))
	if err != nil {
		t.Fatal(err)
	}
	afterUnit, err := os.Stat(unit)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(beforeEnv, afterEnv) || !beforeEnv.ModTime().Equal(afterEnv.ModTime()) || !os.SameFile(beforeUnit, afterUnit) || !beforeUnit.ModTime().Equal(afterUnit.ModTime()) {
		t.Fatal("unchanged files were rewritten")
	}
	if err := store.Set("apps.drain_seconds", "8"); err != nil {
		t.Fatal(err)
	}
	calls = nil
	if err := apps.SetupTimeouts(t.Context(), env, store); err != nil {
		t.Fatal(err)
	}
	want = []commandCall{
		{"systemctl", []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}},
		{"systemctl", []string{"is-active", "ikigenba-notes.socket"}},
		{"systemctl", []string{"restart", "ikigenba-notes.service"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("drain-only commands = %#v, want %#v", calls, want)
	}
	newUnit, err := os.Stat(unit)
	if err != nil || !os.SameFile(beforeUnit, newUnit) || !beforeUnit.ModTime().Equal(newUnit.ModTime()) {
		t.Fatalf("drain-only change rewrote unit: %v", err)
	}
}

func TestSetupTimeoutsUpdatesOnlyInstalledAppsInNameOrder(t *testing.T) {
	// R-CM1B-RL1J
	//  R-Y1GZ-HRRY
	root := t.TempDir()
	store := installStoreAt(t, root, map[string]string{"apps.drain_seconds": "8", "apps.stop_seconds": "20"})
	for name, content := range map[string]string{
		"alpha": "TOKEN=one\n", // append when absent
		"idle":  "DRAIN_SECONDS=5\n",
		"zeta":  "DRAIN_SECONDS=5\nDRAIN_SECONDS=6\nMODE=prod\n", // remove stale duplicate
	} {
		appRoot := filepath.Join(root, "opt", name)
		writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte("app = \""+name+"\"\n"), 0o640)
		writeFixture(t, filepath.Join(root, "etc", "opt", "ikigenba", name, "env"), []byte(content), 0o640)
		writeFixture(t, filepath.Join(appRoot, "bin", name), []byte("binary"), 0o750)
		writeFixture(t, filepath.Join(appRoot, "state", "keep"), []byte("state"), 0o600)
		writeFixture(t, filepath.Join(appRoot, "cache", "keep"), []byte("cache"), 0o600)
		writeFixture(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-"+name+".socket"), []byte("socket sentinel"), 0o644)
	}
	var calls []commandCall
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		calls = append(calls, commandCall{command.Name, append([]string(nil), command.Args...)})
		if len(command.Args) > 0 {
			switch command.Args[0] {
			case "show":
				return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
			case "is-active":
				if command.Args[1] == "ikigenba-idle.socket" {
					return host.Result{ExitCode: 3, Stdout: []byte("inactive\n")}, nil
				}
				return host.Result{Stdout: []byte("active\n")}, nil
			}
		}
		return host.Result{}, nil
	}}
	if err := apps.SetupTimeouts(t.Context(), env, store); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "etc", "opt", "ikigenba", "alpha", "env"), "TOKEN=one\nDRAIN_SECONDS=8\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n")
	assertFile(t, filepath.Join(root, "etc", "opt", "ikigenba", "zeta", "env"), "DRAIN_SECONDS=8\nMODE=prod\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n")
	for _, name := range []string{"alpha", "idle", "zeta"} {
		info, err := os.Stat(filepath.Join(root, "etc", "opt", "ikigenba", name, "env"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s env mode = %v", name, info.Mode())
		}
		assertFile(t, filepath.Join(root, "opt", name, "state", "keep"), "state")
		assertFile(t, filepath.Join(root, "opt", name, "cache", "keep"), "cache")
		assertFile(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-"+name+".socket"), "socket sentinel")
	}
	var got []string
	for _, call := range calls {
		got = append(got, call.name+" "+strings.Join(call.args, " "))
	}
	want := []string{
		"systemctl daemon-reload",
		"systemctl show --property=LoadState --property=UnitFileState ikigenba-alpha.socket",
		"systemctl is-active ikigenba-alpha.socket",
		"systemctl restart ikigenba-alpha.service",
		"systemctl show --property=LoadState --property=UnitFileState ikigenba-idle.socket",
		"systemctl is-active ikigenba-idle.socket",
		"systemctl show --property=LoadState --property=UnitFileState ikigenba-zeta.socket",
		"systemctl is-active ikigenba-zeta.socket",
		"systemctl restart ikigenba-zeta.service",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func TestSetupTimeoutsStopsAfterReloadFailure(t *testing.T) {
	// R-ZI7A-VEAB
	//
	root := t.TempDir()
	store := installStoreAt(t, root, nil)
	appRoot := filepath.Join(root, "opt", "notes")
	writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte("app = \"notes\"\n"), 0o640)
	writeFixture(t, filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env"), []byte("DRAIN_SECONDS=1\n"), 0o600)
	writeFixture(t, filepath.Join(appRoot, "bin", "notes"), []byte("binary"), 0o750)
	transport := errors.New("systemd unavailable")
	calls := 0
	err := apps.SetupTimeouts(t.Context(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		calls++
		if command.Name != "systemctl" || !reflect.DeepEqual(command.Args, []string{"daemon-reload"}) {
			t.Fatalf("unexpected command after failure: %#v", command)
		}
		return host.Result{}, transport
	}}, store)
	var commandErr *host.CommandError
	if !errors.Is(err, transport) || !errors.As(err, &commandErr) || calls != 1 {
		t.Fatalf("failure = %v, calls = %d", err, calls)
	}
	assertFile(t, filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env"), "DRAIN_SECONDS=5\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n")
}

func TestSetupTimeoutsReturnsMissingEnvWithoutWritingUnit(t *testing.T) {
	// R-CM1B-RL1J
	//
	root := t.TempDir()
	store := installStoreAt(t, root, nil)
	appRoot := filepath.Join(root, "opt", "notes")
	writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte("app = \"notes\"\n"), 0o640)
	writeFixture(t, filepath.Join(appRoot, "bin", "notes"), []byte("binary"), 0o750)
	err := apps.SetupTimeouts(t.Context(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("unexpected system command")
		return host.Result{}, nil
	}}, store)
	if err == nil || !strings.Contains(err.Error(), "notes") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")); !os.IsNotExist(statErr) {
		t.Fatalf("unit was created: %v", statErr)
	}
}

func TestSetupTimeoutsLeavesDisabledAppInactive(t *testing.T) {
	// R-Y1GZ-HRRY
	root := t.TempDir()
	store := installStoreAt(t, root, map[string]string{"apps.drain_seconds": "6", "apps.stop_seconds": "12"})
	appRoot := filepath.Join(root, "opt", "notes")
	writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte("app = \"notes\"\n"), 0o640)
	writeFixture(t, filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env"), []byte("DRAIN_SECONDS=5\n"), 0o600)
	writeFixture(t, filepath.Join(appRoot, "bin", "notes"), []byte("binary"), 0o750)
	var calls []commandCall
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		calls = append(calls, commandCall{command.Name, append([]string(nil), command.Args...)})
		if command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] == "show" {
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
		}
		return host.Result{}, nil
	}}
	if err := apps.SetupTimeouts(t.Context(), env, store); err != nil {
		t.Fatal(err)
	}
	want := []commandCall{
		{"systemctl", []string{"daemon-reload"}},
		{"systemctl", []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("commands = %#v, want %#v", calls, want)
	}
	assertFile(t, filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env"), "DRAIN_SECONDS=6\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n")
}

func TestSetupTimeoutsIgnoresServicesWithoutBinary(t *testing.T) {
	//
	root := t.TempDir()
	store := installStoreAt(t, root, nil)
	state := filepath.Join(root, "opt", "notes", "state", "keep")
	writeFixture(t, state, []byte("persistent"), 0o600)
	if err := apps.SetupTimeouts(t.Context(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("system command for service without app binary")
		return host.Result{}, nil
	}}, store); err != nil {
		t.Fatal(err)
	}
	assertFile(t, state, "persistent")
	if _, err := os.Stat(filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")); !os.IsNotExist(err) {
		t.Fatalf("unit created for service without binary: %v", err)
	}
}

func TestSetupTimeoutsReplacesServicesEntryInPlaceAndKeepsUnitBytes(t *testing.T) {
	//
	tests := []struct{ name, before, after string }{
		{"replace both", "# header\nIKIGENBA_SERVICES=\"old\"\nKEEP='literal'\nDRAIN_SECONDS=0005\nTAIL=x", "# header\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\nKEEP='literal'\nDRAIN_SECONDS=5\nTAIL=x"},
		{"append drain", "IKIGENBA_SERVICES=old\nKEEP=x", "IKIGENBA_SERVICES=/var/lib/ikigenba/services.json\nKEEP=x\nDRAIN_SECONDS=5\n"},
		{"append both", "KEEP=x", "KEEP=x\nDRAIN_SECONDS=5\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n"},
		{"unchanged", "DRAIN_SECONDS=5\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n", "DRAIN_SECONDS=5\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, filepath.Join(root, "opt", "notes", "bin", "notes"), []byte("binary"), 0o755)
			writeFixture(t, filepath.Join(root, "opt", "notes", "etc", "manifest.toml"), []byte("app='notes'\n"), 0o644)
			writeFixture(t, filepath.Join(root, apps.EnvRoot, "notes", "env"), []byte("DRAIN_SECONDS=5\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n"), 0o600)
			writeFixture(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service"), []byte(expectedResourceService(root, 10, "Slice=ikigenba-apps.slice\nCPUWeight=100\nMemoryMax=134217728\nEnvironment=GOMEMLIMIT=100663296\n")), 0o644)
			envPath := filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env")
			writeFixture(t, envPath, []byte(test.before), 0o600)
			unitPath := filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")
			unitBytes, err := readFixturePath(root, "etc/systemd/system/ikigenba-notes.service")
			if err != nil {
				t.Fatal(err)
			}
			setTimeoutFileTimes(t, envPath, unitPath)
			beforeEnv, err := os.Stat(envPath)
			if err != nil {
				t.Fatal(err)
			}
			beforeUnit, err := os.Stat(unitPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := apps.SetupTimeouts(t.Context(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				if command.Args[0] != "show" {
					t.Fatalf("unexpected command: %#v", command)
				}
				return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
			}}, installStoreAt(t, root, nil)); err != nil {
				t.Fatal(err)
			}
			assertFile(t, envPath, test.after)
			assertMode(t, envPath, 0o600)
			assertFile(t, unitPath, string(unitBytes))
			afterUnit, err := os.Stat(unitPath)
			if err != nil || !os.SameFile(beforeUnit, afterUnit) || !beforeUnit.ModTime().Equal(afterUnit.ModTime()) {
				t.Fatalf("unchanged unit rewritten: %v", err)
			}
			if test.before == test.after {
				afterEnv, err := os.Stat(envPath)
				if err != nil || !os.SameFile(beforeEnv, afterEnv) || !beforeEnv.ModTime().Equal(afterEnv.ModTime()) {
					t.Fatalf("unchanged env rewritten: %v", err)
				}
			}
		})
	}
}

func TestSetupTimeoutsRejectsTimingBeforeWritingAndNonAppsStayUntouched(t *testing.T) {
	//
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "opt", "notes", "bin", "notes"), []byte("binary"), 0o755)
	writeFixture(t, filepath.Join(root, "opt", "notes", "etc", "manifest.toml"), []byte("app='notes'\n"), 0o644)
	writeFixture(t, filepath.Join(root, apps.EnvRoot, "notes", "env"), []byte("DRAIN_SECONDS=5\nIKIGENBA_SERVICES=/var/lib/ikigenba/services.json\n"), 0o600)
	writeFixture(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service"), []byte(expectedResourceService(root, 10, "Slice=ikigenba-apps.slice\nCPUWeight=100\nMemoryMax=134217728\nEnvironment=GOMEMLIMIT=100663296\n")), 0o644)
	envPath := filepath.Join(root, "etc", "opt", "ikigenba", "notes", "env")
	setTimeoutFileTimes(t, envPath)
	before, err := os.Stat(envPath)
	if err != nil {
		t.Fatal(err)
	}
	store := installStoreAt(t, root, map[string]string{"apps.drain_seconds": "bad"})
	env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("unexpected command")
		return host.Result{}, nil
	}}
	if err := apps.SetupTimeouts(t.Context(), env, store); err == nil {
		t.Fatal("invalid timing accepted")
	}
	after, err := os.Stat(envPath)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("timing error rewrote env: %v", err)
	}

	root = t.TempDir()
	for _, name := range []string{"host", "folder", "linked"} {
		writeFixture(t, filepath.Join(root, "etc", "opt", "ikigenba", name, "env"), []byte("KEEP=x\n"), 0o600)
		binary := filepath.Join(root, "opt", name, "bin", name)
		if name == "folder" {
			if err := os.MkdirAll(binary, 0o750); err != nil {
				t.Fatal(err)
			}
		} else {
			writeFixture(t, binary, []byte("binary"), 0o750)
			if name == "linked" {
				if err := os.Remove(binary); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "target"), binary); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	env.Root = root
	if err := apps.SetupTimeouts(t.Context(), env, installStoreAt(t, root, nil)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"host", "folder", "linked"} {
		assertFile(t, filepath.Join(root, "etc", "opt", "ikigenba", name, "env"), "KEEP=x\n")
		if _, err := os.Stat(filepath.Join(root, "etc", "systemd", "system", "ikigenba-"+name+".service")); !os.IsNotExist(err) {
			t.Fatalf("unit for %s: %v", name, err)
		}
	}
}

func setTimeoutFileTimes(t *testing.T, paths ...string) {
	t.Helper()
	past := time.Unix(946684800, 0)
	for _, name := range paths {
		if err := os.Chtimes(name, past, past); err != nil {
			t.Fatal(err)
		}
	}
}
