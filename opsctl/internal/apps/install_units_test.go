package apps_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestInstallPublishesAndEnablesRootedAppUnit(t *testing.T) {
	// R-UL38-H654 R-EN8B-K8GR R-UJVC-3EEF R-UMB4-UXVT
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
	wantSocket := "[Unit]\nDescription=Ikigenba notes socket\n\n" +
		"[Socket]\nListenStream=" + filepath.Join(root, "run", "ikigenba", "notes.sock") + "\n" +
		"SocketUser=ikigenba\nSocketGroup=nginx\nSocketMode=0660\nRemoveOnStop=yes\nBacklog=4096\n\n" +
		"[Install]\nWantedBy=sockets.target\n"
	wantUnit := "[Unit]\nDescription=Ikigenba notes app\nRequires=ikigenba-notes.socket\nAfter=ikigenba-notes.socket\n\n" +
		"[Service]\nType=notify\n" +
		"ExecStart=" + filepath.Join(appRoot, "bin", "notes") + "\n" +
		"WorkingDirectory=" + appRoot + "\n" +
		"EnvironmentFile=" + filepath.Join(appRoot, "etc", "env") + "\n" +
		"User=ikigenba\nRestart=on-failure\nTimeoutStopSec=10\n\n" +
		"[Install]\nWantedBy=multi-user.target\n"
	assertFile(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.socket"), wantSocket)
	assertFile(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service"), wantUnit)
	assertFile(t, statePath, "state")
	assertFile(t, cachePath, "cache")
	assertMode(t, appRoot, 0o750)
	assertMode(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service"), 0o644)
	assertMode(t, filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.socket"), 0o644)
	assertMode(t, statePath, 0o600)
	assertMode(t, cachePath, 0o600)

	wantCommands := []commandCall{
		{"xz", []string{"--decompress", "--stdout"}},
		{"systemctl", []string{"is-active", "ikigenba-notes.service"}},
		{"systemctl", []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}},
		{"id", []string{"--user", "ikigenba"}},
		{"useradd", []string{"--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "--user-group", "ikigenba"}},
		{"chown", []string{"ikigenba:ikigenba", appRoot}},
		{"chown", []string{"--recursive", "root:ikigenba", filepath.Join(appRoot, "bin"), filepath.Join(appRoot, "etc")}},
		{"systemctl", []string{"daemon-reload"}},
		{"systemctl", []string{"enable", "ikigenba-notes.service"}},
		{"systemctl", []string{"enable", "--now", "ikigenba-notes.socket"}},
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
	// R-UL38-H654
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

	loginShell := newCompletedInstallFixture(t, t.TempDir(), false)
	loginShell.accountShell = "/bin/bash"
	err = loginShell.run()
	if err != nil {
		t.Fatal(err)
	}
	if loginShell.accountShell != "/usr/sbin/nologin" {
		t.Fatalf("account shell = %q, want /usr/sbin/nologin", loginShell.accountShell)
	}
	wantLoginCommands := completedInstallCommands(loginShell.root, false)
	wantLoginCommands = append(wantLoginCommands[:6], append([]commandCall{
		{"usermod", []string{"--shell", "/usr/sbin/nologin", "ikigenba"}},
	}, wantLoginCommands[6:]...)...)
	if !reflect.DeepEqual(loginShell.commands, wantLoginCommands) {
		t.Fatalf("commands = %#v, want %#v", loginShell.commands, wantLoginCommands)
	}
}

func TestInstallRejectsRootServiceAccount(t *testing.T) {
	// R-UL38-H654
	fixture := newCompletedInstallFixture(t, t.TempDir(), false)
	fixture.accountUID = "0"
	err := fixture.run()
	var failure *apps.InstallError
	if !errors.As(err, &failure) || failure.Code != 1 || failure.Cause.Error() != "ikigenba account must not be root" {
		t.Fatalf("failure = %#v", err)
	}
	wantReports := []installReport{
		{"fetch", "notes-from-uri-v0.tar.xz, 0.0 MiB", true},
		{"file", "notes", true},
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
	if rerunInOwnershipNamespace(t) {
		return
	}
	missingRoot := t.TempDir()
	missing := newCompletedInstallFixture(t, missingRoot, false)
	missing.mutateOwnership = true
	if err := missing.run(); err != nil {
		t.Fatal(err)
	}
	missingOpt := filepath.Join(missingRoot, "opt")
	assertMode(t, missingOpt, 0o755)
	assertOwnership(t, missingOpt, 0, 0)

	existingRoot := t.TempDir()
	opt := filepath.Join(existingRoot, "opt")
	if err := os.Mkdir(opt, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(opt, 1, 1); err != nil {
		t.Fatal(err)
	}
	existing := newCompletedInstallFixture(t, existingRoot, false)
	existing.mutateOwnership = true
	if err := existing.run(); err != nil {
		t.Fatal(err)
	}
	assertMode(t, opt, 0o700)
	assertOwnership(t, opt, 1, 1)
}

func TestInstallAppliesInstalledTreeOwnershipAndModes(t *testing.T) {
	// R-EM0F-6GQ2 R-EN8B-K8GR
	if rerunInOwnershipNamespace(t) {
		return
	}
	root := t.TempDir()
	state := filepath.Join(root, "opt", "notes", "state", "keep")
	cache := filepath.Join(root, "opt", "notes", "cache", "keep")
	writeFixture(t, state, []byte("state"), 0o400)
	writeFixture(t, cache, []byte("cache"), 0o500)
	archive := tarEntries(t, []installTarEntry{
		regularEntry("etc/manifest.toml", []byte("app = \"notes\"\n"), 0o666),
		regularEntry("etc/config", []byte("config"), 0o777),
		regularEntry("bin/notes", []byte("binary"), 0o711),
		regularEntry("bin/data", []byte("data"), 0o666),
		regularEntry("share/nested/page", []byte("page"), 0o644),
	})
	fixture := newCompletedInstallFixture(t, root, false)
	fixture.archive = archive
	fixture.mutateOwnership = true
	if err := fixture.run(); err != nil {
		t.Fatal(err)
	}

	appRoot := filepath.Join(root, "opt", "notes")
	for _, directory := range []string{"bin", "etc", "share", filepath.Join("share", "nested")} {
		name := filepath.Join(appRoot, directory)
		assertMode(t, name, 0o750)
		assertOwnership(t, name, 0, 1)
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
		assertOwnership(t, filepath.Join(appRoot, name), 0, 1)
	}
	assertMode(t, appRoot, 0o750)
	assertOwnership(t, appRoot, 1, 1)
	assertMode(t, state, 0o400)
	assertMode(t, cache, 0o500)
	assertOwnership(t, state, 0, 0)
	assertOwnership(t, cache, 0, 0)

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
	reinstall.mutateOwnership = true
	if err := reinstall.run(); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(appRoot, "bin", "notes"), 0o750)
	assertMode(t, state, 0o400)
	assertMode(t, cache, 0o500)
}

func TestInstallReportsInstalledTreeShapingFailures(t *testing.T) {
	// R-EOG7-Y07G R-XLMA-IR4X
	ownershipFailure := errors.New("ownership failed")
	for _, test := range []struct {
		name      string
		configure func(*completedInstallFixture)
		wantCause error
	}{
		{
			name: "ownership",
			configure: func(fixture *completedInstallFixture) {
				fixture.ownershipFailure = ownershipFailure
			},
			wantCause: ownershipFailure,
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
			if test.wantCause != nil && !errors.Is(failure.Cause, test.wantCause) {
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

func TestInstallStageActionAndReportFailuresStopInOrder(t *testing.T) {
	// R-XLMA-IR4X
	stages := []struct {
		name      string
		configure func(*completedInstallFixture, error)
	}{
		{"fetch", func(fixture *completedInstallFixture, cause error) { fixture.downloadFailure = cause }},
		{"file", func(fixture *completedInstallFixture, cause error) { fixture.fileFailure = cause }},
		{"secrets", func(fixture *completedInstallFixture, cause error) {
			fixture.archive = validInstallTar(fixture.t, "app = \"notes\"\nsecrets = [\"TOKEN\"]\n")
			fixture.secretsFailure = cause
		}},
		{"unpack", func(fixture *completedInstallFixture, cause error) { fixture.optOwnershipFailure = cause }},
		{"unit", func(fixture *completedInstallFixture, cause error) { fixture.ownershipFailure = cause }},
		{"service", func(fixture *completedInstallFixture, cause error) { fixture.serviceFailure = cause }},
	}
	for _, stage := range stages {
		t.Run(stage.name+" action and failed-report", func(t *testing.T) {
			actionErr := errors.New(stage.name + " action failed")
			reportErr := errors.New(stage.name + " report failed")
			fixture := newCompletedInstallFixture(t, t.TempDir(), false)
			stage.configure(fixture, actionErr)
			fixture.reportFailureStage = stage.name
			fixture.reportFailureSuccess = false
			fixture.reportFailure = reportErr
			err := fixture.run()
			if !errors.Is(err, actionErr) || !errors.Is(err, reportErr) {
				t.Fatalf("failure = %#v, want action %v and report %v", err, actionErr, reportErr)
			}
			assertLastAndOnlyStageReport(t, fixture.reports, stage.name, false)
		})

		t.Run(stage.name+" successful-report", func(t *testing.T) {
			reportErr := errors.New(stage.name + " success report failed")
			fixture := newCompletedInstallFixture(t, t.TempDir(), false)
			fixture.reportFailureStage = stage.name
			fixture.reportFailureSuccess = true
			fixture.reportFailure = reportErr
			err := fixture.run()
			if !errors.Is(err, reportErr) {
				t.Fatalf("failure = %#v, want report %v", err, reportErr)
			}
			assertLastAndOnlyStageReport(t, fixture.reports, stage.name, true)
		})
	}
}

func assertLastAndOnlyStageReport(t *testing.T, reports []installReport, stage string, success bool) {
	t.Helper()
	if len(reports) == 0 || reports[len(reports)-1].step != stage || reports[len(reports)-1].success != success {
		t.Fatalf("reports = %#v, want final %s success=%v", reports, stage, success)
	}
	count := 0
	for _, report := range reports {
		if report.step == stage {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reports = %#v, want exactly one %s outcome", reports, stage)
	}
}

func TestInstallRejectsAppUnitSymlinkWithoutFollowingIt(t *testing.T) {
	// R-UJVC-3EEF, R-OXY2-H0UX
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
					[]byte("app = \"other\"\n"), 0o600)
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
				{"file", "notes", true},
				{"secrets", "0 keys", true},
				{"unpack", "/opt/notes", true},
				{"unit", failureDetail, false},
			}
			if !reflect.DeepEqual(fixture.reports, wantReports) || fixture.configureCalls != 0 {
				t.Fatalf("reports = %#v, Configure calls = %d, want %#v and 0", fixture.reports, fixture.configureCalls, wantReports)
			}
			wantCommands := completedInstallCommands(root, false)
			if test.name == "another service" {
				wantCommands = append(wantCommands[:3], wantCommands[4:]...)
			}
			wantCommands = wantCommands[:len(wantCommands)-6]
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
			wantCommands := completedInstallCommands(fixture.root, false)[:10]
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
	// R-XMU6-WIVM
	fixture := newCompletedInstallFixture(t, t.TempDir(), false)
	configureAt := -1
	fixture.configure = func(_ context.Context, manifest apps.Manifest) error {
		configureAt = len(fixture.commands)
		if manifest.App != "notes" {
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
	if configureAt < 1 || !reflect.DeepEqual(fixture.commands[configureAt-1], commandCall{"systemctl", []string{"enable", "--now", "ikigenba-notes.socket"}}) {
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
	wantFailedCommands := completedInstallCommands(failed.root, false)[:12]
	if !reflect.DeepEqual(failed.commands, wantFailedCommands) {
		t.Fatalf("commands = %#v, want %#v", failed.commands, wantFailedCommands)
	}
}

func TestInstallStartsOrRestartsAndReportsBinaryVersion(t *testing.T) {
	// R-UNJ1-8PMI
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

func TestInstallReplacesDisabledUnitsWithoutActivation(t *testing.T) {
	// R-UJVC-3EEF R-UMB4-UXVT R-UNJ1-8PMI
	root := t.TempDir()
	servicePath := filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.service")
	socketPath := filepath.Join(root, "etc", "systemd", "system", "ikigenba-notes.socket")
	writeFixture(t, servicePath, []byte("old service\n"), 0o644)
	writeFixture(t, socketPath, []byte("old socket\n"), 0o644)
	fixture := newCompletedInstallFixture(t, root, false)
	fixture.disabled = true
	if err := fixture.run(); err != nil {
		t.Fatal(err)
	}
	appRoot := filepath.Join(root, "opt", "notes")
	wantSocket := "[Unit]\nDescription=Ikigenba notes socket\n\n" +
		"[Socket]\nListenStream=" + filepath.Join(root, "run", "ikigenba", "notes.sock") + "\n" +
		"SocketUser=ikigenba\nSocketGroup=nginx\nSocketMode=0660\nRemoveOnStop=yes\nBacklog=4096\n\n" +
		"[Install]\nWantedBy=sockets.target\n"
	wantService := "[Unit]\nDescription=Ikigenba notes app\nRequires=ikigenba-notes.socket\nAfter=ikigenba-notes.socket\n\n" +
		"[Service]\nType=notify\nExecStart=" + filepath.Join(appRoot, "bin", "notes") + "\n" +
		"WorkingDirectory=" + appRoot + "\n" +
		"EnvironmentFile=" + filepath.Join(appRoot, "etc", "env") + "\n" +
		"User=ikigenba\nRestart=on-failure\nTimeoutStopSec=10\n\n" +
		"[Install]\nWantedBy=multi-user.target\n"
	assertFile(t, socketPath, wantSocket)
	assertFile(t, servicePath, wantService)
	wantCommands := completedInstallCommands(root, false)[:10]
	wantCommands = append(wantCommands, commandCall{filepath.Join(appRoot, "bin", "notes"), []string{"--version"}})
	if !reflect.DeepEqual(fixture.commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", fixture.commands, wantCommands)
	}
	if fixture.configureCalls != 1 || !reflect.DeepEqual(fixture.reports, completedInstallReports("notes v9.8.7 disabled")) {
		t.Fatalf("configure calls = %d, reports = %#v", fixture.configureCalls, fixture.reports)
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
	wantStartFailureCommands := completedInstallCommands(fixture.root, false)[:13]
	wantStartFailureCommands = append(wantStartFailureCommands,
		commandCall{"journalctl", []string{"--unit", "ikigenba-notes.service", "--no-pager", "--lines", "50"}})
	if !reflect.DeepEqual(fixture.commands, wantStartFailureCommands) {
		t.Fatalf("commands = %#v, want %#v", fixture.commands, wantStartFailureCommands)
	}

	fixture = newCompletedInstallFixture(t, t.TempDir(), false)
	fixture.resultingInactive = true
	err = fixture.run()
	wantInactiveCommands := completedInstallCommands(fixture.root, false)[:14]
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
	accountShell             string
	archive                  []byte
	ownershipFailure         error
	removeTreeAfterOwnership bool
	mutateOwnership          bool
	downloadFailure          error
	fileFailure              error
	secretsFailure           error
	optOwnershipFailure      error
	serviceFailure           error
	reportFailureStage       string
	reportFailureSuccess     bool
	reportFailure            error
	activated                bool
	unitEnabled              bool
	unitReportedAfterEnable  bool
	disabled                 bool
}

func newCompletedInstallFixture(t *testing.T, root string, active bool) *completedInstallFixture {
	t.Helper()
	return &completedInstallFixture{
		t: t, root: root, initiallyActive: active,
		accountUID: "998", accountGroup: "ikigenba", accountShell: "/usr/sbin/nologin",
	}
}

func (fixture *completedInstallFixture) run() error {
	fixture.t.Helper()
	store := installStoreAt(fixture.t, fixture.root, map[string]string{"host.name": "host.example", "aws.region": "us-east-1"})
	client := &installCloudClient{get: func(context.Context, string) (io.ReadCloser, error) {
		if fixture.downloadFailure != nil {
			return nil, fixture.downloadFailure
		}
		return &trackedReadCloser{Reader: strings.NewReader("compressed")}, nil
	}, readSecrets: func(context.Context, string) (map[string]string, error) {
		if fixture.secretsFailure != nil {
			return nil, fixture.secretsFailure
		}
		return map[string]string{"TOKEN": "value"}, nil
	}}
	return apps.Install(fixture.t.Context(), host.Env{Root: fixture.root, Execute: fixture.execute}, cloud.Env{
		Open: func(context.Context, string) (cloud.Client, error) { return client, nil },
	}, store, "s3://bucket/releases/notes-from-uri-v0.tar.xz", apps.InstallHooks{
		Report: func(step, detail string, success bool) error {
			if step == "unit" && success {
				fixture.unitReportedAfterEnable = fixture.unitEnabled
			}
			fixture.reports = append(fixture.reports, installReport{step, detail, success})
			if step == fixture.reportFailureStage && success == fixture.reportFailureSuccess {
				return fixture.reportFailure
			}
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
		if fixture.fileFailure != nil {
			return host.Result{}, fixture.fileFailure
		}
		archive := fixture.archive
		if archive == nil {
			archive = validInstallTar(fixture.t, "app = \"notes\"\n")
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
	case "getent":
		return host.Result{Stdout: []byte("ikigenba:x:" + fixture.accountUID + ":998::/nonexistent:" + fixture.accountShell + "\n")}, nil
	case "usermod":
		if !reflect.DeepEqual(command.Args, []string{"--shell", "/usr/sbin/nologin", "ikigenba"}) {
			fixture.t.Fatalf("usermod args = %#v", command.Args)
		}
		fixture.accountShell = "/usr/sbin/nologin"
		return host.Result{}, nil
	case "systemctl":
		switch command.Args[0] {
		case "show":
			if fixture.disabled {
				return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
			}
			return host.Result{Stdout: []byte("LoadState=not-found\nUnitFileState=disabled\n")}, nil
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
			if command.Args[0] == "enable" && len(command.Args) > 2 {
				fixture.unitEnabled = true
			}
		case "start", "restart":
			fixture.activated = true
			if fixture.serviceFailure != nil {
				return host.Result{}, fixture.serviceFailure
			}
			if fixture.startFailure != nil {
				return *fixture.startFailure, nil
			}
		}
		return host.Result{}, nil
	case "chown":
		if len(command.Args) > 0 && command.Args[0] == "root:root" && fixture.optOwnershipFailure != nil {
			return host.Result{}, fixture.optOwnershipFailure
		}
		if fixture.mutateOwnership {
			fixture.applyOwnership(command.Args)
		}
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

func (fixture *completedInstallFixture) applyOwnership(args []string) {
	fixture.t.Helper()
	uid, gid, paths := 0, 0, args[1:]
	if args[0] == "ikigenba:ikigenba" {
		uid, gid, paths = 1, 1, args[1:]
	}
	if args[0] == "--recursive" {
		gid, paths = 1, args[2:]
	}
	for _, name := range paths {
		if args[0] == "--recursive" {
			if err := chownFixtureTree(name, uid, gid); err != nil {
				fixture.t.Fatal(err)
			}
		} else if err := os.Chown(name, uid, gid); err != nil {
			fixture.t.Fatal(err)
		}
	}
}

func chownFixtureTree(name string, uid, gid int) error {
	filesystem, err := os.OpenRoot(name)
	if err != nil {
		return err
	}
	defer func() { _ = filesystem.Close() }()
	var apply func(string) error
	apply = func(relative string) error {
		if err := filesystem.Chown(relative, uid, gid); err != nil {
			return err
		}
		info, err := filesystem.Lstat(relative)
		if err != nil || !info.IsDir() {
			return err
		}
		directory, err := filesystem.Open(relative)
		if err != nil {
			return err
		}
		entries, readErr := directory.ReadDir(-1)
		closeErr := directory.Close()
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		for _, entry := range entries {
			if err := apply(filepath.Join(relative, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	return apply(".")
}

func completedInstallReports(serviceDetail string) []installReport {
	return []installReport{
		{"fetch", "notes-from-uri-v0.tar.xz, 0.0 MiB", true},
		{"file", "notes", true},
		{"secrets", "0 keys", true},
		{"unpack", "/opt/notes", true},
		{"unit", "ikigenba-notes.socket, ikigenba-notes.service", true},
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
		{"systemctl", []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}},
		{"chown", []string{"root:root", filepath.Join(root, "opt")}},
		{"id", []string{"--user", "ikigenba"}},
		{"getent", []string{"passwd", "ikigenba"}},
		{"id", []string{"--group", "--name", "ikigenba"}},
		{"chown", []string{"ikigenba:ikigenba", appRoot}},
		{"chown", []string{"--recursive", "root:ikigenba", filepath.Join(appRoot, "bin"), filepath.Join(appRoot, "etc")}},
		{"systemctl", []string{"daemon-reload"}},
		{"systemctl", []string{"enable", "ikigenba-notes.service"}},
		{"systemctl", []string{"enable", "--now", "ikigenba-notes.socket"}},
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

func assertOwnership(t *testing.T, name string, uid, gid uint32) {
	t.Helper()
	info, err := os.Lstat(name)
	if err != nil {
		t.Fatalf("lstat %s: %v", name, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("%s has no syscall stat", name)
	}
	if stat.Uid != uid || stat.Gid != gid {
		t.Fatalf("%s ownership = %d:%d, want %d:%d", name, stat.Uid, stat.Gid, uid, gid)
	}
}

func rerunInOwnershipNamespace(t *testing.T) bool {
	t.Helper()
	key := "OPSCTL_OWNERSHIP_NAMESPACE_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	if os.Getenv(key) == "1" {
		return false
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, err := strconv.Atoi(current.Uid)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := strconv.Atoi(current.Gid)
	if err != nil {
		t.Fatal(err)
	}
	subuid := subordinateIDStart(t, "/etc/subuid", current.Username)
	subgid := subordinateIDStart(t, "/etc/subgid", current.Username)
	mapping := func(host, namespace, count int) string {
		return strconv.Itoa(host) + "," + strconv.Itoa(namespace) + "," + strconv.Itoa(count)
	}
	command := exec.Command("unshare")
	command.Args = []string{"unshare",
		"--user",
		"--map-users=" + mapping(uid, 0, 1),
		"--map-users=" + mapping(subuid, 1, 65536),
		"--map-groups=" + mapping(gid, 0, 1),
		"--map-groups=" + mapping(subgid, 1, 65536),
		os.Args[0], "-test.run=^" + t.Name() + "$",
	}
	command.Env = append(os.Environ(), key+"=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ownership namespace: %v\n%s", err, output)
	}
	return true
}

func subordinateIDStart(t *testing.T, name, username string) int {
	t.Helper()
	var contents []byte
	var err error
	switch name {
	case "/etc/subuid":
		contents, err = os.ReadFile("/etc/subuid")
	case "/etc/subgid":
		contents, err = os.ReadFile("/etc/subgid")
	default:
		t.Fatalf("unexpected subordinate-id file %q", name)
	}
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(contents), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) == 3 && fields[0] == username {
			start, parseErr := strconv.Atoi(fields[1])
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			return start
		}
	}
	t.Fatalf("no subordinate id range for %s in %s", username, name)
	return 0
}
