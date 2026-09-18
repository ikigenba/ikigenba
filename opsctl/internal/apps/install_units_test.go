package apps_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInstallPublishesAndEnablesRootedAppUnit(t *testing.T) {
	// R-EKSI-SOZD R-EN8B-K8GR R-P2TO-03TP R-AMEV-4J2G
	root := t.TempDir()
	statePath := filepath.Join(root, "opt", "notes", "state", "db")
	cachePath := filepath.Join(root, "opt", "notes", "cache", "item")
	writeFixture(t, statePath, []byte("state"), 0o600)
	writeFixture(t, cachePath, []byte("cache"), 0o600)

	fixture := newCompletedInstallFixture(t, root, false)
	fixture.accountMissing = true
	err := fixture.run()
	if err != nil {
		t.Fatal(err)
	}

	appRoot := filepath.Join(root, "opt", "notes")
	wantUnit := "[Unit]\nDescription=Ikigenba notes app\n\n" +
		"[Service]\n" +
		"ExecStart=" + filepath.Join(appRoot, "bin", "notes") + "\n" +
		"WorkingDirectory=" + appRoot + "\n" +
		"EnvironmentFile=" + filepath.Join(appRoot, "etc", "env") + "\n" +
		"User=ikigenba\nRestart=on-failure\n\n" +
		"[Install]\nWantedBy=multi-user.target\n"
	assertFile(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service"), wantUnit)
	assertFile(t, statePath, "state")
	assertFile(t, cachePath, "cache")
	assertMode(t, appRoot, 0o750)
	assertMode(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service"), 0o644)
	assertMode(t, statePath, 0o600)
	assertMode(t, cachePath, 0o600)

	wantCommands := []commandCall{
		{"xz", []string{"--decompress", "--stdout"}},
		{"systemctl", []string{"is-active", "ikigenba-notes.service"}},
		{"id", []string{"--user", "ikigenba"}},
		{"useradd", []string{"--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "--user-group", "ikigenba"}},
		{"chown", []string{"ikigenba:ikigenba", appRoot}},
		{"chown", []string{"--recursive", "root:ikigenba", filepath.Join(appRoot, "bin"), filepath.Join(appRoot, "etc")}},
		{"systemctl", []string{"daemon-reload"}},
		{"systemctl", []string{"enable", "ikigenba-notes.service"}},
		{"systemctl", []string{"start", "ikigenba-notes.service"}},
		{"systemctl", []string{"is-active", "ikigenba-notes.service"}},
		{filepath.Join(appRoot, "bin", "notes"), []string{"--version"}},
	}
	if !reflect.DeepEqual(fixture.commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", fixture.commands, wantCommands)
	}
	wantReports := completedInstallReports("notes v9.8.7 active")
	if !reflect.DeepEqual(fixture.reports, wantReports) {
		t.Fatalf("reports = %#v, want %#v", fixture.reports, wantReports)
	}
	if !fixture.unitReportedAfterEnable {
		t.Fatalf("unit success was reported before enable: commands %#v", fixture.commands)
	}
}

func TestInstallRequiresExpectedExistingAccountGroup(t *testing.T) {
	// R-EKSI-SOZD
	fixture := newCompletedInstallFixture(t, t.TempDir(), false)
	if err := fixture.run(); err != nil {
		t.Fatal(err)
	}
	wantCommands := completedInstallCommands(fixture.root, false)
	if !reflect.DeepEqual(fixture.commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", fixture.commands, wantCommands)
	}

	wrongGroup := newCompletedInstallFixture(t, t.TempDir(), false)
	wrongGroup.accountGroup = "users"
	err := wrongGroup.run()
	var failure *apps.InstallError
	if !errors.As(err, &failure) || failure.Code != 1 || !strings.Contains(failure.Cause.Error(), "primary group") {
		t.Fatalf("wrong-group failure = %#v", err)
	}
	if got := wrongGroup.reports[len(wrongGroup.reports)-1]; got.step != "unit" || got.success || wrongGroup.configureCalls != 0 {
		t.Fatalf("reports = %#v, Configure calls = %d", wrongGroup.reports, wrongGroup.configureCalls)
	}
}

func TestInstallRejectsRootServiceAccount(t *testing.T) {
	// R-EKSI-SOZD
	fixture := newCompletedInstallFixture(t, t.TempDir(), false)
	fixture.accountUID = "0"
	err := fixture.run()
	var failure *apps.InstallError
	if !errors.As(err, &failure) || failure.Code != 1 || failure.Cause.Error() != "ikigenba account must not be root" {
		t.Fatalf("failure = %#v", err)
	}
	wantReports := []installReport{
		{"fetch", "notes-from-uri-v0.tar.xz, 0.0 MiB", true},
		{"file", "notes, port 4100", true},
		{"secrets", "0 keys", true},
		{"unpack", "/opt/notes", true},
		{"unit", "ikigenba account must not be root", false},
	}
	if !reflect.DeepEqual(fixture.reports, wantReports) || fixture.configureCalls != 0 {
		t.Fatalf("reports = %#v, Configure calls = %d", fixture.reports, fixture.configureCalls)
	}
}

func TestInstallCreatesOptWithoutChangingExistingOpt(t *testing.T) {
	// R-EPO4-BRY5
	missingRoot := t.TempDir()
	if err := newCompletedInstallFixture(t, missingRoot, false).run(); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(missingRoot, "opt"), 0o755)

	existingRoot := t.TempDir()
	opt := filepath.Join(existingRoot, "opt")
	if err := os.Mkdir(opt, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := newCompletedInstallFixture(t, existingRoot, false).run(); err != nil {
		t.Fatal(err)
	}
	assertMode(t, opt, 0o700)
}

func TestInstallAppliesInstalledTreeOwnershipAndModes(t *testing.T) {
	// R-EM0F-6GQ2 R-EN8B-K8GR
	root := t.TempDir()
	state := filepath.Join(root, "opt", "notes", "state", "keep")
	cache := filepath.Join(root, "opt", "notes", "cache", "keep")
	writeFixture(t, state, []byte("state"), 0o400)
	writeFixture(t, cache, []byte("cache"), 0o500)
	archive := tarEntries(t, []installTarEntry{
		regularEntry("etc/manifest.toml", []byte("app = \"notes\"\nport = 4100\n"), 0o666),
		regularEntry("etc/config", []byte("config"), 0o777),
		regularEntry("bin/notes", []byte("binary"), 0o711),
		regularEntry("bin/data", []byte("data"), 0o666),
		regularEntry("share/nested/page", []byte("page"), 0o644),
	})
	fixture := newCompletedInstallFixture(t, root, false)
	fixture.archive = archive
	if err := fixture.run(); err != nil {
		t.Fatal(err)
	}

	appRoot := filepath.Join(root, "opt", "notes")
	for _, directory := range []string{"bin", "etc", "share", filepath.Join("share", "nested")} {
		assertMode(t, filepath.Join(appRoot, directory), 0o750)
	}
	for name, mode := range map[string]os.FileMode{
		filepath.Join("bin", "notes"):            0o750,
		filepath.Join("bin", "data"):             0o640,
		filepath.Join("etc", "manifest.toml"):    0o640,
		filepath.Join("etc", "config"):           0o750,
		filepath.Join("etc", "env"):              0o600,
		filepath.Join("share", "nested", "page"): 0o640,
	} {
		assertMode(t, filepath.Join(appRoot, name), mode)
	}
	assertMode(t, appRoot, 0o750)
	assertMode(t, state, 0o400)
	assertMode(t, cache, 0o500)

	var recursive []commandCall
	for _, command := range fixture.commands {
		if command.name == "chown" && len(command.args) > 0 && command.args[0] == "--recursive" {
			recursive = append(recursive, command)
		}
	}
	want := commandCall{"chown", []string{"--recursive", "root:ikigenba", filepath.Join(appRoot, "bin"), filepath.Join(appRoot, "etc"), filepath.Join(appRoot, "share")}}
	if !reflect.DeepEqual(recursive, []commandCall{want}) {
		t.Fatalf("recursive ownership commands = %#v, want %#v", recursive, []commandCall{want})
	}

	if err := os.Chmod(filepath.Join(appRoot, "bin", "notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	reinstall := newCompletedInstallFixture(t, root, true)
	reinstall.archive = archive
	if err := reinstall.run(); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(appRoot, "bin", "notes"), 0o750)
	assertMode(t, state, 0o400)
	assertMode(t, cache, 0o500)
}

func TestInstallReportsInstalledTreeShapingFailures(t *testing.T) {
	// R-EOG7-Y07G R-AJZ2-XSUE
	for _, test := range []struct {
		name      string
		configure func(*completedInstallFixture)
		wantCause error
	}{
		{
			name: "ownership",
			configure: func(fixture *completedInstallFixture) {
				fixture.ownershipFailure = errors.New("ownership failed")
			},
			wantCause: errors.New("ownership failed"),
		},
		{
			name: "modes",
			configure: func(fixture *completedInstallFixture) {
				fixture.removeTreeAfterOwnership = true
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCompletedInstallFixture(t, t.TempDir(), false)
			test.configure(fixture)
			err := fixture.run()
			var failure *apps.InstallError
			if !errors.As(err, &failure) || failure.Code != 1 || failure.Cause == nil {
				t.Fatalf("failure = %#v", err)
			}
			if test.wantCause != nil && !strings.Contains(failure.Cause.Error(), test.wantCause.Error()) {
				t.Fatalf("cause = %v, want %v", failure.Cause, test.wantCause)
			}
			if got := fixture.reports[len(fixture.reports)-1]; got.step != "unit" || got.success || fixture.configureCalls != 0 {
				t.Fatalf("reports = %#v, Configure calls = %d", fixture.reports, fixture.configureCalls)
			}
			for _, report := range fixture.reports {
				if report.step == "unit" && report.success {
					t.Fatalf("successful unit report after shaping failure: %#v", fixture.reports)
				}
			}
		})
	}
}

func TestInstallRejectsAppUnitSymlinkWithoutFollowingIt(t *testing.T) {
	// R-EKSI-SOZD, R-OXY2-H0UX
	tests := []struct {
		name       string
		target     string
		linkTarget string
		contents   string
	}{
		{
			name:       "another unit",
			target:     filepath.Join("etc", "systemd", "system", "ikigenba-other.service"),
			linkTarget: "ikigenba-other.service",
			contents:   "other unit\n",
		},
		{
			name:       "another service",
			target:     filepath.Join("opt", "other", "etc", "config"),
			linkTarget: filepath.Join("..", "..", "..", "opt", "other", "etc", "config"),
			contents:   "other service config\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, test.target)
			writeFixture(t, target, []byte(test.contents), 0o600)
			if test.name == "another service" {
				writeFixture(t, filepath.Join(root, "opt", "other", "etc", "manifest.toml"),
					[]byte("app = \"other\"\nport = 4200\n"), 0o600)
			}
			unitPath := filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")
			if err := os.MkdirAll(filepath.Dir(unitPath), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(test.linkTarget, unitPath); err != nil {
				t.Fatal(err)
			}

			fixture := newCompletedInstallFixture(t, root, false)
			err := fixture.run()
			failureDetail := "unit /etc/systemd/system/ikigenba-notes.service is a symbolic link"
			var failure *apps.InstallError
			if !errors.As(err, &failure) || failure.Code != 1 || failure.Message != "install failed" ||
				failure.Cause == nil || failure.Cause.Error() != failureDetail {
				t.Fatalf("failure = %#v, want Code 1 unit publication failure %q", err, failureDetail)
			}
			wantReports := []installReport{
				{"fetch", "notes-from-uri-v0.tar.xz, 0.0 MiB", true},
				{"file", "notes, port 4100", true},
				{"secrets", "0 keys", true},
				{"unpack", "/opt/notes", true},
				{"unit", failureDetail, false},
			}
			if !reflect.DeepEqual(fixture.reports, wantReports) || fixture.configureCalls != 0 {
				t.Fatalf("reports = %#v, Configure calls = %d, want %#v and 0", fixture.reports, fixture.configureCalls, wantReports)
			}
			wantCommands := completedInstallCommands(root, false)[:6]
			if !reflect.DeepEqual(fixture.commands, wantCommands) {
				t.Fatalf("commands = %#v, want %#v", fixture.commands, wantCommands)
			}
			assertFile(t, target, test.contents)
			info, statErr := os.Lstat(unitPath)
			if statErr != nil {
				t.Fatalf("lstat unit path: %v", statErr)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("unit path mode = %v; want preserved symlink", info.Mode())
			}
			if got, readErr := os.Readlink(unitPath); readErr != nil || got != test.linkTarget {
				t.Fatalf("unit symlink = %q, error = %v; want %q", got, readErr, test.linkTarget)
			}
		})
	}
}

func TestInstallReportsUnitFailureBeforeConfiguration(t *testing.T) {
	// R-GWME-QK2L
	for _, action := range []string{"daemon-reload", "enable"} {
		t.Run(action, func(t *testing.T) {
			fixture := newCompletedInstallFixture(t, t.TempDir(), false)
			fixture.unitFailureAction = action
			err := fixture.run()
			var failure *apps.InstallError
			var commandErr *host.CommandError
			if !errors.As(err, &failure) || failure.Code != 1 || !errors.As(failure.Cause, &commandErr) {
				t.Fatalf("failure = %#v", err)
			}
			if got := fixture.reports[len(fixture.reports)-1]; got.step != "unit" || got.success {
				t.Fatalf("reports = %#v", fixture.reports)
			}
			wantCommands := []commandCall{
				{"xz", []string{"--decompress", "--stdout"}},
				{"systemctl", []string{"is-active", "ikigenba-notes.service"}},
				{"id", []string{"--user", "ikigenba"}},
				{"id", []string{"--group", "--name", "ikigenba"}},
				{"chown", []string{"ikigenba:ikigenba", filepath.Join(fixture.root, "opt", "notes")}},
				{"chown", []string{"--recursive", "root:ikigenba", filepath.Join(fixture.root, "opt", "notes", "bin"), filepath.Join(fixture.root, "opt", "notes", "etc")}},
				{"systemctl", []string{"daemon-reload"}},
			}
			if action == "enable" {
				wantCommands = append(wantCommands, commandCall{"systemctl", []string{"enable", "ikigenba-notes.service"}})
			}
			if fixture.configureCalls != 0 || !reflect.DeepEqual(fixture.commands, wantCommands) {
				t.Fatalf("Configure calls = %d, commands = %#v, want %#v", fixture.configureCalls, fixture.commands, wantCommands)
			}
		})
	}
}

func TestInstallConfiguresOnceBeforeActivation(t *testing.T) {
	// R-P0DV-8KCB
	fixture := newCompletedInstallFixture(t, t.TempDir(), false)
	configureAt := -1
	fixture.configure = func(_ context.Context, manifest apps.Manifest) error {
		configureAt = len(fixture.commands)
		if manifest.App != "notes" || manifest.Port != 4100 {
			t.Fatalf("configured manifest = %#v", manifest)
		}
		return nil
	}
	if err := fixture.run(); err != nil {
		t.Fatal(err)
	}
	if fixture.configureCalls != 1 {
		t.Fatalf("Configure calls = %d, want 1", fixture.configureCalls)
	}
	if configureAt < 1 || !reflect.DeepEqual(fixture.commands[configureAt-1], commandCall{"systemctl", []string{"enable", "ikigenba-notes.service"}}) {
		t.Fatalf("Configure at command %d in %#v", configureAt, fixture.commands)
	}
	if !reflect.DeepEqual(fixture.commands[configureAt], commandCall{"systemctl", []string{"start", "ikigenba-notes.service"}}) {
		t.Fatalf("command after Configure = %#v", fixture.commands[configureAt])
	}

	stop := errors.New("configuration stopped")
	failed := newCompletedInstallFixture(t, t.TempDir(), false)
	failed.configure = func(context.Context, apps.Manifest) error { return stop }
	err := failed.run()
	if !errors.Is(err, stop) || failed.configureCalls != 1 {
		t.Fatalf("error = %v, Configure calls = %d", err, failed.configureCalls)
	}
	wantFailedCommands := completedInstallCommands(failed.root, false)[:8]
	if !reflect.DeepEqual(failed.commands, wantFailedCommands) {
		t.Fatalf("commands = %#v, want %#v", failed.commands, wantFailedCommands)
	}
}

func TestInstallStartsOrRestartsAndReportsBinaryVersion(t *testing.T) {
	// R-P1LR-MC30
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "inactive", true: "active"}[active], func(t *testing.T) {
			fixture := newCompletedInstallFixture(t, t.TempDir(), active)
			if err := fixture.run(); err != nil {
				t.Fatal(err)
			}
			wantCommands := completedInstallCommands(fixture.root, active)
			if !reflect.DeepEqual(fixture.commands, wantCommands) {
				t.Fatalf("commands = %#v, want %#v", fixture.commands, wantCommands)
			}
			wantReports := completedInstallReports("notes v9.8.7 active")
			if !reflect.DeepEqual(fixture.reports, wantReports) {
				t.Fatalf("reports = %#v, want %#v", fixture.reports, wantReports)
			}
		})
	}
}

func TestInstallActivationFailureObtainsJournal(t *testing.T) {
	// R-P8X5-WYJ6
	startFailure := host.Result{ExitCode: 7, Stderr: []byte("start rejected\n")}
	fixture := newCompletedInstallFixture(t, t.TempDir(), false)
	fixture.startFailure = &startFailure
	err := fixture.run()
	var failure *apps.InstallError
	var commandErr *host.CommandError
	if !errors.As(err, &failure) || failure.Message != "notes: service failed to start" ||
		!errors.As(failure.Cause, &commandErr) || string(commandErr.Result.Stdout) != "first line\nsecond line\n" {
		t.Fatalf("failure = %#v, command error = %#v", err, commandErr)
	}
	wantReports := completedInstallReports("notes: service failed to start")
	wantReports[len(wantReports)-1].success = false
	if !reflect.DeepEqual(fixture.reports, wantReports) {
		t.Fatalf("reports = %#v, want %#v", fixture.reports, wantReports)
	}
	wantStartFailureCommands := completedInstallCommands(fixture.root, false)[:9]
	wantStartFailureCommands = append(wantStartFailureCommands,
		commandCall{"journalctl", []string{"--unit", "ikigenba-notes.service", "--no-pager", "--lines", "50"}})
	if !reflect.DeepEqual(fixture.commands, wantStartFailureCommands) {
		t.Fatalf("commands = %#v, want %#v", fixture.commands, wantStartFailureCommands)
	}

	fixture = newCompletedInstallFixture(t, t.TempDir(), false)
	fixture.resultingInactive = true
	err = fixture.run()
	wantInactiveCommands := completedInstallCommands(fixture.root, false)[:10]
	wantInactiveCommands = append(wantInactiveCommands,
		commandCall{"journalctl", []string{"--unit", "ikigenba-notes.service", "--no-pager", "--lines", "50"}})
	if !errors.As(err, &failure) || failure.Message != "notes: service failed to start" ||
		!reflect.DeepEqual(fixture.commands, wantInactiveCommands) {
		t.Fatalf("inactive result failure = %#v, commands = %#v", err, fixture.commands)
	}

	journalFailure := errors.New("journal transport failed")
	fixture = newCompletedInstallFixture(t, t.TempDir(), false)
	fixture.startFailure = &startFailure
	fixture.journalFailure = journalFailure
	err = fixture.run()
	if !errors.Is(err, journalFailure) || !strings.Contains(errUnwrapText(err), "start ikigenba-notes.service") ||
		!strings.Contains(errUnwrapText(err), "obtain ikigenba-notes.service journal") {
		t.Fatalf("journal failure = %#v, cause = %q", err, errUnwrapText(err))
	}
}

type commandCall struct {
	name string
	args []string
}

type completedInstallFixture struct {
	t                        *testing.T
	root                     string
	initiallyActive          bool
	accountMissing           bool
	configure                func(context.Context, apps.Manifest) error
	configureCalls           int
	commands                 []commandCall
	reports                  []installReport
	startFailure             *host.Result
	journalFailure           error
	resultingInactive        bool
	unitFailureAction        string
	accountUID               string
	accountGroup             string
	archive                  []byte
	ownershipFailure         error
	removeTreeAfterOwnership bool
	activated                bool
	unitEnabled              bool
	unitReportedAfterEnable  bool
}

func newCompletedInstallFixture(t *testing.T, root string, active bool) *completedInstallFixture {
	t.Helper()
	return &completedInstallFixture{t: t, root: root, initiallyActive: active, accountUID: "998", accountGroup: "ikigenba"}
}

func (fixture *completedInstallFixture) run() error {
	fixture.t.Helper()
	store := installStoreAt(fixture.t, fixture.root, map[string]string{"host.name": "host.example", "aws.region": "us-east-1"})
	client := &installCloudClient{get: func(context.Context, string) (io.ReadCloser, error) {
		return &trackedReadCloser{Reader: strings.NewReader("compressed")}, nil
	}}
	return apps.Install(fixture.t.Context(), host.Env{Root: fixture.root, Execute: fixture.execute}, cloud.Env{
		Open: func(context.Context, string) (cloud.Client, error) { return client, nil },
	}, store, "s3://bucket/releases/notes-from-uri-v0.tar.xz", apps.InstallHooks{
		Report: func(step, detail string, success bool) error {
			if step == "unit" && success {
				fixture.unitReportedAfterEnable = fixture.unitEnabled
			}
			fixture.reports = append(fixture.reports, installReport{step, detail, success})
			return nil
		},
		Configure: func(ctx context.Context, manifest apps.Manifest) error {
			fixture.configureCalls++
			if fixture.configure != nil {
				return fixture.configure(ctx, manifest)
			}
			return nil
		},
	})
}

func (fixture *completedInstallFixture) execute(_ context.Context, command host.Command) (host.Result, error) {
	fixture.commands = append(fixture.commands, commandCall{command.Name, append([]string(nil), command.Args...)})
	switch command.Name {
	case "xz":
		archive := fixture.archive
		if archive == nil {
			archive = validInstallTar(fixture.t, "app = \"notes\"\nport = 4100\n")
		}
		return host.Result{Stdout: archive}, nil
	case "id":
		if reflect.DeepEqual(command.Args, []string{"--user", "ikigenba"}) && fixture.accountMissing {
			fixture.accountMissing = false
			return host.Result{ExitCode: 1}, nil
		}
		if reflect.DeepEqual(command.Args, []string{"--group", "--name", "ikigenba"}) {
			return host.Result{Stdout: []byte(fixture.accountGroup + "\n")}, nil
		}
		return host.Result{Stdout: []byte(fixture.accountUID + "\n")}, nil
	case "systemctl":
		switch command.Args[0] {
		case "is-active":
			if !fixture.activated && !fixture.initiallyActive {
				return host.Result{ExitCode: 3, Stdout: []byte("inactive\n")}, nil
			}
			if fixture.activated && fixture.resultingInactive {
				return host.Result{ExitCode: 3, Stdout: []byte("inactive\n")}, nil
			}
			return host.Result{Stdout: []byte("active\n")}, nil
		case "daemon-reload", "enable":
			if command.Args[0] == fixture.unitFailureAction {
				return host.Result{ExitCode: 9, Stderr: []byte("unit failure\n")}, nil
			}
			if command.Args[0] == "enable" {
				fixture.unitEnabled = true
			}
		case "start", "restart":
			fixture.activated = true
			if fixture.startFailure != nil {
				return *fixture.startFailure, nil
			}
		}
		return host.Result{}, nil
	case "chown":
		if len(command.Args) > 0 && command.Args[0] == "--recursive" {
			if fixture.ownershipFailure != nil {
				return host.Result{}, fixture.ownershipFailure
			}
			if fixture.removeTreeAfterOwnership {
				etc := filepath.Join(fixture.root, "opt", "notes", "etc")
				if err := os.RemoveAll(etc); err != nil {
					fixture.t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(fixture.root, "missing"), etc); err != nil {
					fixture.t.Fatal(err)
				}
			}
		}
		return host.Result{}, nil
	case "journalctl":
		if fixture.journalFailure != nil {
			return host.Result{}, fixture.journalFailure
		}
		return host.Result{Stdout: []byte("first line\nsecond line\n")}, nil
	case filepath.Join(fixture.root, "opt", "notes", "bin", "notes"):
		return host.Result{Stdout: []byte("v9.8.7\r\n")}, nil
	default:
		return host.Result{}, nil
	}
}

func completedInstallReports(serviceDetail string) []installReport {
	return []installReport{
		{"fetch", "notes-from-uri-v0.tar.xz, 0.0 MiB", true},
		{"file", "notes, port 4100", true},
		{"secrets", "0 keys", true},
		{"unpack", "/opt/notes", true},
		{"unit", "ikigenba-notes.service", true},
		{"service", serviceDetail, true},
	}
}

func completedInstallCommands(root string, initiallyActive bool) []commandCall {
	action := "start"
	if initiallyActive {
		action = "restart"
	}
	appRoot := filepath.Join(root, "opt", "notes")
	return []commandCall{
		{"xz", []string{"--decompress", "--stdout"}},
		{"systemctl", []string{"is-active", "ikigenba-notes.service"}},
		{"id", []string{"--user", "ikigenba"}},
		{"id", []string{"--group", "--name", "ikigenba"}},
		{"chown", []string{"ikigenba:ikigenba", appRoot}},
		{"chown", []string{"--recursive", "root:ikigenba", filepath.Join(appRoot, "bin"), filepath.Join(appRoot, "etc")}},
		{"systemctl", []string{"daemon-reload"}},
		{"systemctl", []string{"enable", "ikigenba-notes.service"}},
		{"systemctl", []string{action, "ikigenba-notes.service"}},
		{"systemctl", []string{"is-active", "ikigenba-notes.service"}},
		{filepath.Join(appRoot, "bin", "notes"), []string{"--version"}},
	}
}

func errUnwrapText(err error) string {
	var failure *apps.InstallError
	if errors.As(err, &failure) {
		return failure.Cause.Error()
	}
	return err.Error()
}

func assertMode(t *testing.T, name string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(name)
	if err != nil {
		t.Fatalf("stat %s: %v", name, err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %v, want %v", name, info.Mode().Perm(), want)
	}
}
