package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigHelp(t *testing.T) {
	// R-NYYC-5RSR
	user := depsAt(t, 1)
	for _, args := range [][]string{
		{"config", "--help"},
		{"config", "-h"},
	} {
		stdout, stderr, code := invoke(args, user)
		if code != 0 {
			t.Errorf("%q: exit %d, want 0", args, code)
		}
		if stderr != "" {
			t.Errorf("%q: stderr = %q, want empty", args, stderr)
		}
		if stdout != wantConfigUsage {
			t.Errorf("%q: stdout = %q, want config usage", args, stdout)
		}
	}
}

func TestConfigGet(t *testing.T) {
	// R-O068-JJJG
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
	want := "opsctl config: key not set: backup.s3_uri\n"
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
	// R-O2M1-B30U
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
		{"missing equals", "noequals", "opsctl config: set requires KEY=VALUE\n"},
		{"invalid key", "INVALID=x", "opsctl config: invalid key: INVALID\n"},
		{"newline value", "ok=line\nfeed", "opsctl config: invalid value\n"},
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
	if stderr != "opsctl config: set requires KEY=VALUE\n" {
		t.Errorf("missing store: stderr = %q, want set-requires diagnostic", stderr)
	}
	if _, err := os.Stat(configFile(empty.Root)); !os.IsNotExist(err) {
		t.Errorf("usage error created the store file: %v", err)
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
	if stderr != "opsctl config: key not set: drop\n" {
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
	// R-O69Q-GE8X
	deps := depsAt(t, 0)
	wantErr := prefixEachLine(wantConfigUsage, "opsctl config: ")
	for _, args := range [][]string{
		{"config"},
		{"config", "nosuch"},
		{"config", "status"},
	} {
		stdout, stderr, code := invoke(args, deps)
		if code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
		if stdout != "" {
			t.Errorf("%q: stdout = %q, want empty", args, stdout)
		}
		if stderr != wantErr {
			t.Errorf("%q: stderr = %q, want prefixed config usage", args, stderr)
		}
	}
}

func TestConfigCorrupt(t *testing.T) {
	// R-O7HM-U5ZM
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
		assertEveryLinePrefixed(t, strings.Join(args, " "), stderr, "opsctl config: ")
		if !strings.Contains(stderr, "config.json") {
			t.Errorf("%q: stderr = %q, want to name config.json", args, stderr)
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
