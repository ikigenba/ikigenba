package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestRestartCommandReportsResultingServiceState(t *testing.T) {
	// R-VO40-PTVC
	for _, test := range []struct {
		state      string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{state: "active", wantCode: 0, wantStdout: "service: ok (notes v7.8.9 active)\n"},
		{state: "inactive", wantCode: 1, wantStdout: "service: failed: notes: service failed to start\n", wantStderr: "opsctl: restart failed\n\n> journal for inactive\n"},
		{state: "failed", wantCode: 1, wantStdout: "service: failed: notes: service failed to start\n", wantStderr: "opsctl: restart failed\n\n> journal for failed\n"},
	} {
		t.Run(test.state, func(t *testing.T) {
			root := cliRestartRoot(t)
			state := "active"
			var commands []host.Command
			execute := func(_ context.Context, command host.Command) (host.Result, error) {
				commands = append(commands, command)
				switch {
				case command.Name == "systemctl" && command.Args[0] == "show":
					return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
				case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"restart", "ikigenba-notes.service"}):
					state = test.state
					return host.Result{}, nil
				case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}):
					exitCode := 3
					if state == "active" {
						exitCode = 0
					}
					return host.Result{Stdout: []byte(state + "\n"), ExitCode: exitCode}, nil
				case command.Name == "journalctl" && reflect.DeepEqual(command.Args, []string{"--unit", "ikigenba-notes.service", "--no-pager", "--lines", "50"}):
					return host.Result{Stdout: []byte("journal for " + state + "\n")}, nil
				case command.Name == filepath.Join(root, "opt", "notes", "bin", "notes") && reflect.DeepEqual(command.Args, []string{"--version"}):
					return host.Result{Stdout: []byte("v7.8.9\n")}, nil
				default:
					return host.Result{}, errors.New("unexpected command")
				}
			}

			stdout, stderr, code := invoke([]string{"restart", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: execute})
			if code != test.wantCode || stdout != test.wantStdout || stderr != test.wantStderr {
				t.Fatalf("restart = exit %d stdout %q stderr %q", code, stdout, stderr)
			}
			wantCommands := []host.Command{
				{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}},
				{Name: "systemctl", Args: []string{"restart", "ikigenba-notes.service"}},
				{Name: "systemctl", Args: []string{"is-active", "ikigenba-notes.service"}},
			}
			if test.state == "active" {
				wantCommands = append(wantCommands, host.Command{Name: filepath.Join(root, "opt", "notes", "bin", "notes"), Args: []string{"--version"}})
			} else {
				wantCommands = append(wantCommands, host.Command{Name: "journalctl", Args: []string{"--unit", "ikigenba-notes.service", "--no-pager", "--lines", "50"}})
			}
			if !reflect.DeepEqual(commands, wantCommands) {
				t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
			}
			if state != test.state {
				t.Fatalf("service state = %q, want %q", state, test.state)
			}
		})
	}
}

func TestRestartCommandReportsFailureOnceWithStartupJournal(t *testing.T) {
	// R-VO40-PTVC
	root := cliRestartRoot(t)
	var commands []host.Command
	execute := func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		if command.Name == "systemctl" && command.Args[0] == "show" {
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
		}
		if command.Name == "journalctl" {
			return host.Result{Stdout: []byte("line one\nline two\n")}, nil
		}
		return host.Result{ExitCode: 7, Stderr: []byte("restart rejected\n")}, nil
	}
	stdout, stderr, code := invoke([]string{"restart", "notes"}, cli.Deps{Root: root, EUID: 0, Execute: execute})
	if code != 1 || stdout != "service: failed: notes: service failed to start\n" ||
		stderr != "opsctl: restart failed\n\n> line one\n> line two\n" {
		t.Fatalf("restart failure = exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	want := []host.Command{
		{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=UnitFileState", "ikigenba-notes.socket"}},
		{Name: "systemctl", Args: []string{"restart", "ikigenba-notes.service"}},
		{Name: "journalctl", Args: []string{"--unit", "ikigenba-notes.service", "--no-pager", "--lines", "50"}},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestRestartReportsDisabledServiceWithoutStartingIt(t *testing.T) {
	// R-VO40-PTVC
	root := cliRestartRoot(t)
	var commands []host.Command
	stdout, stderr, code := invoke([]string{"restart", "notes"}, cli.Deps{Root: root, EUID: 0,
		Execute: func(_ context.Context, command host.Command) (host.Result, error) {
			commands = append(commands, command)
			if command.Name == "systemctl" && command.Args[0] == "show" {
				return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=disabled\n")}, nil
			}
			if command.Name == filepath.Join(root, "opt/notes/bin/notes") {
				return host.Result{Stdout: []byte("v7.8.9\n")}, nil
			}
			t.Fatalf("unexpected command: %#v", command)
			return host.Result{}, nil
		}})
	if code != 0 || stdout != "service: ok (notes v7.8.9 disabled)\n" || stderr != "" || len(commands) != 2 {
		t.Fatalf("disabled restart = exit %d stdout %q stderr %q commands %#v", code, stdout, stderr, commands)
	}
}

func TestRestartCommandValidationPrecedesServiceStage(t *testing.T) {
	for _, test := range []struct {
		name string
		app  string
		root func(*testing.T) string
		want string
		code int
	}{
		{name: "invalid", app: "bad/name", root: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }, want: "opsctl: 'bad/name' is not a usable app name\n", code: 2},
		{name: "missing", app: "notes", root: func(t *testing.T) string { return t.TempDir() }, want: "opsctl: no service 'notes'\n", code: 1},
		{name: "binary missing", app: "notes", root: func(t *testing.T) string {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "opt/notes/etc"), 0o750); err != nil {
				t.Fatal(err)
			}
			return root
		}, want: "opsctl: notes is not installed\n", code: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			executed := false
			stdout, stderr, code := invoke([]string{"restart", test.app}, cli.Deps{Root: test.root(t), EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
				executed = true
				return host.Result{}, nil
			}})
			wantStdout, wantStderr := "", test.want
			if code != test.code || stdout != wantStdout || stderr != wantStderr || executed {
				t.Fatalf("preflight = exit %d stdout %q stderr %q executed %t", code, stdout, stderr, executed)
			}
		})
	}
}

func TestRestartCommandDoesNotRetryFailedOutcomeWrite(t *testing.T) {
	// R-VO40-PTVC
	root := cliRestartRoot(t)
	writeFailure := errors.New("service output unavailable")
	output := &countingFailWriter{err: writeFailure}
	var diagnostic bytes.Buffer
	execute := func(_ context.Context, command host.Command) (host.Result, error) {
		switch {
		case command.Name == "systemctl" && command.Args[0] == "show":
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=enabled\n")}, nil
		case command.Name == "systemctl" && command.Args[0] == "restart":
			return host.Result{}, nil
		case command.Name == "systemctl":
			return host.Result{Stdout: []byte("active\n")}, nil
		default:
			return host.Result{Stdout: []byte("v1\n")}, nil
		}
	}
	code := cli.Run([]string{"restart", "notes"}, nil, output, &diagnostic, cli.Deps{Root: root, EUID: 0, Execute: execute})
	if code != 1 || output.calls != 1 || diagnostic.String() != "opsctl: restart failed\n" {
		t.Fatalf("write failure = exit %d calls %d stderr %q", code, output.calls, diagnostic.String())
	}
}

type countingFailWriter struct {
	calls int
	err   error
}

func (writer *countingFailWriter) Write([]byte) (int, error) {
	writer.calls++
	return 0, writer.err
}

func cliRestartRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "opt", "notes", "bin", "notes")
	if err := os.MkdirAll(filepath.Dir(binary), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}
