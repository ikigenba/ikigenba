package apps_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func lifecycleRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFixturePath(t, root, "opt/notes/bin/notes", "installed binary")
	writeFixturePath(t, root, "opt/notes/etc/manifest.toml", "app = \"notes\"\n")
	writeFixturePath(t, root, "etc/systemd/system/ikigenba-notes.socket", "socket unit")
	writeFixturePath(t, root, "etc/systemd/system/ikigenba-notes.service", "service unit")
	return root
}

type lifecycleModel struct {
	commands                      []host.Command
	reports                       []uninstallReport
	configure                     []apps.Manifest
	socketActive, serviceActive   bool
	socketEnabled, serviceEnabled bool
	version                       string
	failCommand                   string
}

func (model *lifecycleModel) execute(_ context.Context, command host.Command) (host.Result, error) {
	model.commands = append(model.commands, command)
	if reflect.DeepEqual(command.Args, []string{"start", "ikigenba-notes.service"}) {
		model.serviceActive = true
	}
	if reflect.DeepEqual(command.Args, []string{"start", "ikigenba-notes.socket"}) {
		model.socketActive = true
	}
	if reflect.DeepEqual(command.Args, []string{"stop", "ikigenba-notes.socket"}) {
		model.socketActive = false
	}
	if reflect.DeepEqual(command.Args, []string{"stop", "ikigenba-notes.service"}) {
		model.serviceActive = false
	}
	if reflect.DeepEqual(command.Args, []string{"enable", "ikigenba-notes.socket", "ikigenba-notes.service"}) {
		model.socketEnabled, model.serviceEnabled = true, true
	}
	if reflect.DeepEqual(command.Args, []string{"disable", "ikigenba-notes.socket", "ikigenba-notes.service"}) {
		model.socketEnabled, model.serviceEnabled = false, false
	}
	if command.Name+" "+joinArgs(command.Args) == model.failCommand {
		return host.Result{}, errors.New("process failed")
	}
	if command.Name == "systemctl" && len(command.Args) >= 3 && command.Args[0] == "show" {
		unit := command.Args[len(command.Args)-1]
		active, enabled := false, false
		if unit == "ikigenba-notes.socket" {
			active, enabled = model.socketActive, model.socketEnabled
		} else {
			active, enabled = model.serviceActive, model.serviceEnabled
		}
		activeState, fileState := "inactive", "disabled"
		if active {
			activeState = "active"
		}
		if enabled {
			fileState = "enabled"
		}
		return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=" + activeState + "\nUnitFileState=" + fileState + "\n")}, nil
	}
	if reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}) {
		if model.serviceActive {
			return host.Result{Stdout: []byte("active\n")}, nil
		}
		return host.Result{Stdout: []byte("inactive\n"), ExitCode: 3}, nil
	}
	if command.Name == "journalctl" {
		return host.Result{Stdout: []byte("failed startup\n")}, nil
	}
	if filepath.Base(command.Name) == "notes" && reflect.DeepEqual(command.Args, []string{"--version"}) {
		return host.Result{Stdout: []byte(model.version + "\r\n")}, nil
	}
	return host.Result{}, nil
}

func joinArgs(args []string) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		result += arg
	}
	return result
}
func (model *lifecycleModel) hooks() apps.LifecycleHooks {
	return apps.LifecycleHooks{
		Report: func(step, detail string, success bool) error {
			model.reports = append(model.reports, uninstallReport{step, detail, success})
			return nil
		},
		Configure: func(_ context.Context, manifest apps.Manifest) error {
			model.configure = append(model.configure, manifest)
			return nil
		},
	}
}

func TestLifecycleDomainAPIsAndNameValidation(t *testing.T) {
	// R-V71F-D1HM R-EMXS-NYLN
	for _, operation := range []func(context.Context, host.Env, string, apps.LifecycleHooks) error{apps.Disable, apps.Enable} {
		called := false
		err := operation(context.Background(), host.Env{Root: "/missing", Execute: func(context.Context, host.Command) (host.Result, error) { called = true; return host.Result{}, nil }}, "../bad", apps.LifecycleHooks{})
		var failure *apps.LifecycleError
		if !errors.As(err, &failure) || failure.Code != 2 || failure.Message != "'../bad' is not a usable app name" || called {
			t.Fatalf("invalid name: %v called=%t", err, called)
		}
	}
}

func TestDisabledReadsOnlySocketEnablement(t *testing.T) {
	// R-V89B-QT8B R-VU7I-MOKT
	for _, test := range []struct {
		load, file string
		want       bool
	}{{"loaded", "disabled", true}, {"loaded", "enabled", false}, {"not-found", "disabled", false}, {"", "", false}} {
		var commands []host.Command
		disabled, err := apps.Disabled(context.Background(), host.Env{Execute: func(_ context.Context, c host.Command) (host.Result, error) {
			commands = append(commands, c)
			return host.Result{Stdout: []byte("LoadState=" + test.load + "\nUnitFileState=" + test.file + "\n")}, nil
		}}, "notes")
		wantCommand := host.Command{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}}
		if err != nil || disabled != test.want || !reflect.DeepEqual(commands, []host.Command{wantCommand}) {
			t.Fatalf("Disabled=%t,%v commands=%#v", disabled, err, commands)
		}
	}
	for _, fault := range []struct {
		result host.Result
		err    error
	}{{err: errors.New("transport")}, {result: host.Result{ExitCode: 1}}} {
		_, err := apps.Disabled(context.Background(), host.Env{Execute: func(context.Context, host.Command) (host.Result, error) { return fault.result, fault.err }}, "notes")
		var commandErr *host.CommandError
		if !errors.As(err, &commandErr) {
			t.Fatalf("error did not wrap command: %v", err)
		}
	}
	called := false
	_, err := apps.Disabled(context.Background(), host.Env{Execute: func(context.Context, host.Command) (host.Result, error) { called = true; return host.Result{}, nil }}, "bad/name")
	if err == nil || called {
		t.Fatalf("invalid name reached host: %v", err)
	}
}

func TestDisableAuthRefusalPrecedesHostAccess(t *testing.T) {
	// R-F40E-0QZD
	called := false
	var reports []uninstallReport
	err := apps.Disable(context.Background(), host.Env{Root: "/missing", Execute: func(context.Context, host.Command) (host.Result, error) { called = true; return host.Result{}, nil }}, "auth", apps.LifecycleHooks{
		Report: func(step, detail string, success bool) error {
			reports = append(reports, uninstallReport{step, detail, success})
			return nil
		},
		Configure: func(context.Context, apps.Manifest) error { called = true; return nil },
	})
	var failure *apps.LifecycleError
	want := "auth is the authenticator and cannot be disabled"
	if !errors.As(err, &failure) || failure.Code != 1 || failure.Message != want || called || !reflect.DeepEqual(reports, []uninstallReport{{"stop", want, false}}) {
		t.Fatalf("refusal=%v reports=%#v called=%t", err, reports, called)
	}
}

func TestDisableOrderAndIdempotence(t *testing.T) {
	// R-HFWW-NIZS
	root := lifecycleRoot(t)
	before := readRestartTree(t, root)
	model := &lifecycleModel{socketActive: true, serviceActive: true, socketEnabled: true, serviceEnabled: true}
	hooks := model.hooks()
	err := apps.Disable(context.Background(), host.Env{Root: root, Execute: model.execute}, "notes", hooks)
	if err != nil {
		t.Fatal(err)
	}
	want := []host.Command{
		{Name: "systemctl", Args: []string{"show", "--property=ActiveState", "--property=UnitFileState", "ikigenba-notes.socket"}},
		{Name: "systemctl", Args: []string{"show", "--property=ActiveState", "--property=UnitFileState", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"stop", "ikigenba-notes.socket"}},
		{Name: "systemctl", Args: []string{"stop", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"disable", "ikigenba-notes.socket", "ikigenba-notes.service"}},
	}
	if !reflect.DeepEqual(model.commands, want) || !reflect.DeepEqual(model.reports, []uninstallReport{{"stop", "ikigenba-notes.socket, ikigenba-notes.service stopped, disabled", true}}) || len(model.configure) != 1 || model.configure[0].App != "notes" {
		t.Fatalf("commands=%#v reports=%#v configure=%#v", model.commands, model.reports, model.configure)
	}
	if after := readRestartTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatal("disable changed app or unit files")
	}
	model.commands = nil
	model.reports = nil
	err = apps.Disable(context.Background(), host.Env{Root: root, Execute: model.execute}, "notes", hooks)
	if err != nil || len(model.commands) != 2 || len(model.reports) != 1 || model.reports[0].detail != "ikigenba-notes.socket, ikigenba-notes.service already inactive, disabled" {
		t.Fatalf("idempotent disable: %v %#v %#v", err, model.commands, model.reports)
	}
}

func TestEnableOrderIdempotenceAndServiceReport(t *testing.T) {
	// R-XWLD-YOT6 R-F58A-EIQ2
	root := lifecycleRoot(t)
	before := readRestartTree(t, root)
	model := &lifecycleModel{version: "v9.2"}
	err := apps.Enable(context.Background(), host.Env{Root: root, Execute: model.execute}, "notes", model.hooks())
	if err != nil {
		t.Fatal(err)
	}
	want := []host.Command{
		{Name: "systemctl", Args: []string{"show", "--property=ActiveState", "--property=UnitFileState", "ikigenba-notes.socket"}},
		{Name: "systemctl", Args: []string{"show", "--property=ActiveState", "--property=UnitFileState", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"enable", "ikigenba-notes.socket", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"start", "ikigenba-notes.socket"}},
		{Name: "systemctl", Args: []string{"start", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"is-active", "ikigenba-notes.service"}},
		{Name: filepath.Join(root, "opt/notes/bin/notes"), Args: []string{"--version"}},
	}
	if !reflect.DeepEqual(model.commands, want) || len(model.configure) != 1 || !reflect.DeepEqual(model.reports, []uninstallReport{{"enable", "ikigenba-notes.socket, ikigenba-notes.service", true}, {"service", "notes v9.2 active", true}}) {
		t.Fatalf("enable: commands=%#v reports=%#v", model.commands, model.reports)
	}
	if after := readRestartTree(t, root); !reflect.DeepEqual(after, before) {
		t.Fatal("enable changed app or unit files")
	}
	model.commands = nil
	model.reports = nil
	err = apps.Enable(context.Background(), host.Env{Root: root, Execute: model.execute}, "notes", model.hooks())
	if err != nil || !reflect.DeepEqual(model.reports, []uninstallReport{{"enable", "ikigenba-notes.socket, ikigenba-notes.service already enabled", true}, {"service", "notes v9.2 active", true}}) {
		t.Fatalf("idempotent enable: %v %#v", err, model.reports)
	}
	for _, command := range model.commands {
		if command.Name == "systemctl" && slices.Contains([]string{"enable", "start", "stop", "disable"}, command.Args[0]) {
			t.Fatalf("idempotent enable controlled unit: %#v", command)
		}
	}
}

func TestStatusSocketFieldRetainsDisabledAndIndependentFacts(t *testing.T) {
	// R-F1KL-97HZ
	root := lifecycleRoot(t)
	model := &lifecycleModel{socketActive: true, serviceActive: false, version: "v3"}
	rows, err := apps.Status(context.Background(), host.Env{Root: root, Execute: model.execute})
	if err != nil || !reflect.DeepEqual(rows, []apps.StatusRow{{Name: "notes", Version: "v3", Label: "-", State: "inactive", Socket: "disabled", JournalMode: "-"}}) {
		t.Fatalf("Status=%#v,%v", rows, err)
	}
	wantSocket := host.Command{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=ActiveState", "--property=UnitFileState", "ikigenba-notes.socket"}}
	if !slices.ContainsFunc(model.commands, func(c host.Command) bool { return reflect.DeepEqual(c, wantSocket) }) {
		t.Fatalf("socket was not queried: %#v", model.commands)
	}
}

func TestUninstallRetainsStateOnlyDiscoveryWithoutCreatingState(t *testing.T) {
	// R-EO5P-1QCC
	for _, state := range []bool{false, true} {
		fixture := newUninstallFixture(t, "inactive")
		if state {
			writeFixturePath(t, fixture.root, "var/opt/ikigenba/notes/state/data.db", "preserved")
		}
		if err := fixture.uninstall(); err != nil {
			t.Fatal(err)
		}
		rows, err := apps.Status(context.Background(), host.Env{Root: fixture.root})
		if err != nil {
			t.Fatal(err)
		}
		if state {
			if !reflect.DeepEqual(rows, []apps.StatusRow{{Name: "notes", Version: "-", Label: "-", State: "-", Socket: "-", JournalMode: "-"}}) {
				t.Fatalf("state-only rows=%#v", rows)
			}
			data, readErr := os.ReadFile(filepath.Join(fixture.root, "var/opt/ikigenba/notes/state/data.db"))
			if readErr != nil || string(data) != "preserved" {
				t.Fatalf("state changed %q,%v", data, readErr)
			}
		} else if len(rows) != 0 {
			t.Fatalf("uninstall created discovered service: %#v", rows)
		}
	}
}

func TestAllLifecycleActionsRejectMissingAndUninstalledWithoutExecution(t *testing.T) {
	// R-EMXS-NYLN
	type action struct {
		name string
		run  func(context.Context, host.Env, string) error
	}
	actions := []action{
		{"uninstall", func(ctx context.Context, env host.Env, app string) error {
			return apps.Uninstall(ctx, env, app, apps.UninstallHooks{Report: func(string, string, bool) error { return nil }, Configure: func(context.Context, apps.Manifest) error { return nil }})
		}},
		{"restart", func(ctx context.Context, env host.Env, app string) error {
			_, err := apps.Restart(ctx, env, app)
			return err
		}},
		{"disable", func(ctx context.Context, env host.Env, app string) error {
			return apps.Disable(ctx, env, app, apps.LifecycleHooks{Report: func(string, string, bool) error { return nil }, Configure: func(context.Context, apps.Manifest) error { return nil }})
		}},
		{"enable", func(ctx context.Context, env host.Env, app string) error {
			return apps.Enable(ctx, env, app, apps.LifecycleHooks{Report: func(string, string, bool) error { return nil }, Configure: func(context.Context, apps.Manifest) error { return nil }})
		}},
	}
	for _, operation := range actions {
		for _, setup := range []struct {
			name, app, want string
			prepare         func(*testing.T, string)
		}{
			{name: "invalid", app: "bad/name", want: "'bad/name' is not a usable app name", prepare: func(*testing.T, string) {}},
			{name: "missing", app: "notes", want: "no service 'notes'", prepare: func(*testing.T, string) {}},
			{name: "data-only", app: "notes", want: "notes is not installed", prepare: func(t *testing.T, root string) {
				writeFixturePath(t, root, "var/opt/ikigenba/notes/state/item", "saved")
			}},
			{name: "old-state-only", app: "notes", want: "no service 'notes'", prepare: func(t *testing.T, root string) {
				writeFixturePath(t, root, "opt/notes/state/item", "old")
			}},
			{name: "no binary", app: "notes", want: "notes is not installed", prepare: func(t *testing.T, root string) {
				if err := os.MkdirAll(filepath.Join(root, "opt/notes/etc"), 0o750); err != nil {
					t.Fatal(err)
				}
			}},
		} {
			t.Run(operation.name+"/"+setup.name, func(t *testing.T) {
				root := t.TempDir()
				setup.prepare(t, root)
				before := readRestartTree(t, root)
				called := false
				err := operation.run(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) { called = true; return host.Result{}, nil }}, setup.app)
				var failure *apps.LifecycleError
				if !errors.As(err, &failure) || failure.Message != setup.want || called {
					t.Fatalf("error=%v called=%t", err, called)
				}
				if after := readRestartTree(t, root); !reflect.DeepEqual(before, after) {
					t.Fatal("preflight mutated files")
				}
			})
		}
	}
}

func TestEnableFailureReportsJournalAndLeavesEnabledUnits(t *testing.T) {
	// R-F58A-EIQ2
	root := lifecycleRoot(t)
	model := &lifecycleModel{version: "v5", failCommand: "systemctl start ikigenba-notes.service"}
	err := apps.Enable(context.Background(), host.Env{Root: root, Execute: model.execute}, "notes", model.hooks())
	var failure *apps.LifecycleError
	var journal *host.CommandError
	if !errors.As(err, &failure) || failure.Code != 1 || failure.Message != "notes: service failed to start" || !errors.As(err, &journal) || string(journal.Result.Stdout) != "failed startup\n" {
		t.Fatalf("Enable error=%#v journal=%#v", err, journal)
	}
	if !model.socketEnabled || !model.serviceEnabled || !model.socketActive || len(model.configure) != 1 {
		t.Fatalf("state after failure=%#v", model)
	}
	if !reflect.DeepEqual(model.reports, []uninstallReport{{"enable", "ikigenba-notes.socket, ikigenba-notes.service", true}, {"service", "notes: service failed to start", false}}) {
		t.Fatalf("reports=%#v", model.reports)
	}
	if slices.ContainsFunc(model.commands, func(c host.Command) bool { return c.Name == "systemctl" && len(c.Args) > 0 && c.Args[0] == "disable" }) {
		t.Fatal("enable rolled back")
	}
}

func TestDisableAndEnableFailAtFirstStageForMissingUnit(t *testing.T) {
	// R-F40E-0QZD
	for _, operation := range []struct {
		name, step string
		run        func(context.Context, host.Env, string, apps.LifecycleHooks) error
	}{{"disable", "stop", apps.Disable}, {"enable", "enable", apps.Enable}} {
		t.Run(operation.name, func(t *testing.T) {
			root := lifecycleRoot(t)
			removeFixturePath(t, root, "etc/systemd/system/ikigenba-notes.socket")
			model := &lifecycleModel{}
			before := readRestartTree(t, root)
			err := operation.run(context.Background(), host.Env{Root: root, Execute: model.execute}, "notes", model.hooks())
			var failure *apps.LifecycleError
			if !errors.As(err, &failure) || failure.Message != "notes is not installed" || len(model.commands) != 0 || len(model.configure) != 0 || !reflect.DeepEqual(model.reports, []uninstallReport{{operation.step, "notes is not installed", false}}) {
				t.Fatalf("error=%v model=%#v", err, model)
			}
			if after := readRestartTree(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("missing unit preflight mutated files")
			}
		})
	}
}

func TestRestartDisabledKeepsBothUnitsDownAndReadsInstalledVersion(t *testing.T) {
	// R-EXWW-3W9W
	root := lifecycleRoot(t)
	model := &lifecycleModel{version: "v7.4", socketEnabled: false, serviceEnabled: false}
	before := readRestartTree(t, root)
	report, err := apps.Restart(context.Background(), host.Env{Root: root, Execute: model.execute}, "notes")
	if err != nil || report != (apps.ServiceReport{Name: "notes", Version: "v7.4", State: "disabled"}) {
		t.Fatalf("Restart=%#v,%v", report, err)
	}
	want := []host.Command{
		{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}},
		{Name: filepath.Join(root, "opt/notes/bin/notes"), Args: []string{"--version"}},
	}
	if !reflect.DeepEqual(model.commands, want) {
		t.Fatalf("disabled restart commands=%#v", model.commands)
	}
	if after := readRestartTree(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("disabled restart wrote files")
	}
}

func TestUninstallDataMovesBetweenStopAndUnitRemoval(t *testing.T) {
	// R-F7O3-627G R-FBBS-BDFJ
	for _, tc := range []struct {
		name                   string
		state, cache, newCache bool
		want                   string
	}{
		{"both", true, true, false, "/var/opt/ikigenba/notes; moved /opt/notes/state, /opt/notes/cache"},
		{"state", true, false, false, "/var/opt/ikigenba/notes; moved /opt/notes/state"},
		{"cache", false, true, false, "/var/opt/ikigenba/notes; moved /opt/notes/cache"},
		{"drop", false, true, true, "/var/opt/ikigenba/notes; dropped /opt/notes/cache"},
		{"state-drop", true, true, true, "/var/opt/ikigenba/notes; moved /opt/notes/state; dropped /opt/notes/cache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newUninstallFixture(t, "active")
			removeFixturePath(t, fixture.root, "var")
			if tc.state {
				writeFixturePath(t, fixture.root, "opt/notes/state/item", "saved")
			}
			if tc.cache {
				writeFixturePath(t, fixture.root, "opt/notes/cache/item", "old cache")
			}
			if tc.newCache {
				writeFixturePath(t, fixture.root, "var/opt/ikigenba/notes/cache/item", "new cache")
			}
			oldInfo, _ := os.Stat(filepath.Join(fixture.root, "opt/notes/state/item"))
			fixture.observe = func(command host.Command) {
				if len(fixture.reports) == 1 && fixture.reports[0].step == "stop" && command.Name == "systemctl" {
					t.Fatalf("systemctl executed in data stage: %#v", command)
				}
			}
			fixture.report = func(step, detail string, success bool) error {
				fixture.reports = append(fixture.reports, uninstallReport{step, detail, success})
				if step == "data" {
					for _, name := range []string{"opt/notes/bin/notes", "etc/systemd/system/ikigenba-notes.socket", "etc/systemd/system/ikigenba-notes.service"} {
						if _, err := os.Lstat(filepath.Join(fixture.root, name)); err != nil {
							t.Fatalf("removed before data completed: %s: %v", name, err)
						}
					}
					if tc.state {
						newInfo, err := os.Stat(filepath.Join(fixture.root, "var/opt/ikigenba/notes/state/item"))
						if err != nil || !os.SameFile(oldInfo, newInfo) {
							t.Fatalf("state was not renamed whole: %v", err)
						}
					}
				}
				return nil
			}
			if err := fixture.uninstall(); err != nil {
				t.Fatal(err)
			}
			if len(fixture.reports) != 4 || fixture.reports[1] != (uninstallReport{"data", tc.want, true}) || fixture.reports[2].step != "unit" || fixture.reports[3].step != "files" {
				t.Fatalf("reports = %#v", fixture.reports)
			}
			if tc.state {
				data, err := os.ReadFile(filepath.Join(fixture.root, "var/opt/ikigenba/notes/state/item"))
				if err != nil || string(data) != "saved" {
					t.Fatalf("retained state %q: %v", data, err)
				}
			}
			if _, err := os.Lstat(filepath.Join(fixture.root, "opt/notes")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("opt tree remains: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(fixture.root, "var/opt/ikigenba/notes/cache")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("cache remains: %v", err)
			}
			for _, command := range fixture.commands {
				if command.Name == "chown" && !reflect.DeepEqual(command.Args, []string{"ikigenba:ikigenba", filepath.Join(fixture.root, apps.DataRoot, "notes")}) {
					t.Fatalf("unrooted data owner command: %#v", command)
				}
			}
		})
	}
}

func TestUninstallDataConflictRetainsStoppedAppAndBothTrees(t *testing.T) {
	// R-F7O3-627G R-FCJO-P568
	fixture := newUninstallFixture(t, "active")
	writeFixturePath(t, fixture.root, "opt/notes/state/item", "old")
	writeFixturePath(t, fixture.root, "var/opt/ikigenba/notes/state/item", "new")
	writeFixturePath(t, fixture.root, "opt/notes/cache/item", "old cache")
	before := snapshotUninstallTree(t, fixture.root)
	err := fixture.uninstall()
	var failure *apps.LifecycleError
	want := []uninstallReport{
		{"stop", "ikigenba-notes.socket, ikigenba-notes.service stopped, disabled", true},
		{"data", "/opt/notes/state and /var/opt/ikigenba/notes/state both exist", false},
	}
	if !errors.As(err, &failure) || failure.Code != 1 || !reflect.DeepEqual(fixture.reports, want) || fixture.configureCalls != 0 {
		t.Fatalf("error %v, reports %#v", err, fixture.reports)
	}
	if after := snapshotUninstallTree(t, fixture.root); !reflect.DeepEqual(after, before) {
		t.Fatalf("refusal changed files: %#v", after)
	}
	if len(fixture.commands) != 5 {
		t.Fatalf("commands past stop: %#v", fixture.commands)
	}
	fixture.state = "inactive"
	fixture.reports, fixture.commands = nil, nil
	if err := fixture.uninstall(); err == nil || fixture.reports[0].detail != "ikigenba-notes.socket, ikigenba-notes.service already inactive, disabled" {
		t.Fatalf("retry = %v reports %#v", err, fixture.reports)
	}
}

func TestUninstallEmptyDataDoesNotCreateDataRoot(t *testing.T) {
	// R-F7O3-627G
	fixture := newUninstallFixture(t, "inactive")
	removeFixturePath(t, fixture.root, "var")
	if err := fixture.uninstall(); err != nil {
		t.Fatal(err)
	}
	if len(fixture.reports) != 3 || fixture.reports[1].step != "unit" {
		t.Fatalf("unexpected data outcome: %#v", fixture.reports)
	}
	if _, err := os.Lstat(filepath.Join(fixture.root, apps.DataRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("data root created: %v", err)
	}
}

func TestUninstallDataReportFailureStopsBeforeRemovingUnit(t *testing.T) {
	// R-FBBS-BDFJ R-FCJO-P568
	fixture := newUninstallFixture(t, "active")
	writeFixturePath(t, fixture.root, "opt/notes/state/item", "saved")
	failure := errors.New("data report unavailable")
	fixture.report = func(step, detail string, success bool) error {
		fixture.reports = append(fixture.reports, uninstallReport{step, detail, success})
		if step == "data" {
			return failure
		}
		return nil
	}
	if err := fixture.uninstall(); !errors.Is(err, failure) {
		t.Fatalf("error = %v", err)
	}
	if len(fixture.reports) != 2 || fixture.configureCalls != 0 {
		t.Fatalf("reports retried: %#v", fixture.reports)
	}
	for _, name := range []string{"opt/notes/bin/notes", "etc/systemd/system/ikigenba-notes.socket", "etc/systemd/system/ikigenba-notes.service", "var/opt/ikigenba/notes/state/item"} {
		if _, err := os.Lstat(filepath.Join(fixture.root, name)); err != nil {
			t.Fatalf("completed move or later unit lost: %s: %v", name, err)
		}
	}
}

func TestUninstallMigrationPreparesExistingDataDirectoryOnlyWhenMoving(t *testing.T) {
	// R-F7O3-627G
	for _, mode := range []string{"move", "drop", "unchanged"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newUninstallFixture(t, "inactive")
			directory := filepath.Join(fixture.root, apps.DataRoot, "notes")
			removeFixturePath(t, fixture.root, "var/opt/ikigenba/notes")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			writeFixturePath(t, fixture.root, "var/opt/ikigenba/notes/cache/item", "new cache")
			var original os.FileInfo
			if mode == "move" {
				writeFixturePath(t, fixture.root, "opt/notes/state/item", "preserved")
				if err := os.Chmod(filepath.Join(fixture.root, "opt/notes/state/item"), 0o600); err != nil {
					t.Fatal(err)
				}
				var err error
				original, err = os.Stat(filepath.Join(fixture.root, "opt/notes/state/item"))
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "drop" {
				writeFixturePath(t, fixture.root, "opt/notes/cache/item", "old cache")
			}
			if err := fixture.uninstall(); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(directory)
			if err != nil {
				t.Fatal(err)
			}
			var chowns []host.Command
			for _, command := range fixture.commands {
				if command.Name == "chown" {
					chowns = append(chowns, command)
				}
			}
			if mode == "move" {
				if info.Mode().Perm() != 0o700 || len(chowns) != 0 {
					t.Fatalf("data mode=%o chowns=%#v", info.Mode().Perm(), chowns)
				}
				moved, err := os.Stat(filepath.Join(directory, "state/item"))
				if err != nil || !os.SameFile(original, moved) || moved.Mode() != original.Mode() || !moved.ModTime().Equal(original.ModTime()) {
					t.Fatalf("moved metadata changed: %v", err)
				}
			} else if info.Mode().Perm() != 0o700 || len(chowns) != 0 {
				t.Fatalf("no move normalized data directory: mode=%o chowns=%#v", info.Mode().Perm(), chowns)
			}
		})
	}
}

func TestLifecycleRejectsSymlinkServiceParentsBeforeExecution(t *testing.T) {
	// R-EMXS-NYLN
	for _, parent := range []string{"opt", "var/opt/ikigenba"} {
		for _, action := range []string{"uninstall", "restart", "disable", "enable"} {
			t.Run(parent+"/"+action, func(t *testing.T) {
				root := t.TempDir()
				writeFixturePath(t, root, "opt/actual/bin/notes", "binary")
				writeFixturePath(t, root, "opt/actual/etc/manifest.toml", "app = \"notes\"\n")
				writeFixturePath(t, root, "opt/actual/state/item", "state")
				if err := os.MkdirAll(filepath.Join(root, parent), 0o750); err != nil {
					t.Fatal(err)
				}
				alias := filepath.Join(root, parent, "notes")
				if err := os.Symlink(filepath.Join(root, "opt/actual"), alias); err != nil {
					t.Fatal(err)
				}
				called := false
				env := host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) { called = true; return host.Result{}, nil }}
				hooks := apps.LifecycleHooks{Report: func(string, string, bool) error { return nil }, Configure: func(context.Context, apps.Manifest) error { called = true; return nil }}
				var err error
				switch action {
				case "restart":
					_, err = apps.Restart(context.Background(), env, "notes")
				case "uninstall":
					err = apps.Uninstall(context.Background(), env, "notes", apps.UninstallHooks(hooks))
				case "disable":
					err = apps.Disable(context.Background(), env, "notes", hooks)
				case "enable":
					err = apps.Enable(context.Background(), env, "notes", hooks)
				}
				var failure *apps.LifecycleError
				if !errors.As(err, &failure) || failure.Code != 1 || failure.Message != "no service 'notes'" || called {
					t.Fatalf("action = %v, executed=%t", err, called)
				}
				if target, err := os.Readlink(alias); err != nil || target != filepath.Join(root, "opt/actual") {
					t.Fatalf("alias changed: %q: %v", target, err)
				}
				if data, err := readFixturePath(root, "opt/actual/state/item"); err != nil || string(data) != "state" {
					t.Fatalf("target changed: %q: %v", data, err)
				}
			})
		}
	}
}

func TestLifecycleServiceCriterionAcceptsAlternateRealParent(t *testing.T) {
	// R-EMXS-NYLN
	for _, symlinkParent := range []string{"opt", "var/opt/ikigenba"} {
		root := t.TempDir()
		writeFixturePath(t, root, "actual/marker", "untouched")
		if err := os.MkdirAll(filepath.Join(root, symlinkParent), 0o750); err != nil {
			t.Fatal(err)
		}
		target := "../actual"
		if symlinkParent != "opt" {
			target = "../../../actual"
		}
		if err := os.Symlink(target, filepath.Join(root, symlinkParent, "notes")); err != nil {
			t.Fatal(err)
		}
		validMarker := "var/opt/ikigenba/notes/state/item"
		if symlinkParent != "opt" {
			validMarker = "opt/notes/etc/manifest.toml"
		}
		writeFixturePath(t, root, validMarker, "valid real marker")
		called := false
		_, err := apps.Restart(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) { called = true; return host.Result{}, nil }}, "notes")
		var failure *apps.LifecycleError
		if !errors.As(err, &failure) || failure.Message != "notes is not installed" || called {
			t.Fatalf("alternate real service = %v, executed=%t", err, called)
		}
	}
}
