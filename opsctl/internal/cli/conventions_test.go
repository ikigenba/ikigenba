package cli_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
)

const wantUsage = `Usage: opsctl [options] <command> [arguments]

Operate the ikigenba platform host. Must run as root.

Commands:
  config    read and write the host configuration store
  version   print the version

Options:
  -h, --help     print this help
  -V, --version  print the version

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: opsctl must run as root

Run 'opsctl <command> --help' for details on a command.
`

const wantConfigUsage = `Usage: opsctl config <subcommand> [arguments]

Read and write the host configuration store (/etc/ikigenba/config.json).

Subcommands:
  get KEY        print the value of KEY; exit 1 if KEY is not set
  set KEY=VALUE  set KEY to VALUE, creating or replacing it
  del KEY        remove KEY; succeeds whether or not KEY is set
  list           print every KEY=VALUE, one per line, sorted by key

Keys match ^[a-z0-9_.-]+$. Values may not contain newlines.
`

const wantVersion = "v0.1.0"

func invoke(args []string, deps cli.Deps) (stdout, stderr string, code int) {
	var outBuf, errBuf bytes.Buffer
	code = cli.Run(args, strings.NewReader(""), &outBuf, &errBuf, deps)
	return outBuf.String(), errBuf.String(), code
}

func depsAt(t *testing.T, euid int) cli.Deps {
	t.Helper()
	return cli.Deps{Root: t.TempDir(), EUID: euid}
}

func TestTopLevelGrammar(t *testing.T) {
	// R-N211-TYS0
	user := depsAt(t, 1)
	root := depsAt(t, 0)

	stdout, stderr, code := invoke([]string{"-h"}, user)
	if code != 0 || stderr != "" || stdout != wantUsage {
		t.Errorf("-h: exit %d stdout %q stderr %q, want exit 0, usage on stdout, empty stderr", code, stdout, stderr)
	}

	stdout, stderr, code = invoke([]string{"--help", "config"}, user)
	if code != 0 || stderr != "" || stdout != wantUsage {
		t.Errorf("--help before command: exit %d stdout %q stderr %q, want top-level usage", code, stdout, stderr)
	}

	stdout, stderr, code = invoke([]string{"-V"}, user)
	if code != 0 || stderr != "" || stdout != wantVersion+"\n" {
		t.Errorf("-V: exit %d stdout %q stderr %q, want %q", code, stdout, stderr, wantVersion+"\n")
	}

	stdout, stderr, code = invoke([]string{"--version"}, user)
	if code != 0 || stderr != "" || stdout != wantVersion+"\n" {
		t.Errorf("--version: exit %d stdout %q stderr %q, want %q", code, stdout, stderr, wantVersion+"\n")
	}

	stdout, stderr, code = invoke([]string{"config", "--help"}, user)
	if code != 0 || stderr != "" || stdout != wantConfigUsage {
		t.Errorf("config --help: exit %d stdout %q stderr %q, want config usage (option after command is not top-level)", code, stdout, stderr)
	}

	stdout, stderr, code = invoke([]string{"config", "--version"}, root)
	if code != 2 {
		t.Errorf("config --version: exit %d stderr %q, want 2 (not a top-level option)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("config --version: stdout = %q, want empty", stdout)
	}

	stdout, stderr, code = invoke([]string{"--not-an-option"}, user)
	if code != 2 || stdout != "" || stderr != wantUsage {
		t.Errorf("unknown option: exit %d stdout %q stderr %q, want exit 2, empty stdout, usage on stderr", code, stdout, stderr)
	}
}

func TestCommandSet(t *testing.T) {
	// R-N38Y-7QIP
	user := depsAt(t, 1)

	stdout, stderr, code := invoke([]string{"config", "--help"}, user)
	if code != 0 || stderr != "" || stdout != wantConfigUsage {
		t.Errorf("config: exit %d stdout %q stderr %q, want config usage", code, stdout, stderr)
	}

	stdout, stderr, code = invoke([]string{"version"}, user)
	if code != 0 || stderr != "" || stdout != wantVersion+"\n" {
		t.Errorf("version: exit %d stdout %q stderr %q, want %q", code, stdout, stderr, wantVersion+"\n")
	}

	stdout, stderr, code = invoke([]string{"status"}, user)
	if code != 2 {
		t.Errorf("status: exit %d, want 2", code)
	}
	wantErr := "opsctl: unknown command: \"status\"\n" + wantUsage
	if stdout != "" || stderr != wantErr {
		t.Errorf("status: stdout %q stderr %q, want empty stdout and %q", stdout, stderr, wantErr)
	}
}

func TestTopLevelHelp(t *testing.T) {
	// R-N4GU-LI9E
	user := depsAt(t, 1)
	for _, args := range [][]string{{"--help"}, {"-h"}} {
		stdout, stderr, code := invoke(args, user)
		if code != 0 {
			t.Errorf("%q: exit %d, want 0", args, code)
		}
		if stderr != "" {
			t.Errorf("%q: stderr = %q, want empty", args, stderr)
		}
		if stdout != wantUsage {
			t.Errorf("%q: stdout = %q, want usage text once", args, stdout)
		}
	}
}

func TestNoCommandPrintsUsage(t *testing.T) {
	// R-N5OQ-ZA03
	stdout, stderr, code := invoke(nil, depsAt(t, 0))
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if stderr != wantUsage {
		t.Errorf("stderr = %q, want usage text", stderr)
	}
}

func TestUnknownCommand(t *testing.T) {
	// R-N6WN-D1QS
	for _, name := range []string{"nosuch", "othercmd"} {
		stdout, stderr, code := invoke([]string{name}, depsAt(t, 0))
		if code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
		if stdout != "" {
			t.Errorf("%s: stdout = %q, want empty", name, stdout)
		}
		want := "opsctl: unknown command: " + strconv.Quote(name) + "\n" + wantUsage
		if stderr != want {
			t.Errorf("%s: stderr = %q, want %q", name, stderr, want)
		}
	}
}

func TestUnknownOption(t *testing.T) {
	// R-N84J-QTHH
	for _, args := range [][]string{{"--not-an-option"}, {"-Z"}} {
		stdout, stderr, code := invoke(args, depsAt(t, 0))
		if code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
		if stdout != "" {
			t.Errorf("%q: stdout = %q, want empty", args, stdout)
		}
		if stderr != wantUsage {
			t.Errorf("%q: stderr = %q, want usage text", args, stderr)
		}
	}
}

func TestVersionVar(t *testing.T) {
	// R-N9CG-4L86
	re := regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var value string
	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(".", entry.Name())
		parsed, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if name.Name != "version" {
						continue
					}
					if found {
						t.Fatalf("multiple package-level var version declarations")
					}
					found = true
					if vs.Type != nil {
						ident, ok := vs.Type.(*ast.Ident)
						if !ok || ident.Name != "string" {
							t.Errorf("%s: var version type = %T, want string", path, vs.Type)
						}
					}
					if i >= len(vs.Values) {
						t.Fatalf("%s: var version has no value", path)
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s: var version value is not a string literal", path)
					}
					value, err = strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquote version in %s: %v", path, err)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("package-level var version string not found")
	}
	if !re.MatchString(value) {
		t.Errorf("version = %q, want to match %s", value, re.String())
	}
}

func TestVersionOutput(t *testing.T) {
	// R-NAKC-ICYV
	user := depsAt(t, 1)
	for _, args := range [][]string{{"version"}, {"-V"}, {"--version"}} {
		stdout, stderr, code := invoke(args, user)
		if code != 0 {
			t.Errorf("%q: exit %d, want 0", args, code)
		}
		if stderr != "" {
			t.Errorf("%q: stderr = %q, want empty", args, stderr)
		}
		if stdout != wantVersion+"\n" {
			t.Errorf("%q: stdout = %q, want %q", args, stdout, wantVersion+"\n")
		}
	}
}

func TestActionRequiresRoot(t *testing.T) {
	// R-NBS8-W4PK
	for _, args := range [][]string{
		{"config", "set", "dns.zones=ikigenba.dev"},
		{"config", "get", "dns.zones"},
	} {
		// Root is a regular file, so any read or write under it fails.
		root := filepath.Join(t.TempDir(), "as-file")
		if err := os.WriteFile(root, []byte("not-a-directory"), 0o600); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		code := cli.Run(args, strings.NewReader(""), &stdout, &stderr, cli.Deps{Root: root, EUID: 1})

		if code != 3 {
			t.Errorf("%q: exit %d, want 3 (stderr %q)", args, code, stderr.String())
		}
		if stdout.String() != "" {
			t.Errorf("%q: stdout = %q, want empty", args, stdout.String())
		}
		if stderr.String() != "opsctl: must run as root\n" {
			t.Errorf("%q: stderr = %q, want %q", args, stderr.String(), "opsctl: must run as root\n")
		}

		info, err := os.Stat(root)
		if err != nil {
			t.Fatalf("%q: stat Root: %v", args, err)
		}
		if !info.Mode().IsRegular() {
			t.Errorf("%q: Root is no longer a regular file; a command action wrote under it", args)
		}
	}
}

func TestHelpAndVersionWithoutRoot(t *testing.T) {
	// R-ND05-9WG9
	user := depsAt(t, 1)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, wantUsage},
		{[]string{"-h"}, wantUsage},
		{[]string{"--version"}, wantVersion + "\n"},
		{[]string{"-V"}, wantVersion + "\n"},
		{[]string{"version"}, wantVersion + "\n"},
		{[]string{"config", "--help"}, wantConfigUsage},
	}
	for _, tc := range cases {
		stdout, stderr, code := invoke(tc.args, user)
		if code != 0 {
			t.Errorf("%q: exit %d, want 0", tc.args, code)
		}
		if stderr != "" {
			t.Errorf("%q: stderr = %q, want empty", tc.args, stderr)
		}
		if stdout != tc.want {
			t.Errorf("%q: stdout = %q, want %q", tc.args, stdout, tc.want)
		}
	}
}

func TestStderrPrefix(t *testing.T) {
	// R-NE81-NO6Y
	_, stderr, _ := invoke([]string{"nosuch"}, depsAt(t, 0))
	first, _, _ := strings.Cut(stderr, "\n")
	if first != "opsctl: unknown command: \"nosuch\"" {
		t.Errorf("unknown command first line = %q, want %q", first, "opsctl: unknown command: \"nosuch\"")
	}

	_, stderr, _ = invoke([]string{"config", "set", "dns.zones=x"}, depsAt(t, 1))
	if stderr != "opsctl: must run as root\n" {
		t.Errorf("root refusal stderr = %q, want %q", stderr, "opsctl: must run as root\n")
	}

	_, stderr, _ = invoke([]string{"config", "get", "missing.key"}, depsAt(t, 0))
	if stderr != "opsctl config: key not set\n" {
		t.Errorf("inside config stderr = %q, want %q", stderr, "opsctl config: key not set\n")
	}
}

func TestSuccessWritesNoStderr(t *testing.T) {
	// R-NGNU-F7OC
	root := depsAt(t, 0)

	_, stderr, code := invoke([]string{"version"}, root)
	if code != 0 {
		t.Fatalf("version: exit %d, want 0", code)
	}
	if stderr != "" {
		t.Errorf("version: stderr = %q, want empty", stderr)
	}

	_, stderr, code = invoke([]string{"config", "set", "dns.zones=ikigenba.dev"}, root)
	if code != 0 {
		t.Fatalf("config set: exit %d stderr %q, want 0", code, stderr)
	}
	if stderr != "" {
		t.Errorf("config set: stderr = %q, want empty", stderr)
	}

	_, stderr, code = invoke([]string{"config", "get", "dns.zones"}, root)
	if code != 0 {
		t.Fatalf("config get: exit %d stderr %q, want 0", code, stderr)
	}
	if stderr != "" {
		t.Errorf("config get: stderr = %q, want empty", stderr)
	}
}
