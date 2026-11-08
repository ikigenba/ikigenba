package accounts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func newRestoreFixture(t *testing.T) *userFixture {
	t.Helper()
	f := newUserFixture(t)
	f.write("/etc/passwd", "root:x:0:0::/root:/bin/bash\nacme:x:1004:1004::/srv/acme:/usr/sbin/nologin\nbeta:x:1005:1005::/srv/beta:/usr/sbin/nologin\n", 0600)
	f.write("/etc/group", "root:x:0:\nshell:x:400:\nacme:x:1004:\nbeta:x:1005:\n", 0600)
	f.write("/etc/shadow", "", 0600)
	return f
}

func commandArg(c host.Command, key string) string {
	for i, arg := range c.Args {
		if arg == key && i+1 < len(c.Args) {
			return c.Args[i+1]
		}
	}
	return ""
}

func (f *userFixture) restoreMutation(c host.Command) {
	f.t.Helper()
	name := ""
	if len(c.Args) > 0 {
		name = c.Args[len(c.Args)-1]
	}
	switch c.Name {
	case "groupadd":
		f.write("/etc/group", f.read("/etc/group")+name+":x:"+commandArg(c, "--gid")+":\n", 0600)
	case "useradd":
		gid := commandArg(c, "--uid")
		f.write("/etc/passwd", f.read("/etc/passwd")+name+":x:"+commandArg(c, "--uid")+":"+gid+"::"+commandArg(c, "--home-dir")+":"+commandArg(c, "--shell")+"\n", 0600)
		state := ""
		if commandArg(c, "--expiredate") != "" {
			state = "1"
		}
		f.write("/etc/shadow", f.read("/etc/shadow")+name+":!:1:0:99999:7::"+state+":\n", 0600)
		for _, g := range strings.Split(commandArg(c, "--groups"), ",") {
			if g != "" {
				f.changeGroup(g, name, true)
			}
		}
	case "gpasswd":
		f.changeGroup(c.Args[2], c.Args[1], c.Args[0] == "--add")
	case "usermod":
		rows := strings.Split(f.read("/etc/shadow"), "\n")
		for i, row := range rows {
			parts := strings.Split(row, ":")
			if len(parts) == 9 && parts[0] == name {
				parts[7] = c.Args[1]
				rows[i] = strings.Join(parts, ":")
			}
		}
		f.write("/etc/shadow", strings.Join(rows, "\n"), 0600)
	case "userdel", "groupdel":
		path := "/etc/passwd"
		if c.Name == "groupdel" {
			path = "/etc/group"
		}
		rows := []string{}
		for _, row := range strings.Split(f.read(path), "\n") {
			if row != "" && !strings.HasPrefix(row, name+":") {
				rows = append(rows, row)
			}
		}
		text := strings.Join(rows, "\n")
		if text != "" {
			text += "\n"
		}
		f.write(path, text, 0600)
	}
}

func (f *userFixture) changeGroup(groupName, name string, add bool) {
	f.t.Helper()
	rows := strings.Split(f.read("/etc/group"), "\n")
	for i, row := range rows {
		p := strings.Split(row, ":")
		if len(p) != 4 || p[0] != groupName {
			continue
		}
		members := []string{}
		if p[3] != "" {
			members = strings.Split(p[3], ",")
		}
		members = slices.DeleteFunc(members, func(member string) bool { return member == name })
		if add {
			members = append(members, name)
		}
		p[3] = strings.Join(members, ",")
		rows[i] = strings.Join(p, ":")
	}
	f.write("/etc/group", strings.Join(rows, "\n"), 0600)
}

func TestEnsureShellGroupProtocolAndErrors(t *testing.T) {
	// R-A02U-C3RE R-AS4J-4TTD R-YII3-AGIQ
	for _, code := range []int{0, 2, 1} {
		for _, failed := range []string{"", "getent", "groupadd"} {
			t.Run(fmt.Sprintf("%d/%s", code, failed), func(t *testing.T) {
				f := newRestoreFixture(t)
				before := userTree(t, f.env.Root)
				cause := errors.New("exec unavailable")
				result := host.Result{ExitCode: code, Stdout: []byte("captured"), Stderr: []byte("detail")}
				f.run = func(c host.Command) (host.Result, error) {
					if c.Name == failed {
						return result, cause
					}
					if c.Name == "getent" {
						return result, nil
					}
					return host.Result{}, nil
				}
				err := EnsureShellGroup(context.Background(), f.env)
				want := []host.Command{userCall("getent", "group", "shell")}
				if code == 2 && failed != "getent" {
					want = append(want, userCall("groupadd", "--system", "shell"))
				}
				if !reflect.DeepEqual(f.commands, want) || !reflect.DeepEqual(before, userTree(t, f.env.Root)) {
					t.Fatal(f.commands, want)
				}
				failure := failed == "getent" || code == 1 || code == 2 && failed == "groupadd"
				if failure {
					var command *host.CommandError
					label := "getent"
					if code == 2 && failed == "groupadd" {
						label = "groupadd"
					}
					if !errors.As(err, &command) || command.Label != label || !reflect.DeepEqual(command.Result, result) {
						t.Fatal(err)
					}
					if failed == command.Label && !errors.Is(err, cause) {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRestoreGroupValidationAndExisting(t *testing.T) {
	// R-2JX2-CIYK R-A7E8-MQ7K
	for _, kind := range []string{"name", "member", "uid", "gid", "collision", "existing"} {
		t.Run(kind, func(t *testing.T) {
			f := newRestoreFixture(t)
			g := ManifestGroup{Slug: "acme", UID: 1004, GID: 1004, Members: []string{"alice"}}
			f.write("/srv/acme/kept", "unchanged", 0640)
			f.write("/etc/gitconfig", "unchanged", 0600)
			switch kind {
			case "name":
				g.Slug = "bad!"
			case "member":
				g.Members = []string{"bad!"}
			case "uid":
				g.UID = -1
			case "gid":
				g.GID = -1
			case "collision":
				g.UID = 999
			}
			before := userTree(t, f.env.Root)
			err := RestoreGroup(context.Background(), f.env, g)
			var refused *RefusedError
			switch {
			case kind == "existing":
				if err != nil {
					t.Fatal(err)
				}
			case err == nil:
				t.Fatal("validation succeeded")
			case errors.As(err, &refused) != (kind == "collision"):
				t.Fatal(err)
			}
			if len(f.commands) != 0 || !reflect.DeepEqual(before, userTree(t, f.env.Root)) {
				t.Fatal("validation/existing group had effects", f.commands)
			}
		})
	}
}

func TestRestoreGroupExplicitIDsAndFolder(t *testing.T) {
	// R-YNDO-TJHI R-YII3-AGIQ
	for _, exists := range []bool{false, true} {
		for _, repository := range []bool{false, true} {
			t.Run(fmt.Sprintf("%t/%t", exists, repository), func(t *testing.T) {
				f := newRestoreFixture(t)
				if exists {
					f.write("/srv/newbiz/kept", "precious", 0640)
				}
				if repository {
					f.write("/srv/newbiz/.git/config", "existing repository", 0600)
				}
				dir := rootPath(f.env.Root, "/srv/newbiz")
				f.run = func(c host.Command) (host.Result, error) {
					if c.Name == "chown" {
						info, err := os.Stat(dir)
						if err != nil || !info.IsDir() || !exists && !repository && info.Mode().Perm() != 0700 {
							t.Fatal(info, err)
						}
					}
					return host.Result{}, nil
				}
				g := ManifestGroup{Slug: "newbiz", UID: 12004, GID: 13004, Members: []string{"alice"}}
				if err := RestoreGroup(context.Background(), f.env, g); err != nil {
					t.Fatal(err)
				}
				want := []host.Command{userCall("groupadd", "--system", "--gid", "13004", "newbiz"), userCall("useradd", "--system", "--uid", "12004", "--gid", "newbiz", "--home-dir", "/srv/newbiz", "--no-create-home", "--shell", "/usr/sbin/nologin", "newbiz"), userCall("chown", "newbiz:newbiz", dir), userCall("chmod", "2770", dir), userCall("setfacl", "-d", "-m", "g::rwx,o::-", dir)}
				if !repository {
					git := userCall("runuser", "-u", "newbiz", "--", "git", "init", "--shared=group", dir)
					git.Dir = dir
					want = append(want, git)
				}
				want = append(want, userCall("git", "config", "--file", rootPath(f.env.Root, "/etc/gitconfig"), "--get-all", "safe.directory"), userCall("git", "config", "--file", rootPath(f.env.Root, "/etc/gitconfig"), "--add", "safe.directory", "/srv/newbiz"))
				if !reflect.DeepEqual(f.commands, want) {
					t.Fatal(f.commands, want)
				}
				if exists && f.read("/srv/newbiz/kept") != "precious" {
					t.Fatal("folder rewritten")
				}
			})
		}
	}
	f := newRestoreFixture(t)
	f.write("/srv/newbiz", "regular", 0600)
	err := RestoreGroup(context.Background(), f.env, ManifestGroup{Slug: "newbiz", UID: 12004, GID: 13004})
	var refused *RefusedError
	if !errors.As(err, &refused) || refused.Reason != "not a directory: /srv/newbiz" || len(f.commands) != 0 {
		t.Fatal(err, f.commands)
	}
}

func TestRestoreGroupFailureUndo(t *testing.T) {
	// R-YNDO-TJHI R-AS4J-4TTD
	for fail := 0; fail < 8; fail++ {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			f := newRestoreFixture(t)
			f.write("/srv/newbiz/kept", "precious", 0640)
			failure := host.Result{ExitCode: 9, Stdout: []byte("original"), Stderr: []byte("failure")}
			f.run = func(c host.Command) (host.Result, error) {
				if len(f.commands) == fail+1 {
					return failure, nil
				}
				f.restoreMutation(c)
				return host.Result{}, nil
			}
			err := RestoreGroup(context.Background(), f.env, ManifestGroup{Slug: "newbiz", UID: 12004, GID: 12004})
			var command *host.CommandError
			label := f.commands[fail].Name
			if label == "runuser" {
				label = "git"
			}
			if !errors.As(err, &command) || !reflect.DeepEqual(err, command) || command.Label != label || !reflect.DeepEqual(command.Result, failure) {
				t.Fatal(err, f.commands)
			}
			// First failing operation is followed only by account/group removal.
			tail := f.commands[fail+1:]
			if fail == 0 {
				if len(tail) != 0 {
					t.Fatal(tail)
				}
			} else {
				want := []host.Command{userCall("groupdel", "newbiz")}
				if fail > 1 {
					want = append([]host.Command{userCall("userdel", "newbiz")}, want...)
				}
				if !reflect.DeepEqual(tail, want) {
					t.Fatal(tail, want)
				}
			}
			if f.read("/srv/newbiz/kept") != "precious" {
				t.Fatal("undo changed folder")
			}
		})
	}
}

func TestRestoreUserValidationPrecedence(t *testing.T) {
	// R-A9U1-E9OY
	for _, kind := range []string{"name", "group-name", "duplicate-group", "uid", "gid", "state", "missing-shadow", "malformed-shadow", "collision", "missing-group"} {
		t.Run(kind, func(t *testing.T) {
			f := newRestoreFixture(t)
			u := ManifestUser{Name: "alice", UID: 1001, GID: 1001, State: StateActive, Groups: []string{"zeta", "delta"}}
			switch kind {
			case "name":
				u.Name = "bad!"
			case "group-name":
				u.Groups = []string{"bad!"}
			case "duplicate-group":
				u.Groups = []string{"acme", "acme"}
			case "uid":
				u.UID = -1
			case "gid":
				u.GID = -1
			case "state":
				u.State = "bad"
			case "missing-shadow":
				if err := os.Remove(rootPath(f.env.Root, "/etc/shadow")); err != nil {
					t.Fatal(err)
				}
			case "malformed-shadow":
				f.write("/etc/shadow", "invalid", 0600)
			case "collision":
				f.write("/etc/passwd", f.read("/etc/passwd")+"alice:x:1001:1001::/home/alice:/bin/bash\n", 0600)
			}
			before := userTree(t, f.env.Root)
			err := RestoreUser(context.Background(), f.env, u)
			var refused *RefusedError
			if err == nil || len(f.commands) != 0 || !reflect.DeepEqual(before, userTree(t, f.env.Root)) {
				t.Fatal(err, f.commands)
			}
			refusal := kind == "collision" || kind == "missing-group"
			if errors.As(err, &refused) != refusal {
				t.Fatal(err)
			}
			if kind == "collision" && refused.Reason != "alice on this host is not a user" {
				t.Fatal(err)
			}
			if kind == "missing-group" && refused.Reason != "no group 'delta'" {
				t.Fatal(err)
			}
		})
	}
}

func TestRestoreNewUserCommandsAndState(t *testing.T) {
	// R-AC9U-5T6C R-AJL8-GFMI R-YII3-AGIQ
	for _, state := range []string{StateActive, StateLocked} {
		for _, timer := range []bool{false, true} {
			for _, groups := range [][]string{nil, {"beta", "acme"}} {
				t.Run(fmt.Sprintf("%s/%t/%v", state, timer, groups), func(t *testing.T) {
					f := newRestoreFixture(t)
					if timer {
						installUserTimer(f)
					}
					f.write("/home/alice/kept", "precious", 0640)
					f.write("/etc/ssh/authorized_keys/alice", "saved key", 0644)
					beforeHome, err := os.Stat(rootPath(f.env.Root, "/home/alice/kept"))
					if err != nil {
						t.Fatal(err)
					}
					f.run = func(c host.Command) (host.Result, error) { f.restoreMutation(c); return host.Result{}, nil }
					u := ManifestUser{Name: "alice", UID: 1001, GID: 1001, State: state, Groups: groups}
					if err := RestoreUser(context.Background(), f.env, u); err != nil {
						t.Fatal(err)
					}
					groupList := "shell"
					if len(groups) > 0 {
						groupList += ",acme,beta"
					}
					args := []string{"--uid", "1001", "--gid", "alice", "--groups", groupList, "--home-dir", "/home/alice", "--no-create-home", "--shell", "/bin/bash"}
					if state == StateLocked {
						args = append(args, "--expiredate", "1970-01-02")
					}
					args = append(args, "alice")
					want := []host.Command{userCall("groupadd", "--gid", "1001", "alice"), userCall("useradd", args...)}
					if state == StateLocked {
						want = append(want, userCall("loginctl", "disable-linger", "alice"), userCall("loginctl", "list-users", "--no-legend"))
					} else if timer {
						want = append(want, timerCalls("alice", "1001")...)
					}
					if !reflect.DeepEqual(f.commands, want) {
						t.Fatal(f.commands, want)
					}
					afterHome, err := os.Stat(rootPath(f.env.Root, "/home/alice/kept"))
					if err != nil || !os.SameFile(beforeHome, afterHome) || beforeHome.Mode() != afterHome.Mode() || f.read("/home/alice/kept") != "precious" || f.read("/etc/ssh/authorized_keys/alice") != "saved key" {
						t.Fatal("restore changed home/key", err)
					}
				})
			}
		}
	}
}

func TestRestoreUserCreationFailureUndo(t *testing.T) {
	// R-AEPM-XCNQ R-AS4J-4TTD
	for _, kind := range []string{"groupadd", "private-group", "missing-group", "changed-gid", "account-created", "undo-fails"} {
		t.Run(kind, func(t *testing.T) {
			f := newRestoreFixture(t)
			failure := host.Result{ExitCode: 17, Stderr: []byte("original failure")}
			f.run = func(c host.Command) (host.Result, error) {
				if c.Name == "groupadd" {
					if kind == "groupadd" {
						return failure, nil
					}
					f.restoreMutation(c)
					return host.Result{}, nil
				}
				if c.Name == "useradd" {
					switch kind {
					case "missing-group":
						f.write("/etc/group", strings.Replace(f.read("/etc/group"), "alice:x:1001:\n", "", 1), 0600)
					case "changed-gid":
						f.write("/etc/group", strings.Replace(f.read("/etc/group"), "alice:x:1001:", "alice:x:9999:", 1), 0600)
					case "account-created":
						f.restoreMutation(c)
					}
					return failure, nil
				}
				if c.Name == "groupdel" && kind == "undo-fails" {
					return host.Result{ExitCode: 8}, nil
				}
				f.restoreMutation(c)
				return host.Result{}, nil
			}
			err := RestoreUser(context.Background(), f.env, ManifestUser{Name: "alice", UID: 1001, GID: 1001, State: StateActive})
			var command *host.CommandError
			label := "useradd"
			if kind == "groupadd" {
				label = "groupadd"
			}
			if !errors.As(err, &command) || !reflect.DeepEqual(err, command) || command.Label != label || !reflect.DeepEqual(command.Result, failure) {
				t.Fatal(err)
			}
			count := 2
			if kind == "groupadd" {
				count = 1
			}
			if kind == "private-group" || kind == "undo-fails" {
				count = 3
			}
			if len(f.commands) != count || count == 3 && !reflect.DeepEqual(f.commands[2], userCall("groupdel", "alice")) {
				t.Fatal(f.commands)
			}
		})
	}
}

func TestRestoreExistingMembershipsAndState(t *testing.T) {
	// R-AH5F-OW54 R-AJL8-GFMI R-AUKB-WDAR
	for _, session := range []bool{false, true} {
		t.Run(fmt.Sprint(session), func(t *testing.T) {
			f := newRestoreFixture(t)
			f.write("/etc/passwd", f.read("/etc/passwd")+"alice:x:1001:1001::/home/alice:/bin/bash\nbob:x:1002:1002::/home/bob:/bin/bash\ndave:x:1003:1003::/home/dave:/bin/bash\n", 0600)
			f.write("/etc/group", "shell:x:400:alice,bob,dave\nacme:x:1004:alice,bob,dave\nbeta:x:1005:alice,bob\nwheel:x:10:alice,bob\nalice:x:1001:\nbob:x:1002:\n", 0600)
			f.write("/etc/shadow", "alice:!:1:0:99999:7:::\nbob:!:1:0:99999:7:::\n", 0600)
			f.run = func(c host.Command) (host.Result, error) {
				f.restoreMutation(c)
				if c.Name == "loginctl" && c.Args[0] == "list-users" && session {
					return host.Result{Stdout: []byte("1002 bob\n1003 dave\n")}, nil
				}
				return host.Result{}, nil
			}
			m := restoreManifest()
			if err := CheckManifest(context.Background(), f.env, m); err != nil {
				t.Fatal(err)
			}
			for _, g := range m.Groups {
				if err := RestoreGroup(context.Background(), f.env, g); err != nil {
					t.Fatal(err)
				}
			}
			if len(f.commands) != 0 {
				t.Fatal(f.commands)
			}
			for _, u := range m.Users {
				if err := RestoreUser(context.Background(), f.env, u); err != nil {
					t.Fatal(err)
				}
			}
			want := []host.Command{userCall("gpasswd", "--delete", "alice", "beta"), userCall("usermod", "--expiredate", "1", "bob"), userCall("loginctl", "disable-linger", "bob"), userCall("loginctl", "list-users", "--no-legend")}
			if session {
				want = append(want, userCall("loginctl", "terminate-user", "bob"))
			}
			if !reflect.DeepEqual(f.commands, want) {
				t.Fatal(f.commands, want)
			}
			groups := f.read("/etc/group")
			if !strings.Contains(groups, "acme:x:1004:alice,bob,dave\n") || !strings.Contains(groups, "wheel:x:10:alice,bob\n") || !strings.Contains(groups, "shell:x:400:alice,bob,dave\n") {
				t.Fatal(groups)
			}
		})
	}
	// Deletes are sorted before sorted additions, and unrelated groups are kept.
	f := newUserFixture(t)
	f.write("/etc/group", strings.Replace(userGroups, "beta:x:302:alice", "beta:x:302:", 1), 0600)
	f.run = func(c host.Command) (host.Result, error) { f.restoreMutation(c); return host.Result{}, nil }
	if err := RestoreUser(context.Background(), f.env, ManifestUser{Name: "alice", UID: 1001, GID: 1001, State: StateActive, Groups: []string{"beta"}}); err != nil {
		t.Fatal(err)
	}
	want := []host.Command{userCall("gpasswd", "--delete", "alice", "acme"), userCall("gpasswd", "--add", "alice", "beta")}
	if !reflect.DeepEqual(f.commands, want) {
		t.Fatal(f.commands, want)
	}
}

func TestRestoreUserFailureStopsAndRetry(t *testing.T) {
	// R-AN8X-LQUL R-AS4J-4TTD R-AJL8-GFMI
	for fail := 0; fail < 3; fail++ {
		for _, cannotStart := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", fail, cannotStart), func(t *testing.T) {
				f := newRestoreFixture(t)
				installUserTimer(f)
				f.write("/home/alice/kept", "precious", 0640)
				f.write("/etc/ssh/authorized_keys/alice", "saved", 0600)
				failure := host.Result{ExitCode: 19, Stdout: []byte("stdout"), Stderr: []byte("stderr")}
				cause := errors.New("start failure")
				f.run = func(c host.Command) (host.Result, error) {
					if len(f.commands) == fail+3 {
						if cannotStart {
							return failure, cause
						}
						return failure, nil
					}
					f.restoreMutation(c)
					return host.Result{}, nil
				}
				u := ManifestUser{Name: "alice", UID: 1001, GID: 1001, State: StateActive, Groups: []string{"acme"}}
				err := RestoreUser(context.Background(), f.env, u)
				var command *host.CommandError
				if !errors.As(err, &command) || !reflect.DeepEqual(err, command) || command.Label != f.commands[fail+2].Name || !reflect.DeepEqual(command.Result, failure) || cannotStart && !errors.Is(err, cause) || len(f.commands) != fail+3 {
					t.Fatal(err, f.commands)
				}
				if !strings.Contains(f.read("/etc/passwd"), "alice:x:1001:") || f.read("/home/alice/kept") != "precious" || f.read("/etc/ssh/authorized_keys/alice") != "saved" {
					t.Fatal("failure undid restored state")
				}
				f.commands = nil
				f.run = func(c host.Command) (host.Result, error) { f.restoreMutation(c); return host.Result{}, nil }
				if err := RestoreUser(context.Background(), f.env, u); err != nil || !reflect.DeepEqual(f.commands, timerCalls("alice", "1001")) {
					t.Fatal(err, f.commands)
				}
			})
		}
	}
	for _, operation := range []string{"--delete", "--add", "usermod", "disable-linger", "list-users", "terminate-user"} {
		t.Run(operation, func(t *testing.T) {
			f := newUserFixture(t)
			u := ManifestUser{Name: "alice", UID: 1001, GID: 1001, State: StateLocked, Groups: []string{"beta"}}
			if operation == "--add" {
				f.write("/etc/group", strings.Replace(userGroups, "beta:x:302:alice", "beta:x:302:", 1), 0600)
			}
			failure := host.Result{ExitCode: 23, Stderr: []byte("failed")}
			f.run = func(c host.Command) (host.Result, error) {
				if c.Name == operation || c.Args[0] == operation {
					return failure, nil
				}
				f.restoreMutation(c)
				if c.Name == "loginctl" && c.Args[0] == "list-users" {
					return host.Result{Stdout: []byte("1001 alice\n")}, nil
				}
				return host.Result{}, nil
			}
			err := RestoreUser(context.Background(), f.env, u)
			var command *host.CommandError
			if !errors.As(err, &command) || !reflect.DeepEqual(err, command) || command.Label != f.commands[len(f.commands)-1].Name || !reflect.DeepEqual(command.Result, failure) {
				t.Fatal(err, f.commands)
			}
			last := f.commands[len(f.commands)-1]
			if last.Name != operation && last.Args[0] != operation {
				t.Fatal("later command ran", f.commands)
			}
		})
	}
}

func TestRestoreUserDirectWritesAbsent(t *testing.T) {
	// R-YII3-AGIQ
	f := newUserFixture(t)
	f.write("/home/alice/kept", "precious", 0640)
	f.write("/etc/ssh/authorized_keys/alice", "saved", 0600)
	before := userTree(t, f.env.Root)
	if err := RestoreUser(context.Background(), f.env, ManifestUser{Name: "alice", UID: 1001, GID: 1001, State: StateActive, Groups: []string{"acme"}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, userTree(t, f.env.Root)) || !reflect.DeepEqual(f.commands, []host.Command{userCall("gpasswd", "--delete", "alice", "beta")}) {
		t.Fatal(f.commands)
	}
	// No direct home creation when commands make only the account files.
	fresh := newRestoreFixture(t)
	fresh.run = func(c host.Command) (host.Result, error) { fresh.restoreMutation(c); return host.Result{}, nil }
	if err := RestoreUser(context.Background(), fresh.env, ManifestUser{Name: "alice", UID: 1001, GID: 1001, State: StateActive}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/home/alice", "/etc/ssh/authorized_keys/alice"} {
		if _, err := os.Lstat(filepath.Join(fresh.env.Root, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal(path, err)
		}
	}
}

func TestRestoreMembershipsMultipleGroupsInOrder(t *testing.T) {
	// R-AH5F-OW54
	f := newRestoreFixture(t)
	f.write("/etc/passwd", f.read("/etc/passwd")+"theta:x:1006:1006::/srv/theta:/bin/false\ndelta:x:1007:1007::/srv/delta:/bin/false\nalice:x:1001:1001::/home/alice:/bin/bash\n", 0600)
	f.write("/etc/group", "shell:x:400:alice\ntheta:x:1006:\nbeta:x:1005:alice,dave\nwheel:x:10:alice\ndelta:x:1007:\nacme:x:1004:alice,dave\n", 0600)
	f.write("/etc/shadow", "alice:!:1:0:99999:7:::\n", 0600)
	f.run = func(c host.Command) (host.Result, error) { f.restoreMutation(c); return host.Result{}, nil }
	u := ManifestUser{Name: "alice", UID: 1001, GID: 1001, State: StateActive, Groups: []string{"theta", "delta"}}
	if err := RestoreUser(context.Background(), f.env, u); err != nil {
		t.Fatal(err)
	}
	want := []host.Command{userCall("gpasswd", "--delete", "alice", "acme"), userCall("gpasswd", "--delete", "alice", "beta"), userCall("gpasswd", "--add", "alice", "delta"), userCall("gpasswd", "--add", "alice", "theta")}
	if !reflect.DeepEqual(f.commands, want) {
		t.Fatal(f.commands, want)
	}
	groups := f.read("/etc/group")
	for _, line := range []string{"shell:x:400:alice\n", "wheel:x:10:alice\n", "beta:x:1005:dave\n", "acme:x:1004:dave\n"} {
		if !strings.Contains(groups, line) {
			t.Fatal(groups)
		}
	}
}

func TestRestoreExistingLockedUserActive(t *testing.T) {
	// R-AJL8-GFMI R-AN8X-LQUL R-AS4J-4TTD
	for _, timer := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%t/%t", timer, fail), func(t *testing.T) {
				f := newUserFixture(t)
				if timer {
					installUserTimer(f)
				}
				failure := host.Result{ExitCode: 6, Stdout: []byte("captured"), Stderr: []byte("failed unlock")}
				f.run = func(c host.Command) (host.Result, error) {
					if fail && c.Name == "usermod" {
						return failure, nil
					}
					f.restoreMutation(c)
					return host.Result{}, nil
				}
				u := ManifestUser{Name: "bob", UID: 1002, GID: 1002, State: StateActive, Groups: []string{"acme"}}
				err := RestoreUser(context.Background(), f.env, u)
				want := []host.Command{userCall("usermod", "--expiredate", "", "bob")}
				if fail {
					var command *host.CommandError
					if !errors.As(err, &command) || !reflect.DeepEqual(err, command) || command.Label != "usermod" || !reflect.DeepEqual(command.Result, failure) {
						t.Fatal(err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if timer {
						want = append(want, timerCalls("bob", "1002")...)
					}
				}
				if !reflect.DeepEqual(f.commands, want) {
					t.Fatal(f.commands, want)
				}
			})
		}
	}
}
