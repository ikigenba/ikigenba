package cli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestEnablementConfigurationPrecedesHostEffects(t *testing.T) {
	// R-W3YP-OUID
	for _, action := range []string{"disable", "enable"} {
		t.Run(action+" missing host", func(t *testing.T) {
			root := t.TempDir()
			executed := false
			stdout, stderr, code := invoke([]string{action, "notes"}, cli.Deps{Root: root, EUID: 0,
				Execute: func(context.Context, host.Command) (host.Result, error) {
					executed = true
					return host.Result{}, nil
				}})
			if code != 1 || stdout != "" || stderr != "opsctl: host.name not set\n" || executed {
				t.Fatalf("exit %d stdout %q stderr %q executed %t", code, stdout, stderr, executed)
			}
		})
		t.Run(action+" invalid apex host", func(t *testing.T) {
			root := t.TempDir()
			store := config.Store{Root: root}
			for key, value := range map[string]string{"host.name": "EXAMPLE.TEST.", "host.apex": "notes"} {
				if err := store.Set(key, value); err != nil {
					t.Fatal(err)
				}
			}
			executed := false
			stdout, stderr, code := invoke([]string{action, "notes"}, cli.Deps{Root: root, EUID: 0,
				Execute: func(context.Context, host.Command) (host.Result, error) {
					executed = true
					return host.Result{}, nil
				}})
			if code != 1 || stdout != "" || stderr != "opsctl: host.apex is set but host.name 'example.test' has no parent domain\n" || executed {
				t.Fatalf("exit %d stdout %q stderr %q executed %t", code, stdout, stderr, executed)
			}
		})
		t.Run(action+" corrupt store", func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "etc/ikigenba/config.json")
			if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("not JSON"), 0o600); err != nil {
				t.Fatal(err)
			}
			executed := false
			stdout, stderr, code := invoke([]string{action, "notes"}, cli.Deps{Root: root, EUID: 0,
				Execute: func(context.Context, host.Command) (host.Result, error) {
					executed = true
					return host.Result{}, nil
				}})
			if code != 1 || stdout != "" || !strings.Contains(stderr, "config.json is corrupt") || executed {
				t.Fatalf("exit %d stdout %q stderr %q executed %t", code, stdout, stderr, executed)
			}
		})
	}
}

func TestEnablementFailedStagesStopLaterWork(t *testing.T) {
	// R-W1IW-XB0Z
	for _, test := range []struct {
		action, failCommand, wantLast string
		disabled                      bool
	}{
		{"disable", "stop ikigenba-notes.socket", "stop: failed: ", false},
		{"enable", "enable ikigenba-notes.socket ikigenba-notes.service", "enable: failed: ", true},
		{"enable", "start ikigenba-notes.service", "service: failed: notes: service failed to start", true},
	} {
		t.Run(test.failCommand, func(t *testing.T) {
			root := enablementFailureRoot(t)
			disabled := test.disabled
			var controls []string
			deps := cli.Deps{Root: root, EUID: 0, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				if command.Name == "journalctl" {
					return host.Result{Stdout: []byte("startup detail\n")}, nil
				}
				if command.Name == filepath.Join(root, "opt/notes/bin/notes") {
					return host.Result{Stdout: []byte("v1\n")}, nil
				}
				if command.Name == "nginx" {
					controls = append(controls, "nginx test")
					return host.Result{}, nil
				}
				if command.Name != "systemctl" {
					t.Fatalf("unexpected command %#v", command)
				}
				args := command.Args
				if args[0] == "show" {
					state, active := "enabled", "active"
					if disabled {
						state, active = "disabled", "inactive"
					}
					return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=" + state + "\nActiveState=" + active + "\n")}, nil
				}
				if args[0] == "is-active" {
					return host.Result{Stdout: []byte("active\n")}, nil
				}
				control := strings.Join(args, " ")
				controls = append(controls, control)
				if control == test.failCommand {
					return host.Result{}, errors.New("blocked\r\nnow")
				}
				if args[0] == "enable" {
					disabled = false
				}
				if args[0] == "disable" {
					disabled = true
				}
				return host.Result{}, nil
			}}
			stdout, stderr, code := invoke([]string{test.action, "notes"}, deps)
			lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
			wantLines := 1
			if test.failCommand == "start ikigenba-notes.service" {
				wantLines = 3
			}
			if code != 1 || len(lines) != wantLines || !strings.HasPrefix(lines[len(lines)-1], test.wantLast) ||
				strings.Contains(stdout, "\r") || !strings.HasPrefix(stderr, "opsctl: "+test.action+" failed\n") {
				t.Fatalf("%s = exit %d stdout %q stderr %q controls %#v", test.failCommand, code, stdout, stderr, controls)
			}
			if wantLines == 1 && (!strings.Contains(stdout, `blocked\r\nnow`) || len(controls) != 1) {
				t.Fatalf("failed first stage continued or did not escape detail: %q %#v", stdout, controls)
			}
			if wantLines == 3 && (!strings.HasPrefix(lines[0], "enable: ok (") || !strings.HasPrefix(lines[1], "nginx: ok (") ||
				!strings.Contains(stderr, "\n\n> startup detail\n") || controls[len(controls)-1] != test.failCommand) {
				t.Fatalf("service failure did not preserve completed stages and journal: %q %q %#v", stdout, stderr, controls)
			}
		})
	}
}

func TestEnablementReportWriteFailureOccursOnce(t *testing.T) {
	// R-W1IW-XB0Z
	for _, test := range []struct {
		action, failStep string
		disabled         bool
	}{
		{"disable", "stop", false}, {"disable", "nginx", false},
		{"enable", "enable", true}, {"enable", "nginx", true}, {"enable", "service", true},
	} {
		t.Run(test.action+"/"+test.failStep, func(t *testing.T) {
			root := enablementFailureRoot(t)
			disabled := test.disabled
			var controls []string
			output := &failStepWriter{failStep: test.failStep, err: errors.New("report output unavailable")}
			var diagnostic bytes.Buffer
			code := cli.Run([]string{test.action, "notes"}, nil, output, &diagnostic, cli.Deps{Root: root, EUID: 0,
				Execute: func(_ context.Context, command host.Command) (host.Result, error) {
					if command.Name == filepath.Join(root, "opt/notes/bin/notes") {
						return host.Result{Stdout: []byte("v1\n")}, nil
					}
					if command.Name == "nginx" {
						controls = append(controls, "nginx test")
						return host.Result{}, nil
					}
					if command.Name != "systemctl" {
						t.Fatalf("unexpected command %#v", command)
					}
					args := command.Args
					if args[0] == "show" {
						state, active := "enabled", "active"
						if disabled {
							state, active = "disabled", "inactive"
						}
						return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=" + state + "\nActiveState=" + active + "\n")}, nil
					}
					if args[0] == "is-active" {
						return host.Result{Stdout: []byte("active\n")}, nil
					}
					control := strings.Join(args, " ")
					controls = append(controls, control)
					if args[0] == "enable" {
						disabled = false
					}
					if args[0] == "disable" {
						disabled = true
					}
					return host.Result{}, nil
				}})
			if code != 1 || output.failures != 1 || output.writesAfterFailure != 0 || diagnostic.String() != "opsctl: "+test.action+" failed\n" {
				t.Fatalf("report failure = exit %d failures %d later writes %d stderr %q controls %#v", code, output.failures, output.writesAfterFailure, diagnostic.String(), controls)
			}
			if test.failStep == "stop" || test.failStep == "enable" {
				for _, control := range controls {
					if control == "nginx test" {
						t.Fatalf("nginx ran after %s report failed", test.failStep)
					}
				}
			}
			if test.failStep == "nginx" {
				for _, control := range controls {
					if control == "start ikigenba-notes.service" {
						t.Fatal("service started after nginx report failed")
					}
				}
			}
		})
	}
}

func enablementFailureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := (config.Store{Root: root}).Set("host.name", "sbx.example.test"); err != nil {
		t.Fatal(err)
	}
	for path, value := range map[string]string{
		"opt/notes/bin/notes":                       "binary",
		"opt/notes/etc/manifest.toml":               "app = \"notes\"\n",
		"etc/systemd/system/ikigenba-notes.socket":  "socket",
		"etc/systemd/system/ikigenba-notes.service": "service",
	} {
		writeUninstallFile(t, root, path, value)
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/nginx/conf.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestEnablementStagesAndIdempotence(t *testing.T) {
	// R-VZ34-5RJL R-W1IW-XB0Z R-W56M-2M92
	root := t.TempDir()
	store := config.Store{Root: root}
	for key, value := range map[string]string{"host.name": "SBX.Example.Test.", "host.apex": "notes"} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	for path, value := range map[string]string{
		"opt/notes/bin/notes":                       "binary",
		"opt/notes/etc/manifest.toml":               "app = \"notes\"\ndefault = true\n",
		"opt/notes/state/data":                      "preserved app data",
		"opt/tasks/state/data":                      "preserved sibling data",
		"etc/systemd/system/ikigenba-notes.socket":  "socket",
		"etc/systemd/system/ikigenba-notes.service": "service",
		"etc/systemd/system/ikigenba-tasks.socket":  "sibling socket",
		"etc/systemd/system/ikigenba-tasks.service": "sibling service",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/nginx/conf.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "etc/nginx/conf.d/ikigenba.conf")
	if err := os.WriteFile(file, []byte("old config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	preservedPaths := []string{"etc/ikigenba/config.json", "opt/notes", "opt/tasks", "etc/systemd/system/ikigenba-tasks.socket", "etc/systemd/system/ikigenba-tasks.service"}
	preservedBefore := snapshotUninstallPaths(t, root, preservedPaths...)
	preservedFiles := []string{"etc/ikigenba/config.json", "opt/notes/bin/notes", "opt/notes/etc/manifest.toml", "opt/notes/state/data", "opt/tasks/state/data", "etc/systemd/system/ikigenba-tasks.socket", "etc/systemd/system/ikigenba-tasks.service"}
	preservedInfo := make(map[string]os.FileInfo, len(preservedFiles))
	for _, path := range preservedFiles {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		preservedInfo[path] = info
	}

	disabled, socketActive, serviceActive := false, true, true
	var controls []string
	execute := func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name == filepath.Join(root, "opt/notes/bin/notes") && reflect.DeepEqual(command.Args, []string{"--version"}) {
			return host.Result{Stdout: []byte("v2.3.4\n")}, nil
		}
		if command.Name == "nginx" {
			controls = append(controls, "nginx test")
			return host.Result{}, nil
		}
		if command.Name != "systemctl" {
			t.Fatalf("unexpected command: %#v", command)
		}
		args := command.Args
		if len(args) > 0 && args[0] == "show" {
			fileState := "enabled"
			if disabled {
				fileState = "disabled"
			}
			active := "inactive"
			if strings.HasSuffix(args[len(args)-1], ".socket") && socketActive || strings.HasSuffix(args[len(args)-1], ".service") && serviceActive {
				active = "active"
			}
			return host.Result{Stdout: []byte("LoadState=loaded\nUnitFileState=" + fileState + "\nActiveState=" + active + "\n")}, nil
		}
		if len(args) > 0 && args[0] == "is-active" {
			if serviceActive {
				return host.Result{Stdout: []byte("active\n")}, nil
			}
			return host.Result{Stdout: []byte("inactive\n"), ExitCode: 3}, nil
		}
		control := strings.Join(args, " ")
		controls = append(controls, control)
		switch control {
		case "stop ikigenba-notes.socket":
			socketActive = false
		case "stop ikigenba-notes.service":
			serviceActive = false
		case "disable ikigenba-notes.socket ikigenba-notes.service":
			disabled = true
		case "enable ikigenba-notes.socket ikigenba-notes.service":
			disabled = false
		case "start ikigenba-notes.socket":
			socketActive = true
		case "start ikigenba-notes.service":
			serviceActive = true
		case "reload-or-restart nginx":
		default:
			t.Fatalf("unexpected systemctl command: %q", control)
		}
		return host.Result{}, nil
	}
	deps := cli.Deps{Root: root, EUID: 0, Execute: execute}
	var priorConfig os.FileInfo
	for _, test := range []struct {
		action, want string
		wantControls []string
	}{
		{"disable", "stop: ok (ikigenba-notes.socket, ikigenba-notes.service stopped, disabled)\nnginx: ok (notes.sbx.example.test, sbx.example.test, example.test disabled)\n", []string{"stop ikigenba-notes.socket", "stop ikigenba-notes.service", "disable ikigenba-notes.socket ikigenba-notes.service", "nginx test", "reload-or-restart nginx"}},
		{"disable", "stop: ok (ikigenba-notes.socket, ikigenba-notes.service already inactive, disabled)\nnginx: ok (unchanged)\n", nil},
		{"enable", "enable: ok (ikigenba-notes.socket, ikigenba-notes.service)\nnginx: ok (notes.sbx.example.test, sbx.example.test, example.test)\nservice: ok (notes v2.3.4 active)\n", []string{"enable ikigenba-notes.socket ikigenba-notes.service", "start ikigenba-notes.socket", "nginx test", "reload-or-restart nginx", "start ikigenba-notes.service"}},
		{"enable", "enable: ok (ikigenba-notes.socket, ikigenba-notes.service already enabled)\nnginx: ok (unchanged)\nservice: ok (notes v2.3.4 active)\n", nil},
	} {
		controls = nil
		stdout, stderr, code := invoke([]string{test.action, "notes"}, deps)
		if code != 0 || stdout != test.want || stderr != "" || !reflect.DeepEqual(controls, test.wantControls) {
			t.Fatalf("%s = exit %d stdout %q stderr %q controls %#v, want %#v", test.action, code, stdout, stderr, controls, test.wantControls)
		}
		currentConfig, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if len(test.wantControls) == 0 && (priorConfig == nil || !os.SameFile(priorConfig, currentConfig)) {
			t.Fatalf("%s idempotent rerun republished nginx configuration", test.action)
		}
		priorConfig = currentConfig
		if preservedAfter := snapshotUninstallPaths(t, root, preservedPaths...); !reflect.DeepEqual(preservedAfter, preservedBefore) {
			t.Fatalf("%s changed app files, sibling files, or store", test.action)
		}
		for _, path := range preservedFiles {
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
			if err != nil {
				t.Fatal(err)
			}
			before := preservedInfo[path]
			if !os.SameFile(before, info) || !before.ModTime().Equal(info.ModTime()) || before.Mode() != info.Mode() {
				t.Fatalf("%s replaced or rewrote %s", test.action, path)
			}
		}
	}
	// A failed nginx stage is the last attempted stage; service control is not retried.
	disabled, socketActive, serviceActive = true, false, false
	if err := os.RemoveAll(filepath.Join(root, "etc/nginx")); err != nil {
		t.Fatal(err)
	}
	controls = nil
	stdout, stderr, code := invoke([]string{"enable", "notes"}, deps)
	if code != 1 || !strings.HasPrefix(stdout,
		"enable: ok (ikigenba-notes.socket, ikigenba-notes.service)\nnginx: failed: ") ||
		!strings.HasSuffix(stdout, "\n") || stderr != "opsctl: enable failed\n" ||
		!reflect.DeepEqual(controls, []string{"enable ikigenba-notes.socket ikigenba-notes.service", "start ikigenba-notes.socket"}) {
		t.Fatalf("failed nginx stage = exit %d stdout %q stderr %q controls %#v", code, stdout, stderr, controls)
	}
}

func TestDisableAuthRefusalPrecedesConfiguration(t *testing.T) {
	// R-W3YP-OUID R-VRRP-V53F
	root := t.TempDir()
	configPath := filepath.Join(root, "etc/ikigenba/config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("not JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	executed := false
	stdout, stderr, code := invoke([]string{"disable", "auth"}, cli.Deps{Root: root, EUID: 0,
		Execute: func(context.Context, host.Command) (host.Result, error) {
			executed = true
			return host.Result{}, nil
		}})
	if code != 1 || stdout != "stop: failed: auth is the authenticator and cannot be disabled\n" || stderr != "opsctl: disable failed\n" || executed {
		t.Fatalf("exit %d stdout %q stderr %q executed %t", code, stdout, stderr, executed)
	}
}

func TestEnablementRejectsInvalidNameBeforeConfiguration(t *testing.T) {
	// R-W3YP-OUID
	for _, action := range []string{"disable", "enable"} {
		stdout, stderr, code := invoke([]string{action, "bad/name"}, cli.Deps{Root: filepath.Join(t.TempDir(), "missing"), EUID: 0})
		if code != 2 || stdout != "" || !strings.Contains(stderr, "'bad/name' is not a usable app name") {
			t.Fatalf("%s = exit %d stdout %q stderr %q", action, code, stdout, stderr)
		}
	}
}
