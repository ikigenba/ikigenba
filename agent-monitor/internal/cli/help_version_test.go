package cli_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
)

func TestUsageDeclaration(t *testing.T) {
	// R-2GOG-2JAT: this assignment compiles only while Usage is a string constant.
	const actual string = cli.Usage
	// R-QCVS-17QG
	const want = "Usage: agent-monitor [options]\n       agent-monitor list <harness>\n       agent-monitor tree <harness> <session-id>\n\nObserve the coding agents on this machine through their logs and hooks.\n\nCommands:\n  list <harness>               list the live root sessions of claude, codex, or grok\n  tree <harness> <session-id>  draw the subagent tree of one session\n\nsee 'agent-monitor <command> --help' for command options\n\nOptions:\n  -h, --help      print this help\n  -V, --version   print the version\n\nExit codes:\n  0  success\n  1  the output could not be written\n  2  usage error\n  3  the harness's session data could not be read\n  4  the session was not found\n"
	if actual != want {
		t.Errorf("Usage = %q, want %q", actual, want)
	}
}

func TestVersionDeclaration(t *testing.T) {
	// R-67Z3-IN6V and R-2FGJ-ORK4: verify the sole source declaration and literal initializer.
	data, err := os.ReadFile("version.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "version.go", data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Imports) != 0 || len(file.Decls) != 1 {
		t.Fatalf("version.go has %d imports and %d declarations", len(file.Imports), len(file.Decls))
	}
	decl, ok := file.Decls[0].(*ast.GenDecl)
	if !ok || decl.Tok != token.VAR || len(decl.Specs) != 1 {
		t.Fatalf("version.go must declare one var: %#v", file.Decls[0])
	}
	spec, ok := decl.Specs[0].(*ast.ValueSpec)
	if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "Version" || len(spec.Values) != 1 {
		t.Fatalf("version.go must initialize Version once: %#v", decl.Specs[0])
	}
	if spec.Type != nil {
		valueType, ok := spec.Type.(*ast.Ident)
		if !ok || valueType.Name != "string" {
			t.Fatalf("Version must have type string: %#v", spec.Type)
		}
	}
	literal, ok := spec.Values[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		t.Fatalf("Version initializer must be a string literal: %#v", spec.Values[0])
	}
	sourceValue, err := strconv.Unquote(literal.Value)
	if err != nil {
		t.Fatal(err)
	}
	if sourceValue != cli.Version {
		t.Fatalf("source Version = %q, package Version = %q", sourceValue, cli.Version)
	}

	original := cli.Version
	cli.Version = original + " test override"
	defer func() { cli.Version = original }()

	// The default build comparison is also exercised in cmd/agent-monitor.
	if original == "" {
		t.Error("Version has no source-initialized value")
	}
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"--version"}, cli.System{}, &stdout, &stderr)
	want := original + " test override\n"
	if code != cli.ExitSuccess || stdout.String() != want || stderr.Len() != 0 {
		t.Errorf("Run with overridden Version = (%q, %q, %d), want (%q, empty, %d)", stdout.String(), stderr.String(), code, want, cli.ExitSuccess)
	}
}

func TestVersionShape(t *testing.T) {
	// R-37I8-HHM3
	const pattern = `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*)?(\+[0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*)?$`
	if !regexp.MustCompile(pattern).MatchString(cli.Version) {
		t.Errorf("Version %q does not match the required semver shape", cli.Version)
	}
}

func TestHelpOptions(t *testing.T) {
	// R-352F-PY4P
	for _, arg := range []string{"--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{arg}, cli.System{}, &stdout, &stderr)
			if code != cli.ExitSuccess || stdout.String() != cli.Usage || stderr.Len() != 0 {
				t.Errorf("Run(%q) = (%q, %q, %d), want (%q, empty, %d)", arg, stdout.String(), stderr.String(), code, cli.Usage, cli.ExitSuccess)
			}
		})
	}
}

func TestVersionOptions(t *testing.T) {
	// R-36AC-3PVE
	for _, arg := range []string{"--version", "-V"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{arg}, cli.System{}, &stdout, &stderr)
			want := cli.Version + "\n"
			if code != cli.ExitSuccess || stdout.String() != want || stderr.Len() != 0 {
				t.Errorf("Run(%q) = (%q, %q, %d), want (%q, empty, %d)", arg, stdout.String(), stderr.String(), code, want, cli.ExitSuccess)
			}
		})
	}
}

func TestListUsageDeclaration(t *testing.T) {
	// R-DIOP-AKRC R-EWOK-Z65T
	const actual string = cli.ListUsage
	const want = "Usage: agent-monitor list <harness>\n\nList the live root sessions of one harness, newest activity first.\n\nHarnesses:\n  claude  Claude Code\n  codex   OpenAI Codex CLI\n  grok    Grok Build CLI\n\nOptions:\n  -h, --help  print this help\n"
	if actual != want {
		t.Errorf("ListUsage = %q, want %q", actual, want)
	}
}

func TestListHelpWins(t *testing.T) {
	// R-F0CA-4HDW
	for _, args := range [][]string{{"list", "--help"}, {"list", "-h"}, {"list", "claude", "--help"}, {"list", "bogus", "extra", "--bogus", "-h"}} {
		var stdout, stderr bytes.Buffer
		code := cli.Run(args, cli.System{}, &stdout, &stderr)
		if code != cli.ExitSuccess || stdout.String() != cli.ListUsage || stderr.Len() != 0 {
			t.Errorf("Run(%q) = (%q, %q, %d)", args, stdout.String(), stderr.String(), code)
		}
	}
}

func TestTreeUsageText(t *testing.T) {
	// R-QFBK-SR7U
	const actual string = cli.TreeUsage
	const want = "Usage: agent-monitor tree <harness> <session-id>\n\nDraw the subagent tree of one session.\n\nHarnesses:\n  claude  Claude Code\n  codex   OpenAI Codex CLI\n  grok    Grok Build CLI\n\nOptions:\n  -h, --help  print this help\n"
	if actual != want {
		t.Errorf("TreeUsage = %q, want %q", actual, want)
	}
}

func TestTreeHelpWins(t *testing.T) {
	// R-QGJH-6IYJ
	for _, args := range [][]string{{"tree", "--help"}, {"tree", "-h"}, {"tree", "claude", "sample", "--help"}, {"tree", "bogus", "extra", "more", "--bogus", "-h"}} {
		var stdout, stderr bytes.Buffer
		code := cli.Run(args, cli.System{}, &stdout, &stderr)
		if code != cli.ExitSuccess || stdout.String() != cli.TreeUsage || stderr.Len() != 0 {
			t.Errorf("Run(%q) = (%q, %q, %d)", args, stdout.String(), stderr.String(), code)
		}
	}
}
