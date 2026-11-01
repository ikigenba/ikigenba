package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func (f *transitionFixture) addTurns(sha string) {
	f.t.Helper()
	base := "opt/ikigenba/releases/" + sha + "/"
	f.write(base+release.TurnsPath, "turns binary", 0o600)
	f.write(base+release.TurnsUnitsPath+"/"+release.TurnsService, "service bytes\n", 0o600)
	f.write(base+release.TurnsUnitsPath+"/"+release.TurnsTimer, "timer bytes\n", 0o600)
}

func turnsTransition(t *testing.T, command string) (*transitionFixture, string, []string) {
	t.Helper()
	f := newTransitionFixture(t)
	sha := transitionSHA
	args := []string{"activate", sha}
	if command == "rollback" {
		sha = transitionOld
		f.addRelease(sha, "dummy")
		f.link("current", transitionSHA)
		f.link("previous", sha)
		f.deps.Executable = func() (string, error) {
			return filepath.Join(f.root, release.ReleasesDir, sha, release.OpsctlPath), nil
		}
		args = []string{"rollback"}
	}
	return f, sha, args
}

func TestTransitionsInstallTurnsWithoutManagingUsers(t *testing.T) {
	// R-1GXX-EQHC R-DJJH-2UY4 R-DLZ9-UEFI
	for _, command := range []string{"activate", "rollback"} {
		t.Run(command, func(t *testing.T) {
			f, sha, args := turnsTransition(t, command)
			f.addTurns(sha)
			f.write("usr/local/bin/turns", "replace me", 0o600)
			code, out, stderr := f.run(args...)
			if code != 0 || stderr != "" || !strings.Contains(out, "units: ok (1 app)\n") || !strings.Contains(out, "links: ok (current "+sha[:7]+", previous none)\n") {
				t.Fatalf("%d %s %s", code, out, stderr)
			}
			for path, want := range map[string]string{release.UserUnitDir + "/" + release.TurnsService: "service bytes\n", release.UserUnitDir + "/" + release.TurnsTimer: "timer bytes\n", release.TurnsConfig: "space=sbx.example.test\n"} {
				if got := f.read(strings.TrimPrefix(path, "/")); got != want {
					t.Fatal(path, got)
				}
			}
			f.requireReleaseLink(strings.TrimPrefix(release.TurnsLink, "/"), release.CurrentTurns)
			for _, c := range f.commands {
				if c.Name == "loginctl" {
					t.Fatal(c)
				}
				if c.Name == "systemctl" {
					for _, arg := range c.Args {
						if arg == "--user" || arg == "--machine" || strings.HasPrefix(arg, "--machine=") {
							t.Fatal(c)
						}
					}
				}
			}
		})
	}
}

func TestTransitionsTurnsFailuresStopAtTheirStep(t *testing.T) {
	// R-1GXX-EQHC R-DJJH-2UY4
	for _, command := range []string{"activate", "rollback"} {
		for _, stage := range []string{"units", "links", "opsctl-link"} {
			t.Run(command+"/"+stage, func(t *testing.T) {
				f, sha, args := turnsTransition(t, command)
				f.addTurns(sha)
				switch stage {
				case "units":
					f.write("etc/xdg/turns", "blocked", 0o600)
				case "links":
					f.write("usr/local/bin/turns/keep", "blocked", 0o600)
				case "opsctl-link":
					f.write("usr/local/bin/opsctl/keep", "blocked", 0o600)
					f.write("usr/local/bin/turns", "unchanged", 0o600)
				}
				code, out, stderr := f.run(args...)
				step, path := stage, release.TurnsLink
				switch stage {
				case "opsctl-link":
					step = "links"
				case "units":
					path = release.TurnsConfig
				}
				if code != 1 || stderr != "opsctl: "+command+" failed\n" || !strings.Contains(out, step+": failed: ") || strings.Contains(out, "systemd:") {
					t.Fatalf("%d %s %s", code, out, stderr)
				}
				if stage != "opsctl-link" && !strings.Contains(out, step+": failed: "+path+": ") {
					t.Fatal(out)
				}
				if stage == "opsctl-link" && f.read("usr/local/bin/turns") != "unchanged" {
					t.Fatal("turns linked before opsctl link succeeded")
				}
				if f.read("etc/systemd/user/turns.service") != "service bytes\n" || f.read("etc/systemd/user/turns.timer") != "timer bytes\n" {
					t.Fatal("earlier unit installation lost")
				}
			})
		}
	}
}

func TestTurnsInstallationFollowsDroppedAppCleanup(t *testing.T) {
	// R-1GXX-EQHC
	for _, command := range []string{"activate", "rollback"} {
		t.Run(command, func(t *testing.T) {
			f, sha, args := turnsTransition(t, command)
			f.addTurns(sha)
			if command == "activate" {
				f.addRelease(transitionOld, "dropped")
				f.link("current", transitionOld)
			} else {
				f.addRelease(transitionSHA, "dropped")
			}
			f.write("etc/systemd/system/ikigenba-dropped.service", "old service", 0o644)
			f.write("etc/systemd/system/ikigenba-dropped.socket", "old socket", 0o644)
			f.write("etc/opt/ikigenba/dropped/env", "old env", 0o600)
			f.write("etc/systemd/user/turns.service", "old turns service", 0o644)
			f.write("etc/systemd/user/turns.timer", "old turns timer", 0o644)
			f.write("etc/xdg/turns/config", "old turns config", 0o644)
			f.fail = "systemctl disable --now ikigenba-dropped.socket ikigenba-dropped.service"
			code, out, stderr := f.run(args...)
			if code != 1 || !strings.Contains(out, "units: failed: dropped: "+f.fail+": exit status 1\n") || strings.Contains(out, "links:") || stderr != "opsctl: "+command+" failed\n\n> command output\n> error output\n" {
				t.Fatalf("%d %s %s", code, out, stderr)
			}
			for path, want := range map[string]string{"etc/systemd/user/turns.service": "old turns service", "etc/systemd/user/turns.timer": "old turns timer", "etc/xdg/turns/config": "old turns config"} {
				if f.read(path) != want {
					t.Fatal("turns installed before dropped-app cleanup succeeded", path)
				}
			}
		})
	}
}

func TestTransitionsWithoutTurnsLeaveAllTurnsStateAlone(t *testing.T) {
	// R-1FQ1-0YQN
	for _, command := range []string{"activate", "rollback"} {
		for _, state := range []string{"absent", "files", "symlinks"} {
			t.Run(command+"/"+state, func(t *testing.T) {
				f, _, args := turnsTransition(t, command)
				paths := []string{release.TurnsLink, release.UserUnitDir, "/etc/xdg/turns"}
				switch state {
				case "files":
					for _, p := range paths {
						f.write(strings.TrimPrefix(p, "/"), "untouched", 0o600)
					}
				case "symlinks":
					f.write("keep/file", "keep", 0o600)
					for _, p := range paths {
						name := filepath.Join(f.root, p)
						if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink("/keep", name); err != nil {
							t.Fatal(err)
						}
					}
				}
				before := releaseTree(t, f.root)
				code, out, stderr := f.run(args...)
				if code != 0 || stderr != "" {
					t.Fatalf("%d %s %s", code, out, stderr)
				}
				after := releaseTree(t, f.root)
				for _, p := range paths {
					p = filepath.Join(f.root, p)
					for name, data := range before {
						if name == p || strings.HasPrefix(name, p+"/") {
							if after[name] != data {
								t.Fatal("changed", name)
							}
						}
					}
					if state == "absent" {
						if _, err := os.Lstat(p); !os.IsNotExist(err) {
							t.Fatal("created", p, err)
						}
					}
				}
				if state == "absent" {
					f.missing("etc/xdg")
				}
			})
		}
	}
}

func TestReleaseTurnsValidationBeforeAnyChange(t *testing.T) {
	// R-1DA8-9F99 R-1EI4-N6ZY
	for _, path := range []string{release.TurnsPath, release.TurnsUnitsPath + "/" + release.TurnsService, release.TurnsUnitsPath + "/" + release.TurnsTimer, "turns/bin", "turns/share", "turns/share/systemd"} {
		for _, kind := range []string{"absent", "link", "directory"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				f := newTransitionFixture(t)
				f.addTurns(transitionSHA)
				base := filepath.Join(f.root, release.ReleasesDir, transitionSHA)
				p := filepath.Join(base, path)
				if err := os.RemoveAll(p); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "link":
					if err := os.Symlink("/outside", p); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(p, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				before := releaseTree(t, f.root)
				code, out, stderr := f.run("activate", transitionSHA)
				missing := strings.TrimPrefix(path, "turns/")
				switch missing {
				case "bin":
					missing = "bin/turns"
				case "share", "share/systemd":
					missing = "share/systemd/turns.service"
				}
				if code != 1 || stderr != "opsctl: activate failed\n" || out != "release: failed: turns: "+missing+" is missing\n" || len(f.commands) != 0 || !reflect.DeepEqual(before, releaseTree(t, f.root)) {
					t.Fatalf("%d %s %s commands %v", code, out, stderr, f.commands)
				}
			})
		}
	}
	f := newTransitionFixture(t)
	f.addTurns(transitionSHA)
	f.write("opt/ikigenba/releases/"+transitionSHA+"/dummy/sbin/extra", "x", 0o600)
	if err := os.Remove(filepath.Join(f.root, release.ReleasesDir, transitionSHA, release.TurnsPath)); err != nil {
		t.Fatal(err)
	}
	_, out, _ := f.run("activate", transitionSHA)
	if !strings.HasPrefix(out, "release: failed: dummy: sbin is not allowed;") || len(f.commands) != 0 {
		t.Fatal(out, f.commands)
	}
}

func TestTurnsTreePermitsAdditionalEntriesAndIgnoresNonTrees(t *testing.T) {
	// R-1DA8-9F99 R-UDNX-V6NB
	for _, kind := range []string{"directory", "file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := newTransitionFixture(t)
			base := "opt/ikigenba/releases/" + transitionSHA + "/turns"
			hidden := "opt/ikigenba/releases/.unpack.fixture/keep"
			f.write(hidden, "unpack in progress", 0o600)
			switch kind {
			case "directory":
				f.addTurns(transitionSHA)
				f.write(base+"/anything/extra", "accepted", 0o600)
			case "file":
				f.write(base, "accepted", 0o600)
			case "symlink":
				if err := os.Symlink("/nowhere", filepath.Join(f.root, base)); err != nil {
					t.Fatal(err)
				}
			}
			code, out, stderr := f.run("activate", transitionSHA)
			if code != 0 || stderr != "" || !strings.Contains(out, "manifests: ok (1 app)") {
				t.Fatalf("%d %s %s", code, out, stderr)
			}
			info, err := os.Lstat(filepath.Join(f.root, hidden))
			if err != nil || info.Mode().Perm() != 0o600 || f.read(hidden) != "unpack in progress" {
				t.Fatal("hidden release entry changed", info, err)
			}
		})
	}
}
