package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
)

func TestConfigHelp(t *testing.T) {
	// R-F5WG-50XH
	for _, euid := range []int{0, 1} {
		for _, args := range [][]string{
			{"config", "--help"},
			{"config", "-h"},
		} {
			deps := depsAt(t, euid)
			writeCorrupt(t, deps.Root)
			assertNoAccess := observeFilesystemAccess(t, deps.Root)
			stdout, stderr, code := invoke(args, deps)
			assertNoAccess()
			if code != 0 {
				t.Errorf("euid %d %q: exit %d, want 0", euid, args, code)
			}
			if stderr != "" {
				t.Errorf("euid %d %q: stderr = %q, want empty", euid, args, stderr)
			}
			if stdout != wantConfigUsage {
				t.Errorf("euid %d %q: stdout = %q, want config usage", euid, args, stdout)
			}
		}
	}
}

func TestConfigGet(t *testing.T) {
	// R-R352-8LRC
	deps := depsAt(t, 0)
	_, stderr, code := invoke([]string{"config", "set", "dns.zones=ikigenba.dev"}, deps)
	if code != 0 {
		t.Fatalf("set: exit %d stderr %q", code, stderr)
	}

	stdout, stderr, code := invoke([]string{"config", "get", "dns.zones"}, deps)
	if code != 0 {
		t.Errorf("get set key: exit %d, want 0", code)
	}
	if stdout != "ikigenba.dev\n" {
		t.Errorf("get set key: stdout = %q, want %q", stdout, "ikigenba.dev\n")
	}
	if stderr != "" {
		t.Errorf("get set key: stderr = %q, want empty", stderr)
	}

	stdout, stderr, code = invoke([]string{"config", "get", "backup.s3_uri"}, deps)
	if code != 1 {
		t.Errorf("get missing: exit %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("get missing: stdout = %q, want empty", stdout)
	}
	want := "opsctl: key not set: backup.s3_uri\n"
	if stderr != want {
		t.Errorf("get missing: stderr = %q, want %q", stderr, want)
	}

	_, stderr, code = invoke([]string{"config", "set", "empty="}, deps)
	if code != 0 {
		t.Fatalf("set empty: exit %d stderr %q", code, stderr)
	}
	stdout, stderr, code = invoke([]string{"config", "get", "empty"}, deps)
	if code != 0 || stdout != "\n" || stderr != "" {
		t.Errorf("get empty: exit %d stdout %q stderr %q, want 0, %q, empty stderr", code, stdout, stderr, "\n")
	}
}

func TestConfigSet(t *testing.T) {
	// R-O1E4-XBA5
	deps := depsAt(t, 0)
	stdout, stderr, code := invoke([]string{"config", "set", "dns.zones=a=b=c"}, deps)
	if code != 0 {
		t.Errorf("exit %d, want 0 (stderr %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	stdout, stderr, code = invoke([]string{"config", "get", "dns.zones"}, deps)
	if code != 0 {
		t.Fatalf("get: exit %d stderr %q", code, stderr)
	}
	if stdout != "a=b=c\n" {
		t.Errorf("stored value stdout = %q, want %q", stdout, "a=b=c\n")
	}
}

func TestConfigSetUsageError(t *testing.T) {
	// R-8Q6R-PBF6
	deps := depsAt(t, 0)
	_, stderr, code := invoke([]string{"config", "set", "ok=1"}, deps)
	if code != 0 {
		t.Fatalf("seed set: exit %d stderr %q", code, stderr)
	}
	before, err := os.ReadFile(configFile(deps.Root))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		arg  string
		want string
	}{
		{"missing equals", "noequals", "opsctl: config set needs KEY=VALUE\n\nsee 'opsctl config --help' for usage\n"},
		{"invalid key", "INVALID=x", "opsctl: invalid key: INVALID\n\nsee 'opsctl config --help' for usage\n"},
		{"invalid key before value", "INVALID=line\nfeed", "opsctl: invalid key: INVALID\n\nsee 'opsctl config --help' for usage\n"},
		{"newline value", "ok=line\nfeed", "opsctl: invalid value: newline in value for 'ok'\n\nsee 'opsctl config --help' for usage\n"},
		{"carriage return value", "ok=line\rfeed", "opsctl: invalid value: newline in value for 'ok'\n\nsee 'opsctl config --help' for usage\n"},
	}
	for _, tc := range cases {
		stdout, stderr, code := invoke([]string{"config", "set", tc.arg}, deps)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2", tc.name, code)
		}
		if stdout != "" {
			t.Errorf("%s: stdout = %q, want empty", tc.name, stdout)
		}
		if stderr != tc.want {
			t.Errorf("%s: stderr = %q, want %q", tc.name, stderr, tc.want)
		}
		after, err := os.ReadFile(configFile(deps.Root))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("%s: store changed: %q -> %q", tc.name, before, after)
		}
	}

	empty := depsAt(t, 0)
	stdout, stderr, code := invoke([]string{"config", "set", "noequals"}, empty)
	if code != 2 {
		t.Errorf("missing store: exit %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("missing store: stdout = %q, want empty", stdout)
	}
	want := "opsctl: config set needs KEY=VALUE\n\nsee 'opsctl config --help' for usage\n"
	if stderr != want {
		t.Errorf("missing store: stderr = %q, want %q", stderr, want)
	}
	if _, err := os.Stat(configFile(empty.Root)); !os.IsNotExist(err) {
		t.Errorf("usage error created the store file: %v", err)
	}
}

func TestConfigAcceptsOnlyDefinedForms(t *testing.T) {
	// R-F8C8-WKEV
	accepted := depsAt(t, 0)
	if _, stderr, code := invoke([]string{"config", "set", "key=value"}, accepted); code != 0 {
		t.Fatalf("set accepted form: exit %d stderr %q", code, stderr)
	}
	for _, tc := range []struct {
		args       []string
		wantStdout string
	}{
		{[]string{"config", "get", "key"}, "value\n"},
		{[]string{"config", "list"}, "key=value\n"},
		{[]string{"config", "del", "key"}, ""},
	} {
		stdout, stderr, code := invoke(tc.args, accepted)
		if code != 0 || stdout != tc.wantStdout || stderr != "" {
			t.Errorf("accepted %q: exit %d stdout %q stderr %q, want 0, %q, empty stderr", tc.args, code, stdout, stderr, tc.wantStdout)
		}
	}

	rejected := []struct {
		args []string
		want string
	}{
		{[]string{"config"}, "opsctl: no config subcommand given\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "other"}, "opsctl: unknown config subcommand 'other'\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "get"}, "opsctl: config get requires KEY\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "get", "a", "b"}, "opsctl: config get requires KEY\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "set"}, "opsctl: config set requires KEY=VALUE\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "set", "a=b", "c=d"}, "opsctl: config set requires KEY=VALUE\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "set", "noequals"}, "opsctl: config set needs KEY=VALUE\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "del"}, "opsctl: config del requires KEY\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "del", "a", "b"}, "opsctl: config del requires KEY\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "list", "extra"}, "opsctl: config list takes no arguments\n\nsee 'opsctl config --help' for usage\n"},
	}
	for _, tc := range rejected {
		deps := depsAt(t, 0)
		writeCorrupt(t, deps.Root)
		before, err := os.ReadFile(configFile(deps.Root))
		if err != nil {
			t.Fatal(err)
		}
		assertNoAccess := observeFilesystemAccess(t, deps.Root)
		stdout, stderr, code := invoke(tc.args, deps)
		assertNoAccess()
		if code != 2 || stdout != "" || stderr != tc.want {
			t.Errorf("rejected %q: exit %d stdout %q stderr %q, want 2, empty stdout, stderr %q", tc.args, code, stdout, stderr, tc.want)
		}
		after, err := os.ReadFile(configFile(deps.Root))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("rejected %q changed store: %q -> %q", tc.args, before, after)
		}
	}
}

func TestConfigValidationPrecedesStoreRead(t *testing.T) {
	// R-WHD3-JVVI
	invalid := []struct {
		args []string
		want string
	}{
		{[]string{"config", "other"}, "opsctl: unknown config subcommand 'other'\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "get"}, "opsctl: config get requires KEY\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "set", "noequals"}, "opsctl: config set needs KEY=VALUE\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "set", "BAD=value"}, "opsctl: invalid key: BAD\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "set", "key=line\nfeed"}, "opsctl: invalid value: newline in value for 'key'\n\nsee 'opsctl config --help' for usage\n"},
	}
	for _, setup := range []struct {
		name string
		root func(*testing.T) string
	}{
		{"corrupt", func(t *testing.T) string {
			root := t.TempDir()
			writeCorrupt(t, root)
			return root
		}},
		{"inaccessible", func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "not-a-directory")
			if err := os.WriteFile(path, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	} {
		t.Run(setup.name, func(t *testing.T) {
			for _, tc := range invalid {
				deps := depsAt(t, 0)
				deps.Root = setup.root(t)
				stdout, stderr, code := invoke(tc.args, deps)
				if code != 2 || stdout != "" || stderr != tc.want {
					t.Errorf("%q: exit %d stdout %q stderr %q, want 2, empty stdout, stderr %q", tc.args, code, stdout, stderr, tc.want)
				}
			}

			root := setup.root(t)
			store := config.Store{Root: root}
			if err := store.Set("BAD", "value"); !errors.Is(err, config.ErrInvalidKey) {
				t.Errorf("Store.Set invalid key: err = %v, want ErrInvalidKey", err)
			}
			if err := store.Set("key", "line\nfeed"); !errors.Is(err, config.ErrInvalidValue) {
				t.Errorf("Store.Set invalid value: err = %v, want ErrInvalidValue", err)
			}
		})
	}
}

func TestConfigDel(t *testing.T) {
	// R-O3TX-OURJ
	deps := depsAt(t, 0)
	_, stderr, code := invoke([]string{"config", "set", "drop=1"}, deps)
	if code != 0 {
		t.Fatalf("set drop: exit %d stderr %q", code, stderr)
	}
	_, stderr, code = invoke([]string{"config", "set", "keep=2"}, deps)
	if code != 0 {
		t.Fatalf("set keep: exit %d stderr %q", code, stderr)
	}

	stdout, stderr, code := invoke([]string{"config", "del", "drop"}, deps)
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("del existing: exit %d stdout %q stderr %q, want 0, empty, empty", code, stdout, stderr)
	}
	stdout, stderr, code = invoke([]string{"config", "get", "drop"}, deps)
	if code != 1 {
		t.Errorf("get after del: exit %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("get after del: stdout = %q, want empty", stdout)
	}
	if stderr != "opsctl: key not set: drop\n" {
		t.Errorf("get after del: stderr = %q, want key-not-set", stderr)
	}

	stdout, stderr, code = invoke([]string{"config", "del", "drop"}, deps)
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("del already absent: exit %d stdout %q stderr %q, want 0, empty, empty", code, stdout, stderr)
	}

	stdout, stderr, code = invoke([]string{"config", "get", "keep"}, deps)
	if code != 0 || stdout != "2\n" || stderr != "" {
		t.Errorf("get keep: exit %d stdout %q stderr %q, want 0, %q, empty stderr", code, stdout, stderr, "2\n")
	}

	missing := depsAt(t, 0)
	stdout, stderr, code = invoke([]string{"config", "del", "any.key"}, missing)
	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("del missing store: exit %d stdout %q stderr %q, want 0, empty, empty", code, stdout, stderr)
	}
}

func TestConfigList(t *testing.T) {
	// R-O51U-2MI8
	deps := depsAt(t, 0)
	stdout, stderr, code := invoke([]string{"config", "list"}, deps)
	if code != 0 {
		t.Errorf("empty: exit %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("empty: stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Errorf("empty: stderr = %q, want empty", stderr)
	}

	for _, arg := range []string{"z=last", "a=first", "m=mid"} {
		_, stderr, code = invoke([]string{"config", "set", arg}, deps)
		if code != 0 {
			t.Fatalf("set %s: exit %d stderr %q", arg, code, stderr)
		}
	}
	stdout, stderr, code = invoke([]string{"config", "list"}, deps)
	if code != 0 {
		t.Errorf("list: exit %d, want 0", code)
	}
	if stderr != "" {
		t.Errorf("list: stderr = %q, want empty", stderr)
	}
	want := "a=first\nm=mid\nz=last\n"
	if stdout != want {
		t.Errorf("list: stdout = %q, want %q", stdout, want)
	}
}

func TestConfigMissingOrUnknownSubcommand(t *testing.T) {
	// R-D3B7-CGSQ
	deps := depsAt(t, 0)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"config"}, "opsctl: no config subcommand given\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "nosuch"}, "opsctl: unknown config subcommand 'nosuch'\n\nsee 'opsctl config --help' for usage\n"},
		{[]string{"config", "status"}, "opsctl: unknown config subcommand 'status'\n\nsee 'opsctl config --help' for usage\n"},
	}
	for _, tc := range cases {
		stdout, stderr, code := invoke(tc.args, deps)
		if code != 2 {
			t.Errorf("%q: exit %d, want 2", tc.args, code)
		}
		if stdout != "" {
			t.Errorf("%q: stdout = %q, want empty", tc.args, stdout)
		}
		if stderr != tc.want {
			t.Errorf("%q: stderr = %q, want %q", tc.args, stderr, tc.want)
		}
	}
}

func TestConfigCorrupt(t *testing.T) {
	// R-WG57-644T
	subcmds := [][]string{
		{"config", "get", "a"},
		{"config", "set", "a=b"},
		{"config", "del", "a"},
		{"config", "list"},
	}
	for _, args := range subcmds {
		deps := depsAt(t, 0)
		writeCorrupt(t, deps.Root)
		before, err := os.ReadFile(configFile(deps.Root))
		if err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := invoke(args, deps)
		if code != 1 {
			t.Errorf("%q: exit %d, want 1", args, code)
		}
		if stdout != "" {
			t.Errorf("%q: stdout = %q, want empty", args, stdout)
		}
		want := "opsctl: " + configFile(deps.Root) + " is corrupt\n"
		if stderr != want {
			t.Errorf("%q: stderr = %q, want %q", args, stderr, want)
		}
		after, err := os.ReadFile(configFile(deps.Root))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("%q: corrupt file changed: %q -> %q", args, before, after)
		}
	}
}

func TestConfigFilesystemAccessFailures(t *testing.T) {
	// R-2BT9-PV5V
	for _, tc := range []struct {
		operation string
		args      []string
		failureOp string
		failure   func(string) string
	}{
		{"get", []string{"config", "get", "key"}, "open", func(path string) string { return path }},
		{"set", []string{"config", "set", "key=value"}, "mkdir", filepath.Dir},
		{"del", []string{"config", "del", "key"}, "stat", filepath.Dir},
		{"list", []string{"config", "list"}, "open", func(path string) string { return path }},
	} {
		for _, rootCase := range []struct {
			name   string
			suffix string
		}{
			{name: "ordinary"},
			{name: "escaped", suffix: "root\r\nline"},
		} {
			t.Run(tc.operation+"/"+rootCase.name, func(t *testing.T) {
				deps := depsAt(t, 0)
				if rootCase.suffix != "" {
					deps.Root = filepath.Join(deps.Root, rootCase.suffix)
				}
				path := configFile(deps.Root)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				blocked := filepath.Join(deps.Root, "etc")
				if err := syscall.Chmod(blocked, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = syscall.Chmod(blocked, 0o700) })

				stdout, stderr, code := invoke(tc.args, deps)
				if code != 1 {
					t.Errorf("exit = %d, want 1", code)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want empty", stdout)
				}
				failurePath := tc.failure(path)
				want := "opsctl: config " + tc.operation + " failed for " + strconv.Quote(path) +
					": " + tc.failureOp + " " + failurePath + ": permission denied\n"
				if rootCase.suffix != "" {
					want = "opsctl: config " + tc.operation + " failed for " + strconv.Quote(path) +
						": " + tc.failureOp + " " + strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(failurePath) +
						": permission denied\n"
				}
				if stderr != want {
					t.Errorf("stderr = %q, want %q", stderr, want)
				}
			})
		}
	}
}

func configFile(root string) string {
	return filepath.Join(root, "etc", "ikigenba", "config.json")
}

func writeCorrupt(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "etc", "ikigenba")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
}
