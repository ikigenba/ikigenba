package cli_test

import (
	"bytes"
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestLifecyclePackageOwnership(t *testing.T) {
	// R-LM7Q-0199
	appsDirectory := filepath.Join("..", "apps")
	err := filepath.WalkDir(appsDirectory, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range parsed.Imports {
			if strings.Contains(imported.Path.Value, "/internal/nginx") || strings.Contains(imported.Path.Value, "/internal/backup") ||
				strings.Contains(imported.Path.Value, "/internal/cli") {
				t.Errorf("%s has orchestration dependency %s", name, imported.Path.Value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUninstallCommandComposesLifecycleRoutingAndReplication(t *testing.T) {
	// R-LM7Q-0199 R-M6Y0-I4V2 R-JE5P-1F05 R-X6R5-769H
	root := uninstallCommandRoot(t, true)
	var commands []host.Command
	otherBefore := snapshotUninstallPaths(t, root, "opt/tasks", "etc/systemd/system/ikigenba-tasks.service")
	writeUninstallFile(t, root, "replicas/notes/notes.db", "stale transaction")
	writeUninstallFile(t, root, "replicas/tasks/tasks.db", "stale remaining replica")
	execute := func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		switch {
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}):
			return host.Result{Stdout: []byte("active\n")}, nil
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"stop", "ikigenba-notes.service"}):
			writeUninstallFile(t, root, "replicas/notes/notes.db", readUninstallFile(t, root, "opt/notes/state/notes.db"))
			return host.Result{}, nil
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"restart", "litestream.service"}):
			configuration := readUninstallFile(t, root, "etc/litestream.yml")
			if !strings.Contains(configuration, "opt/tasks/state/tasks.db") || strings.Contains(configuration, "opt/notes/") {
				t.Fatalf("litestream restarted with wrong configuration: %q", configuration)
			}
			writeUninstallFile(t, root, "replicas/tasks/tasks.db", readUninstallFile(t, root, "opt/tasks/state/tasks.db"))
			return host.Result{}, nil
		default:
			return host.Result{}, nil
		}
	}

	stdout, stderr, code := invoke([]string{"uninstall", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: execute})
	wantOutput := "stop: ok (ikigenba-notes.service stopped, disabled)\n" +
		"unit: ok (removed ikigenba-notes.service)\n" +
		"files: ok (removed /opt/notes/bin, etc, share, cache; kept state)\n" +
		"nginx: ok (notes.example.test, example.test removed)\n" +
		"litestream: ok (state/notes.db removed)\n"
	if code != 0 || stdout != wantOutput || stderr != "" {
		t.Fatalf("uninstall = exit %d stdout %q stderr %q", code, stdout, stderr)
	}

	wantCommands := []host.Command{
		{Name: "systemctl", Args: []string{"is-active", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"stop", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"disable", "ikigenba-notes.service"}},
		{Name: "systemctl", Args: []string{"daemon-reload"}},
		{Name: "nginx", Args: []string{"-t"}},
		{Name: "systemctl", Args: []string{"reload-or-restart", "nginx"}},
		{Name: "systemctl", Args: []string{"restart", "litestream.service"}},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
	if got := readUninstallFile(t, root, "replicas/notes/notes.db"); got != "last committed transaction" {
		t.Fatalf("shutdown replica = %q", got)
	}
	if got := readUninstallFile(t, root, "replicas/tasks/tasks.db"); got != "other database" {
		t.Fatalf("remaining replica = %q", got)
	}

	for _, name := range []string{"notes.db", "notes.db-wal", "notes.db-shm", ".notes.db-litestream/meta"} {
		if _, err := os.Stat(filepath.Join(root, "opt/notes/state", filepath.FromSlash(name))); err != nil {
			t.Errorf("retained %s: %v", name, err)
		}
	}
	for _, name := range []string{"bin", "etc", "share", "cache"} {
		if _, err := os.Lstat(filepath.Join(root, "opt/notes", name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("removed directory %s still exists: %v", name, err)
		}
	}
	nginxConfig := readUninstallFile(t, root, "etc/nginx/conf.d/ikigenba.conf")
	if strings.Contains(nginxConfig, "notes.example.test") || !strings.Contains(nginxConfig, "tasks.example.test") {
		t.Fatalf("nginx routes after uninstall = %q", nginxConfig)
	}
	litestream := readUninstallFile(t, root, "etc/litestream.yml")
	if strings.Contains(litestream, "opt/notes/") || !strings.Contains(litestream, "opt/tasks/state/tasks.db") {
		t.Fatalf("litestream configuration after uninstall = %q", litestream)
	}
	if got := readUninstallFile(t, root, "remote/parameters/notes"); got != "owned by devctl" {
		t.Fatalf("remote parameter changed to %q", got)
	}
	if got := readUninstallFile(t, root, "remote/releases/notes.tar.xz"); got != "deployment artifact" {
		t.Fatalf("deployment artifact changed to %q", got)
	}
	if got := readUninstallFile(t, root, "opt/tasks/bin/tasks"); got != "other binary" {
		t.Fatalf("other app changed to %q", got)
	}
	if otherAfter := snapshotUninstallPaths(t, root, "opt/tasks", "etc/systemd/system/ikigenba-tasks.service"); !reflect.DeepEqual(otherAfter, otherBefore) {
		t.Fatalf("other app changed:\nbefore %#v\nafter  %#v", otherBefore, otherAfter)
	}

	databaseFiles := backup.DatabaseFiles(apps.Database{Engine: "sqlite", Path: "state/notes.db"})
	for _, name := range databaseFiles {
		if _, err := os.Stat(filepath.Join(root, "opt/notes", filepath.FromSlash(name))); err != nil {
			t.Errorf("manifest-free backup input %s: %v", name, err)
		}
	}
	if got := readUninstallFile(t, root, "opt/notes/state/private/blob"); got != "private retained state" {
		t.Fatalf("retained state changed to %q", got)
	}
	changed, err := backup.Regenerate(context.Background(), host.Env{Root: root}, config.Store{Root: root})
	if err != nil || changed {
		t.Fatalf("manifest-free Regenerate = %t, %v, want unchanged success", changed, err)
	}

	rows, err := apps.Status(context.Background(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name == "systemctl" && strings.Contains(command.Args[len(command.Args)-1], "notes") {
			return host.Result{Stdout: []byte("LoadState=not-found\nActiveState=inactive\n")}, nil
		}
		return host.Result{}, errors.New("unavailable")
	}})
	if err != nil {
		t.Fatal(err)
	}
	var notes apps.StatusRow
	for _, row := range rows {
		if row.Name == "notes" {
			notes = row
		}
	}
	if notes != (apps.StatusRow{Name: "notes", Version: "-", State: "-", JournalMode: "-"}) {
		t.Fatalf("retained state service row = %#v", notes)
	}
}

func TestUninstallValidationPrecedesOwnedStopStage(t *testing.T) {
	// R-LZMM-7IEW R-EX3N-DN1J R-X4BC-FMS3
	for _, test := range []struct {
		name       string
		app        string
		root       func(*testing.T) string
		wantStdout string
		wantStderr string
		code       int
	}{
		{name: "invalid name", app: "bad/name", root: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }, wantStderr: "opsctl: 'bad/name' is not a usable app name\n", code: 2},
		{name: "host name absent", app: "notes", root: func(t *testing.T) string { return t.TempDir() }, wantStderr: "opsctl: host.name not set\n", code: 1},
		{name: "service absent", app: "notes", root: func(t *testing.T) string {
			root := t.TempDir()
			setUninstallConfig(t, root)
			return root
		}, wantStdout: "stop: failed: no service 'notes'\n", wantStderr: "opsctl: uninstall failed\n", code: 1},
		{name: "binary absent", app: "notes", root: func(t *testing.T) string {
			root := t.TempDir()
			setUninstallConfig(t, root)
			writeUninstallFile(t, root, "opt/notes/etc/manifest.toml", "app = \"notes\"\n")
			return root
		}, wantStdout: "stop: failed: notes is not installed\n", wantStderr: "opsctl: uninstall failed\n", code: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			executed := false
			stdout, stderr, code := invoke([]string{"uninstall", test.app}, cli.Deps{Root: test.root(t), EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
				executed = true
				return host.Result{}, nil
			}})
			if code != test.code || stdout != test.wantStdout || stderr != test.wantStderr || executed {
				t.Fatalf("preflight = exit %d stdout %q stderr %q executed %t", code, stdout, stderr, executed)
			}
		})
	}

	for _, args := range [][]string{{"restart", "bad/name"}, {"restart", "notes"}} {
		root := t.TempDir()
		executed := false
		stdout, stderr, code := invoke(args, cli.Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
			executed = true
			return host.Result{}, nil
		}})
		wantCode := 1
		if args[1] == "bad/name" {
			wantCode = 2
		}
		wantStdout := ""
		if args[1] == "notes" {
			wantStdout = "service: failed: no service 'notes'\n"
		}
		if code != wantCode || stdout != wantStdout || stderr == "" || executed {
			t.Fatalf("restart preflight %q = exit %d stdout %q stderr %q executed %t", args[1], code, stdout, stderr, executed)
		}
	}

	root := cliRestartRoot(t)
	var restartCommands []host.Command
	stdout, stderr, code := invoke([]string{"restart", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		restartCommands = append(restartCommands, command)
		switch {
		case reflect.DeepEqual(command, host.Command{Name: "systemctl", Args: []string{"restart", "ikigenba-notes.service"}}):
			return host.Result{}, nil
		case reflect.DeepEqual(command, host.Command{Name: "systemctl", Args: []string{"is-active", "ikigenba-notes.service"}}):
			return host.Result{Stdout: []byte("active\n")}, nil
		default:
			return host.Result{Stdout: []byte("v2.3.4\n")}, nil
		}
	}})
	if code != 0 || stdout != "service: ok (notes v2.3.4 active)\n" || stderr != "" || len(restartCommands) != 3 {
		t.Fatalf("restart workflow = exit %d stdout %q stderr %q commands %#v", code, stdout, stderr, restartCommands)
	}
}

func TestUninstallRejectsHostNameThatNormalizesEmpty(t *testing.T) {
	// R-X4BC-FMS3
	root := uninstallCommandRoot(t, true)
	if err := (config.Store{Root: root}).Set("host.name", "."); err != nil {
		t.Fatal(err)
	}
	executed := false
	stdout, stderr, code := invoke([]string{"uninstall", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
		executed = true
		return host.Result{}, nil
	}})
	if code != 1 || stdout != "" || stderr != "opsctl: host.name not set\n" || executed {
		t.Fatalf("uninstall = exit %d stdout %q stderr %q executed %t", code, stdout, stderr, executed)
	}
}

func TestUninstallRejectsApexReadFailureBeforeEffects(t *testing.T) {
	// R-X4BC-FMS3
	root := uninstallCommandRoot(t, true)
	configFile := filepath.Join(root, "etc/ikigenba/config.json")
	if err := os.Remove(configFile); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(configFile, 0o600); err != nil {
		t.Fatal(err)
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = filesystem.Close() })
	writeDone := make(chan error, 1)
	go func() {
		for _, contents := range []string{`{"host.name":"example.test"}`, `{`} {
			file, err := filesystem.OpenFile("etc/ikigenba/config.json", os.O_WRONLY, 0)
			if err == nil {
				_, err = file.WriteString(contents)
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
			}
			if err != nil {
				writeDone <- err
				return
			}
		}
		writeDone <- nil
	}()

	before := snapshotUninstallPaths(t, root, "opt/notes", "etc/systemd/system/ikigenba-notes.service", "etc/nginx/conf.d/ikigenba.conf", "etc/litestream.yml")
	executed := false
	stdout, stderr, code := invoke([]string{"uninstall", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
		executed = true
		return host.Result{}, nil
	}})
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	wantStderr := "opsctl: " + configFile + " is corrupt\n"
	if code != 1 || stdout != "" || stderr != wantStderr || executed {
		t.Fatalf("uninstall = exit %d stdout %q stderr %q executed %t", code, stdout, stderr, executed)
	}
	after := snapshotUninstallPaths(t, root, "opt/notes", "etc/systemd/system/ikigenba-notes.service", "etc/nginx/conf.d/ikigenba.conf", "etc/litestream.yml")
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("host state changed:\nbefore %#v\nafter  %#v", before, after)
	}
}

func TestUninstallNormalizesHostAndPreservesApexConfiguration(t *testing.T) {
	// R-X4BC-FMS3 R-X6R5-769H R-JE5P-1F05
	root := uninstallCommandRoot(t, true)
	store := config.Store{Root: root}
	if err := store.Set("host.name", "SBX.Example.Test."); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("host.apex", "notes"); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := invoke([]string{"uninstall", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}) {
			return host.Result{Stdout: []byte("inactive\n"), ExitCode: 3}, nil
		}
		return host.Result{}, nil
	}})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "nginx: ok (notes.sbx.example.test, sbx.example.test, example.test removed)\n") {
		t.Fatalf("uninstall = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if got, err := store.Get("host.apex"); err != nil || got != "notes" {
		t.Fatalf("host.apex = %q, %v", got, err)
	}
	configuration := readUninstallFile(t, root, "etc/nginx/conf.d/ikigenba.conf")
	apexFallback := false
	for _, block := range strings.Split(configuration, "server {") {
		if strings.Contains(block, "example.test;") && strings.Contains(block, "return              404;") {
			apexFallback = true
		}
	}
	if !apexFallback {
		t.Fatalf("apex fallback was not rendered: %q", configuration)
	}
}

func TestUninstallRejectsConfiguredApexWithoutParentBeforeEffects(t *testing.T) {
	// R-X5J8-TEIS
	root := uninstallCommandRoot(t, true)
	store := config.Store{Root: root}
	if err := store.Set("host.name", "EXAMPLE.TEST."); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("host.apex", "notes"); err != nil {
		t.Fatal(err)
	}
	before := snapshotUninstallPaths(t, root, "opt/notes", "etc/systemd/system/ikigenba-notes.service", "etc/nginx/conf.d/ikigenba.conf", "etc/litestream.yml")
	executed := false
	stdout, stderr, code := invoke([]string{"uninstall", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
		executed = true
		return host.Result{}, nil
	}})
	if code != 1 || stdout != "" || stderr != "opsctl: host.apex is set but host.name 'example.test' has no parent domain\n" || executed {
		t.Fatalf("uninstall = exit %d stdout %q stderr %q executed %t", code, stdout, stderr, executed)
	}
	after := snapshotUninstallPaths(t, root, "opt/notes", "etc/systemd/system/ikigenba-notes.service", "etc/nginx/conf.d/ikigenba.conf", "etc/litestream.yml")
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("host state changed:\nbefore %#v\nafter  %#v", before, after)
	}
	if got, err := store.Get("host.apex"); err != nil || got != "notes" {
		t.Fatalf("host.apex = %q, %v", got, err)
	}
}

func TestLifecycleFailureReportsStageOnceAndRetainsCause(t *testing.T) {
	// R-ETFY-8BTG R-EVVQ-ZVAU
	root := uninstallCommandRoot(t, true)
	var commands []host.Command
	execute := func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		if command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}) {
			return host.Result{Stdout: []byte("active\n")}, nil
		}
		if command.Name == "nginx" {
			return host.Result{ExitCode: 7, Stderr: []byte("bad line one\nbad line two\n")}, nil
		}
		return host.Result{}, nil
	}
	stdout, stderr, code := invoke([]string{"uninstall", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: execute})
	wantOutput := "stop: ok (ikigenba-notes.service stopped, disabled)\n" +
		"unit: ok (removed ikigenba-notes.service)\n" +
		"files: ok (removed /opt/notes/bin, etc, share, cache; kept state)\n" +
		"nginx: failed: nginx -t: exit status 7\n"
	if code != 1 || stdout != wantOutput || stderr != "opsctl: uninstall failed\n\n> bad line one\n> bad line two\n" {
		t.Fatalf("failure = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	for _, command := range commands {
		if reflect.DeepEqual(command.Args, []string{"reload-or-restart", "nginx"}) || reflect.DeepEqual(command.Args, []string{"restart", "litestream.service"}) {
			t.Fatalf("later stage command ran: %#v", command)
		}
	}

	restartRoot := cliRestartRoot(t)
	restartOut, restartErr, restartCode := invoke([]string{"restart", "notes"}, cli.Deps{Root: restartRoot, EUID: 0, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name == "journalctl" {
			return host.Result{Stdout: []byte("startup detail\n")}, nil
		}
		return host.Result{ExitCode: 7, Stderr: []byte("restart rejected\n")}, nil
	}})
	if restartCode != 1 || restartOut != "service: failed: notes: service failed to start\n" ||
		restartErr != "opsctl: restart failed\n\n> startup detail\n" {
		t.Fatalf("restart failure = exit %d stdout %q stderr %q", restartCode, restartOut, restartErr)
	}
}

func TestUninstallActionFailuresStopAtOwningStage(t *testing.T) {
	// R-ETFY-8BTG R-EVVQ-ZVAU R-EX3N-DN1J
	tests := []struct {
		stage     string
		wantSteps []string
	}{
		{stage: "stop", wantSteps: []string{"stop: failed: "}},
		{stage: "unit", wantSteps: []string{"stop: ok (", "unit: failed: "}},
		{stage: "files", wantSteps: []string{"stop: ok (", "unit: ok (", "files: failed: "}},
		{stage: "nginx", wantSteps: []string{"stop: ok (", "unit: ok (", "files: ok (", "nginx: failed: "}},
		{stage: "litestream", wantSteps: []string{"stop: ok (", "unit: ok (", "files: ok (", "nginx: ok (", "litestream: failed: "}},
	}
	for _, test := range tests {
		t.Run(test.stage, func(t *testing.T) {
			root := uninstallCommandRoot(t, true)
			var commands []host.Command
			execute := func(_ context.Context, command host.Command) (host.Result, error) {
				commands = append(commands, command)
				switch {
				case reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}):
					return host.Result{Stdout: []byte("active\n")}, nil
				case test.stage == "stop" && reflect.DeepEqual(command.Args, []string{"stop", "ikigenba-notes.service"}):
					return host.Result{}, errors.New("stop transport\r\nfailed")
				case test.stage == "unit" && reflect.DeepEqual(command.Args, []string{"daemon-reload"}):
					return host.Result{}, errors.New("reload transport failed")
				case test.stage == "files" && reflect.DeepEqual(command.Args, []string{"daemon-reload"}):
					if err := os.RemoveAll(root); err != nil {
						t.Fatal(err)
					}
					return host.Result{}, nil
				case test.stage == "nginx" && command.Name == "nginx":
					return host.Result{}, errors.New("nginx transport failed")
				case test.stage == "litestream" && reflect.DeepEqual(command.Args, []string{"restart", "litestream.service"}):
					return host.Result{}, errors.New("litestream transport failed")
				default:
					return host.Result{}, nil
				}
			}

			stdout, stderr, code := invoke([]string{"uninstall", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: execute})
			lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
			if code != 1 || stderr != "opsctl: uninstall failed\n" || len(lines) != len(test.wantSteps) {
				t.Fatalf("%s failure = exit %d stdout %q stderr %q commands %#v", test.stage, code, stdout, stderr, commands)
			}
			for index, prefix := range test.wantSteps {
				if !strings.HasPrefix(lines[index], prefix) {
					t.Fatalf("outcome[%d] = %q, want prefix %q", index, lines[index], prefix)
				}
			}
			if strings.Contains(stdout, "\r") || strings.Count(stdout, "\n") != len(test.wantSteps) {
				t.Fatalf("unsafe or extra report line in %q", stdout)
			}
			if test.stage == "stop" && !strings.Contains(stdout, `stop transport\r\nfailed`) {
				t.Fatalf("CR/LF detail was not escaped: %q", stdout)
			}
			assertNoLifecycleCommandsAfter(t, test.stage, commands)
		})
	}
}

func TestUninstallReportWriteFailuresAreNotRetried(t *testing.T) {
	// R-ETFY-8BTG R-EVVQ-ZVAU
	for _, stage := range []string{"stop", "unit", "files", "nginx", "litestream"} {
		t.Run(stage, func(t *testing.T) {
			root := uninstallCommandRoot(t, true)
			output := &failStepWriter{failStep: stage, err: errors.New("report output unavailable")}
			var diagnostic bytes.Buffer
			var commands []host.Command
			code := cli.Run([]string{"uninstall", "notes"}, nil, output, &diagnostic, cli.Deps{
				Root: root,
				EUID: 0,
				Execute: func(_ context.Context, command host.Command) (host.Result, error) {
					commands = append(commands, command)
					if reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}) {
						return host.Result{Stdout: []byte("active\n")}, nil
					}
					return host.Result{}, nil
				},
			})
			if code != 1 || output.failures != 1 || output.writesAfterFailure != 0 || diagnostic.String() != "opsctl: uninstall failed\n" {
				t.Fatalf("%s report failure = exit %d failures %d later writes %d stderr %q commands %#v", stage, code, output.failures, output.writesAfterFailure, diagnostic.String(), commands)
			}
			assertNoLifecycleCommandsAfter(t, stage, commands)
			switch stage {
			case "stop", "unit":
				if _, err := os.Stat(filepath.Join(root, "opt/notes/bin/notes")); err != nil {
					t.Fatalf("later files effect ran after %s report failure: %v", stage, err)
				}
			case "files":
				if got := readUninstallFile(t, root, "etc/nginx/conf.d/ikigenba.conf"); got != "old nginx\n" {
					t.Fatalf("nginx changed after files report failure: %q", got)
				}
			case "nginx":
				if got := readUninstallFile(t, root, "etc/litestream.yml"); got != "old litestream\n" {
					t.Fatalf("litestream changed after nginx report failure: %q", got)
				}
			}
		})
	}
}

func assertNoLifecycleCommandsAfter(t *testing.T, stage string, commands []host.Command) {
	t.Helper()
	for _, command := range commands {
		if stage == "stop" && reflect.DeepEqual(command.Args, []string{"daemon-reload"}) {
			t.Fatalf("unit stage ran after stop failure: %#v", commands)
		}
		if (stage == "stop" || stage == "unit" || stage == "files") && command.Name == "nginx" {
			t.Fatalf("nginx stage ran after %s failure: %#v", stage, commands)
		}
		if stage != "litestream" && reflect.DeepEqual(command.Args, []string{"restart", "litestream.service"}) {
			t.Fatalf("litestream stage ran after %s failure: %#v", stage, commands)
		}
	}
}

func TestUninstallWithoutStateDoesNotCreateDiscoverableService(t *testing.T) {
	// R-JE5P-1F05
	root := uninstallCommandRoot(t, false)
	stdout, stderr, code := invoke([]string{"uninstall", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		if reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}) {
			return host.Result{Stdout: []byte("inactive\n"), ExitCode: 3}, nil
		}
		return host.Result{}, nil
	}})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "litestream: ok") {
		t.Fatalf("uninstall = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(root, "opt/notes/state")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state directory created: %v", err)
	}
	services, err := apps.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range services {
		if service.Name == "notes" {
			t.Fatalf("payload-only app directory remained discoverable: %#v", service)
		}
	}
}

func uninstallCommandRoot(t *testing.T, state bool) string {
	t.Helper()
	root := t.TempDir()
	setUninstallConfig(t, root)
	notesManifest := "app = \"notes\"\nport = 8080\ndefault = true\n[database]\nengine = \"sqlite\"\npath = \"state/notes.db\"\n"
	tasksManifest := "app = \"tasks\"\nport = 9090\n[database]\nengine = \"sqlite\"\npath = \"state/tasks.db\"\n"
	writeUninstallFile(t, root, "opt/notes/bin/notes", "notes binary")
	writeUninstallFile(t, root, "opt/notes/etc/manifest.toml", notesManifest)
	writeUninstallFile(t, root, "opt/notes/share/asset", "notes asset")
	writeUninstallFile(t, root, "opt/notes/cache/item", "notes cache")
	writeUninstallFile(t, root, "etc/systemd/system/ikigenba-notes.service", "notes unit")
	if state {
		writeUninstallFile(t, root, "opt/notes/state/notes.db", "last committed transaction")
		writeUninstallFile(t, root, "opt/notes/state/notes.db-wal", "wal")
		writeUninstallFile(t, root, "opt/notes/state/notes.db-shm", "shm")
		writeUninstallFile(t, root, "opt/notes/state/.notes.db-litestream/meta", "metadata")
		writeUninstallFile(t, root, "opt/notes/state/private/blob", "private retained state")
	}
	writeUninstallFile(t, root, "opt/tasks/bin/tasks", "other binary")
	writeUninstallFile(t, root, "opt/tasks/etc/manifest.toml", tasksManifest)
	writeUninstallFile(t, root, "opt/tasks/state/tasks.db", "other database")
	writeUninstallFile(t, root, "etc/systemd/system/ikigenba-tasks.service", "other unit")
	writeUninstallFile(t, root, "etc/nginx/conf.d/ikigenba.conf", "old nginx\n")
	writeUninstallFile(t, root, "etc/litestream.yml", "old litestream\n")
	writeUninstallFile(t, root, "remote/parameters/notes", "owned by devctl")
	writeUninstallFile(t, root, "remote/releases/notes.tar.xz", "deployment artifact")
	return root
}

func setUninstallConfig(t *testing.T, root string) {
	t.Helper()
	store := config.Store{Root: root}
	for key, value := range map[string]string{
		"host.name":                  "example.test",
		"aws.region":                 "us-west-2",
		"backup.s3_uri":              "s3://backups/services/",
		"backup.service_db_seconds":  "3600",
		"backup.service_wal_seconds": "5",
	} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
}

func writeUninstallFile(t *testing.T, root, name, contents string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readUninstallFile(t *testing.T, root, name string) string {
	t.Helper()
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	data, err := filesystem.ReadFile(filepath.ToSlash(name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func snapshotUninstallPaths(t *testing.T, root string, names ...string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, name := range names {
		full := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(full)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			result[name] = readUninstallFile(t, root, name)
			continue
		}
		if err := filepath.WalkDir(full, func(pathname string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(root, pathname)
			if err != nil {
				return err
			}
			result[filepath.ToSlash(relative)] = readUninstallFile(t, root, relative)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

type failStepWriter struct {
	bytes.Buffer
	failStep           string
	err                error
	failed             bool
	failures           int
	writesAfterFailure int
}

func (writer *failStepWriter) Write(data []byte) (int, error) {
	if bytes.HasPrefix(data, []byte(writer.failStep+":")) {
		writer.failed = true
		writer.failures++
		return 0, writer.err
	}
	if writer.failed {
		writer.writesAfterFailure++
	}
	return writer.Buffer.Write(data)
}
