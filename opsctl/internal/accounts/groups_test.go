package accounts_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestTakenNamesRefused(t *testing.T) {
	// R-0D6U-3VJS R-0LQ4-S9QN
	for _, tc := range []struct{ passwd, groups string }{
		{"acme:x:8:8::/srv/acme:/no\n", "acme:x:8:\n"},
		{"acme:x:8:8::/home/acme:/bin/bash\n", "shell:x:1:acme\n"},
		{"acme:x:8:8::/other:/no\n", ""}, {"", "acme:x:8:\n"},
	} {
		f := newGroupFixture(t, tc.passwd, tc.groups)
		f.write("/srv/acme/keep", "unchanged")
		err := accounts.AddGroup(context.Background(), f.env(), "acme")
		assertGroupRefusal(t, err, "name taken: acme")
		if len(f.commands) != 0 {
			t.Fatal(f.commands)
		}
		data, err := os.ReadFile(f.path("/srv/acme/keep"))
		if err != nil || string(data) != "unchanged" {
			t.Fatal(string(data), err)
		}
	}
}

func TestAddGroupRejectsNonDirectories(t *testing.T) {
	// R-Y3X5-Q18Y
	for _, symlink := range []bool{false, true} {
		f := newGroupFixture(t, "", "")
		f.write("/srv/keep", "preserved")
		if symlink {
			if err := os.Symlink(f.path("/srv/keep"), f.path("/srv/acme")); err != nil {
				t.Fatal(err)
			}
		} else {
			f.write("/srv/acme", "preserved")
		}
		err := accounts.AddGroup(context.Background(), f.env(), "acme")
		assertGroupRefusal(t, err, "not a directory: /srv/acme")
		if len(f.commands) != 0 {
			t.Fatal(f.commands)
		}
		data, err := os.ReadFile(f.path("/srv/keep"))
		if err != nil || string(data) != "preserved" {
			t.Fatal(string(data), err)
		}
	}
}

func TestAddGroupCreatesInExactOrder(t *testing.T) {
	// R-Y552-3SZN R-0PDT-XKYQ R-0QLQ-BCPF R-KPQS-LP4N R-Y6CY-HKQC R-Y2P9-C9I9
	f := newGroupFixture(t, "", "")
	dir := f.path("/srv/acme")
	f.hook = func(c host.Command) (host.Result, error) {
		if c.Name == "groupadd" || c.Name == "useradd" {
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("folder created before useradd", err)
			}
		}
		if c.Name == "chown" {
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
				t.Fatalf("folder %v %v", info, err)
			}
		}
		return host.Result{}, nil
	}
	if err := accounts.AddGroup(context.Background(), f.env(), "acme"); err != nil {
		t.Fatal(err)
	}
	want := []host.Command{
		{Name: "groupadd", Args: []string{"--system", "acme"}},
		{Name: "useradd", Args: []string{"--system", "--gid", "acme", "--home-dir", "/srv/acme", "--no-create-home", "--shell", "/usr/sbin/nologin", "acme"}},
		{Name: "chown", Args: []string{"acme:acme", dir}},
		{Name: "chmod", Args: []string{"2770", dir}},
		{Name: "setfacl", Args: []string{"-d", "-m", "g::rwx,o::-", dir}},
		{Name: "runuser", Args: []string{"-u", "acme", "--", "git", "init", "--shared=group", dir}, Dir: dir},
		{Name: "git", Args: []string{"config", "--file", f.path("/etc/gitconfig"), "--get-all", "safe.directory"}},
		{Name: "git", Args: []string{"config", "--file", f.path("/etc/gitconfig"), "--add", "safe.directory", "/srv/acme"}},
	}
	if !reflect.DeepEqual(f.commands, want) {
		t.Fatalf("commands %#v, want %#v", f.commands, want)
	}
}

func TestAdoptionPreservesRepositoryAndReusesFreeIDs(t *testing.T) {
	// R-0PDT-XKYQ R-YCGG-EFFT R-0QLQ-BCPF R-KPQS-LP4N
	for _, idsUsed := range []bool{false, true} {
		f := newGroupFixture(t, "", "")
		f.write("/srv/acme/sub/kept", "old content")
		f.write("/srv/acme/.git", "gitdir: elsewhere\n")
		filesystem, err := os.OpenRoot(f.root)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = filesystem.Close() }()
		if err := filesystem.Chmod("srv/acme/sub/kept", 0640); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(f.path("/srv/acme"))
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		uid, gid := strconv.FormatUint(uint64(stat.Uid), 10), strconv.FormatUint(uint64(stat.Gid), 10)
		if idsUsed {
			f.write("/etc/passwd", "other:x:"+uid+":1::/other:/no\n")
			f.write("/etc/group", "other:x:"+gid+":\n")
		}
		f.hook = func(c host.Command) (host.Result, error) {
			if c.Name == "git" {
				return host.Result{Stdout: []byte("/srv/acme/other\n/srv/acme\n")}, nil
			}
			return host.Result{}, nil
		}
		if err := accounts.AddGroup(context.Background(), f.env(), "acme"); err != nil {
			t.Fatal(err)
		}
		groupArgs := []string{"--system"}
		userArgs := []string{"--system"}
		if !idsUsed {
			groupArgs = append(groupArgs, "--gid", gid)
			userArgs = append(userArgs, "--uid", uid)
		}
		groupArgs = append(groupArgs, "acme")
		userArgs = append(userArgs, "--gid", "acme", "--home-dir", "/srv/acme", "--no-create-home", "--shell", "/usr/sbin/nologin", "acme")
		if !reflect.DeepEqual(f.commands[0].Args, groupArgs) || !reflect.DeepEqual(f.commands[1].Args, userArgs) || len(f.commands) != 6 || f.commands[5].Name != "git" {
			t.Fatalf("commands %#v", f.commands)
		}
		data, err := os.ReadFile(f.path("/srv/acme/sub/kept"))
		if err != nil || string(data) != "old content" {
			t.Fatal(string(data), err)
		}
		after, err := os.Stat(f.path("/srv/acme/sub/kept"))
		if err != nil || after.Mode().Perm() != 0640 {
			t.Fatal(after, err)
		}
	}
}

func TestSafeDirectoryAbsentAndExactLine(t *testing.T) {
	// R-KPQS-LP4N
	for _, tc := range []struct {
		output string
		status int
		add    bool
	}{
		{"", 1, true}, {"/srv/acme-extra\n", 0, true}, {" /srv/acme\n", 0, true}, {"/srv/acme\n", 0, false},
	} {
		f := newGroupFixture(t, "", "")
		f.write("/srv/acme/.git", "")
		f.hook = func(c host.Command) (host.Result, error) {
			if c.Name == "git" && c.Args[3] == "--get-all" {
				return host.Result{ExitCode: tc.status, Stdout: []byte(tc.output)}, nil
			}
			return host.Result{}, nil
		}
		if err := accounts.AddGroup(context.Background(), f.env(), "acme"); err != nil {
			t.Fatal(err)
		}
		want := 6
		if tc.add {
			want++
		}
		if len(f.commands) != want {
			t.Fatal(f.commands)
		}
	}
}

func TestAddGroupStopsAndUndoesEveryFailedStep(t *testing.T) {
	// R-Y6CY-HKQC R-0T1J-2W6T R-Y8SR-947Q R-Y2P9-C9I9
	for failStep := 0; failStep < 8; failStep++ {
		for _, startError := range []bool{false, true} {
			t.Run(fmt.Sprintf("step%d-start%t", failStep, startError), func(t *testing.T) {
				f := newGroupFixture(t, "", "")
				failure := errors.New("cannot start fixture")
				f.hook = func(c host.Command) (host.Result, error) {
					if len(f.commands)-1 == failStep {
						if startError {
							return host.Result{Stderr: []byte("failed")}, failure
						}
						return host.Result{ExitCode: 19, Stdout: []byte("out"), Stderr: []byte("err")}, nil
					}
					f.accountMutation(c)
					return host.Result{}, nil
				}
				err := accounts.AddGroup(context.Background(), f.env(), "acme")
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) {
					t.Fatal(err)
				}
				wantLabel := []string{"groupadd", "useradd", "chown", "chmod", "setfacl", "git", "git", "git"}[failStep]
				if commandErr.Label != wantLabel {
					t.Fatalf("error %+v", commandErr)
				}
				if startError {
					if !errors.Is(err, failure) || string(commandErr.Result.Stderr) != "failed" {
						t.Fatal(err)
					}
				} else if commandErr.Result.ExitCode != 19 || string(commandErr.Result.Stdout) != "out" || string(commandErr.Result.Stderr) != "err" {
					t.Fatal(commandErr)
				}
				wantNames := []string{"groupadd", "useradd", "chown", "chmod", "setfacl", "runuser", "git", "git"}[:failStep+1]
				wantNames = append([]string{}, wantNames...)
				if failStep > 1 {
					wantNames = append(wantNames, "userdel")
				}
				if failStep > 0 {
					wantNames = append(wantNames, "groupdel")
				}
				var names []string
				for _, c := range f.commands {
					names = append(names, c.Name)
					if c.Name == "userdel" || c.Name == "groupdel" {
						if !reflect.DeepEqual(c.Args, []string{"acme"}) {
							t.Fatal(c)
						}
					}
				}
				if !reflect.DeepEqual(names, wantNames) {
					t.Fatalf("commands %v want %v", names, wantNames)
				}
				if failStep >= 2 {
					if info, err := os.Stat(f.path("/srv/acme")); err != nil || !info.IsDir() {
						t.Fatal("undo removed folder", err)
					}
				}
			})
		}
	}
}

func TestAddGroupUndoContinuesAndHonorsAutoGroupRemoval(t *testing.T) {
	// R-0T1J-2W6T R-Y8SR-947Q
	for _, undoFails := range []bool{false, true} {
		f := newGroupFixture(t, "", "")
		f.write("/srv/acme/kept", "keep")
		f.hook = func(c host.Command) (host.Result, error) {
			switch c.Name {
			case "setfacl":
				return host.Result{ExitCode: 17}, nil
			case "userdel":
				if undoFails {
					return host.Result{ExitCode: 18}, nil
				}
				f.accountMutation(host.Command{Name: "groupdel", Args: []string{"acme"}})
			case "groupdel":
				return host.Result{ExitCode: 20}, nil
			}
			f.accountMutation(c)
			return host.Result{}, nil
		}
		err := accounts.AddGroup(context.Background(), f.env(), "acme")
		var ce *host.CommandError
		if !errors.As(err, &ce) || ce.Label != "setfacl" || ce.Result.ExitCode != 17 {
			t.Fatal(err)
		}
		var names []string
		for _, c := range f.commands {
			names = append(names, c.Name)
		}
		want := []string{"groupadd", "useradd", "chown", "chmod", "setfacl", "userdel"}
		if undoFails {
			want = append(want, "groupdel")
		}
		if !reflect.DeepEqual(names, want) {
			t.Fatal(names)
		}
		data, err := os.ReadFile(f.path("/srv/acme/kept"))
		if err != nil || string(data) != "keep" {
			t.Fatal(string(data), err)
		}
	}
}

func TestRemoveGroupRefusalsAndLeftovers(t *testing.T) {
	// R-UOQQ-SCKA R-UPYN-64AZ R-0WP8-87EW
	for _, tc := range []struct{ name, passwd, groups, reason string }{
		{"absent", "", "", "no group 'absent'"},
		{"wheel", "", "wheel:x:10:\n", "no group 'wheel'"},
		{"acme", "acme:x:80:80::/srv/acme:/no\nalice:x:1:1::/home/alice:/bin/bash\n", "acme:x:80:alice\nshell:x:1:alice\n", "group not empty: acme"},
	} {
		f := newGroupFixture(t, tc.passwd, tc.groups)
		err := accounts.RemoveGroup(context.Background(), f.env(), tc.name)
		assertGroupRefusal(t, err, tc.reason)
		if len(f.commands) != 0 {
			t.Fatal(f.commands)
		}
	}
	for _, kind := range []string{"matching", "different", "members", "shell", "symlink", "file"} {
		t.Run(kind, func(t *testing.T) {
			f := newGroupFixture(t, "", "")
			slug := "acme"
			if kind == "shell" {
				slug = "shell"
			}
			f.write("/srv/"+slug+"/kept", "kept")
			info, err := os.Stat(f.path("/srv/" + slug))
			if err != nil {
				t.Fatal(err)
			}
			gid := uint64(info.Sys().(*syscall.Stat_t).Gid)
			if kind == "different" {
				gid++
			}
			members := ""
			if kind == "members" {
				members = "nonuser"
			}
			if kind == "symlink" || kind == "file" {
				if err := os.Rename(f.path("/srv/"+slug), f.path("/srv/old")); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					if err := os.Symlink(f.path("/srv/old"), f.path("/srv/"+slug)); err != nil {
						t.Fatal(err)
					}
				} else {
					f.write("/srv/"+slug, "kept")
				}
			}
			f.write("/etc/group", slug+":x:"+strconv.FormatUint(gid, 10)+":"+members+"\n")
			list, err := accounts.ListGroups(context.Background(), f.env())
			if err != nil || len(list.Groups) != 0 {
				t.Fatal(list, err)
			}
			_, err = accounts.GroupMembers(context.Background(), f.env(), slug)
			assertGroupRefusal(t, err, "no group '"+slug+"'")
			err = accounts.RemoveGroup(context.Background(), f.env(), slug)
			if kind == "matching" {
				if err != nil || len(f.commands) != 1 || f.commands[0].Name != "groupdel" {
					t.Fatal(err, f.commands)
				}
			} else {
				assertGroupRefusal(t, err, "no group '"+slug+"'")
				if len(f.commands) != 0 {
					t.Fatal(f.commands)
				}
			}
		})
	}
}

func TestRemoveGroupKeepsFolderAndTrustAndStopsOnFailures(t *testing.T) {
	// R-Y7KU-VCH1 R-YA0N-MVYF
	for _, fail := range []string{"", "userdel", "groupdel", "autoremoved"} {
		f := newGroupFixture(t, "acme:x:80:80::/srv/acme:/no\n", "acme:x:80:nonuser\n")
		f.write("/srv/acme/kept", "content")
		f.write("/etc/gitconfig", "safe directory kept")
		f.hook = func(c host.Command) (host.Result, error) {
			if c.Name == fail {
				return host.Result{ExitCode: 15}, nil
			}
			f.accountMutation(c)
			if fail == "autoremoved" && c.Name == "userdel" {
				f.accountMutation(host.Command{Name: "groupdel", Args: []string{"acme"}})
			}
			return host.Result{}, nil
		}
		err := accounts.RemoveGroup(context.Background(), f.env(), "acme")
		if fail == "userdel" || fail == "groupdel" {
			var ce *host.CommandError
			if !errors.As(err, &ce) || ce.Label != fail || ce.Result.ExitCode != 15 {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		want := []host.Command{{Name: "userdel", Args: []string{"acme"}}}
		if fail != "userdel" && fail != "autoremoved" {
			want = append(want, host.Command{Name: "groupdel", Args: []string{"acme"}})
		}
		if !reflect.DeepEqual(f.commands, want) {
			t.Fatal(f.commands)
		}
		for path, content := range map[string]string{"/srv/acme/kept": "content", "/etc/gitconfig": "safe directory kept"} {
			data, err := os.ReadFile(f.path(path))
			if err != nil || string(data) != content {
				t.Fatal(path, string(data), err)
			}
		}
	}
}

func assertGroupRefusal(t *testing.T, err error, want string) {
	t.Helper()
	var refused *accounts.RefusedError
	if !errors.As(err, &refused) || refused.Reason != want {
		t.Fatalf("refusal %v, want %q", err, want)
	}
}

func TestAdoptionDoesNotUseRecursiveCommands(t *testing.T) {
	// R-Y552-3SZN
	f := newGroupFixture(t, "", "")
	f.write("/srv/acme/sub/keep", "kept")
	if err := accounts.AddGroup(context.Background(), f.env(), "acme"); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.commands {
		for _, arg := range c.Args {
			if arg == "-R" || strings.HasPrefix(arg, "--recursive") {
				t.Fatal(c)
			}
		}
	}
}

func TestAddGroupFolderCreationFailureUndoesAccounts(t *testing.T) {
	// R-0PDT-XKYQ R-0T1J-2W6T R-Y6CY-HKQC R-Y8SR-947Q
	f := newGroupFixture(t, "", "")
	f.hook = func(c host.Command) (host.Result, error) {
		f.accountMutation(c)
		if c.Name == "useradd" {
			f.write("/srv", "blocks directory creation")
		}
		return host.Result{}, nil
	}
	err := accounts.AddGroup(context.Background(), f.env(), "acme")
	var commandErr *host.CommandError
	var refused *accounts.RefusedError
	if err == nil || errors.As(err, &commandErr) || errors.As(err, &refused) {
		t.Fatalf("error %v", err)
	}
	names := make([]string, 0)
	for _, c := range f.commands {
		names = append(names, c.Name)
	}
	if !reflect.DeepEqual(names, []string{"groupadd", "useradd", "userdel", "groupdel"}) {
		t.Fatal(names)
	}
	data, err := os.ReadFile(f.path("/srv"))
	if err != nil || string(data) != "blocks directory creation" {
		t.Fatal(string(data), err)
	}
}

func TestMissingGroupExecutorCannotInvokeHost(t *testing.T) {
	// R-Y2P9-C9I9
	f := newGroupFixture(t, "", "")
	env := f.env()
	env.Execute = nil
	err := accounts.AddGroup(context.Background(), env, "acme")
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Label != "groupadd" || commandErr.Err == nil {
		t.Fatal(err)
	}
	if len(f.commands) != 0 {
		t.Fatal(f.commands)
	}
	if _, err := os.Stat(f.path("/srv")); !os.IsNotExist(err) {
		t.Fatal("missing executor changed files", err)
	}
}
