package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const expectedUserHelp = `Usage: opsctl user <subcommand> [arguments]

Manage the people who sign in to this host over SSH: their keys, their
business groups, and whether they may sign in.

Subcommands:
  add NAME GROUP KEY   create NAME in shell and GROUP, signing in with KEY
  list                 print every user as one JSON document
  key add NAME KEY     let NAME sign in with KEY; succeeds if NAME has it
  key remove NAME KEY  take KEY from NAME; succeeds if NAME lacks it
  join NAME GROUP      add NAME to GROUP; succeeds if already a member
  leave NAME GROUP     take NAME out of GROUP; succeeds if not a member
  lock NAME            end NAME's sessions and refuse every sign-in
  unlock NAME          let a locked NAME sign in again
  delete NAME          remove a locked NAME and their home

NAME is a Linux account name, unique on the host. GROUP is a business group
made by 'opsctl group add'. KEY is one argument, a whole ed25519 OpenSSH
public key line, 'ssh-ed25519 BODY [COMMENT]'; keys match on type and BODY,
never COMMENT. A deleted user's files under /srv/SLUG go to the account SLUG.
`

const cliUserKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDzTUG112+rxfh42mBeuJ5MyIW8NWSwHtmfqZMvJsR+B label<&>"

type userCLIFixture struct {
	t     *testing.T
	deps  Deps
	calls []host.Command
	run   func(host.Command) (host.Result, error)
}

func newUserCLI(t *testing.T) *userCLIFixture {
	t.Helper()
	f := &userCLIFixture{t: t}
	f.deps = Deps{Root: t.TempDir(), EUID: 0, Now: func() time.Time { return time.Date(2026, 10, 29, 18, 0, 0, 0, time.UTC) }}
	f.deps.Execute = func(_ context.Context, c host.Command) (host.Result, error) {
		f.calls = append(f.calls, c)
		if f.run != nil {
			return f.run(c)
		}
		return host.Result{Stdout: []byte("name **Never logged in**\n")}, nil
	}
	f.write("/etc/passwd", "acme:x:301:301::/srv/acme:/usr/sbin/nologin\nalice:x:1001:1001::/home/alice:/bin/bash\nbob:x:1002:1002::/home/bob:/bin/bash\n")
	f.write("/etc/group", "shell:x:400:alice,bob\nacme:x:301:alice\nalice:x:1001:\nbob:x:1002:\n")
	f.write("/etc/shadow", "alice:!:1:0:99999:7:::\nbob:!:1:0:99999:7::1:\n")
	return f
}
func (f *userCLIFixture) write(path, data string) {
	f.t.Helper()
	path = filepath.Join(f.deps.Root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *userCLIFixture) invoke(args ...string) (int, string, string) {
	f.t.Helper()
	var out, err bytes.Buffer
	code := Run(append([]string{"user"}, args...), nil, &out, &err, f.deps)
	return code, out.String(), err.String()
}

func TestUserHelpAndRootPrecedence(t *testing.T) {
	// R-BEWG-RIKJ R-BG4D-5AB8 R-BHC9-J21X
	for _, uid := range []int{0, 1000} {
		for _, flag := range []string{"--help", "-h"} {
			f := newUserCLI(t)
			f.deps.Root = filepath.Join(t.TempDir(), "absent")
			f.deps.EUID = uid
			code, out, err := f.invoke(flag)
			if code != 0 || out != expectedUserHelp || err != "" || len(f.calls) != 0 {
				t.Fatal(code, out, err)
			}
		}
	}
	for _, args := range [][]string{nil, {"unknown"}, {"key"}, {"add"}, {"add", "invalid/name", "bad", "bad"}, {"--help", "extra"}} {
		f := newUserCLI(t)
		f.deps.EUID = 1000
		f.deps.Root = filepath.Join(t.TempDir(), "absent")
		code, out, err := f.invoke(args...)
		if code != 3 || out != "" || err != "opsctl: must run as root\n" || len(f.calls) != 0 {
			t.Fatal(code, out, err)
		}
	}
}

func TestUserGrammar(t *testing.T) {
	// R-BIK5-WTSM R-BKZY-ODA0 R-5OG1-SR7O R-BNFR-FWRE
	cases := []struct {
		args    []string
		message string
	}{{nil, "no user subcommand given"}, {[]string{"other"}, "unknown user subcommand 'other'"}, {[]string{"key"}, "no user key subcommand given"}, {[]string{"key", "other"}, "unknown user key subcommand 'other'"}}
	for _, op := range []struct {
		args       []string
		need, take string
		count      int
	}{{[]string{"add"}, "NAME GROUP KEY", "NAME, GROUP and KEY", 3}, {[]string{"key", "add"}, "NAME KEY", "NAME and KEY", 2}, {[]string{"key", "remove"}, "NAME KEY", "NAME and KEY", 2}, {[]string{"join"}, "NAME GROUP", "NAME and GROUP", 2}, {[]string{"leave"}, "NAME GROUP", "NAME and GROUP", 2}, {[]string{"lock"}, "NAME", "one NAME", 1}, {[]string{"unlock"}, "NAME", "one NAME", 1}, {[]string{"delete"}, "NAME", "one NAME", 1}} {
		for n := 0; n < op.count; n++ {
			args := append([]string{}, op.args...)
			for i := 0; i < n; i++ {
				args = append(args, "bad/name")
			}
			cases = append(cases, struct {
				args    []string
				message string
			}{args, "user " + strings.Join(op.args, " ") + " needs " + op.need})
		}
		args := append([]string{}, op.args...)
		for i := 0; i <= op.count; i++ {
			args = append(args, "bad/name")
		}
		cases = append(cases, struct {
			args    []string
			message string
		}{args, "user " + strings.Join(op.args, " ") + " takes " + op.take})
	}
	cases = append(cases, struct {
		args    []string
		message string
	}{[]string{"list", "extra"}, "user list takes no arguments"})
	cases = append(cases, []struct {
		args    []string
		message string
	}{{[]string{"add", "bad/name", "bad/group", "bad"}, "invalid name: bad/name"}, {[]string{"add", "alice", "bad/group", "bad"}, "invalid name: bad/group"}, {[]string{"add", "alice", "acme", "bad"}, "invalid key: not an ed25519 OpenSSH public key"}, {[]string{"key", "add", "bad/name", "bad"}, "invalid name: bad/name"}, {[]string{"key", "remove", "alice", "bad"}, "invalid key: not an ed25519 OpenSSH public key"}, {[]string{"join", "alice", "bad/group"}, "invalid name: bad/group"}, {[]string{"leave", "bad/name", "acme"}, "invalid name: bad/name"}, {[]string{"lock", "bad/name"}, "invalid name: bad/name"}, {[]string{"unlock", "bad/name"}, "invalid name: bad/name"}, {[]string{"delete", "bad/name"}, "invalid name: bad/name"}, {[]string{"key", "add", "alice", "ssh-ed25519", "body", "comment"}, "user key add takes NAME and KEY"}}...)
	for _, tc := range cases {
		f := newUserCLI(t)
		f.deps.Root = filepath.Join(t.TempDir(), "absent")
		code, out, err := f.invoke(tc.args...)
		if code != 2 || out != "" || err != "opsctl: "+tc.message+"\n\nsee 'opsctl user --help' for usage\n" || len(f.calls) != 0 {
			t.Fatal(tc.args, code, out, err)
		}
	}
}

func TestUserActions(t *testing.T) {
	// R-BEWG-RIKJ R-BONN-TOI3 R-7V1R-0MVT
	for _, args := range [][]string{{"add", "carol", "acme", "  " + cliUserKey + "  "}, {"key", "add", "alice", "  " + cliUserKey + "  "}, {"key", "remove", "alice", cliUserKey}, {"join", "bob", "acme"}, {"leave", "alice", "acme"}, {"lock", "alice"}, {"unlock", "bob"}, {"delete", "bob"}} {
		f := newUserCLI(t)
		if args[0] == "key" && args[1] == "remove" {
			f.write("/etc/ssh/authorized_keys/alice", cliUserKey+"\n")
		}
		code, out, err := f.invoke(args...)
		if code != 0 || out != "" || err != "" {
			t.Fatal(args, code, out, err)
		}
		if args[0] == "key" && args[1] == "remove" {
			data, readErr := os.ReadFile(filepath.Join(f.deps.Root, "/etc/ssh/authorized_keys/alice"))
			if readErr != nil || len(data) != 0 {
				t.Fatal(string(data), readErr)
			}
		}
		if args[0] == "add" || args[0] == "key" && args[1] == "add" {
			name := "alice"
			if args[0] == "add" {
				name = "carol"
			}
			data, err := os.ReadFile(filepath.Join(f.deps.Root, "/etc/ssh/authorized_keys", name))
			if err != nil || string(data) != cliUserKey+"\n" {
				t.Fatal(string(data), err)
			}
		}
	}
	for _, args := range [][]string{{"key", "remove", "alice", cliUserKey}, {"join", "alice", "acme"}, {"leave", "bob", "acme"}, {"lock", "bob"}, {"unlock", "alice"}} {
		f := newUserCLI(t)
		for i := 0; i < 2; i++ {
			code, out, err := f.invoke(args...)
			if code != 0 || out != "" || err != "" {
				t.Fatal(code, out, err)
			}
		}
	}
}

func TestUserStateRefusals(t *testing.T) {
	// R-7V1R-0MVT
	for _, tc := range []struct {
		args   []string
		reason string
	}{{[]string{"add", "acme", "acme", cliUserKey}, "name taken: acme"}, {[]string{"join", "alice", "shell"}, "no group 'shell'"}, {[]string{"lock", "carol"}, "no user 'carol'"}, {[]string{"delete", "alice"}, "user not locked: alice"}} {
		f := newUserCLI(t)
		before := groupCLISnapshot(t, f.deps.Root)
		code, out, err := f.invoke(tc.args...)
		if code != 4 || out != "" || err != "opsctl: "+tc.reason+"\n" || len(f.calls) != 0 {
			t.Fatal(code, out, err)
		}
		if after := groupCLISnapshot(t, f.deps.Root); !reflect.DeepEqual(before, after) {
			t.Fatal("refusal changed files")
		}
	}
}

func TestUserListJSON(t *testing.T) {
	// R-5PNY-6IYD R-A9FV-RBCX
	f := newUserCLI(t)
	f.write("/etc/ssh/authorized_keys/alice", cliUserKey)
	for _, fraction := range []string{"", ".125"} {
		f.run = func(c host.Command) (host.Result, error) {
			if reflect.DeepEqual(c.Args, []string{"-u", "alice"}) {
				return host.Result{Stdout: []byte("alice pts/0 host Thu Oct 29 09:03:00" + fraction + " -0500 2026\n")}, nil
			}
			return host.Result{Stdout: []byte("bob **Never logged in**\n")}, nil
		}
		code, out, err := f.invoke("list")
		want := "{\"users\":[{\"name\":\"alice\",\"groups\":[\"acme\"],\"keys\":[\"" + cliUserKey + "\"],\"state\":\"" + accounts.StateActive + "\",\"last_sign_in\":\"2026-10-29T14:03:00Z\"},{\"name\":\"bob\",\"groups\":[],\"keys\":[],\"state\":\"" + accounts.StateLocked + "\",\"last_sign_in\":null}]}\n"
		if code != 0 || out != want || err != "" {
			t.Fatal(code, out, err)
		}
	}

	f.write("/etc/group", "shell:x:400:\n")
	code, out, err := f.invoke("list")
	if code != 0 || out != "{\"users\":[]}\n" || err != "" {
		t.Fatal(code, out, err)
	}
}

func TestUserCommandDiagnostic(t *testing.T) {
	// R-53ZL-NX14
	f := newUserCLI(t)
	f.run = func(c host.Command) (host.Result, error) {
		if c.Name != "useradd" {
			t.Fatal(c)
		}
		return host.Result{ExitCode: 12, Stderr: []byte("useradd: cannot create directory /home/alice")}, nil
	}
	// A free account name is required before useradd can be reached.
	f.write("/etc/passwd", "acme:x:301:301::/srv/acme:/usr/sbin/nologin\n")
	f.write("/etc/group", "shell:x:400:\nacme:x:301:\n")
	code, out, err := f.invoke("add", "alice", "acme", cliUserKey)
	if code != 1 || out != "" || err != "opsctl: useradd: exit status 12\n\n> useradd: cannot create directory /home/alice\n" || len(f.calls) != 1 {
		t.Fatal(code, out, err)
	}
	for _, path := range []string{"/home/alice", "/etc/ssh/authorized_keys/alice"} {
		if _, err := os.Lstat(filepath.Join(f.deps.Root, path)); !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}
