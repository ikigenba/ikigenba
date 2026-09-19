package apps_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestUninstallAPISignatureAndCompleteDomainWorkflow(t *testing.T) {
	// R-LONI-RKQN R-X6R5-769H
	want := reflect.TypeFor[func(context.Context, host.Env, string, apps.UninstallHooks) error]()
	if got := reflect.TypeOf(apps.Uninstall); got != want {
		t.Fatalf("Uninstall type = %v, want %v", got, want)
	}

	fixture := newUninstallFixture(t, "active")
	var configured []apps.Manifest
	fixture.configure = func(_ context.Context, manifest apps.Manifest) error {
		configured = append(configured, manifest)
		return nil
	}
	if err := fixture.uninstall(); err != nil {
		t.Fatal(err)
	}
	wantCommands := []host.Command{
		{Name: "systemctl", Args: []string{"is-active", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"stop", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"disable", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"daemon-reload"}},
	}
	if !reflect.DeepEqual(fixture.commands, wantCommands) || len(configured) != 1 || configured[0].App != "notes" || configured[0].Port != 8080 {
		t.Fatalf("commands = %#v, configured = %#v", fixture.commands, configured)
	}
}

func TestUninstallRejectsEveryMissingPrerequisiteBeforeEffects(t *testing.T) {
	// R-X4BC-FMS3
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *uninstallFixture)
	}{
		{name: "nil report", mutate: func(_ *testing.T, fixture *uninstallFixture) { fixture.report = nil }},
		{name: "nil configure", mutate: func(_ *testing.T, fixture *uninstallFixture) { fixture.configure = nil }},
		{name: "missing manifest", mutate: func(t *testing.T, fixture *uninstallFixture) {
			removeFixturePath(t, fixture.root, "opt/notes/etc/manifest.toml")
		}},
		{name: "invalid manifest", mutate: func(t *testing.T, fixture *uninstallFixture) {
			writeFixturePath(t, fixture.root, "opt/notes/etc/manifest.toml", "app = [\n")
		}},
		{name: "mismatched manifest", mutate: func(t *testing.T, fixture *uninstallFixture) {
			writeFixturePath(t, fixture.root, "opt/notes/etc/manifest.toml", "app = \"other\"\nport = 8080\n")
		}},
		{name: "missing unit", mutate: func(t *testing.T, fixture *uninstallFixture) {
			removeFixturePath(t, fixture.root, "etc/systemd/system/ikigenba-notes.service")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newUninstallFixture(t, "active")
			test.mutate(t, fixture)
			before := snapshotUninstallTree(t, fixture.root)
			err := fixture.uninstall()
			var failure *apps.LifecycleError
			wantReports := 1
			if test.name == "nil report" || test.name == "nil configure" {
				wantReports = 0
			}
			if !errors.As(err, &failure) || len(fixture.commands) != 0 || len(fixture.reports) != wantReports ||
				wantReports == 1 && (fixture.reports[0].step != "stop" || fixture.reports[0].success) || fixture.configureCalls != 0 {
				t.Fatalf("Uninstall = %#v, commands = %#v, reports = %#v, configure calls = %d", err, fixture.commands, fixture.reports, fixture.configureCalls)
			}
			if after := snapshotUninstallTree(t, fixture.root); !reflect.DeepEqual(after, before) {
				t.Fatalf("host tree mutated:\nbefore %#v\nafter  %#v", before, after)
			}
		})
	}
}

func TestUninstallActionAndReportFailuresAreJoined(t *testing.T) {
	// R-GWME-QK2L
	// R-ETFY-8BTG R-EVVQ-ZVAU R-EX3N-DN1J
	fixture := newUninstallFixture(t, "active")
	actionErr := errors.New("stop transport failed")
	reportErr := errors.New("report write failed")
	fixture.report = func(step, _ string, success bool) error {
		if step != "stop" || success {
			t.Fatalf("outcome = %s success=%t, want failed stop", step, success)
		}
		return reportErr
	}
	err := apps.Uninstall(context.Background(), host.Env{Root: fixture.root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}) {
			return host.Result{Stdout: []byte("active\n")}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"stop", "ikigenba-notes.service"}) {
			return host.Result{}, actionErr
		}
		t.Fatalf("unexpected command %#v", command)
		return host.Result{}, nil
	}}, "notes", apps.UninstallHooks{Report: fixture.report, Configure: fixture.configure})
	var commandErr *host.CommandError
	if !errors.Is(err, actionErr) || !errors.Is(err, reportErr) || !errors.As(err, &commandErr) ||
		commandErr.Label != "stop ikigenba-notes.service" {
		t.Fatalf("Uninstall error = %#v, command error = %#v", err, commandErr)
	}
}

func TestUninstallStopsOnlyActiveUnitsAndAlwaysDisables(t *testing.T) {
	// R-M22E-Z1WA
	for _, state := range []string{"active", "inactive", "failed"} {
		t.Run(state, func(t *testing.T) {
			fixture := newUninstallFixture(t, state)
			fixture.observe = func(command host.Command) {
				if command.Name != "systemctl" || len(command.Args) == 0 || command.Args[0] != "stop" && command.Args[0] != "disable" {
					return
				}
				for _, name := range []string{"opt/notes/bin/notes", "opt/notes/etc/manifest.toml", "etc/systemd/system/ikigenba-notes.service"} {
					if _, err := os.Lstat(filepath.Join(fixture.root, filepath.FromSlash(name))); err != nil {
						t.Fatalf("%s was removed before %s: %v", name, command.Args[0], err)
					}
				}
			}
			if err := fixture.uninstall(); err != nil {
				t.Fatal(err)
			}
			wantCommands := []host.Command{{Name: "systemctl", Args: []string{"is-active", "ikigenba-notes.service"}}}
			wantDetail := "ikigenba-notes.service already inactive, disabled"
			if state == "active" {
				wantCommands = append(wantCommands, host.Command{Name: "systemctl", Args: []string{"stop", "ikigenba-notes.service"}})
				wantDetail = "ikigenba-notes.service stopped, disabled"
			}
			wantCommands = append(wantCommands,
				host.Command{Name: "systemctl", Args: []string{"disable", "ikigenba-notes.service"}},
				host.Command{Name: "systemctl", Args: []string{"daemon-reload"}},
			)
			if !reflect.DeepEqual(fixture.commands, wantCommands) || fixture.reports[0] != (uninstallReport{"stop", wantDetail, true}) {
				t.Fatalf("commands = %#v, reports = %#v", fixture.commands, fixture.reports)
			}
		})
	}
}

func TestUninstallRemovesUnitSymlinkThenReloads(t *testing.T) {
	// R-M3AB-CTMZ
	fixture := newUninstallFixture(t, "inactive")
	unit := filepath.Join(fixture.root, "etc/systemd/system/ikigenba-notes.service")
	outside := filepath.Join(t.TempDir(), "outside.service")
	writeFixturePath(t, filepath.Dir(outside), filepath.Base(outside), "outside unchanged")
	if err := os.Remove(unit); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, unit); err != nil {
		t.Fatal(err)
	}
	fixture.observe = func(command host.Command) {
		if reflect.DeepEqual(command, host.Command{Name: "systemctl", Args: []string{"daemon-reload"}}) {
			if _, err := os.Lstat(unit); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unit existed at daemon-reload: %v", err)
			}
		}
	}

	if err := fixture.uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(unit); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unit still exists: %v", err)
	}
	data, err := readFixturePath(filepath.Dir(outside), filepath.Base(outside))
	if err != nil || string(data) != "outside unchanged" {
		t.Fatalf("outside unit changed: %q, %v", data, err)
	}
	wantReport := uninstallReport{"unit", "removed ikigenba-notes.service", true}
	if fixture.reports[1] != wantReport || !reflect.DeepEqual(fixture.commands[len(fixture.commands)-1], host.Command{Name: "systemctl", Args: []string{"daemon-reload"}}) {
		t.Fatalf("commands = %#v, reports = %#v", fixture.commands, fixture.reports)
	}
}

func TestUninstallRemovesOnlyAppPayloadWithoutFollowingSymlinks(t *testing.T) {
	// R-M4I7-QLDO
	fixture := newUninstallFixture(t, "inactive")
	stateFile := filepath.Join(fixture.root, "opt/notes/state/data.db")
	writeFixturePath(t, fixture.root, "opt/notes/state/data.db", "preserved state")
	stateTime := time.Unix(1_600_000_000, 0)
	if err := os.Chtimes(stateFile, stateTime, stateTime); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	writeFixturePath(t, outside, "sentinel", "outside unchanged")
	other := filepath.Join(fixture.root, "opt/other/share")
	writeFixturePath(t, other, "sentinel", "other unchanged")

	removeFixturePath(t, fixture.root, "opt/notes/share")
	removeFixturePath(t, fixture.root, "opt/notes/cache")
	if err := os.Symlink(outside, filepath.Join(fixture.root, "opt/notes/bin/escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(fixture.root, "opt/notes/share")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("state", filepath.Join(fixture.root, "opt/notes/cache")); err != nil {
		t.Fatal(err)
	}

	if err := fixture.uninstall(); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"bin", "etc", "share", "cache"} {
		if _, err := os.Lstat(filepath.Join(fixture.root, "opt/notes", directory)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists: %v", directory, err)
		}
	}
	stateInfo, err := os.Stat(stateFile)
	stateData, readErr := readFixturePath(fixture.root, "opt/notes/state/data.db")
	outsideData, outsideErr := readFixturePath(outside, "sentinel")
	otherData, otherErr := readFixturePath(other, "sentinel")
	if err != nil || readErr != nil || string(stateData) != "preserved state" || !stateInfo.ModTime().Equal(stateTime) ||
		outsideErr != nil || string(outsideData) != "outside unchanged" || otherErr != nil || string(otherData) != "other unchanged" {
		t.Fatalf("preserved data changed: state=%q (%v, %v), outside=%q (%v), other=%q (%v)", stateData, err, readErr, outsideData, outsideErr, otherData, otherErr)
	}
	want := uninstallReport{"files", "removed /opt/notes/bin, etc, share, cache; kept state", true}
	if fixture.reports[2] != want {
		t.Fatalf("reports = %#v", fixture.reports)
	}
}

type uninstallReport struct {
	step    string
	detail  string
	success bool
}

type uninstallFixture struct {
	root           string
	state          string
	commands       []host.Command
	reports        []uninstallReport
	report         func(string, string, bool) error
	configure      func(context.Context, apps.Manifest) error
	configureCalls int
	observe        func(host.Command)
}

func newUninstallFixture(t *testing.T, state string) *uninstallFixture {
	t.Helper()
	root := t.TempDir()
	writeFixturePath(t, root, "opt/notes/bin/notes", "binary")
	writeFixturePath(t, root, "opt/notes/etc/manifest.toml", "app = \"notes\"\nport = 8080\n")
	writeFixturePath(t, root, "opt/notes/share/asset", "asset")
	writeFixturePath(t, root, "opt/notes/cache/item", "cache")
	writeFixturePath(t, root, "etc/systemd/system/ikigenba-notes.service", "unit")
	fixture := &uninstallFixture{root: root, state: state}
	fixture.report = func(step, detail string, success bool) error {
		fixture.reports = append(fixture.reports, uninstallReport{step, detail, success})
		return nil
	}
	fixture.configure = func(context.Context, apps.Manifest) error { return nil }
	return fixture
}

func (fixture *uninstallFixture) uninstall() error {
	configure := fixture.configure
	if configure != nil {
		configure = func(ctx context.Context, manifest apps.Manifest) error {
			fixture.configureCalls++
			return fixture.configure(ctx, manifest)
		}
	}
	return apps.Uninstall(context.Background(), host.Env{Root: fixture.root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		fixture.commands = append(fixture.commands, command)
		if fixture.observe != nil {
			fixture.observe(command)
		}
		if reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}) {
			exitCode := 3
			if fixture.state == "active" {
				exitCode = 0
			}
			return host.Result{Stdout: []byte(fixture.state + "\n"), ExitCode: exitCode}, nil
		}
		return host.Result{}, nil
	}}, "notes", apps.UninstallHooks{
		Report:    fixture.report,
		Configure: configure,
	})
}

func writeFixturePath(t *testing.T, root, name, contents string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func removeFixturePath(t *testing.T, root, name string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(name))); err != nil {
		t.Fatal(err)
	}
}

func readFixturePath(root, name string) ([]byte, error) {
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = filesystem.Close() }()
	return filesystem.ReadFile(filepath.ToSlash(name))
}

func snapshotUninstallTree(t *testing.T, root string) map[string]string {
	t.Helper()
	got := make(map[string]string)
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = filesystem.Close() })
	err = filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			got[relative+"/"] = "directory"
			return nil
		}
		data, err := filesystem.ReadFile(filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		got[relative] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
