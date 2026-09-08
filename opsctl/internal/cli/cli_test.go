package cli_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
)

func TestRunReturnsWithoutTerminating(t *testing.T) {
	// R-MUPN-JCBU
	assertNoOsExit(t, ".")

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"--help"}, strings.NewReader(""), &stdout, &stderr, cli.Deps{
		Root: t.TempDir(),
		EUID: 0,
	})
	if code != 0 {
		t.Fatalf("Run returned %d, want 0", code)
	}
	code = cli.Run([]string{"--help"}, strings.NewReader(""), &stdout, &stderr, cli.Deps{
		Root: t.TempDir(),
		EUID: 0,
	})
	if code != 0 {
		t.Fatalf("second Run returned %d, want 0", code)
	}
}

func TestDepsFields(t *testing.T) {
	// R-MVXJ-X42J
	typ := reflect.TypeOf(cli.Deps{})
	if typ.Kind() != reflect.Struct {
		t.Fatalf("Deps is %s, want struct", typ.Kind())
	}
	want := map[string]reflect.Kind{
		"Root": reflect.String,
		"EUID": reflect.Int,
	}
	if typ.NumField() != len(want) {
		t.Fatalf("Deps has %d fields, want %d", typ.NumField(), len(want))
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		kind, ok := want[field.Name]
		if !ok {
			t.Errorf("unexpected Deps field %s %s", field.Name, field.Type)
			continue
		}
		if field.Type.Kind() != kind {
			t.Errorf("Deps.%s has type %s, want %s", field.Name, field.Type, kind)
		}
	}
}

func TestHostPathsResolveUnderRoot(t *testing.T) {
	// R-MYDC-ONJX
	root := t.TempDir()
	deps := cli.Deps{Root: root, EUID: 0}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"config", "set", "dns.zones=ikigenba.dev"}, strings.NewReader(""), &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("config set exit %d, stderr %q", code, stderr.String())
	}
	if stdout.String() != "" {
		t.Errorf("config set stdout = %q, want empty", stdout.String())
	}

	wantFile := filepath.Join(root, "etc", "ikigenba", "config.json")
	wantLock := filepath.Join(root, "etc", "ikigenba", "config.lock")
	if _, err := os.Stat(wantFile); err != nil {
		t.Fatalf("config file missing under Root at %s: %v", wantFile, err)
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			t.Errorf("file %s is not under Root %s: %v", path, root, relErr)
			return nil
		}
		if strings.HasPrefix(rel, "..") {
			t.Errorf("file %s escaped Root %s", path, root)
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{wantFile: true, wantLock: true}
	if len(files) != len(want) {
		t.Errorf("files under Root = %v, want %s and %s", files, wantFile, wantLock)
	}
	for _, path := range files {
		if !want[path] {
			t.Errorf("unexpected file under Root: %s", path)
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"config", "get", "dns.zones"}, strings.NewReader(""), &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("config get exit %d, stderr %q", code, stderr.String())
	}
	if got, want := stdout.String(), "ikigenba.dev\n"; got != want {
		t.Errorf("config get stdout = %q, want %q", got, want)
	}
}

func assertNoOsExit(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		parsed, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if ok && ident.Name == "os" && sel.Sel.Name == "Exit" {
				t.Errorf("%s calls os.Exit", path)
			}
			return true
		})
	}
}
