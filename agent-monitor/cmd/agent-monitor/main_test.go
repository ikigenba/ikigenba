package main_test

import (
	"bytes"
	"errors"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-monitor")
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("find go: %v", err)
	}
	cmd := &exec.Cmd{Path: goPath, Args: []string{"go", "build", "-o", path, "./cmd/agent-monitor"}}
	cmd.Dir = "../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v: %s", err, output)
	}
	return path
}

func TestMainWiring(t *testing.T) {
	// R-2MHM-3BBW: main forwards arguments and real streams, then exits with Run's code.
	path := buildBinary(t)
	cmd := &exec.Cmd{Path: path, Args: []string{path, "mystery"}}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !asExitError(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("exit = %v, want 2", err)
	}
	if stdout.Len() != 0 || stderr.String() != "agent-monitor: unknown command 'mystery'\n\nsee 'agent-monitor --help' for usage\n" {
		t.Fatalf("streams: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

}

func TestMainSourceWiring(t *testing.T) {
	// R-2MHM-3BBW: exact main body permits one Run call and one Exit call only.
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 2 {
		t.Fatalf("main.go has %d declarations, want imports and main only", len(file.Decls))
	}
	imports, ok := file.Decls[0].(*ast.GenDecl)
	if !ok || imports.Tok != token.IMPORT || len(file.Imports) != 2 {
		t.Fatalf("main.go imports = %#v", file.Imports)
	}
	wantImports := map[string]bool{"os": true, "github.com/ikigenba/ikigenba/agent-monitor/internal/cli": true}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !wantImports[path] || spec.Name != nil {
			t.Fatalf("unexpected main import %#v: %v", spec, err)
		}
		delete(wantImports, path)
	}
	if len(wantImports) != 0 {
		t.Fatalf("missing main imports: %v", wantImports)
	}
	main, ok := file.Decls[1].(*ast.FuncDecl)
	if !ok || main.Name.Name != "main" || main.Recv != nil || main.Type.Params.NumFields() != 0 || main.Type.Results != nil || len(main.Body.List) != 2 {
		t.Fatalf("main must have exactly the Run and Exit statements")
	}
	want := []string{
		"code := cli.Run(os.Args[1:], os.Stdout, os.Stderr)",
		"os.Exit(int(code))",
	}
	for i, statement := range main.Body.List {
		var actual bytes.Buffer
		if err := format.Node(&actual, token.NewFileSet(), statement); err != nil {
			t.Fatal(err)
		}
		if actual.String() != want[i] {
			t.Errorf("main statement %d = %q, want %q", i, actual.String(), want[i])
		}
	}
}

func TestBareBinary(t *testing.T) {
	// R-2KC5-7UIW: A bare built binary greets on stdout and succeeds.
	path := buildBinary(t)
	cmd := &exec.Cmd{Path: path, Args: []string{path}}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("bare exit: %v", err)
	}
	if stdout.String() != "hello, world\n" || stderr.Len() != 0 {
		t.Fatalf("streams: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestFullDeviceWriteFailure(t *testing.T) {
	// R-2LK1-LM9L: A real stdout write failure reaches stderr and the process exit.
	output, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := output.Close(); err != nil {
			t.Error(err)
		}
	})

	path := buildBinary(t)
	cmd := &exec.Cmd{Path: path, Args: []string{path}}
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = output, &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if !asExitError(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("exit = %v, want 1", err)
	}
	line := stderr.String()
	if !strings.HasPrefix(line, "agent-monitor: write error: ") || !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Fatalf("write failure diagnostic = %q", line)
	}
}

func asExitError(err error, target **exec.ExitError) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	ok := errors.As(err, &exitErr)
	if ok {
		*target = exitErr
	}
	return ok
}
