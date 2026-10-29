package accounts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestAddSSHUser(t *testing.T) {
	// R-AANS-533M R-5FWR-4D0T R-AMUR-YSIK
	f := newUserFixture(t)
	f.write("/etc/ssh/authorized_keys/carol", "old", 0600)
	var skel string
	f.run = func(c host.Command) (host.Result, error) {
		skel = c.Args[4]
		entries, err := os.ReadDir(skel)
		if err != nil || len(entries) != 0 || !strings.HasPrefix(skel, f.env.Root+"/") {
			t.Fatal(skel, entries, err)
		}
		want := userCall("useradd", "--create-home", "--home-dir", "/home/carol", "--skel", skel, "--key", "HOME_MODE=0700", "--user-group", "--groups", "shell,acme", "--shell", "/bin/bash", "carol")
		if !reflect.DeepEqual(c, want) || f.read("/etc/ssh/authorized_keys/carol") != "old" {
			t.Fatal(c)
		}
		return host.Result{}, nil
	}
	if err := AddUser(context.Background(), f.env, "carol", "acme", f.key()); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || f.read("/etc/ssh/authorized_keys/carol") != f.key().Line()+"\n" {
		t.Fatal(f.commands)
	}
	if _, err := os.Lstat(skel); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestAddUserRefusals(t *testing.T) {
	// R-UTMC-BFJ2
	for _, tc := range []struct{ name, group, reason string }{{"alice", "missing", "name taken: alice"}, {"acme", "acme", "name taken: acme"}, {"wheel", "acme", "name taken: wheel"}, {"carol", "shell", "no group 'shell'"}, {"carol", "missing", "no group 'missing'"}} {
		f := newUserFixture(t)
		f.write("/home/"+tc.name+"/kept", "value", 0640)
		f.write("/etc/ssh/authorized_keys/"+tc.name, "old", 0600)
		err := AddUser(context.Background(), f.env, tc.name, tc.group, f.key())
		var refused *RefusedError
		if !errors.As(err, &refused) || refused.Reason != tc.reason || len(f.commands) != 0 || f.read("/home/"+tc.name+"/kept") != "value" || f.read("/etc/ssh/authorized_keys/"+tc.name) != "old" {
			t.Fatal(err)
		}
	}
}

func TestAddUserUndo(t *testing.T) {
	// R-50BW-ILT1 R-51JS-WDJQ R-AMUR-YSIK R-52RP-A5AF
	for _, homeExists := range []bool{false, true} {
		for _, keyExists := range []bool{false, true} {
			for _, writeFails := range []bool{false, true} {
				for _, undoFails := range []bool{false, true} {
					t.Run(fmt.Sprintf("%t/%t/%t/%t", homeExists, keyExists, writeFails, undoFails), func(t *testing.T) {
						f := newUserFixture(t)
						if homeExists {
							f.write("/home/carol/kept", "precious", 0640)
						}
						if keyExists {
							f.write("/etc/ssh/authorized_keys/carol", "previous\nbytes", 0600)
						}
						var skel string
						f.run = func(c host.Command) (host.Result, error) {
							switch c.Name {
							case "useradd":
								skel = c.Args[4]
								f.write("/etc/passwd", userAccountRows+"carol:x:1003:1003::/home/carol:/bin/bash\n", 0600)
								f.write("/etc/group", userGroups+"carol:x:1003:\n", 0600)
								if !homeExists {
									f.write("/home/carol/new", "new", 0600)
								}
								if !writeFails {
									return host.Result{ExitCode: 12, Stderr: []byte("original")}, nil
								}
								if keyExists {
									if err := os.Remove(keyPath(f.env.Root, "carol")); err != nil {
										t.Fatal(err)
									}
								} else {
									if err := os.MkdirAll(filepath.Dir(keyPath(f.env.Root, "carol")), 0700); err != nil {
										t.Fatal(err)
									}
								}
								if err := os.Mkdir(keyPath(f.env.Root, "carol"), 0700); err != nil {
									t.Fatal(err)
								}
							case "userdel":
								args := []string{"carol"}
								if !homeExists {
									args = []string{"--remove", "carol"}
								}
								if !reflect.DeepEqual(c.Args, args) {
									t.Fatal(c)
								}
								if writeFails {
									if err := os.Remove(keyPath(f.env.Root, "carol")); err != nil {
										t.Fatal(err)
									}
								}
								if undoFails {
									return host.Result{ExitCode: 9}, nil
								}
								f.write("/etc/passwd", userAccountRows, 0600)
							case "groupdel":
								if undoFails {
									return host.Result{ExitCode: 8}, nil
								}
								f.write("/etc/group", userGroups, 0600)
							default:
								t.Fatal(c)
							}
							return host.Result{}, nil
						}
						err := AddUser(context.Background(), f.env, "carol", "acme", f.key())
						if err == nil {
							t.Fatal("expected failure")
						}
						var command *host.CommandError
						if !writeFails && (!errors.As(err, &command) || command.Label != "useradd" || command.Result.ExitCode != 12) {
							t.Fatal(err)
						}
						if len(f.commands) != 3 || f.commands[1].Name != "userdel" || f.commands[2].Name != "groupdel" {
							t.Fatal(f.commands)
						}
						if homeExists {
							if f.read("/home/carol/kept") != "precious" {
								t.Fatal("home changed")
							}
							info, _ := os.Stat(rootPath(f.env.Root, "/home/carol/kept"))
							if info.Mode().Perm() != 0640 {
								t.Fatal(info.Mode())
							}
						} else if _, err := os.Lstat(rootPath(f.env.Root, "/home/carol")); !errors.Is(err, os.ErrNotExist) {
							t.Fatal(err)
						}
						if keyExists {
							if f.read("/etc/ssh/authorized_keys/carol") != "previous\nbytes" {
								t.Fatal("key changed")
							}
							info, _ := os.Stat(keyPath(f.env.Root, "carol"))
							if info.Mode().Perm() != 0600 {
								t.Fatal(info.Mode())
							}
						} else if _, err := os.Lstat(keyPath(f.env.Root, "carol")); !errors.Is(err, os.ErrNotExist) {
							t.Fatal(err)
						}
						if _, err := os.Lstat(skel); !errors.Is(err, os.ErrNotExist) {
							t.Fatal(err)
						}
					})
				}
			}
		}
	}
	f := newUserFixture(t)
	f.run = func(host.Command) (host.Result, error) { return host.Result{ExitCode: 12}, nil }
	if err := AddUser(context.Background(), f.env, "carol", "acme", f.key()); err == nil || len(f.commands) != 1 {
		t.Fatal(err, f.commands)
	}
}

func TestSSHUserList(t *testing.T) {
	// R-A87Z-DJM8 R-A9FV-RBCX R-ARQD-HVHC R-5N85-EZGZ
	if StateActive != "active" || StateLocked != "locked" {
		t.Fatal(StateActive, StateLocked)
	}
	f := newUserFixture(t)
	f.write("/etc/ssh/authorized_keys/alice", f.key().Line()+"\n\n"+KeyType+" "+secondKeyBody+" desktop", 0644)
	f.run = func(c host.Command) (host.Result, error) {
		if !reflect.DeepEqual(c.Env, []string{"LC_ALL=C"}) || c.Name != "lastlog" || c.Args[0] != "-u" {
			t.Fatal(c)
		}
		if c.Args[1] == "alice" {
			return host.Result{Stdout: []byte("Header\nalice pts/0 host Thu Oct 29 09:03:00 -0500 2026\n\n"), Stderr: []byte("ignored")}, nil
		}
		return host.Result{Stdout: []byte("bob **Never logged in**\n")}, nil
	}
	got, err := ListUsers(context.Background(), f.env)
	last := time.Date(2026, 10, 29, 14, 3, 0, 0, time.UTC)
	want := UserList{Users: []User{{Name: "alice", Groups: []string{"acme", "beta"}, Keys: []string{f.key().Line(), KeyType + " " + secondKeyBody + " desktop"}, State: StateActive, LastSignIn: &last}, {Name: "bob", Groups: []string{"acme"}, Keys: []string{}, State: StateLocked}}}
	if err != nil || !reflect.DeepEqual(got, want) || len(f.commands) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	f.write("/etc/group", "shell:x:400:\n", 0600)
	f.commands = nil
	got, err = ListUsers(context.Background(), f.env)
	if err != nil || got.Users == nil || len(got.Users) != 0 || len(f.commands) != 0 {
		t.Fatal(got, err)
	}
}

func TestExpirationAndLastlogErrors(t *testing.T) {
	// R-ASY9-VN81 R-5ZF5-8OVX
	f := newUserFixture(t)
	today := f.env.Now().Unix() / 86400
	for _, tc := range []struct {
		value  string
		locked bool
	}{{"", false}, {"-1", false}, {"0", false}, {"1", true}, {strconv.FormatInt(today, 10), true}, {strconv.FormatInt(today+1, 10), false}, {"oops", false}} {
		f.write("/etc/shadow", "alice:!:1:0:99999:7::"+tc.value+":\n", 0600)
		list, err := ListUsers(context.Background(), f.env)
		if err != nil || (list.Users[0].State == StateLocked) != tc.locked || list.Users[1].State != StateActive {
			t.Fatal(tc, list, err)
		}
	}
	for _, output := range []string{"", "\n", "header only", "alice wrong time", "alice host Thu\tOct\t29\t09:03:00\t-0500\t2026\n", "alice host Thu\u2003Oct\u200329\u200309:03:00\u2003-0500\u20032026\n", "alice host Thu Oct 29 09:03:00 -0500 2026\n \n"} {
		f.run = func(host.Command) (host.Result, error) { return host.Result{Stdout: []byte(output)}, nil }
		_, err := ListUsers(context.Background(), f.env)
		var refused *RefusedError
		if err == nil || errors.As(err, &refused) {
			t.Fatal(output, err)
		}
	}
	f.run = func(host.Command) (host.Result, error) { return host.Result{ExitCode: 2}, nil }
	_, err := ListUsers(context.Background(), f.env)
	var command *host.CommandError
	if !errors.As(err, &command) || command.Label != "lastlog" {
		t.Fatal(err)
	}
}

func TestUserRefusalsAndShadowErrors(t *testing.T) {
	// R-AHZ6-FPJS R-5ICJ-VWI7
	ctx := context.Background()
	userOps := []func(*userFixture, string) error{
		func(f *userFixture, n string) error { return AddKey(ctx, f.env, n, f.key()) }, func(f *userFixture, n string) error { return RemoveKey(ctx, f.env, n, f.key()) }, func(f *userFixture, n string) error { return JoinGroup(ctx, f.env, n, "missing") }, func(f *userFixture, n string) error { return LeaveGroup(ctx, f.env, n, "missing") }, func(f *userFixture, n string) error { return LockUser(ctx, f.env, n) }, func(f *userFixture, n string) error { return UnlockUser(ctx, f.env, n) }, func(f *userFixture, n string) error { return DeleteUser(ctx, f.env, n) },
	}
	for _, name := range []string{"missing", "ec2-user", "acme"} {
		for _, op := range userOps {
			f := newUserFixture(t)
			err := op(f, name)
			var refused *RefusedError
			if !errors.As(err, &refused) || refused.Reason != "no user '"+name+"'" || len(f.commands) != 0 || f.read("/etc/passwd") != userAccountRows || f.read("/etc/group") != userGroups || f.read("/etc/shadow") != userShadow {
				t.Fatal(err)
			}
		}
	}
	shadowOps := append([]func(*userFixture, string) error{}, userOps[4:]...)
	shadowOps = append(shadowOps, func(f *userFixture, _ string) error { _, err := ListUsers(ctx, f.env); return err })
	for _, bad := range []string{"missing", "directory", "malformed"} {
		for _, op := range shadowOps {
			f := newUserFixture(t)
			path := rootPath(f.env.Root, "/etc/shadow")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			switch bad {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				f.write("/etc/shadow", "alice:bad\n", 0600)
			}
			err := op(f, "alice")
			var refused *RefusedError
			if err == nil || errors.As(err, &refused) || len(f.commands) != 0 || f.read("/etc/passwd") != userAccountRows || f.read("/etc/group") != userGroups {
				t.Fatal(bad, err)
			}
		}
	}
}

func TestMembershipAndLocking(t *testing.T) {
	// R-UUU8-P79R R-UW25-2Z0G R-60N1-MGMM R-B3XD-BKWA
	f := newUserFixture(t)
	ctx := context.Background()
	if err := JoinGroup(ctx, f.env, "alice", "acme"); err != nil || len(f.commands) != 0 {
		t.Fatal(err)
	}
	if err := LeaveGroup(ctx, f.env, "bob", "beta"); err != nil || len(f.commands) != 0 {
		t.Fatal(err)
	}
	if err := JoinGroup(ctx, f.env, "bob", "beta"); err != nil {
		t.Fatal(err)
	}
	if err := LeaveGroup(ctx, f.env, "bob", "acme"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.commands, []host.Command{userCall("gpasswd", "--add", "bob", "beta"), userCall("gpasswd", "--delete", "bob", "acme")}) || f.read("/etc/group") != userGroups || f.read("/etc/shadow") != userShadow {
		t.Fatal(f.commands)
	}
	for _, op := range []func(context.Context, host.Env, string, string) error{JoinGroup, LeaveGroup} {
		err := op(ctx, f.env, "alice", "shell")
		var refused *RefusedError
		if !errors.As(err, &refused) || refused.Reason != "no group 'shell'" {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"alice", "bob"} {
		for _, sessions := range []bool{false, true} {
			f := newUserFixture(t)
			f.run = func(c host.Command) (host.Result, error) {
				if c.Name == "loginctl" && c.Args[0] == "list-users" && sessions {
					return host.Result{Stdout: []byte("1001 " + name + "\n1002 other\n")}, nil
				}
				return host.Result{}, nil
			}
			if err := LockUser(ctx, f.env, name); err != nil {
				t.Fatal(err)
			}
			want := []host.Command{}
			if name == "alice" {
				want = append(want, userCall("usermod", "--lock", "--expiredate", "1", name))
			}
			want = append(want, userCall("loginctl", "list-users", "--no-legend"))
			if sessions {
				want = append(want, userCall("loginctl", "terminate-user", name))
			}
			if !reflect.DeepEqual(f.commands, want) {
				t.Fatal(f.commands, want)
			}
		}
	}
	f = newUserFixture(t)
	if err := UnlockUser(ctx, f.env, "alice"); err != nil || len(f.commands) != 0 {
		t.Fatal(err)
	}
	if err := UnlockUser(ctx, f.env, "bob"); err != nil || !reflect.DeepEqual(f.commands, []host.Command{userCall("usermod", "--unlock", "--expiredate", "", "bob")}) {
		t.Fatal(err, f.commands)
	}
	for step := 0; step < 3; step++ {
		f := newUserFixture(t)
		f.run = func(host.Command) (host.Result, error) {
			if len(f.commands)-1 == step {
				return host.Result{ExitCode: 7}, nil
			}
			return host.Result{Stdout: []byte("1001 alice\n")}, nil
		}
		err := LockUser(ctx, f.env, "alice")
		if err == nil || len(f.commands) != step+1 {
			t.Fatal(err, f.commands)
		}
	}
}

func TestDeleteUserReassignAndOrder(t *testing.T) {
	// R-5QVU-KAP2 R-5JKG-9O8W R-B559-PCMZ
	f := newUserFixture(t)
	ctx := context.Background()
	err := DeleteUser(ctx, f.env, "alice")
	var refused *RefusedError
	if !errors.As(err, &refused) || refused.Reason != "user not locked: alice" || len(f.commands) != 0 {
		t.Fatal(err)
	}
	uid := strconv.Itoa(os.Getuid())
	f.write("/etc/passwd", strings.Replace(userAccountRows, "bob:x:1002:", "bob:x:"+uid+":", 1), 0600)
	f.write("/srv/acme/owned", "shared", 0640)
	f.write("/srv/misc/deep/owned", "other", 0600)
	f.write("/srv/direct", "direct", 0600)
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, rootPath(f.env.Root, "/srv/acme/link")); err != nil {
		t.Fatal(err)
	}
	f.write("/etc/ssh/authorized_keys/bob", f.key().Line(), 0644)
	var reassigned []string
	f.run = func(c host.Command) (host.Result, error) {
		switch c.Name {
		case "chown":
			if len(c.Args) < 4 || c.Args[0] != "--no-dereference" || c.Args[2] != "--" {
				t.Fatal(c)
			}
			for _, path := range c.Args[3:] {
				wantOwner := "root"
				if strings.HasPrefix(path, rootPath(f.env.Root, "/srv/acme")) {
					wantOwner = "acme"
				}
				if c.Args[1] != wantOwner {
					t.Fatal(c)
				}
				reassigned = append(reassigned, path)
			}
			if _, err := os.Stat(keyPath(f.env.Root, "bob")); err != nil {
				t.Fatal("key removed before reassignment")
			}
		case "userdel":
			if !reflect.DeepEqual(c.Args, []string{"--remove", "bob"}) {
				t.Fatal(c)
			}
			if _, err := os.Stat(keyPath(f.env.Root, "bob")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("key not removed first")
			}
			f.write("/etc/passwd", strings.Replace(f.read("/etc/passwd"), "bob:x:"+uid+":1002::/home/bob:/bin/bash\n", "", 1), 0600)
		case "groupdel":
			if !reflect.DeepEqual(c.Args, []string{"bob"}) {
				t.Fatal(c)
			}
		default:
			t.Fatal(c)
		}
		return host.Result{}, nil
	}
	if err := DeleteUser(ctx, f.env, "bob"); err != nil {
		t.Fatal(err)
	}
	want := []string{rootPath(f.env.Root, "/srv/acme"), rootPath(f.env.Root, "/srv/acme/link"), rootPath(f.env.Root, "/srv/acme/owned"), rootPath(f.env.Root, "/srv/direct"), rootPath(f.env.Root, "/srv/misc"), rootPath(f.env.Root, "/srv/misc/deep"), rootPath(f.env.Root, "/srv/misc/deep/owned")}
	slices.Sort(reassigned)
	slices.Sort(want)
	if !reflect.DeepEqual(reassigned, want) || f.commands[len(f.commands)-2].Name != "userdel" || f.commands[len(f.commands)-1].Name != "groupdel" {
		t.Fatal(reassigned, f.commands)
	}
	outsideRoot, err := os.OpenRoot(outsideDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outsideRoot.Close() }()
	if data, err := outsideRoot.ReadFile("outside"); err != nil || string(data) != "outside" {
		t.Fatal(err)
	}
}

func TestDeleteRetriesAndFailure(t *testing.T) {
	// R-61UY-08DB R-52RP-A5AF
	for _, stage := range []string{"chown", "userdel", "groupdel"} {
		f := newUserFixture(t)
		f.write("/etc/passwd", strings.Replace(userAccountRows, "bob:x:1002:", fmt.Sprintf("bob:x:%d:", os.Getuid()), 1), 0600)
		f.write("/srv/acme/file", "x", 0600)
		f.write("/etc/ssh/authorized_keys/bob", "key", 0644)
		f.run = func(c host.Command) (host.Result, error) {
			if c.Name == stage {
				return host.Result{ExitCode: 8}, nil
			}
			return host.Result{}, nil
		}
		err := DeleteUser(context.Background(), f.env, "bob")
		if err == nil || f.commands[len(f.commands)-1].Name != stage {
			t.Fatal(err, f.commands)
		}
		f.commands = nil
		f.run = func(host.Command) (host.Result, error) { return host.Result{}, nil }
		if err := DeleteUser(context.Background(), f.env, "bob"); err != nil {
			t.Fatal(err)
		}
	}
	f := newUserFixture(t)
	f.write("/srv", "not a directory", 0600)
	err := DeleteUser(context.Background(), f.env, "bob")
	var refused *RefusedError
	if err == nil || errors.As(err, &refused) || len(f.commands) != 0 {
		t.Fatal(err)
	}
	for _, gid := range []string{"1002", "5000"} {
		f := newUserFixture(t)
		f.run = func(c host.Command) (host.Result, error) {
			if c.Name == "userdel" {
				f.write("/etc/group", strings.Replace(userGroups, "bob:x:1002:", "bob:x:"+gid+":", 1), 0600)
			}
			return host.Result{}, nil
		}
		if err := DeleteUser(context.Background(), f.env, "bob"); err != nil {
			t.Fatal(err)
		}
		want := 1
		if gid == "1002" {
			want = 2
		}
		if len(f.commands) != want {
			t.Fatal(f.commands)
		}
	}
	f = newUserFixture(t)
	f.run = func(c host.Command) (host.Result, error) {
		if c.Name == "userdel" {
			f.write("/etc/passwd", strings.Replace(userAccountRows, "bob:x:1002:1002::/home/bob:/bin/bash\n", "", 1), 0600)
			f.write("/etc/group", strings.Replace(userGroups, "bob:x:1002:\n", "", 1), 0600)
		}
		return host.Result{}, nil
	}
	if err := DeleteUser(context.Background(), f.env, "bob"); err != nil {
		t.Fatal(err)
	}
	f.commands = nil
	err = DeleteUser(context.Background(), f.env, "bob")
	if !errors.As(err, &refused) || refused.Reason != "no user 'bob'" || len(f.commands) != 0 {
		t.Fatal(err)
	}
	f = newUserFixture(t)
	f.write("/etc/passwd", strings.Replace(userAccountRows, "bob:x:1002:", "bob:x:"+strconv.Itoa(os.Getuid()+1)+":", 1), 0600)
	f.write("/srv/acme/file", "x", 0600)
	if err := DeleteUser(context.Background(), f.env, "bob"); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.commands {
		if c.Name == "chown" {
			t.Fatal(c)
		}
	}
}

func TestUserCommandErrors(t *testing.T) {
	// R-5S3Q-Y2FR
	for _, startFailure := range []bool{false, true} {
		for _, op := range []struct {
			label string
			call  func(*userFixture) error
		}{
			{"useradd", func(f *userFixture) error { return AddUser(context.Background(), f.env, "carol", "acme", f.key()) }},
			{"gpasswd", func(f *userFixture) error { return JoinGroup(context.Background(), f.env, "bob", "beta") }},
			{"usermod", func(f *userFixture) error { return UnlockUser(context.Background(), f.env, "bob") }},
			{"loginctl", func(f *userFixture) error { return LockUser(context.Background(), f.env, "bob") }},
			{"lastlog", func(f *userFixture) error { _, err := ListUsers(context.Background(), f.env); return err }},
			{"userdel", func(f *userFixture) error { return DeleteUser(context.Background(), f.env, "bob") }},
			{"groupdel", func(f *userFixture) error { return DeleteUser(context.Background(), f.env, "bob") }},
			{"chown", func(f *userFixture) error {
				f.write("/etc/passwd", strings.Replace(userAccountRows, "bob:x:1002:", fmt.Sprintf("bob:x:%d:", os.Getuid()), 1), 0600)
				f.write("/srv/file", "x", 0600)
				return DeleteUser(context.Background(), f.env, "bob")
			}},
		} {
			t.Run(op.label+strconv.FormatBool(startFailure), func(t *testing.T) {
				f := newUserFixture(t)
				sentinel := errors.New("cannot start")
				result := host.Result{ExitCode: 7, Stdout: []byte("out"), Stderr: []byte("err")}
				f.run = func(c host.Command) (host.Result, error) {
					if op.label == "groupdel" && c.Name == "userdel" {
						return host.Result{}, nil
					}
					if startFailure {
						return result, sentinel
					}
					return result, nil
				}
				err := op.call(f)
				var command *host.CommandError
				if !errors.As(err, &command) || command.Label != op.label || !reflect.DeepEqual(command.Result, result) {
					t.Fatal(err, command)
				}
				if startFailure && !errors.Is(err, sentinel) {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestUserOperationsRejectNamesAndAccountFiles(t *testing.T) {
	ctx := context.Background()
	ops := []func(*userFixture, string) error{
		func(f *userFixture, n string) error { return AddUser(ctx, f.env, n, "acme", f.key()) }, func(f *userFixture, n string) error { return AddUser(ctx, f.env, "carol", n, f.key()) },
		func(f *userFixture, n string) error { return AddKey(ctx, f.env, n, f.key()) }, func(f *userFixture, n string) error { return RemoveKey(ctx, f.env, n, f.key()) },
		func(f *userFixture, n string) error { return JoinGroup(ctx, f.env, n, "acme") }, func(f *userFixture, n string) error { return JoinGroup(ctx, f.env, "alice", n) }, func(f *userFixture, n string) error { return LeaveGroup(ctx, f.env, n, "acme") }, func(f *userFixture, n string) error { return LeaveGroup(ctx, f.env, "alice", n) },
		func(f *userFixture, n string) error { return LockUser(ctx, f.env, n) }, func(f *userFixture, n string) error { return UnlockUser(ctx, f.env, n) }, func(f *userFixture, n string) error { return DeleteUser(ctx, f.env, n) },
	}
	for _, op := range ops {
		f := newUserFixture(t)
		err := op(f, "bad/name")
		var refused *RefusedError
		if err == nil || errors.As(err, &refused) || len(f.commands) != 0 || f.read("/etc/passwd") != userAccountRows || f.read("/etc/group") != userGroups || f.read("/etc/shadow") != userShadow {
			t.Fatal(err)
		}
	}
	malformedOps := []func(*userFixture) error{
		func(f *userFixture) error { return AddUser(ctx, f.env, "carol", "acme", f.key()) }, func(f *userFixture) error { _, err := ListUsers(ctx, f.env); return err }, func(f *userFixture) error { return AddKey(ctx, f.env, "alice", f.key()) }, func(f *userFixture) error { return RemoveKey(ctx, f.env, "alice", f.key()) }, func(f *userFixture) error { return JoinGroup(ctx, f.env, "alice", "acme") }, func(f *userFixture) error { return LeaveGroup(ctx, f.env, "alice", "acme") }, func(f *userFixture) error { return LockUser(ctx, f.env, "alice") }, func(f *userFixture) error { return UnlockUser(ctx, f.env, "alice") }, func(f *userFixture) error { return DeleteUser(ctx, f.env, "alice") },
	}
	for _, path := range []string{"/etc/passwd", "/etc/group"} {
		for _, fault := range []string{"missing", "malformed", "directory"} {
			for _, op := range malformedOps {
				f := newUserFixture(t)
				if err := os.Remove(rootPath(f.env.Root, path)); err != nil {
					t.Fatal(err)
				}
				switch fault {
				case "malformed":
					f.write(path, "bad:fields\n", 0600)
				case "directory":
					if err := os.Mkdir(rootPath(f.env.Root, path), 0700); err != nil {
						t.Fatal(err)
					}
				}
				err := op(f)
				var refused *RefusedError
				if err == nil || errors.As(err, &refused) || len(f.commands) != 0 {
					t.Fatal(path, fault, err)
				}
			}
		}
	}
}

func TestAddUserPreservesPreexistingSymlinkHome(t *testing.T) {
	// R-50BW-ILT1
	f := newUserFixture(t)
	f.write("/preserved/valuable", "kept", 0640)
	if err := os.MkdirAll(rootPath(f.env.Root, "/home"), 0700); err != nil {
		t.Fatal(err)
	}
	home := rootPath(f.env.Root, "/home/carol")
	if err := os.Symlink(rootPath(f.env.Root, "/preserved"), home); err != nil {
		t.Fatal(err)
	}
	f.run = func(c host.Command) (host.Result, error) {
		if c.Name == "useradd" {
			f.write("/etc/passwd", userAccountRows+"carol:x:1003:1003::/home/carol:/bin/bash\n", 0600)
			f.write("/etc/group", userGroups+"carol:x:1003:\n", 0600)
			return host.Result{ExitCode: 12}, nil
		}
		if c.Name == "userdel" && !reflect.DeepEqual(c.Args, []string{"carol"}) {
			t.Fatal(c)
		}
		return host.Result{}, nil
	}
	if err := AddUser(context.Background(), f.env, "carol", "acme", f.key()); err == nil {
		t.Fatal("expected failure")
	}
	info, err := os.Lstat(home)
	if err != nil || info.Mode()&os.ModeSymlink == 0 || f.read("/preserved/valuable") != "kept" {
		t.Fatal(info, err)
	}
}
