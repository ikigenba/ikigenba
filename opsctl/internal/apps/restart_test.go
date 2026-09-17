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

func TestRestartAPISignature(t *testing.T) {
	// R-LPVF-5CHC
	want := reflect.TypeFor[func(context.Context, host.Env, string) (apps.StatusRow, error)]()
	if got := reflect.TypeOf(apps.Restart); got != want {
		t.Fatalf("Restart type = %v, want %v", got, want)
	}
}

func TestRestartRejectsInvalidAndUninstalledAppsBeforeExecution(t *testing.T) {
	for _, test := range []struct {
		name    string
		app     string
		prepare func(string) string
		code    int
		message string
	}{
		{name: "invalid", app: "bad/name", prepare: func(root string) string { return filepath.Join(root, "absent") }, code: 2, message: "'bad/name' is not a usable app name"},
		{name: "missing", app: "notes", prepare: func(root string) string { return root }, code: 1, message: "no service 'notes'"},
		{name: "no binary", app: "notes", prepare: func(root string) string {
			if err := os.MkdirAll(filepath.Join(root, "opt", "notes", "etc"), 0o750); err != nil {
				t.Fatal(err)
			}
			return root
		}, code: 1, message: "notes is not installed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := test.prepare(t.TempDir())
			executed := false
			_, err := apps.Restart(context.Background(), host.Env{Root: root, Execute: func(context.Context, host.Command) (host.Result, error) {
				executed = true
				return host.Result{}, nil
			}}, test.app)
			var failure *apps.LifecycleError
			if !errors.As(err, &failure) || failure.Code != test.code || failure.Message != test.message || executed {
				t.Fatalf("Restart = (%#v, executed %t), want code %d message %q without execution", err, executed, test.code, test.message)
			}
		})
	}
}

func TestRestartUsesInstalledUnitAndBinaryWithoutChangingHostFiles(t *testing.T) {
	// R-MALP-NG35
	// R-MBTM-17TU R-AMEV-4J2G
	for _, initialState := range []string{"active", "inactive", "failed"} {
		t.Run(initialState, func(t *testing.T) {
			root := restartRoot(t)
			before := readRestartTree(t, root)
			service := &restartServiceModel{state: initialState, resultingState: "active"}
			env := host.Env{Root: root, Execute: service.execute(root)}

			row, err := apps.Restart(context.Background(), env, "notes")
			wantRow := apps.StatusRow{Name: "notes", Version: "v2.4.6", State: "active", JournalMode: "-"}
			if err != nil || !reflect.DeepEqual(row, wantRow) {
				t.Fatalf("Restart = (%#v, %v), want (%#v, nil)", row, err, wantRow)
			}
			if service.restartObservedState != initialState {
				t.Fatalf("restart observed initial state %q, want %q", service.restartObservedState, initialState)
			}
			wantCommands := []host.Command{
				{Name: "systemctl", Args: []string{"restart", "ikigenba-notes.service"}},
				{Name: "systemctl", Args: []string{"is-active", "ikigenba-notes.service"}},
				{Name: filepath.Join(root, "opt", "notes", "bin", "notes"), Args: []string{"--version"}},
			}
			if !reflect.DeepEqual(service.commands, wantCommands) {
				t.Fatalf("commands = %#v, want %#v", service.commands, wantCommands)
			}
			if after := readRestartTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("host files changed:\nbefore %#v\nafter  %#v", before, after)
			}
		})
	}
}

func TestRestartChecksResultingStateAfterSuccessfulRestart(t *testing.T) {
	// R-MALP-NG35
	// R-ME9E-SRB8
	for _, resultingState := range []string{"active", "inactive", "failed"} {
		t.Run(resultingState, func(t *testing.T) {
			root := restartRoot(t)
			before := readRestartTree(t, root)
			service := &restartServiceModel{state: "active", resultingState: resultingState}

			row, err := apps.Restart(context.Background(), host.Env{Root: root, Execute: service.execute(root)}, "notes")
			wantCommands := []host.Command{
				{Name: "systemctl", Args: []string{"restart", "ikigenba-notes.service"}},
				{Name: "systemctl", Args: []string{"is-active", "ikigenba-notes.service"}},
			}
			if resultingState == "active" {
				wantRow := apps.StatusRow{Name: "notes", Version: "v2.4.6", State: "active", JournalMode: "-"}
				if err != nil || !reflect.DeepEqual(row, wantRow) {
					t.Fatalf("Restart = (%#v, %v), want (%#v, nil)", row, err, wantRow)
				}
				wantCommands = append(wantCommands, host.Command{
					Name: filepath.Join(root, "opt", "notes", "bin", "notes"), Args: []string{"--version"},
				})
			} else {
				var failure *apps.LifecycleError
				var journal *host.CommandError
				if !reflect.DeepEqual(row, apps.StatusRow{}) || !errors.As(err, &failure) || failure.Code != 1 ||
					failure.Message != "notes: service failed to start" || !errors.As(err, &journal) ||
					journal.Label != "journal captured" ||
					string(journal.Result.Stdout) != "journal for "+resultingState+"\n" {
					t.Fatalf("Restart = (%#v, %#v), journal = %#v", row, err, journal)
				}
				var inspection *host.CommandError
				if !errors.As(journal.Err, &inspection) || inspection.Label != "inspect ikigenba-notes.service" ||
					inspection.Result.ExitCode != 3 || string(inspection.Result.Stdout) != resultingState+"\n" {
					t.Fatalf("state inspection failure = %#v", journal.Err)
				}
				wantCommands = append(wantCommands, host.Command{
					Name: "journalctl", Args: []string{"--unit", "ikigenba-notes.service", "--no-pager", "--lines", "50"},
				})
				if resultingState == "failed" {
					rows, statusErr := apps.Status(context.Background(), host.Env{Root: root, Execute: service.execute(root)})
					wantRows := []apps.StatusRow{
						{Name: "notes", Version: "v2.4.6", State: "failed", JournalMode: "-"},
						{Name: "other", Version: "-", State: "-", JournalMode: "-"},
					}
					if statusErr != nil || !reflect.DeepEqual(rows, wantRows) {
						t.Fatalf("Status after resulting failure = (%#v, %v), want (%#v, nil)", rows, statusErr, wantRows)
					}
					wantCommands = append(wantCommands,
						host.Command{Name: filepath.Join(root, "opt", "notes", "bin", "notes"), Args: []string{"--version"}},
						host.Command{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=ActiveState", "ikigenba-notes.service"}},
						host.Command{Name: filepath.Join(root, "opt", "other", "bin", "other"), Args: []string{"--version"}},
						host.Command{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=ActiveState", "ikigenba-other.service"}},
					)
				}
			}
			if service.state != resultingState {
				t.Fatalf("service state = %q, want %q", service.state, resultingState)
			}
			if !reflect.DeepEqual(service.commands, wantCommands) {
				t.Fatalf("commands = %#v, want %#v", service.commands, wantCommands)
			}
			if after := readRestartTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("host files changed:\nbefore %#v\nafter  %#v", before, after)
			}
		})
	}
}

func TestRestartFailureCapturesJournalAndLeavesFailedStateVisible(t *testing.T) {
	// R-ME9E-SRB8
	root := restartRoot(t)
	restartFailure := errors.New("restart transport failed")
	var commands []host.Command
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		commands = append(commands, command)
		switch {
		case reflect.DeepEqual(command.Args, []string{"restart", "ikigenba-notes.service"}):
			return host.Result{}, restartFailure
		case command.Name == "journalctl":
			return host.Result{Stdout: []byte("startup line one\nstartup line two\n")}, nil
		case command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] == "show":
			return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=failed\n")}, nil
		case filepath.Base(command.Name) == "notes":
			return host.Result{Stdout: []byte("v2\n")}, nil
		default:
			return host.Result{}, errors.New("unexpected command")
		}
	}}

	_, err := apps.Restart(context.Background(), env, "notes")
	var failure *apps.LifecycleError
	var journal *host.CommandError
	if !errors.As(err, &failure) || failure.Code != 1 || failure.Message != "notes: service failed to start" ||
		!errors.Is(err, restartFailure) || !errors.As(err, &journal) || string(journal.Result.Stdout) != "startup line one\nstartup line two\n" {
		t.Fatalf("Restart failure = %#v, journal = %#v", err, journal)
	}
	rows, statusErr := apps.Status(context.Background(), env)
	var notesState string
	for _, row := range rows {
		if row.Name == "notes" {
			notesState = row.State
		}
	}
	if statusErr != nil || notesState != "failed" {
		t.Fatalf("Status after failure = (%#v, %v), want failed visible", rows, statusErr)
	}
	wantCommands := []host.Command{
		{Name: "systemctl", Args: []string{"restart", "ikigenba-notes.service"}},
		{Name: "journalctl", Args: []string{"--unit", "ikigenba-notes.service", "--no-pager", "--lines", "50"}},
		{Name: filepath.Join(root, "opt", "notes", "bin", "notes"), Args: []string{"--version"}},
		{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=ActiveState", "ikigenba-notes.service"}},
		{Name: filepath.Join(root, "opt", "other", "bin", "other"), Args: []string{"--version"}},
		{Name: "systemctl", Args: []string{"show", "--property=LoadState", "--property=ActiveState", "ikigenba-other.service"}},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
}

func TestRestartFailurePreservesJournalQueryFailure(t *testing.T) {
	// R-GWME-QK2L
	// R-ME9E-SRB8
	root := restartRoot(t)
	restartFailure := errors.New("restart transport failed")
	journalFailure := errors.New("journal transport failed")
	call := 0
	_, err := apps.Restart(context.Background(), host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		call++
		if command.Name == "journalctl" {
			return host.Result{}, journalFailure
		}
		return host.Result{}, restartFailure
	}}, "notes")
	var failure *apps.LifecycleError
	if call != 2 || !errors.As(err, &failure) || !errors.Is(err, restartFailure) || !errors.Is(err, journalFailure) ||
		!strings.Contains(failure.Cause.Error(), "restart ikigenba-notes.service") ||
		!strings.Contains(failure.Cause.Error(), "obtain ikigenba-notes.service journal") {
		t.Fatalf("failure = %#v, cause = %q, calls = %d", err, failure.Cause, call)
	}
}

type restartServiceModel struct {
	state                string
	resultingState       string
	restartObservedState string
	commands             []host.Command
}

func (service *restartServiceModel) execute(root string) func(context.Context, host.Command) (host.Result, error) {
	return func(_ context.Context, command host.Command) (host.Result, error) {
		service.commands = append(service.commands, command)
		switch {
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"restart", "ikigenba-notes.service"}):
			service.restartObservedState = service.state
			service.state = service.resultingState
			return host.Result{}, nil
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"is-active", "ikigenba-notes.service"}):
			exitCode := 3
			if service.state == "active" {
				exitCode = 0
			}
			return host.Result{Stdout: []byte(service.state + "\n"), ExitCode: exitCode}, nil
		case command.Name == "journalctl" && reflect.DeepEqual(command.Args, []string{"--unit", "ikigenba-notes.service", "--no-pager", "--lines", "50"}):
			return host.Result{Stdout: []byte("journal for " + service.state + "\n")}, nil
		case command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"show", "--property=LoadState", "--property=ActiveState", "ikigenba-notes.service"}):
			return host.Result{Stdout: []byte("LoadState=loaded\nActiveState=" + service.state + "\n")}, nil
		case command.Name == filepath.Join(root, "opt", "notes", "bin", "notes") && reflect.DeepEqual(command.Args, []string{"--version"}):
			return host.Result{Stdout: []byte("v2.4.6\r\n")}, nil
		default:
			return host.Result{}, errors.New("unexpected command")
		}
	}
}

func restartRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"opt/notes/bin/notes":                       "installed binary",
		"opt/notes/etc/env":                         "TOKEN=unchanged\n",
		"opt/notes/etc/manifest.toml":               "app = \"notes\"\nport = 8080\n",
		"opt/other/etc/env":                         "OTHER=unchanged\n",
		"etc/systemd/system/ikigenba-notes.service": "EnvironmentFile=/opt/notes/etc/env\n",
		"etc/systemd/system/ikigenba-other.service": "EnvironmentFile=/opt/other/etc/env\n",
	}
	for name, contents := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func readRestartTree(t *testing.T, root string) map[string]string {
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
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		data, err := filesystem.ReadFile(relative)
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
