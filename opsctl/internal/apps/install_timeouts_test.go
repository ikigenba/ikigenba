package apps_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestSetupTimeoutsUpdatesRunningAppsAndRerunIsInert(t *testing.T) {
	// R-UUUF-JC2O R-UW2B-X3TD R-Y1GZ-HRRY
	root := t.TempDir()
	store := installStoreAt(t, root, map[string]string{
		"apps.drain_seconds": "7", "apps.stop_seconds": "19",
	})
	appRoot := filepath.Join(root, "opt", "notes")
	writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte("app = \"notes\"\n"), 0o640)
	writeFixture(t, filepath.Join(appRoot, "etc", "env"), []byte("TOKEN=\"value\"\nDRAIN_SECONDS=5\nMODE=\"prod\"\n"), 0o600)
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
	assertFile(t, filepath.Join(appRoot, "etc", "env"), "TOKEN=\"value\"\nDRAIN_SECONDS=7\nMODE=\"prod\"\n")
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
	beforeEnv, err := os.Stat(filepath.Join(appRoot, "etc", "env"))
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
	afterEnv, err := os.Stat(filepath.Join(appRoot, "etc", "env"))
	if err != nil {
		t.Fatal(err)
	}
	afterUnit, err := os.Stat(unit)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(beforeEnv, afterEnv) || !os.SameFile(beforeUnit, afterUnit) {
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
	if err != nil || !os.SameFile(beforeUnit, newUnit) {
		t.Fatalf("drain-only change rewrote unit: %v", err)
	}
}

func TestSetupTimeoutsUpdatesOnlyInstalledAppsInNameOrder(t *testing.T) {
	// R-UW2B-X3TD R-Y1GZ-HRRY R-UYI4-ONAR
	root := t.TempDir()
	store := installStoreAt(t, root, map[string]string{"apps.drain_seconds": "8", "apps.stop_seconds": "20"})
	for name, content := range map[string]string{
		"alpha": "TOKEN=one\n", // append when absent
		"idle":  "DRAIN_SECONDS=5\n",
		"zeta":  "DRAIN_SECONDS=5\nDRAIN_SECONDS=6\nMODE=prod\n", // remove stale duplicate
	} {
		appRoot := filepath.Join(root, "opt", name)
		writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte("app = \""+name+"\"\n"), 0o640)
		writeFixture(t, filepath.Join(appRoot, "etc", "env"), []byte(content), 0o640)
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
	assertFile(t, filepath.Join(root, "opt", "alpha", "etc", "env"), "TOKEN=one\nDRAIN_SECONDS=8\n")
	assertFile(t, filepath.Join(root, "opt", "zeta", "etc", "env"), "DRAIN_SECONDS=8\nMODE=prod\n")
	for _, name := range []string{"alpha", "idle", "zeta"} {
		info, err := os.Stat(filepath.Join(root, "opt", name, "etc", "env"))
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
	// R-UYI4-ONAR
	root := t.TempDir()
	store := installStoreAt(t, root, nil)
	appRoot := filepath.Join(root, "opt", "notes")
	writeFixture(t, filepath.Join(appRoot, "etc", "manifest.toml"), []byte("app = \"notes\"\n"), 0o640)
	writeFixture(t, filepath.Join(appRoot, "etc", "env"), []byte("DRAIN_SECONDS=1\n"), 0o600)
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
	assertFile(t, filepath.Join(appRoot, "etc", "env"), "DRAIN_SECONDS=5\n")
}

func TestSetupTimeoutsReturnsMissingEnvWithoutWritingUnit(t *testing.T) {
	// R-UYI4-ONAR
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
	writeFixture(t, filepath.Join(appRoot, "etc", "env"), []byte("DRAIN_SECONDS=5\n"), 0o600)
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
	assertFile(t, filepath.Join(appRoot, "etc", "env"), "DRAIN_SECONDS=6\n")
}

func TestSetupTimeoutsIgnoresServicesWithoutBinary(t *testing.T) {
	// R-UW2B-X3TD
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
