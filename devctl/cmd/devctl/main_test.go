package main

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantTopLevelUsage = `Usage: devctl [options] <command> [arguments]

Manage the ikigenba platform from the developer's machine. Never run as root.

Commands:
  version   print the version
  space     list, create, destroy, stop, start, initialise, and inspect spaces
  secrets   push and list an app's secrets for a space
  build     build one app into its deployable file
  deploy    put a built app file on a space
  remove    take an app off a space
  restore   put a space's app back from its backups

Options:
  --help              print this help
  --version           print the version
  --account <name>    AWS shared-config profile to act in

Exit codes:
  0  success
  1  the operation failed
  2  usage error, or a preflight check failed
  3  refused: devctl must not run as root

Run 'devctl <command> --help' for details on a command.
`

func TestMainDeclaresAndWiresEveryDependency(t *testing.T) {
	// R-A782-RJCD R-BWJO-QXJ1
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	wantFields := make([]string, reflect.TypeFor[seam.Deps]().NumField())
	for i := range wantFields {
		wantFields[i] = reflect.TypeFor[seam.Deps]().Field(i).Name
	}
	sort.Strings(wantFields)

	var literal *ast.CompositeLit
	ast.Inspect(file, func(node ast.Node) bool {
		candidate, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		selector, ok := candidate.Type.(*ast.SelectorExpr)
		if ok && expression(fset, selector) == "seam.Deps" {
			if literal != nil {
				t.Fatal("main.go contains more than one seam.Deps composite literal")
			}
			literal = candidate
		}
		return true
	})
	if literal == nil {
		t.Fatal("main.go contains no seam.Deps composite literal")
	}

	gotFields := make([]string, 0, len(literal.Elts))
	gotValues := make(map[string]string, len(literal.Elts))
	for _, element := range literal.Elts {
		entry, ok := element.(*ast.KeyValueExpr)
		if !ok {
			t.Fatal("seam.Deps literal contains an unnamed field")
		}
		name := expression(fset, entry.Key)
		gotFields = append(gotFields, name)
		gotValues[name] = expression(fset, entry.Value)
	}
	sort.Strings(gotFields)
	if !reflect.DeepEqual(gotFields, wantFields) {
		t.Fatalf("seam.Deps fields = %v, want %v", gotFields, wantFields)
	}

	wantValues := map[string]string{
		"Dir": "dir", "EUID": "os.Geteuid()", "Getenv": "os.Getenv",
		"Cloud": "awssdk.Open", "Exec": "seam.Exec", "Stream": "seam.Stream",
		"Now": "time.Now", "After": "time.After",
	}
	if !reflect.DeepEqual(gotValues, wantValues) {
		t.Fatalf("seam.Deps wiring = %#v, want %#v", gotValues, wantValues)
	}

	wantCalls := map[string]bool{
		"signal.NotifyContext(context.Background(), os.Interrupt)":        true,
		"cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, deps)": true,
		"cancel()":      true,
		"os.Exit(code)": true,
	}
	codeAssignedFromRun := false
	var runPos, cancelPos, exitPos token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok {
			rendered := expression(fset, call)
			delete(wantCalls, rendered)
			switch rendered {
			case "cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, deps)":
				runPos = call.Pos()
			case "cancel()":
				cancelPos = call.Pos()
			case "os.Exit(code)":
				exitPos = call.Pos()
			}
		}
		assignment, ok := node.(*ast.AssignStmt)
		if ok && len(assignment.Lhs) == 1 && len(assignment.Rhs) == 1 &&
			expression(fset, assignment.Lhs[0]) == "code" &&
			expression(fset, assignment.Rhs[0]) == "cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, deps)" {
			codeAssignedFromRun = true
		}
		return true
	})
	for missing := range wantCalls {
		t.Errorf("main.go does not contain call %q", missing)
	}
	if !codeAssignedFromRun {
		t.Error("main.go does not pass cli.Run's result to os.Exit through code")
	}
	if runPos == token.NoPos || runPos >= cancelPos || cancelPos >= exitPos {
		t.Error("main.go must call cli.Run, cancel its interrupt context, then call os.Exit")
	}
}

func TestBuiltBinaryHelp(t *testing.T) {
	// R-F59F-A9GW
	if os.Geteuid() == 0 {
		t.Fatal("test must run as a non-root user")
	}
	binary := filepath.Join(t.TempDir(), "devctl")
	moduleDir := filepath.Clean(filepath.Join("..", ".."))
	build, err := seam.Exec(t.Context(), seam.Cmd{
		Path: "go", Args: []string{"build", "-o", binary, "./cmd/devctl"}, Dir: moduleDir,
	})
	if err != nil {
		t.Fatalf("start go build: %v", err)
	}
	if build.ExitCode != 0 {
		t.Fatalf("go build exited %d: %s", build.ExitCode, build.Stderr)
	}

	result, err := seam.Exec(t.Context(), seam.Cmd{Path: binary, Args: []string{"--help"}, Dir: moduleDir})
	if err != nil {
		t.Fatalf("start devctl: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
	if got := string(result.Stdout); got != wantTopLevelUsage {
		t.Errorf("stdout = %q, want %q", got, wantTopLevelUsage)
	}
	if len(result.Stderr) != 0 {
		t.Errorf("stderr = %q, want empty", result.Stderr)
	}
}

func expression(fset *token.FileSet, expr ast.Expr) string {
	var rendered strings.Builder
	if err := format.Node(&rendered, fset, expr); err != nil {
		panic(err)
	}
	return rendered.String()
}
