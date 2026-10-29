package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type groupCLIResult struct {
	code     int
	out, err string
}

func invokeGroupCLI(args []string, deps Deps) groupCLIResult {
	var out, err bytes.Buffer
	code := Run(append([]string{"group"}, args...), strings.NewReader(""), &out, &err, deps)
	return groupCLIResult{code, out.String(), err.String()}
}
func groupCLIWrite(t *testing.T, root, path, content string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGroupHelpAndRootPrecedence(t *testing.T) {
	const expectedHelp = `Usage: opsctl group <subcommand> [arguments]

Manage the host's business groups. A group SLUG is a Linux group, a service
account of the same name, and the folder /srv/SLUG, a shared git repository
the group's members can write and no one else can read.

Subcommands:
  add SLUG      create the group, its account and /srv/SLUG, adopting a folder
                already there
  remove SLUG   remove the group and its account; refused while it has members;
                /srv/SLUG is kept
  list          print every business group and its member count as JSON
  members SLUG  print the group's members as JSON

SLUG must be valid as a Linux account name, and add refuses one any account or
group on the host already has.
`
	// R-12SQ-524D R-140M-ITV2 R-158I-WLLR
	for _, uid := range []int{0, 1000} {
		for _, help := range []string{"--help", "-h"} {
			root := t.TempDir()
			// A non-directory root prevents any successful host-file read or write.
			path := filepath.Join(root, "not-a-directory")
			if err := os.WriteFile(path, []byte("kept"), 0600); err != nil {
				t.Fatal(err)
			}
			deps := Deps{Root: path, EUID: uid, Execute: func(context.Context, host.Command) (host.Result, error) {
				t.Fatal("executed process")
				return host.Result{}, nil
			}}
			got := invokeGroupCLI([]string{help}, deps)
			if got.code != 0 || got.err != "" || got.out != expectedHelp {
				t.Fatalf("help %+v", got)
			}
		}
	}
	for _, args := range [][]string{nil, {"unknown"}, {"add"}, {"remove", "bad/name"}, {"members", "x", "y"}, {"list"}, {"--help", "extra"}} {
		got := invokeGroupCLI(args, Deps{Root: t.TempDir(), EUID: 1000})
		if got.code != 3 || got.out != "" || got.err != "opsctl: must run as root\n" {
			t.Fatalf("%v: %+v", args, got)
		}
	}
}

func TestGroupUsageErrorsBeforeHostAccess(t *testing.T) {
	// R-12SQ-524D R-16GF-ADCG R-17OB-O535 R-18W8-1WTU
	cases := []struct {
		args    []string
		message string
	}{
		{nil, "no group subcommand given"},
		{[]string{"unknown"}, "unknown group subcommand 'unknown'"},
		{[]string{"--other"}, "unknown group subcommand '--other'"},
		{[]string{"list", "extra"}, "group list takes no arguments"},
	}
	for _, sub := range []string{"add", "remove", "members"} {
		cases = append(cases, struct {
			args    []string
			message string
		}{[]string{sub}, "group " + sub + " needs SLUG"},
			struct {
				args    []string
				message string
			}{[]string{sub, "bad/name", "other"}, "group " + sub + " takes one SLUG"},
			struct {
				args    []string
				message string
			}{[]string{sub, "acme/corp"}, "invalid name: acme/corp"},
			struct {
				args    []string
				message string
			}{[]string{sub, "--help"}, "invalid name: --help"})
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			root := t.TempDir()
			got := invokeGroupCLI(tc.args, Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
				t.Fatal("executed process")
				return host.Result{}, nil
			}})
			want := "opsctl: " + tc.message + "\n\nsee 'opsctl group --help' for usage\n"
			if got.code != 2 || got.out != "" || got.err != want {
				t.Fatalf("got %+v want %q", got, want)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal(entries, err)
			}
		})
	}
}

func TestGroupJSONAndSilentSuccess(t *testing.T) {
	// R-12SQ-524D R-1A44-FOKJ R-1BC0-TGB8 R-1CJX-781X
	root := t.TempDir()
	groupCLIWrite(t, root, "/etc/passwd", "globex:x:10:10::/srv/globex:/no\nacme:x:11:11::/srv/acme:/no\nalice:x:12:12::/home/alice:/bin/bash\nbob:x:13:13::/home/bob:/bin/bash\n")
	groupCLIWrite(t, root, "/etc/group", "globex:x:10:\nacme:x:11:bob,alice,bob,missing\nshell:x:1:bob,alice\n")
	deps := Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
		t.Fatal("query executed process")
		return host.Result{}, nil
	}}
	for _, tc := range []struct {
		args []string
		json string
	}{
		{[]string{"list"}, `{"groups":[{"slug":"acme","members":2},{"slug":"globex","members":0}]}` + "\n"},
		{[]string{"members", "acme"}, `{"group":"acme","members":["alice","bob"]}` + "\n"},
		{[]string{"members", "globex"}, `{"group":"globex","members":[]}` + "\n"},
	} {
		got := invokeGroupCLI(tc.args, deps)
		if got.code != 0 || got.err != "" || got.out != tc.json {
			t.Fatalf("%v: %+v", tc.args, got)
		}
	}
	groupCLIWrite(t, root, "/etc/passwd", "")
	groupCLIWrite(t, root, "/etc/group", "")
	got := invokeGroupCLI([]string{"list"}, deps)
	if got.code != 0 || got.err != "" || got.out != "{\"groups\":[]}\n" {
		t.Fatal(got)
	}
	for _, sub := range []string{"add", "remove"} {
		if sub == "remove" {
			groupCLIWrite(t, root, "/etc/passwd", "acme:x:1:1::/srv/acme:/no\n")
			groupCLIWrite(t, root, "/etc/group", "acme:x:1:\n")
		}
		commands := 0
		deps.Execute = func(context.Context, host.Command) (host.Result, error) { commands++; return host.Result{}, nil }
		got := invokeGroupCLI([]string{sub, "acme"}, deps)
		if got.code != 0 || got.err != "" || got.out != "" || commands == 0 {
			t.Fatalf("%s: %+v commands%d", sub, got, commands)
		}
	}
}

func TestGroupStateRefusals(t *testing.T) {
	// R-YDOC-S76I R-Y3X5-Q18Y
	for _, tc := range []struct {
		args                   []string
		passwd, groups, reason string
		nondir                 bool
	}{
		{[]string{"add", "acme"}, "acme:x:1:1::/else:/no\n", "", "name taken: acme", false},
		{[]string{"members", "shell"}, "", "shell:x:1:\n", "no group 'shell'", false},
		{[]string{"remove", "wheel"}, "", "wheel:x:1:\n", "no group 'wheel'", false},
		{[]string{"remove", "acme"}, "acme:x:1:1::/srv/acme:/no\nalice:x:2:2::/home/alice:/bin/bash\n", "acme:x:1:alice\nshell:x:2:alice\n", "group not empty: acme", false},
		{[]string{"add", "acme"}, "", "", "not a directory: /srv/acme", true},
	} {
		root := t.TempDir()
		groupCLIWrite(t, root, "/etc/passwd", tc.passwd)
		groupCLIWrite(t, root, "/etc/group", tc.groups)
		if tc.nondir {
			groupCLIWrite(t, root, "/srv/acme", "keep")
		}
		before := groupCLISnapshot(t, root)
		got := invokeGroupCLI(tc.args, Deps{Root: root, EUID: 0, Execute: func(context.Context, host.Command) (host.Result, error) {
			t.Fatal("refusal executed process")
			return host.Result{}, nil
		}})
		if got.code != 4 || got.err != "opsctl: "+tc.reason+"\n" || got.out != "" {
			t.Fatal(got)
		}
		if !reflect.DeepEqual(before, groupCLISnapshot(t, root)) {
			t.Fatal("refusal changed files")
		}
	}
}

func TestGroupLeftoverRemovalThroughRun(t *testing.T) {
	// R-UR6J-JW1O
	for _, kind := range []string{"leftover", "wronggid", "shell"} {
		root := t.TempDir()
		slug := "acme"
		if kind == "shell" {
			slug = "shell"
		}
		groupCLIWrite(t, root, "/srv/"+slug+"/keep", "content")
		info, err := os.Stat(filepath.Join(root, "/srv/"+slug))
		if err != nil {
			t.Fatal(err)
		}
		gid := uint64(info.Sys().(*syscall.Stat_t).Gid)
		if kind == "wronggid" {
			gid++
		}
		groupCLIWrite(t, root, "/etc/passwd", "")
		groupCLIWrite(t, root, "/etc/group", slug+":x:"+strconv.FormatUint(gid, 10)+":\n")
		groupCLIWrite(t, root, "/etc/gitconfig", "kept")
		before := groupCLISnapshot(t, root)
		var commands []host.Command
		got := invokeGroupCLI([]string{"remove", slug}, Deps{Root: root, EUID: 0, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
			commands = append(commands, c)
			return host.Result{}, nil
		}})
		if kind == "leftover" {
			if got.code != 0 || got.out != "" || got.err != "" || !reflect.DeepEqual(commands, []host.Command{{Name: "groupdel", Args: []string{"acme"}}}) {
				t.Fatal(got, commands)
			}
		} else if got.code != 4 || got.out != "" || got.err != "opsctl: no group '"+slug+"'\n" || len(commands) != 0 {
			t.Fatal(got, commands)
		}
		if !reflect.DeepEqual(before, groupCLISnapshot(t, root)) {
			t.Fatal("changed files")
		}
	}
}

func TestGroupCommandFailureDiagnosticAndUndo(t *testing.T) {
	// R-1G7M-CJA0
	root := t.TempDir()
	groupCLIWrite(t, root, "/etc/passwd", "")
	groupCLIWrite(t, root, "/etc/group", "")
	var names []string
	deps := Deps{Root: root, EUID: 0, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
		names = append(names, c.Name)
		if c.Name == "groupadd" {
			groupCLIWrite(t, root, "/etc/group", "acme:x:1:\n")
		}
		if c.Name == "setfacl" {
			return host.Result{ExitCode: 1, Stderr: []byte("setfacl: /srv/acme: Operation not supported\n")}, nil
		}
		return host.Result{}, nil
	}}
	got := invokeGroupCLI([]string{"add", "acme"}, deps)
	if got.code != 1 || got.out != "" || got.err != "opsctl: setfacl: exit status 1\n\n> setfacl: /srv/acme: Operation not supported\n" {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(names, []string{"groupadd", "useradd", "chown", "chmod", "setfacl", "userdel", "groupdel"}) {
		t.Fatal(names)
	}
	info, err := os.Stat(filepath.Join(root, "/srv/acme"))
	if err != nil || !info.IsDir() {
		t.Fatal(info, err)
	}
}

func groupCLISnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	entries := make(map[string]string)
	filesystem, openErr := os.OpenRoot(root)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer func() { _ = filesystem.Close() }()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries[path] = fmt.Sprint(info.Mode())
		if !d.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := filesystem.ReadFile(rel)
			if err != nil {
				return err
			}
			entries[path] += string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}
