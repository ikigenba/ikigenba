package accounts

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func timerCalls(name, uid string) []host.Command {
	return []host.Command{
		userCall("loginctl", "enable-linger", name),
		userCall("systemctl", "start", "user@"+uid+".service"),
		userCall("systemctl", "--user", "--machine="+name+"@.host", "enable", "--now", release.TurnsTimer),
	}
}

func installUserTimer(f *userFixture) {
	f.t.Helper()
	f.write(filepath.Join(release.UserUnitDir, release.TurnsTimer), "timer fixture", 0644)
}

func userTree(t *testing.T, root string, ignored ...string) map[string]string {
	t.Helper()
	files, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = files.Close() }()
	tree := map[string]string{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		for _, skip := range ignored {
			if path == skip {
				return nil
			}
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		content := ""
		if info.Mode().IsRegular() {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			data, readErr := files.ReadFile(relative)
			if readErr != nil {
				return readErr
			}
			content = string(data)
		}
		tree[path] = fmt.Sprintf("%v:%s", info.Mode(), content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestUserTimerInstallationAndUnlock(t *testing.T) {
	// R-X1WA-KPJ5 R-X346-YH9U R-KV2L-Q7BD
	for _, kind := range []string{"absent", "file", "symlink", "dangling"} {
		for _, name := range []string{"alice", "bob"} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				f := newUserFixture(t)
				path := rootPath(f.env.Root, filepath.Join(release.UserUnitDir, release.TurnsTimer))
				if kind == "file" {
					installUserTimer(f)
				} else if kind != "absent" {
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if kind == "symlink" {
						f.write("/timer-target", "fixture", 0644)
					}
					if err := os.Symlink(rootPath(f.env.Root, "/timer-target"), path); err != nil {
						t.Fatal(err)
					}
				}
				want := []host.Command{}
				uid := "1001"
				if name == "bob" {
					uid = "1002"
					want = append(want, userCall("usermod", "--expiredate", "", name))
				}
				if kind == "file" || kind == "symlink" {
					want = append(want, timerCalls(name, uid)...)
				}
				if err := UnlockUser(context.Background(), f.env, name); err != nil || len(f.commands) != len(want) || len(want) > 0 && !reflect.DeepEqual(f.commands, want) {
					t.Fatal(err, f.commands, want)
				}
				if f.read("/etc/passwd") != userAccountRows || f.read("/etc/group") != userGroups || f.read("/etc/shadow") != userShadow {
					t.Fatal("operation wrote account state outside process boundary")
				}
			})
		}
	}
}

func TestUnlockTimerFailuresAndRetry(t *testing.T) {
	// R-X346-YH9U R-KV2L-Q7BD R-XCVE-0N7E
	for failedStep := 0; failedStep < 4; failedStep++ {
		for _, cannotStart := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", failedStep, cannotStart), func(t *testing.T) {
				f := newUserFixture(t)
				installUserTimer(f)
				failure := host.Result{ExitCode: 7, Stdout: []byte("out"), Stderr: []byte("failure")}
				sentinel := errors.New("cannot start timer command")
				f.run = func(c host.Command) (host.Result, error) {
					if len(f.commands)-1 == failedStep {
						if cannotStart {
							return failure, sentinel
						}
						return failure, nil
					}
					if c.Name == "usermod" {
						f.write("/etc/shadow", "bob:!:1:0:99999:7:::\n", 0600)
						// The timer step must read this updated uid after usermod.
						f.write("/etc/passwd", strings.Replace(userAccountRows, "bob:x:1002:", "bob:x:002345:", 1), 0600)
					}
					return host.Result{}, nil
				}
				want := append([]host.Command{userCall("usermod", "--expiredate", "", "bob")}, timerCalls("bob", "2345")...)
				err := UnlockUser(context.Background(), f.env, "bob")
				var command *host.CommandError
				if !errors.As(err, &command) || command.Label != want[failedStep].Name || !reflect.DeepEqual(command.Result, failure) || cannotStart && !errors.Is(err, sentinel) || !reflect.DeepEqual(f.commands, want[:failedStep+1]) {
					t.Fatal(err, f.commands, want[:failedStep+1])
				}
				if failedStep > 0 && f.read("/etc/shadow") != "bob:!:1:0:99999:7:::\n" {
					t.Fatal("unlock was undone")
				}
				f.commands = nil
				f.run = nil
				if failedStep > 0 {
					if err := UnlockUser(context.Background(), f.env, "bob"); err != nil || !reflect.DeepEqual(f.commands, timerCalls("bob", "2345")) {
						t.Fatal(err, f.commands)
					}
				}
			})
		}
	}
}

func TestUserTimerMissingPasswdEntry(t *testing.T) {
	// R-X346-YH9U R-KV2L-Q7BD
	f := newUserFixture(t)
	installUserTimer(f)
	f.run = func(c host.Command) (host.Result, error) {
		if !reflect.DeepEqual(c, userCall("usermod", "--expiredate", "", "bob")) {
			t.Fatal(c)
		}
		f.write("/etc/passwd", "", 0600)
		return host.Result{}, nil
	}
	err := UnlockUser(context.Background(), f.env, "bob")
	var refused *RefusedError
	if err == nil || errors.As(err, &refused) || len(f.commands) != 1 {
		t.Fatal(err, f.commands)
	}
}

func TestAddUserWithTimer(t *testing.T) {
	// R-2SGU-DN33 R-X346-YH9U R-AMUR-YSIK
	f := newUserFixture(t)
	installUserTimer(f)
	f.write("/etc/ssh/authorized_keys/carol", "replaced", 0600)
	var skel string
	var before map[string]string
	f.run = func(c host.Command) (host.Result, error) {
		if c.Name == "useradd" {
			skel = c.Args[4]
			f.write("/etc/passwd", userAccountRows+"carol:x:7890:7890::/home/carol:/bin/bash\n", 0600)
			f.write("/home/carol/.config/systemd/user/timers.target.wants/fixture", "process-owned fixture", 0600)
			before = userTree(t, f.env.Root, skel, keyPath(f.env.Root, "carol"))
		} else if f.read("/etc/ssh/authorized_keys/carol") != f.key().Line()+"\n" {
			t.Fatal("timer started before key publication")
		}
		return host.Result{}, nil
	}
	if err := AddUser(context.Background(), f.env, "carol", "acme", f.key()); err != nil {
		t.Fatal(err)
	}
	want := append([]host.Command{userCall("useradd", "--create-home", "--home-dir", "/home/carol", "--skel", skel, "--key", "HOME_MODE=0700", "--user-group", "--groups", "shell,acme", "--shell", "/bin/bash", "carol")}, timerCalls("carol", "7890")...)
	if !reflect.DeepEqual(f.commands, want) {
		t.Fatal(f.commands, want)
	}
	if _, err := os.Lstat(skel); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if after := userTree(t, f.env.Root, keyPath(f.env.Root, "carol")); !reflect.DeepEqual(before, after) {
		t.Fatal("add wrote files beyond the key and its temporary skeleton", before, after)
	}
	entries, err := os.ReadDir(rootPath(f.env.Root, "/home/carol"))
	if err != nil || len(entries) != 1 || entries[0].Name() != ".config" {
		t.Fatal(entries, err)
	}
}

func TestAddUserTimerRollback(t *testing.T) {
	// R-2UWN-56KH R-2W4J-IYB6 R-XCVE-0N7E
	for failedStep := 0; failedStep < 3; failedStep++ {
		for _, existed := range []bool{false, true} {
			for _, undoFails := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/%t/%t", failedStep, existed, undoFails), func(t *testing.T) {
					f := newUserFixture(t)
					installUserTimer(f)
					var homeBefore os.FileInfo
					if existed {
						f.write("/home/carol/kept", "precious", 0640)
						f.write("/etc/ssh/authorized_keys/carol", "prior bytes", 0600)
						var err error
						homeBefore, err = os.Lstat(rootPath(f.env.Root, "/home/carol/kept"))
						if err != nil {
							t.Fatal(err)
						}
					}
					failure := host.Result{ExitCode: 9, Stdout: []byte("original stdout"), Stderr: []byte("original stderr")}
					f.run = func(c host.Command) (host.Result, error) {
						if c.Name == "useradd" {
							f.write("/etc/passwd", userAccountRows+"carol:x:9123:9123::/home/carol:/bin/bash\n", 0600)
							f.write("/etc/group", userGroups+"carol:x:9123:\n", 0600)
							if !existed {
								f.write("/home/carol/new", "process fixture", 0600)
							}
							return host.Result{}, nil
						}
						if len(f.commands)-2 == failedStep {
							if failedStep == 2 {
								f.write("/etc/passwd", userAccountRows+"carol:x:5555:9123::/home/carol:/bin/bash\n", 0600)
							}
							return failure, nil
						}
						if len(f.commands)-2 > failedStep && undoFails {
							return host.Result{ExitCode: 17}, nil
						}
						return host.Result{}, nil
					}
					err := AddUser(context.Background(), f.env, "carol", "acme", f.key())
					var command *host.CommandError
					steps := timerCalls("carol", "9123")
					if !errors.As(err, &command) || command.Label != steps[failedStep].Name || !reflect.DeepEqual(command.Result, failure) {
						t.Fatal(err)
					}
					want := append([]host.Command{f.commands[0]}, steps[:failedStep+1]...)
					if failedStep > 0 {
						want = append(want, userCall("loginctl", "disable-linger", "carol"), userCall("systemctl", "stop", "user@9123.service"))
					}
					if existed {
						want = append(want, userCall("userdel", "carol"))
					} else {
						want = append(want, userCall("userdel", "--remove", "carol"))
					}
					want = append(want, userCall("groupdel", "carol"))
					if !reflect.DeepEqual(f.commands, want) {
						t.Fatal(f.commands, want)
					}
					if existed {
						after, err := os.Lstat(rootPath(f.env.Root, "/home/carol/kept"))
						if err != nil {
							t.Fatal(err)
						}
						beforeOwner := homeBefore.Sys().(*syscall.Stat_t)
						afterOwner := after.Sys().(*syscall.Stat_t)
						if homeBefore.Mode() != after.Mode() || beforeOwner.Uid != afterOwner.Uid || beforeOwner.Gid != afterOwner.Gid || f.read("/home/carol/kept") != "precious" {
							t.Fatal(after, err)
						}
						info, err := os.Stat(keyPath(f.env.Root, "carol"))
						if err != nil || info.Mode().Perm() != 0600 || f.read("/etc/ssh/authorized_keys/carol") != "prior bytes" {
							t.Fatal(info, err)
						}
					} else {
						for _, path := range []string{"/home/carol", "/etc/ssh/authorized_keys/carol"} {
							if _, err := os.Lstat(rootPath(f.env.Root, path)); !errors.Is(err, os.ErrNotExist) {
								t.Fatal(path, err)
							}
						}
					}
				})
			}
		}
	}
}
