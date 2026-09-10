package cli_test

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
)

const wantUsage = `Usage: opsctl [options] <command> [arguments]

Operate the ikigenba platform host. Must run as root.

Commands:
  config    read and write the host configuration store
  dns       manage DNS records in the zones opsctl owns
  init      run the setup sequence behind one preflight
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

func assertEveryLinePrefixed(t *testing.T, name, stderr, prefix string) {
	t.Helper()
	if stderr == "" {
		t.Errorf("%s: stderr is empty, want lines beginning with %q", name, prefix)
		return
	}
	lines := strings.Split(stderr, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	} else {
		t.Errorf("%s: stderr is not newline-terminated: %q", name, stderr)
	}
	if len(lines) == 0 {
		t.Errorf("%s: stderr has no lines", name)
		return
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			t.Errorf("%s: stderr line %d = %q, want prefix %q", name, i, line, prefix)
		}
	}
}

func invoke(args []string, deps cli.Deps) (stdout, stderr string, code int) {
	var outBuf, errBuf bytes.Buffer
	code = cli.Run(args, strings.NewReader(""), &outBuf, &errBuf, deps)
	return outBuf.String(), errBuf.String(), code
}

func depsAt(t *testing.T, euid int) cli.Deps {
	t.Helper()
	return cli.Deps{Root: t.TempDir(), EUID: euid}
}

func observeFilesystemAccess(t *testing.T, root string) func() {
	t.Helper()
	var paths []string
	if walkErr := filepath.WalkDir(root, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		paths = append(paths, path)
		return nil
	}); walkErr != nil {
		t.Fatalf("enumerate Root tree: %v", walkErr)
	}

	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		t.Fatalf("initialize filesystem access observer: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := syscall.Close(fd); closeErr != nil {
			t.Errorf("close filesystem access observer: %v", closeErr)
		}
	})

	const events = syscall.IN_ACCESS | syscall.IN_ATTRIB | syscall.IN_CLOSE_WRITE |
		syscall.IN_CLOSE_NOWRITE | syscall.IN_CREATE | syscall.IN_DELETE |
		syscall.IN_DELETE_SELF | syscall.IN_MODIFY | syscall.IN_MOVE_SELF |
		syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_OPEN
	for _, path := range paths {
		if _, watchErr := syscall.InotifyAddWatch(fd, path, events); watchErr != nil {
			t.Fatalf("watch %s: %v", path, watchErr)
		}
	}

	return func() {
		t.Helper()
		buffer := make([]byte, 4096)
		n, readErr := syscall.Read(fd, buffer)
		if readErr != nil {
			if errors.Is(readErr, syscall.EAGAIN) {
				return
			}
			t.Fatalf("read filesystem access observer: %v", readErr)
		}
		if n != 0 {
			t.Errorf("observed %d bytes of filesystem events under Root", n)
		}
	}
}

func TestTopLevelGrammar(t *testing.T) {
	// R-N211-TYS0
	user := depsAt(t, 1)
	root := depsAt(t, 0)

	if got, want := functionSwitchCases(t, "unknownTopLevelOption"), []string{"--help", "--version", "-V", "-h"}; !slices.Equal(got, want) {
		t.Fatalf("accepted top-level option names = %q, want exactly %q", got, want)
	}

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
	wantErr := "opsctl: unknown option '--not-an-option'\n\nsee 'opsctl --help' for usage\n"
	if code != 2 || stdout != "" || stderr != wantErr {
		t.Errorf("unknown option: exit %d stdout %q stderr %q, want exit 2, empty stdout, stderr %q", code, stdout, stderr, wantErr)
	}

	for _, option := range []string{
		"--help=true", "--help=false", "-h=true", "-h=false",
		"--version=true", "--version=false", "-V=true", "-V=false",
		"-help", "-version", "--",
	} {
		stdout, stderr, code = invoke([]string{option}, user)
		wantErr = "opsctl: unknown option '" + option + "'\n\nsee 'opsctl --help' for usage\n"
		if code != 2 || stdout != "" || stderr != wantErr {
			t.Errorf("%q: exit %d stdout %q stderr %q, want exit 2, empty stdout, stderr %q", option, code, stdout, stderr, wantErr)
		}
	}
}

func TestCommandSet(t *testing.T) {
	// R-EALS-YXXE
	user := depsAt(t, 1)

	if got, want := functionSwitchCases(t, "dispatch"), []string{"config", "dns", "init", "version"}; !slices.Equal(got, want) {
		t.Fatalf("top-level dispatch cases = %q, want exactly %q", got, want)
	}

	stdout, stderr, code := invoke([]string{"config", "--help"}, user)
	if code != 0 || stderr != "" || stdout != wantConfigUsage {
		t.Errorf("config: exit %d stdout %q stderr %q, want config usage", code, stdout, stderr)
	}

	stdout, stderr, code = invoke([]string{"version"}, user)
	if code != 0 || stderr != "" || stdout != wantVersion+"\n" {
		t.Errorf("version: exit %d stdout %q stderr %q, want %q", code, stdout, stderr, wantVersion+"\n")
	}

	stdout, stderr, code = invoke([]string{"dns"}, user)
	if code != 3 || stdout != "" || stderr != "opsctl: must run as root\n" {
		t.Errorf("dns: exit %d stdout %q stderr %q, want recognized action refused as non-root", code, stdout, stderr)
	}

	stdout, stderr, code = invoke([]string{"init", "--help"}, user)
	if code != 0 || stderr != "" || stdout != wantInitUsage {
		t.Errorf("init: exit %d stdout %q stderr %q, want init usage", code, stdout, stderr)
	}

	for _, name := range []string{"status", "other", "config-backup", "VERSION"} {
		stdout, stderr, code = invoke([]string{name}, user)
		wantErr := "opsctl: unknown command '" + name + "'\n\nsee 'opsctl --help' for usage\n"
		if code != 2 || stdout != "" || stderr != wantErr {
			t.Errorf("%s: exit %d stdout %q stderr %q, want exit 2, empty stdout and %q", name, code, stdout, stderr, wantErr)
		}
	}
}

func parseCLIFile(t *testing.T) *ast.File {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), "cli.go", nil, 0)
	if err != nil {
		t.Fatalf("parse cli.go: %v", err)
	}
	return parsed
}

func namedFunction(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range parseCLIFile(t).Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("function %s not found in cli.go", name)
	return nil
}

func functionSwitchCases(t *testing.T, function string) []string {
	t.Helper()
	var values []string
	ast.Inspect(namedFunction(t, function).Body, func(node ast.Node) bool {
		clause, ok := node.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, expression := range clause.List {
			literal, ok := expression.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatalf("unquote dispatch case: %v", err)
			}
			values = append(values, value)
		}
		return true
	})
	slices.Sort(values)
	return values
}

func TestTopLevelHelp(t *testing.T) {
	// R-EBTP-CPO3
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

func TestNoCommand(t *testing.T) {
	// R-CYFL-TDTY
	stdout, stderr, code := invoke(nil, depsAt(t, 0))
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	want := "opsctl: no command given\n\nsee 'opsctl --help' for usage\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestUnknownCommand(t *testing.T) {
	// R-CZNI-75KN
	for _, name := range []string{"nosuch", "othercmd"} {
		stdout, stderr, code := invoke([]string{name}, depsAt(t, 0))
		if code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
		if stdout != "" {
			t.Errorf("%s: stdout = %q, want empty", name, stdout)
		}
		want := "opsctl: unknown command '" + name + "'\n\nsee 'opsctl --help' for usage\n"
		if stderr != want {
			t.Errorf("%s: stderr = %q, want %q", name, stderr, want)
		}
	}
}

func TestUnknownOption(t *testing.T) {
	// R-D0VE-KXBC
	for _, args := range [][]string{
		{"--not-an-option"},
		{"-Z"},
		{"--help=true"},
		{"-h=true"},
		{"--version=true"},
		{"-V=true"},
		{"-help"},
		{"-version"},
		{"--"},
	} {
		stdout, stderr, code := invoke(args, depsAt(t, 0))
		if code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
		if stdout != "" {
			t.Errorf("%q: stdout = %q, want empty", args, stdout)
		}
		want := "opsctl: unknown option '" + args[0] + "'\n\nsee 'opsctl --help' for usage\n"
		if stderr != want {
			t.Errorf("%q: stderr = %q, want %q", args, stderr, want)
		}
	}
}

func TestVersionVar(t *testing.T) {
	// R-N9CG-4L86
	re := regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	value := versionLiteral(t)
	if !re.MatchString(value) {
		t.Errorf("version = %q, want to match %s", value, re.String())
	}
}

func versionLiteral(t *testing.T) string {
	t.Helper()
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
	return value
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
		{"dns", "list", "ikigenba.dev"},
	} {
		root := t.TempDir()
		configDir := filepath.Join(root, "etc", "ikigenba")
		if err := os.MkdirAll(configDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertNoAccess := observeFilesystemAccess(t, root)

		var stdout, stderr bytes.Buffer
		code := cli.Run(args, strings.NewReader(""), &stdout, &stderr, cli.Deps{Root: root, EUID: 1})
		assertNoAccess()

		if code != 3 {
			t.Errorf("%q: exit %d, want 3 (stderr %q)", args, code, stderr.String())
		}
		if stdout.String() != "" {
			t.Errorf("%q: stdout = %q, want empty", args, stdout.String())
		}
		if stderr.String() != "opsctl: must run as root\n" {
			t.Errorf("%q: stderr = %q, want %q", args, stderr.String(), "opsctl: must run as root\n")
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
		{[]string{"version", "--help"}, wantVersion + "\n"},
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

	stdout, stderr, code := invoke([]string{"dns", "--help"}, user)
	if code != 0 || stderr != "" || stdout == "" {
		t.Errorf("dns --help: exit %d stdout %q stderr %q, want non-empty help on stdout and success", code, stdout, stderr)
	}
}

func TestStderrPrefix(t *testing.T) {
	// R-R0P9-H29Y
	user := depsAt(t, 1)
	root := depsAt(t, 0)
	configured := configuredDNSDeps(t, &fakeDNSProvider{recordsErr: map[string]error{"ZONE": errors.New("provider failed")}}, "example.com")

	cases := []struct {
		name string
		args []string
		deps cli.Deps
	}{
		{"no command", nil, root},
		{"unknown command", []string{"nosuch"}, root},
		{"unknown command with newline", []string{"no\nsuch"}, root},
		{"unknown option", []string{"--not-an-option"}, user},
		{"root refusal", []string{"config", "set", "dns.zones=x"}, user},
		{"config missing subcommand", []string{"config"}, root},
		{"config unknown subcommand", []string{"config", "wat"}, root},
		{"config malformed set", []string{"config", "set", "missing-equals"}, root},
		{"config invalid key", []string{"config", "set", "BAD=value"}, root},
		{"config missing key", []string{"config", "get", "missing.key"}, root},
		{"dns missing subcommand", []string{"dns"}, root},
		{"dns unknown subcommand", []string{"dns", "wat"}, root},
		{"dns missing configuration", []string{"dns", "list", "example.com"}, root},
		{"dns malformed list", []string{"dns", "list"}, configured},
		{"dns unconfigured zone", []string{"dns", "list", "other.test"}, configured},
		{"dns provider failure", []string{"dns", "list", "example.com"}, configured},
	}
	for _, tc := range cases {
		_, stderr, _ := invoke(tc.args, tc.deps)
		if stderr == "" {
			t.Errorf("%s: stderr is empty, want diagnostic", tc.name)
			continue
		}
		first, _, _ := strings.Cut(stderr, "\n")
		if !strings.HasPrefix(first, "opsctl: ") {
			t.Errorf("%s: first stderr line = %q, want prefix %q", tc.name, first, "opsctl: ")
		}
		if strings.Contains(stderr, wantUsage) || strings.Contains(stderr, "Usage: opsctl") {
			t.Errorf("%s: stderr contains usage text: %q", tc.name, stderr)
		}
	}
}

func TestSuccessWritesNoStderr(t *testing.T) {
	// R-NGNU-F7OC
	root := depsAt(t, 0)

	configCases := []struct {
		name       string
		args       []string
		wantStdout string
	}{
		{"version", []string{"version"}, wantVersion + "\n"},
		{"config set", []string{"config", "set", "test.key=value"}, ""},
		{"config get", []string{"config", "get", "test.key"}, "value\n"},
		{"config list", []string{"config", "list"}, "test.key=value\n"},
		{"config del", []string{"config", "del", "test.key"}, ""},
	}
	for _, tc := range configCases {
		stdout, stderr, code := invoke(tc.args, root)
		if code != 0 || stdout != tc.wantStdout || stderr != "" {
			t.Errorf("%s: exit %d stdout %q stderr %q, want exit 0, stdout %q, empty stderr", tc.name, code, stdout, stderr, tc.wantStdout)
		}
	}

	provider := &fakeDNSProvider{records: map[string][]dns.Record{"ZONE": {
		{Name: "example.com", Type: "SOA"},
		{Name: "example.com", Type: "NS", TTL: 300, Values: []string{"ns.example"}},
	}}}
	dnsDeps := configuredDNSDeps(t, provider, "example.com")
	dnsDeps.DNS.LookupNS = func(context.Context, string) ([]string, error) {
		return []string{"ns.example"}, nil
	}
	dnsDeps.Getenv = func(key string) string {
		if key == "CERTBOT_DOMAIN" {
			return "example.com"
		}
		return "token"
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"dns list", []string{"dns", "list", "example.com"}},
		{"dns add", []string{"dns", "add", "record.example.com", "TXT", "value"}},
		{"dns remove", []string{"dns", "remove", "record.example.com", "TXT", "value"}},
		{"dns check", []string{"dns", "check"}},
		{"dns acme-auth", []string{"dns", "acme-auth"}},
		{"dns acme-cleanup", []string{"dns", "acme-cleanup"}},
	} {
		_, stderr, code := invoke(tc.args, dnsDeps)
		if code != 0 || stderr != "" {
			t.Errorf("%s: exit %d stderr %q, want exit 0 and empty stderr", tc.name, code, stderr)
		}
	}
}
