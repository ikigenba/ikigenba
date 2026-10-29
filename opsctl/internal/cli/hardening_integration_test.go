package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/hardening"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func prepareHardeningTransition(t *testing.T, action string) (*transitionFixture, []string) {
	t.Helper()
	f := newTransitionFixture(t)
	args := []string{"activate", transitionSHA}
	if action == "rollback" {
		f.addRelease(transitionOld, "dummy")
		f.link("current", transitionSHA)
		f.link("previous", transitionOld)
		f.deps.Executable = func() (string, error) {
			return filepath.Join(f.root, "opt/ikigenba/releases", transitionOld, "opsctl/bin/opsctl"), nil
		}
		args = []string{"rollback"}
	}
	return f, args
}

func TestReleaseHardeningReportsChangeAndRunsOnce(t *testing.T) {
	// R-9AHK-LJYN R-96TV-G8QK R-981R-U0H9
	for _, action := range []string{"activate", "rollback"} {
		for _, changed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/changed=%t", action, changed), func(t *testing.T) {
				f, args := prepareHardeningTransition(t, action)
				seedHardeningRules(t, f.root)
				keys := filepath.Join(f.root, hardening.KeysDir)
				if changed {
					if err := fixtureRootChmod(t, f.root, hardening.KeysDir, 0700); err != nil {
						t.Fatal(err)
					}
				}
				code, out, diagnostic := f.run(args...)
				detail := "unchanged"
				if changed {
					detail = "updated"
				}
				if code != 0 || diagnostic != "" || !strings.Contains(out, "hardening: ok ("+detail+")\n") {
					t.Fatalf("%d %s %s", code, out, diagnostic)
				}
				lines := strings.Split(strings.TrimSpace(out), "\n")
				index := -1
				for i, line := range lines {
					if strings.HasPrefix(line, "hardening:") {
						if index != -1 {
							t.Fatal("hardening reported twice")
						}
						index = i
					}
				}
				if index < 1 || !strings.HasPrefix(lines[index-1], "resources: ok (") || !strings.HasPrefix(lines[index+1], map[string]string{"activate": "label:", "rollback": "env:"}[action]) {
					t.Fatalf("step order %s", out)
				}
				calls := 0
				for _, command := range f.commands {
					if command.Name == "getent" && reflect.DeepEqual(command.Args, []string{"group", "shell"}) {
						calls++
					}
				}
				if calls != 1 {
					t.Fatalf("Apply's shell-group query ran %d times", calls)
				}
				info, err := os.Lstat(keys)
				if err != nil || info.Mode().Perm() != 0755 {
					t.Fatalf("keys mode %v %v", info, err)
				}
			})
		}
	}
}

func TestReleaseHardeningFailureKeepsEarlierRulesAndHostInputs(t *testing.T) {
	// R-9AHK-LJYN R-95LZ-2GZV R-9CXD-D3G1
	for _, action := range []string{"activate", "rollback"} {
		t.Run(action, func(t *testing.T) {
			f, args := prepareHardeningTransition(t, action)
			f.write("etc/opt/ikigenba/dummy/env", "kept secret\n", 0600)
			f.write("etc/systemd/system/ikigenba-dummy.service", "kept unit\n", 0644)
			f.write("etc/nginx/conf.d/ikigenba.conf", "kept nginx\n", 0644)
			f.write("etc/litestream.yml", "kept replication\n", 0644)
			f.write("run/ikigenba/services.json", "kept services\n", 0640)
			f.write("var/opt/ikigenba/dummy/state/record", "kept database\n", 0666)
			seedHardeningRules(t, f.root)
			// This earlier hardening change must survive the later package failure.
			keys := filepath.Join(f.root, hardening.KeysDir)
			if err := fixtureRootChmod(t, f.root, hardening.KeysDir, 0700); err != nil {
				t.Fatal(err)
			}
			before := releaseTree(t, f.root)
			base := f.deps.Execute
			f.deps.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
				if command.Name == "rpm" && reflect.DeepEqual(command.Args, []string{"-q", "fail2ban-server"}) {
					f.commands = append(f.commands, command)
					return host.Result{ExitCode: 1}, nil
				}
				if command.Name == "dnf" {
					f.commands = append(f.commands, command)
					return host.Result{ExitCode: 1, Stdout: []byte("package output\n"), Stderr: []byte("package error")}, nil
				}
				return base(ctx, command)
			}
			code, out, diagnostic := f.run(args...)
			if code != 1 || !strings.HasSuffix(out, "hardening: failed: dnf install: exit status 1\n") || diagnostic != "opsctl: "+action+" failed\n\n> package output\n> package error\n" {
				t.Fatalf("%d %q %q", code, out, diagnostic)
			}
			last := f.commands[len(f.commands)-1]
			if last.Name != "dnf" || !reflect.DeepEqual(last.Args, []string{"install", "-y", "fail2ban-server"}) {
				t.Fatalf("last command %+v", last)
			}
			if !reflect.DeepEqual(before, releaseTree(t, f.root)) {
				t.Fatal("failure changed file bytes, entries, or links")
			}
			info, err := os.Lstat(keys)
			if err != nil || info.Mode().Perm() != 0755 {
				t.Fatalf("earlier key-directory fix was undone: %v %v", info, err)
			}
		})
	}
}

func TestRunStateRefusalRecognizesWrappedTypeAndOverridesCapture(t *testing.T) {
	reason := "fixture host state refuses this operation"
	capture := host.Result{Stdout: []byte("captured stdout\n"), Stderr: []byte("captured stderr\n")}
	refused := &accounts.RefusedError{Reason: reason}
	cases := []struct {
		name       string
		cause      error
		code       int
		diagnostic string
	}{
		{"wrapped", fmt.Errorf("outer: %w", refused), 4, "opsctl: " + reason + "\n"},
		{"joined", errors.Join(&host.CommandError{Label: "inner", Result: capture}, fmt.Errorf("outer: %w", refused)), 4, "opsctl: " + reason + "\n"},
		{"same words without type", errors.New(reason), 1, "opsctl: groupadd: " + reason + "\n\n> captured stdout\n> captured stderr\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"passwd", "group"} {
				path := filepath.Join(root, "etc", name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			var out, diagnostic bytes.Buffer
			code := Run([]string{"group", "add", "example"}, nil, &out, &diagnostic, Deps{Root: root, EUID: 0, Now: func() time.Time { return time.Unix(1, 0) }, Getenv: func(string) string { return "" }, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				calls++
				if command.Name != "groupadd" {
					t.Fatalf("unexpected command %+v", command)
				}
				return capture, test.cause
			}})
			if calls != 1 || code != test.code || out.Len() != 0 || diagnostic.String() != test.diagnostic {
				t.Fatalf("calls %d code %d out %q diagnostic %q", calls, code, out.String(), diagnostic.String())
			}
		})
	}
}

func TestReleaseCreatesRestrictedDataParentsAndOwnsThemOnce(t *testing.T) {
	// R-9BPG-ZBPC
	for _, action := range []string{"activate", "rollback"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing-parent=%t", action, existing), func(t *testing.T) {
				f, args := prepareHardeningTransition(t, action)
				parentMode := os.FileMode(0755)
				if existing {
					parentMode = 0700
					if err := os.MkdirAll(filepath.Join(f.root, "var/opt"), parentMode); err != nil {
						t.Fatal(err)
					}
				}
				code, out, diagnostic := f.run(args...)
				if code != 0 || diagnostic != "" {
					t.Fatalf("%d %s %s", code, out, diagnostic)
				}
				for name, want := range map[string]os.FileMode{"var/opt": parentMode, "var/opt/ikigenba": 0750, "var/opt/ikigenba/dummy": 0750} {
					info, err := os.Lstat(filepath.Join(f.root, name))
					if err != nil || !info.IsDir() || info.Mode().Perm() != want {
						t.Fatalf("%s mode %v %v, want %o", name, info, err, want)
					}
				}
				for _, command := range f.commands {
					if command.Name == "chown" {
						for _, arg := range command.Args {
							if arg == filepath.Join(f.root, "var/opt") {
								t.Fatalf("parent re-owned %+v", command)
							}
						}
					}
				}
				for _, test := range []struct {
					owner, path string
					want        int
				}{{"root:ikigenba", "var/opt/ikigenba", 1}, {"ikigenba:ikigenba", "var/opt/ikigenba/dummy", 1}, {"root:ikigenba", "var/opt", 0}} {
					calls := 0
					for _, command := range f.commands {
						if command.Name == "chown" && reflect.DeepEqual(command.Args, []string{test.owner, filepath.Join(f.root, test.path)}) {
							calls++
						}
					}
					if calls != test.want {
						t.Fatalf("%s ownership calls %d want %d", test.path, calls, test.want)
					}
				}
			})
		}
	}
}

func TestReleaseHardeningOnlyClearsOthersBitsInExistingData(t *testing.T) {
	// R-9CXD-D3G1
	for _, action := range []string{"activate", "rollback"} {
		t.Run(action, func(t *testing.T) {
			f, args := prepareHardeningTransition(t, action)
			for _, app := range []string{"dummy", "dataonly"} {
				f.write("var/opt/ikigenba/"+app+"/state/record", "persisted bytes", 0666)
				if err := fixtureRootChmod(t, f.root, "var/opt/ikigenba/"+app+"/state", 0777); err != nil {
					t.Fatal(err)
				}
			}
			f.write("outside/data", "outside bytes", 0666)
			if err := os.Symlink(filepath.Join(f.root, "outside/data"), filepath.Join(f.root, "var/opt/ikigenba/dummy/link")); err != nil {
				t.Fatal(err)
			}
			before, err := preflightTree(f.root, "", false)
			if err != nil {
				t.Fatal(err)
			}
			configBefore := f.read("etc/ikigenba/config.json")
			code, out, diagnostic := f.run(args...)
			if code != 0 || diagnostic != "" {
				t.Fatalf("%d %s %s", code, out, diagnostic)
			}
			after, err := preflightTree(f.root, "", false)
			if err != nil {
				t.Fatal(err)
			}
			isData := func(path string) bool {
				return path == "var/opt/ikigenba/dummy" || strings.HasPrefix(path, "var/opt/ikigenba/dummy/") || path == "var/opt/ikigenba/dataonly" || strings.HasPrefix(path, "var/opt/ikigenba/dataonly/")
			}
			for path, want := range before {
				if isData(path) {
					if want.mode&os.ModeSymlink == 0 {
						want.mode &^= 0007
					}
					if got, ok := after[path]; !ok || got != want {
						t.Fatalf("data changed beyond others bits: %s got %+v want %+v", path, got, want)
					}
				}
			}
			for path := range after {
				if isData(path) {
					if _, ok := before[path]; !ok {
						t.Fatalf("data entry created: %s", path)
					}
				}
			}
			if f.read("etc/ikigenba/config.json") != configBefore {
				t.Fatal("configuration changed")
			}
			for _, app := range []string{"dummy", "dataonly"} {
				data := filepath.Join(f.root, "var/opt/ikigenba", app)
				for name, want := range map[string]os.FileMode{"state": 0770, "state/record": 0660} {
					info, err := os.Lstat(filepath.Join(data, name))
					if err != nil || info.Mode().Perm() != want {
						t.Fatalf("%s mode %v %v want %o", name, info, err, want)
					}
				}
				if f.read("var/opt/ikigenba/"+app+"/state/record") != "persisted bytes" {
					t.Fatal("data bytes changed")
				}
				for _, command := range f.commands {
					if command.Name == "chown" {
						for _, arg := range command.Args {
							if arg == data || strings.HasPrefix(arg, data+"/") {
								t.Fatalf("existing data re-owned %+v", command)
							}
						}
					}
				}
			}
			if target, err := os.Readlink(filepath.Join(f.root, "var/opt/ikigenba/dummy/link")); err != nil || target != filepath.Join(f.root, "outside/data") {
				t.Fatalf("symlink changed %q %v", target, err)
			}
			outside, err := os.Lstat(filepath.Join(f.root, "outside/data"))
			if err != nil || outside.Mode().Perm() != 0666 || f.read("outside/data") != "outside bytes" {
				t.Fatalf("symlink target changed %v %v", outside, err)
			}
			for _, command := range f.commands {
				if command.Name == "systemctl" && len(command.Args) > 1 && (command.Args[0] == "stop" || command.Args[0] == "restart" || command.Args[0] == "reload") {
					for _, arg := range command.Args[1:] {
						if strings.HasSuffix(arg, ".socket") {
							t.Fatalf("socket controlled %+v", command)
						}
					}
				}
			}
		})
	}
}

func fixtureRootChmod(t *testing.T, root, name string, mode os.FileMode) error {
	t.Helper()
	fs, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = fs.Close() }()
	return fs.Chmod(strings.TrimPrefix(name, "/"), mode)
}

func TestReleaseHardeningFilesystemErrorReportsOwnTextAndStops(t *testing.T) {
	// R-9AHK-LJYN R-95LZ-2GZV
	for _, action := range []string{"activate", "rollback"} {
		t.Run(action, func(t *testing.T) {
			f, args := prepareHardeningTransition(t, action)
			f.write("etc/opt", "blocked parent", 0600)
			_, expectedErr := hardening.Apply(context.Background(), host.Env{Root: f.root, Getenv: f.deps.Getenv, Now: f.deps.Now, Execute: f.deps.Execute})
			var commandErr *host.CommandError
			if expectedErr == nil || errors.As(expectedErr, &commandErr) {
				t.Fatalf("expected plain filesystem error, got %v", expectedErr)
			}
			f.commands = nil
			before := releaseTree(t, f.root)
			code, out, diagnostic := f.run(args...)
			if code != 1 || !strings.HasSuffix(out, "hardening: failed: "+expectedErr.Error()+"\n") || diagnostic != "opsctl: "+action+" failed\n" {
				t.Fatalf("%d %q %q", code, out, diagnostic)
			}
			if !reflect.DeepEqual(before, releaseTree(t, f.root)) {
				t.Fatal("filesystem failure changed host file bytes, entries, or links")
			}
			for _, command := range f.commands {
				if command.Name == "nginx" || command.Name == "zstd" || command.Name == "systemctl" && len(command.Args) > 0 && (command.Args[0] == "start" || command.Args[0] == "restart") {
					t.Fatalf("later operation ran %+v", command)
				}
			}
		})
	}
}
