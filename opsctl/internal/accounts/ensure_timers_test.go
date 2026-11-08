package accounts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func timerLink(f *userFixture, home, target string) {
	f.t.Helper()
	path := rootPath(f.env.Root, filepath.Join(home, ".config/systemd/user/timers.target.wants", release.TurnsTimer))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		f.t.Fatal(err)
	}
}

func TestEnsureTimersNotInstalled(t *testing.T) {
	// R-NBSA-6CW3 R-NFFZ-BO46
	ensure := EnsureTimers
	for _, kind := range []string{"absent", "dangling"} {
		for _, shadow := range []string{"missing", "malformed"} {
			t.Run(kind+"/"+shadow, func(t *testing.T) {
				f := newUserFixture(t)
				if shadow == "missing" {
					if err := os.Remove(rootPath(f.env.Root, "/etc/shadow")); err != nil {
						t.Fatal(err)
					}
				} else {
					f.write("/etc/shadow", "bad", 0600)
				}
				if kind == "dangling" {
					path := rootPath(f.env.Root, filepath.Join(release.UserUnitDir, release.TurnsTimer))
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("missing-target", path); err != nil {
						t.Fatal(err)
					}
				}
				before := userTree(t, f.env.Root)
				if err := ensure(context.Background(), f.env); err != nil || len(f.commands) != 0 || !reflect.DeepEqual(before, userTree(t, f.env.Root)) {
					t.Fatal(err, f.commands)
				}
			})
		}
	}
}

func TestEnsureTimersFileJudgementAndOrdering(t *testing.T) {
	// R-NJ3O-GZC9 R-NMRD-MAKC
	for _, linger := range []string{"absent", "file", "dangling"} {
		for _, timer := range []string{"absent", "file", "link", "dangling"} {
			t.Run(linger+"/"+timer, func(t *testing.T) {
				f := newUserFixture(t)
				installUserTimer(f)
				f.write("/etc/passwd", strings.Replace(userAccountRows, "/home/alice", "/homes/custom", 1), 0600)
				switch linger {
				case "file":
					f.write("/var/lib/systemd/linger/alice", "", 0600)
				case "dangling":
					path := rootPath(f.env.Root, "/var/lib/systemd/linger/alice")
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("missing", path); err != nil {
						t.Fatal(err)
					}
				}
				switch timer {
				case "file":
					f.write("/homes/custom/.config/systemd/user/timers.target.wants/"+release.TurnsTimer, "ordinary file", 0600)
				case "link":
					timerLink(f, "/homes/custom", "/etc/systemd/user/"+release.TurnsTimer)
				case "dangling":
					timerLink(f, "/homes/custom", "elsewhere")
				}
				before := userTree(t, f.env.Root)
				if err := EnsureTimers(context.Background(), f.env); err != nil {
					t.Fatal(err)
				}
				want := []host.Command{}
				if linger == "absent" || timer == "absent" || timer == "file" {
					want = timerCalls("alice", "1001")
				}
				if len(want) != len(f.commands) || len(want) > 0 && !reflect.DeepEqual(f.commands, want) {
					t.Fatal(f.commands, want)
				}
				if !reflect.DeepEqual(before, userTree(t, f.env.Root)) {
					t.Fatal("operation directly changed state")
				}
			})
		}
	}
	f := newUserFixture(t)
	installUserTimer(f)
	f.write("/etc/passwd", userAccountRows+"dave:x:1007:1007::/home/dave:/bin/bash\ncarol:x:1003:1003::/home/carol:/bin/bash\n", 0600)
	f.write("/etc/group", strings.Replace(userGroups, "bob,alice,missing", "dave,bob,carol,alice,missing", 1), 0600)
	// Every user's missing-state decision must happen before the first command.
	f.run = func(host.Command) (host.Result, error) {
		f.write("/var/lib/systemd/linger/carol", "", 0600)
		timerPath := rootPath(f.env.Root, "/home/carol/.config/systemd/user/timers.target.wants/"+release.TurnsTimer)
		if _, err := os.Lstat(timerPath); errors.Is(err, os.ErrNotExist) {
			timerLink(f, "/home/carol", "somewhere")
		}
		return host.Result{}, nil
	}
	if err := EnsureTimers(context.Background(), f.env); err != nil {
		t.Fatal(err)
	}
	want := append(timerCalls("alice", "1001"), timerCalls("carol", "1003")...)
	want = append(want, timerCalls("dave", "1007")...)
	if !reflect.DeepEqual(f.commands, want) {
		t.Fatal(f.commands, want)
	}
}

func TestEnsureTimersFailureAndRetry(t *testing.T) {
	// R-FDZ7-PEMH R-NMRD-MAKC
	f := newUserFixture(t)
	installUserTimer(f)
	f.write("/etc/passwd", userAccountRows+"carol:x:1003:1003::/home/carol:/bin/bash\ndave:x:1007:1007::/home/dave:/bin/bash\n", 0600)
	f.write("/etc/group", strings.Replace(userGroups, "bob,alice,missing", "dave,bob,carol,alice", 1), 0600)
	failed := host.Result{ExitCode: 1, Stderr: []byte("timer failure")}
	f.run = func(c host.Command) (host.Result, error) {
		if c.Name == "systemctl" && len(c.Args) > 1 && c.Args[1] == "--machine=alice@.host" {
			f.write("/var/lib/systemd/linger/alice", "", 0600)
			timerLink(f, "/home/alice", "elsewhere")
		}
		if c.Name == "systemctl" && len(c.Args) > 1 && c.Args[1] == "--machine=carol@.host" {
			return failed, nil
		}
		return host.Result{}, nil
	}
	err := EnsureTimers(context.Background(), f.env)
	var command *host.CommandError
	ok := errors.As(err, &command)
	want := append(timerCalls("alice", "1001"), timerCalls("carol", "1003")...)
	if !ok || !reflect.DeepEqual(err, command) || command.Error() != "systemctl: exit status 1" || !reflect.DeepEqual(command.Result, failed) || !reflect.DeepEqual(f.commands, want) {
		t.Fatal(err, f.commands)
	}
	f.run = nil
	f.commands = nil
	if err := EnsureTimers(context.Background(), f.env); err != nil {
		t.Fatal(err)
	}
	want = append(timerCalls("carol", "1003"), timerCalls("dave", "1007")...)
	if !reflect.DeepEqual(f.commands, want) {
		t.Fatal(f.commands, want)
	}
	for _, path := range []string{"/etc/passwd", "/etc/group", "/etc/shadow"} {
		for _, kind := range []string{"missing", "directory", "malformed"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				broken := newUserFixture(t)
				installUserTimer(broken)
				if kind == "malformed" {
					broken.write(path, "malformed", 0600)
				} else {
					if err := os.Remove(rootPath(broken.env.Root, path)); err != nil {
						t.Fatal(err)
					}
					if kind == "directory" {
						if err := os.Mkdir(rootPath(broken.env.Root, path), 0700); err != nil {
							t.Fatal(err)
						}
					}
				}
				before := userTree(t, broken.env.Root)
				err := EnsureTimers(context.Background(), broken.env)
				var refused *RefusedError
				if err == nil || errors.As(err, &refused) || len(broken.commands) != 0 || !reflect.DeepEqual(before, userTree(t, broken.env.Root)) {
					t.Fatal(err, broken.commands)
				}
			})
		}
	}
}
