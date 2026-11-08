package cli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func makeInitTurnsRelease(t *testing.T, root, sha, content string) string {
	t.Helper()
	seedInitAccountFiles(t, root, "", "", "")
	folder := filepath.Join(root, release.ReleasesDir, sha)
	executable := filepath.Join(folder, release.OpsctlPath)
	writeCLIInstallFile(t, executable, "binary")
	writeCLIInstallFile(t, filepath.Join(folder, release.MetadataName), `{"sha":"`+sha+`"}`)
	writeCLIInstallFile(t, filepath.Join(folder, release.LabelName), "kept label\n")
	for _, name := range []string{release.TurnsService, release.TurnsTimer} {
		writeCLIInstallFile(t, filepath.Join(folder, release.TurnsUnitsPath, name), content+name)
	}
	return executable
}

func initTurnsState(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	state := treeState(t, root)
	for path, entry := range state {
		if entry.Mode&os.ModeSymlink != 0 {
			target, err := os.Readlink(filepath.Join(root, path))
			if err != nil {
				t.Fatal(err)
			}
			entry.Content = target
			state[path] = entry
		}
	}
	return state
}

func stopInitAtCertificate(t *testing.T, deps *cli.Deps) *int {
	t.Helper()
	calls := new(int)
	deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
		*calls++
		if command.Name != "certbot" {
			t.Fatalf("unexpected command %+v", command)
		}
		return host.Result{}, errors.New("certificate fixture stop")
	}
	return calls
}

func TestInitTurnsSetupSourceAndExistingLink(t *testing.T) {
	// R-W89S-07BM R-WAPK-RQT0 R-WJ8V-G4ZV
	for _, layout := range []string{"fresh", "per-app", "released"} {
		for _, kind := range []string{"absent", "file", "link", "dangling", "directory"} {
			t.Run(layout+"/"+kind, func(t *testing.T) {
				deps := readyStateGuardDeps(t)
				if err := (config.Store{Root: deps.Root}).Set("host.name", "API.Example.Com."); err != nil {
					t.Fatal(err)
				}
				sha, currentSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
				executable := makeInitTurnsRelease(t, deps.Root, sha, "executable ")
				deps.Executable = func() (string, error) { return executable, nil }
				writeCLIInstallFile(t, filepath.Join(deps.Root, release.OpsctlLink), "existing opsctl")
				content := "executable "
				switch layout {
				case "per-app":
					makeStateGuardApp(t, deps.Root, "old", true)
				case "released":
					makeInitTurnsRelease(t, deps.Root, currentSHA, "current ")
					if err := os.Symlink("releases/"+currentSHA, filepath.Join(deps.Root, release.CurrentLink)); err != nil {
						t.Fatal(err)
					}
					content = "current "
				}
				turnsPath := filepath.Join(deps.Root, release.TurnsLink)
				switch kind {
				case "file":
					writeCLIInstallFile(t, turnsPath, "keep")
				case "link", "dangling":
					target := "/usr/local/bin/opsctl"
					if kind == "dangling" {
						target = "/missing"
					}
					if err := os.Symlink(target, turnsPath); err != nil {
						t.Fatal(err)
					}
				case "directory":
					writeCLIInstallFile(t, filepath.Join(turnsPath, "keep"), "directory preserved")
				}
				before := initTurnsState(t, deps.Root)
				calls := stopInitAtCertificate(t, &deps)
				stdout, stderr, code := invoke([]string{"init"}, deps)
				if code != 1 || *calls != 1 || stderr != "opsctl: certbot certonly: certificate fixture stop\n" || !strings.HasSuffix(stdout, "wildcard api.example.com: ok (192.0.2.10)\n") {
					t.Fatalf("%d %d %s %s", code, *calls, stdout, stderr)
				}
				after := initTurnsState(t, deps.Root)
				if kind == "absent" {
					target := release.ReleasesDir + "/" + sha + "/" + release.TurnsPath
					if layout == "released" {
						target = release.CurrentTurns
					}
					if got, err := os.Readlink(turnsPath); err != nil || got != target {
						t.Fatal(got, err)
					}
				} else {
					for path, entry := range before {
						if path == "usr/local/bin/turns" || strings.HasPrefix(path, "usr/local/bin/turns/") {
							if !reflect.DeepEqual(after[path], entry) {
								t.Fatal("existing turns entry changed", path)
							}
						}
					}
				}
				for path, want := range map[string]string{"etc/systemd/user/turns.service": content + release.TurnsService, "etc/systemd/user/turns.timer": content + release.TurnsTimer, "etc/xdg/turns/config": "space=api.example.com\n"} {
					if after[path].Content != want {
						t.Fatal(path, after[path])
					}
				}
				for _, state := range []map[string]treeEntry{before, after} {
					for path := range state {
						if strings.HasPrefix(path, "opt/ikigenba/") || strings.HasPrefix(path, "usr/local/bin/") && !strings.HasPrefix(path, "usr/local/bin/turns") {
							if !reflect.DeepEqual(before[path], after[path]) {
								t.Fatal("protected state changed", path)
							}
						}
					}
				}
			})
		}
	}
}

func TestInitTurnsAbsentSourceIsInertAndLinkFailureStopsInstall(t *testing.T) {
	// R-W89S-07BM R-WAPK-RQT0
	for _, kind := range []string{"no-tree", "outside-release", "executable-error", "bad-current", "link-failure"} {
		t.Run(kind, func(t *testing.T) {
			deps := readyStateGuardDeps(t)
			sha := strings.Repeat("a", 40)
			executable := makeInitTurnsRelease(t, deps.Root, sha, "source ")
			writeCLIInstallFile(t, filepath.Join(deps.Root, release.OpsctlLink), "keep")
			deps.Executable = func() (string, error) { return executable, nil }
			var injected map[string]treeEntry
			switch kind {
			case "no-tree":
				if err := os.RemoveAll(filepath.Join(deps.Root, release.ReleasesDir, sha, release.TurnsDir)); err != nil {
					t.Fatal(err)
				}
			case "outside-release":
				deps.Executable = func() (string, error) { return filepath.Join(deps.Root, "bin/opsctl"), nil }
			case "executable-error":
				deps.Executable = func() (string, error) { return "", errors.New("fixture executable error") }
			case "bad-current":
				writeCLIInstallFile(t, filepath.Join(deps.Root, release.CurrentLink), "bad current")
			case "link-failure":
				deps.Executable = func() (string, error) {
					// After the existing opsctl entry has been checked, make the
					// absent turns link's parent unresolvable without permissions.
					bin := filepath.Join(deps.Root, "/usr/local/bin")
					if err := os.Rename(bin, filepath.Join(deps.Root, "/usr/local/saved-bin")); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("missing", bin); err != nil {
						t.Fatal(err)
					}
					injected = initTurnsState(t, deps.Root)
					return executable, nil
				}
			}
			if kind == "no-tree" || kind == "outside-release" || kind == "executable-error" {
				// An accidental EnsureTimers call must fail, even when old
				// timer units remain from an earlier release.
				writeCLIInstallFile(t, filepath.Join(deps.Root, release.UserUnitDir, release.TurnsTimer), "existing timer")
				writeCLIInstallFile(t, filepath.Join(deps.Root, "etc/shadow"), "malformed shadow")
			}
			before := initTurnsState(t, deps.Root)
			calls := stopInitAtCertificate(t, &deps)
			stdout, stderr, code := invoke([]string{"init"}, deps)
			if code != 1 || !strings.HasSuffix(stdout, "wildcard api.example.com: ok (192.0.2.10)\n") {
				t.Fatalf("%d %s %s", code, stdout, stderr)
			}
			switch kind {
			case "bad-current":
				if *calls != 0 || stderr != "opsctl: /opt/ikigenba/current does not name a release\n" {
					t.Fatal(*calls, stderr)
				}
			case "link-failure":
				if *calls != 0 || !strings.HasPrefix(stderr, "opsctl: "+release.TurnsLink+": ") || injected == nil {
					t.Fatal(*calls, stderr)
				}
				before = injected
			default:
				if *calls != 1 || stderr != "opsctl: certbot certonly: certificate fixture stop\n" {
					t.Fatal(*calls, stderr)
				}
			}
			if !reflect.DeepEqual(before, initTurnsState(t, deps.Root)) {
				t.Fatal("absent setup or failed link changed state; installation must not run")
			}
		})
	}
}

func TestInitDiscoveryFailureAfterHardeningStopsBeforeNginx(t *testing.T) {
	// R-DKOD-V9HM
	deps := readyStateGuardDeps(t)
	makeStateGuardApp(t, deps.Root, "notes", true)
	writeCLIInstallFile(t, filepath.Join(deps.Root, "opt/notes/etc/manifest.toml"), "app='notes'\n")
	writeCLIInstallFile(t, filepath.Join(deps.Root, "etc/opt/ikigenba/notes/env"), "kept environment\n")
	for _, path := range []string{"etc/opt/ikigenba", "etc/opt/ikigenba/notes"} {
		if err := initFixtureRootChmod(t, deps.Root, path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	var afterHardening map[string]treeEntry
	certificate, slices := false, false
	deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
		if command.Name == "certbot" {
			certificate = true
		}
		if command.Name == "systemctl" && reflect.DeepEqual(command.Args, []string{"restart", "nginx"}) {
			if !certificate {
				t.Fatal("slices ran before certificate")
			}
			slices = true
		}
		if command.Name == "stat" && len(command.Args) > 0 && command.Args[0] == "--format=%U:%G" {
			if !slices {
				t.Fatal("hardening ran before slices")
			}
			// The initial per-app layout is valid. The hardening process fixture
			// supplies a later enumeration fault for the nginx.conf guard.
			writeCLIInstallFile(t, filepath.Join(deps.Root, apps.DataRoot), "not a directory")
			afterHardening = initTurnsState(t, deps.Root)
		}
		if command.Name == "nginx" {
			t.Fatal("nginx ran after discovery failure")
		}
		if result, ok := hardeningFixtureQuery(command); ok {
			return result, nil
		}
		return host.Result{}, nil
	}
	stdout, stderr, code := invoke([]string{"init"}, deps)
	if code != 1 || !strings.HasSuffix(stdout, "wildcard api.example.com: ok (192.0.2.10)\n") || !strings.HasPrefix(stderr, "opsctl: discover services: read "+apps.DataRoot+": ") {
		t.Fatalf("%d %s %s", code, stdout, stderr)
	}
	if afterHardening == nil || !reflect.DeepEqual(afterHardening, initTurnsState(t, deps.Root)) {
		t.Fatal("discovery failure changed state after hardening")
	}
}

func TestInitTurnsPrecedesCertificateAndFailuresStopSequence(t *testing.T) {
	// R-W89S-07BM R-WGT2-OLIH R-WAPK-RQT0 R-WJ8V-G4ZV
	for _, kind := range []string{"ready", "missing-unit", "config-blocked", "bad-current", "no-turns"} {
		t.Run(kind, func(t *testing.T) {
			deps := readyStateGuardDeps(t)
			seedInitAccountFiles(t, deps.Root, "", "", "")
			sha := strings.Repeat("d", 40)
			folder := release.ReleasesDir + "/" + sha
			executable := filepath.Join(deps.Root, folder, release.OpsctlPath)
			writeCLIInstallFile(t, executable, "binary")
			writeCLIInstallFile(t, filepath.Join(deps.Root, folder, release.MetadataName), `{"sha":"`+sha+`"}`)
			writeCLIInstallFile(t, filepath.Join(deps.Root, folder, release.LabelName), "kept-label\n")
			deps.Executable = func() (string, error) { return executable, nil }
			if kind != "no-turns" {
				writeCLIInstallFile(t, filepath.Join(deps.Root, folder, release.TurnsUnitsPath, release.TurnsService), "new service")
				if kind != "missing-unit" {
					writeCLIInstallFile(t, filepath.Join(deps.Root, folder, release.TurnsUnitsPath, release.TurnsTimer), "new timer")
				}
			}
			switch kind {
			case "config-blocked":
				writeCLIInstallFile(t, filepath.Join(deps.Root, "/etc/xdg/turns"), "blocked")
			case "bad-current":
				writeCLIInstallFile(t, filepath.Join(deps.Root, release.CurrentLink), "bad current")
			}
			before := initTurnsState(t, deps.Root)
			calls := 0
			deps.Execute = func(_ context.Context, c host.Command) (host.Result, error) {
				calls++
				if c.Name != "certbot" || kind != "ready" && kind != "no-turns" {
					t.Fatalf("unexpected command %+v", c)
				}
				if kind == "ready" {
					filesystem, err := os.OpenRoot(deps.Root)
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = filesystem.Close() }()
					for path, want := range map[string]string{release.TurnsConfig: "space=api.example.com\n", release.UserUnitDir + "/" + release.TurnsService: "new service", release.UserUnitDir + "/" + release.TurnsTimer: "new timer"} {
						data, err := filesystem.ReadFile(strings.TrimPrefix(path, "/"))
						if err != nil || string(data) != want {
							t.Fatalf("certificate before turns setup: %s %q %v", path, data, err)
						}
					}
				}
				return host.Result{}, errors.New("certificate fixture stop")
			}
			stdout, stderr, code := invoke([]string{"init"}, deps)
			if code != 1 || !strings.HasSuffix(stdout, "wildcard api.example.com: ok (192.0.2.10)\n") || strings.Contains(stdout, "turns") {
				t.Fatalf("%d %q %q", code, stdout, stderr)
			}
			switch kind {
			case "ready", "no-turns":
				if calls != 1 || stderr != "opsctl: certbot certonly: certificate fixture stop\n" {
					t.Fatal(calls, stderr)
				}
			case "missing-unit":
				if !strings.HasPrefix(stderr, "opsctl: "+folder+"/"+release.TurnsUnitsPath+"/"+release.TurnsTimer+": ") {
					t.Fatal(stderr)
				}
			case "config-blocked":
				if !strings.HasPrefix(stderr, "opsctl: "+release.TurnsConfig+": ") {
					t.Fatal(stderr)
				}
			case "bad-current":
				if stderr != "opsctl: /opt/ikigenba/current does not name a release\n" {
					t.Fatal(stderr)
				}
			}
			after := initTurnsState(t, deps.Root)
			for path, entry := range before {
				if strings.HasPrefix(path, "opt/ikigenba/") && !reflect.DeepEqual(after[path], entry) {
					t.Fatal("release state changed", path)
				}
			}
			if kind == "no-turns" {
				for _, path := range []string{release.TurnsLink, release.UserUnitDir, "/etc/xdg"} {
					if _, err := os.Lstat(filepath.Join(deps.Root, path)); !os.IsNotExist(err) {
						t.Fatal("unexpected turns setup", path, err)
					}
				}
			}
		})
	}
}

func seedInitAccountFiles(t *testing.T, root, passwd, group, shadow string) {
	t.Helper()
	for name, data := range map[string]string{"passwd": passwd, "group": group, "shadow": shadow} {
		writeCLIInstallFile(t, filepath.Join(root, "etc", name), data)
	}
}

func initTimerUsers(t *testing.T, root string) {
	t.Helper()
	seedInitAccountFiles(t, root,
		"dave:x:1004:1004::/home/dave:/bin/bash\ncarol:x:1003:1003::/home/carol:/bin/bash\nbob:x:1002:1002::/home/bob:/bin/bash\nalice:x:1001:1001::/home/alice:/bin/bash\nerin:x:1005:1005::/home/erin:/bin/bash\n",
		"shell:x:900:alice,bob,carol,dave,erin\n",
		"alice:!!:0:0:99999:7:::\nbob:!!:0:0:99999:7::1:\ncarol:!!:0:0:99999:7:::\ndave:!!:0:0:99999:7:::\nerin:!!:0:0:99999:7:::\n")
	initTimerReady(t, root, "erin")
}

func initTimerReady(t *testing.T, root, name string) {
	t.Helper()
	writeCLIInstallFile(t, filepath.Join(root, "var/lib/systemd/linger", name), "")
	link := filepath.Join(root, "home", name, ".config/systemd/user/timers.target.wants", release.TurnsTimer)
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(release.UserUnitDir+"/"+release.TurnsTimer, link); err != nil {
		t.Fatal(err)
	}
}

func TestInitGivesActiveUsersTimersAfterInstall(t *testing.T) {
	// R-W89S-07BM R-WAPK-RQT0 R-WGT2-OLIH R-WJ8V-G4ZV
	deps := readyStateGuardDeps(t)
	executable := makeInitTurnsRelease(t, deps.Root, strings.Repeat("a", 40), "source ")
	deps.Executable = func() (string, error) { return executable, nil }
	initTimerUsers(t, deps.Root)
	before := initTurnsState(t, deps.Root)
	filesystem, err := os.OpenRoot(deps.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filesystem.Close() }()
	var calls []string
	deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
		calls = append(calls, command.Name+" "+strings.Join(command.Args, " "))
		for path, want := range map[string]string{release.TurnsConfig: "space=api.example.com\n", release.UserUnitDir + "/" + release.TurnsTimer: "source " + release.TurnsTimer} {
			data, err := filesystem.ReadFile(strings.TrimPrefix(path, "/"))
			if err != nil || string(data) != want {
				t.Fatalf("timer commands before install: %s = %q, %v", path, data, err)
			}
		}
		if command.Name == "certbot" {
			return host.Result{}, errors.New("certificate fixture stop")
		}
		return host.Result{}, nil
	}
	stdout, stderr, code := invoke([]string{"init"}, deps)
	want := []string{
		"loginctl enable-linger alice", "systemctl start user@1001.service", "systemctl --user --machine=alice@.host enable --now turns.timer",
		"loginctl enable-linger carol", "systemctl start user@1003.service", "systemctl --user --machine=carol@.host enable --now turns.timer",
		"loginctl enable-linger dave", "systemctl start user@1004.service", "systemctl --user --machine=dave@.host enable --now turns.timer",
	}
	if code != 1 || stderr != "opsctl: certbot certonly: certificate fixture stop\n" || !strings.HasSuffix(stdout, "wildcard api.example.com: ok (192.0.2.10)\n") || strings.Contains(stdout, "turns") {
		t.Fatalf("init = %d %q %q", code, stdout, stderr)
	}
	if len(calls) != len(want)+1 || !reflect.DeepEqual(calls[:len(want)], want) || !strings.HasPrefix(calls[len(want)], "certbot ") {
		t.Fatalf("commands = %v", calls)
	}
	after := initTurnsState(t, deps.Root)
	for path, entry := range before {
		if strings.HasPrefix(path, "opt/ikigenba/") || strings.HasPrefix(path, "home/") || strings.HasPrefix(path, "var/lib/systemd/linger/") {
			if !reflect.DeepEqual(after[path], entry) {
				t.Fatal("setup itself changed protected state", path)
			}
		}
	}
}

func TestInitTimerFailureStopsBeforeCertificateAndResumes(t *testing.T) {
	// R-W89S-07BM R-WAPK-RQT0
	deps := readyStateGuardDeps(t)
	executable := makeInitTurnsRelease(t, deps.Root, strings.Repeat("a", 40), "source ")
	deps.Executable = func() (string, error) { return executable, nil }
	initTimerUsers(t, deps.Root)
	var calls []string
	fail := true
	deps.Execute = func(_ context.Context, command host.Command) (host.Result, error) {
		line := command.Name + " " + strings.Join(command.Args, " ")
		calls = append(calls, line)
		if command.Name == "certbot" {
			return host.Result{}, errors.New("certificate fixture stop")
		}
		if line == "systemctl --user --machine=carol@.host enable --now turns.timer" && fail {
			return host.Result{ExitCode: 1, Stderr: []byte("Failed to connect to bus: No such file or directory\n")}, nil
		}
		if command.Name == "systemctl" && len(command.Args) > 0 && command.Args[0] == "--user" {
			name := strings.TrimSuffix(strings.TrimPrefix(command.Args[1], "--machine="), "@.host")
			initTimerReady(t, deps.Root, name)
		}
		return host.Result{}, nil
	}
	stdout, stderr, code := invoke([]string{"init"}, deps)
	wantErr := "opsctl: systemctl: exit status 1\n\n> Failed to connect to bus: No such file or directory\n"
	want := []string{
		"loginctl enable-linger alice", "systemctl start user@1001.service", "systemctl --user --machine=alice@.host enable --now turns.timer",
		"loginctl enable-linger carol", "systemctl start user@1003.service", "systemctl --user --machine=carol@.host enable --now turns.timer",
	}
	if code != 1 || stderr != wantErr || !strings.HasSuffix(stdout, "wildcard api.example.com: ok (192.0.2.10)\n") || !reflect.DeepEqual(calls, want) {
		t.Fatalf("init = %d %q %q, calls %v", code, stdout, stderr, calls)
	}
	fail, calls = false, nil
	stdoutAgain, stderr, code := invoke([]string{"init"}, deps)
	if code != 1 || stdoutAgain != stdout || stderr != "opsctl: certbot certonly: certificate fixture stop\n" || len(calls) != 7 || calls[0] != "loginctl enable-linger carol" || calls[3] != "loginctl enable-linger dave" || !strings.HasPrefix(calls[6], "certbot ") {
		t.Fatalf("rerun = %d %q %q, calls %v", code, stdoutAgain, stderr, calls)
	}
}

func TestInitInvalidApexAllowsOnlyTurnsTimerCommands(t *testing.T) {
	// R-WED9-X213
	deps := readyStateGuardDeps(t)
	store := config.Store{Root: deps.Root}
	for key, value := range map[string]string{"host.name": "EXAMPLE.COM.", "host.apex": "home"} {
		if err := store.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	executable := makeInitTurnsRelease(t, deps.Root, strings.Repeat("a", 40), "source ")
	deps.Executable = func() (string, error) { return executable, nil }
	seedInitAccountFiles(t, deps.Root, "alice:x:1001:1001::/home/alice:/bin/bash\n", "shell:x:900:alice\n", "alice:!!:0:0:99999:7:::\n")
	before := initTurnsState(t, deps.Root)
	var calls []string
	deps.Execute = func(_ context.Context, c host.Command) (host.Result, error) {
		calls = append(calls, c.Name+" "+strings.Join(c.Args, " "))
		return host.Result{}, nil
	}
	stdout, stderr, code := invoke([]string{"init"}, deps)
	want := []string{"loginctl enable-linger alice", "systemctl start user@1001.service", "systemctl --user --machine=alice@.host enable --now turns.timer"}
	if code != 1 || stderr != "opsctl: host.apex is set but host.name 'example.com' has no parent domain\n" || !strings.HasSuffix(stdout, "wildcard example.com: ok (192.0.2.10)\n") || !reflect.DeepEqual(calls, want) {
		t.Fatalf("init = %d %q %q calls %v", code, stdout, stderr, calls)
	}
	after := initTurnsState(t, deps.Root)
	for name, entry := range after {
		if name == "usr" || strings.HasPrefix(name, "usr/local") || name == "etc/systemd/user" || strings.HasPrefix(name, "etc/systemd/user/") || name == "etc/xdg" || strings.HasPrefix(name, "etc/xdg/turns") {
			continue
		}
		if !reflect.DeepEqual(before[name], entry) {
			t.Fatal("unexpected persistent change", name)
		}
	}
	for name, entry := range before {
		if !reflect.DeepEqual(after[name], entry) {
			t.Fatal("existing persistent state changed", name)
		}
	}
}
