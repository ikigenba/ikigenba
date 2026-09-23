package cli_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agent-monitor/internal/cli"
)

const modulePath = "github.com/ikigenba/ikigenba/agent-monitor"

func TestPackageGraph(t *testing.T) {
	// R-N69Q-W7X1 and R-29D1-RWUN: The declared module has exactly the two packages.
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}} {{.Name}} {{if .Module}}{{.Module.Path}}{{end}}", "./...")
	cmd.Dir = "../.."
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	want := modulePath + "/cmd/agent-monitor main " + modulePath + "\n" +
		modulePath + "/internal/cli cli " + modulePath + "\n"
	if string(got) != want {
		t.Fatalf("package graph:\n got %q\nwant %q", got, want)
	}
}

func TestModuleHasNoRequirements(t *testing.T) {
	// R-29D1-RWUN: inspect the module file, not just the resolved package graph.
	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "require (" || strings.HasPrefix(line, "require ") {
			t.Fatalf("unexpected require directive: %q", line)
		}
	}
}

func TestCLIImports(t *testing.T) {
	// R-HFHC-NLE5: The CLI has only the permitted direct imports.
	cmd := exec.Command("go", "list", "-f", "{{join .Imports \" \"}}", "./internal/cli")
	cmd.Dir = "../.."
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	allowed := map[string]bool{"io": true, "strings": true, "unicode": true, "unicode/utf8": true}
	for _, path := range strings.Fields(string(got)) {
		if !allowed[path] {
			t.Errorf("unexpected CLI import %q", path)
		}
	}
}

func TestRunReturnsToCaller(t *testing.T) {
	// R-2L9P-PJL7: A typed Run result returns without ending the test process.
	wantType := reflect.TypeOf((func([]string, io.Writer, io.Writer) cli.ExitCode)(nil))
	if gotType := reflect.TypeOf(cli.Run); gotType != wantType {
		t.Fatalf("Run type = %v, want %v", gotType, wantType)
	}
	run := cli.Run
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != cli.ExitSuccess {
		t.Fatalf("Run returned %d", code)
	}
	if stdout.String() != "hello, world\n" || stderr.Len() != 0 {
		t.Fatalf("Run streams: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestExitCodes(t *testing.T) {
	// R-2ITW-Y03T and R-2K1T-BRUI: The three outcomes have a named type and fixed values.
	codes := [3]cli.ExitCode{cli.ExitSuccess, cli.ExitWriteFailed, cli.ExitUsage}
	if codes != [3]cli.ExitCode{0, 1, 2} {
		t.Fatalf("exit codes = %v", codes)
	}
	typ := reflect.TypeOf(cli.ExitCode(0))
	if typ.Name() != "ExitCode" || typ.Kind() != reflect.Int || typ.PkgPath() != modulePath+"/internal/cli" {
		t.Fatalf("ExitCode is not a named int: %v", typ)
	}
	for _, constant := range []any{cli.ExitSuccess, cli.ExitWriteFailed, cli.ExitUsage} {
		if reflect.TypeOf(constant) != typ {
			t.Fatalf("constant has type %T, want %v", constant, typ)
		}
	}
}

func TestCLISourceRestrictions(t *testing.T) {
	// R-HFHC-NLE5: the import check above covers direct imports; check forbidden builtins too.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok && (id.Name == "print" || id.Name == "println") {
				t.Errorf("%s calls forbidden builtin %s", path, id.Name)
			}
			return true
		})
	}
}
