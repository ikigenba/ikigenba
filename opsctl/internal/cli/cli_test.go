package cli_test

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/cli"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
)

var _ func([]string, io.Reader, io.Writer, io.Writer, cli.Deps) int = cli.Run

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
	// R-E9DW-L66P
	typ := reflect.TypeOf(cli.Deps{})
	if typ.Kind() != reflect.Struct {
		t.Fatalf("Deps is %s, want struct", typ.Kind())
	}
	want := map[string]reflect.Type{
		"Root":       reflect.TypeFor[string](),
		"EUID":       reflect.TypeFor[int](),
		"Getenv":     reflect.TypeFor[func(string) string](),
		"DNS":        reflect.TypeFor[dns.Env](),
		"LookPath":   reflect.TypeFor[func(string) (string, error)](),
		"LookupHost": reflect.TypeFor[func(context.Context, string) ([]string, error)](),
	}
	if typ.NumField() != len(want) {
		t.Fatalf("Deps has %d fields, want %d", typ.NumField(), len(want))
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fieldType, ok := want[field.Name]
		if !ok {
			t.Errorf("unexpected Deps field %s %s", field.Name, field.Type)
			continue
		}
		if field.Type != fieldType {
			t.Errorf("Deps.%s has type %s, want %s", field.Name, field.Type, fieldType)
		}
	}
}

func TestHostPathsResolveUnderRoot(t *testing.T) {
	// R-MYDC-ONJX
	sandbox := t.TempDir()
	root := filepath.Join(sandbox, "root")
	outside := filepath.Join(sandbox, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outside, "marker")
	if err := os.WriteFile(marker, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := filesystemStateOutside(t, sandbox, root)
	t.Chdir(sandbox)
	t.Setenv("HOME", outside)
	t.Setenv("TMPDIR", outside)
	t.Setenv("XDG_CONFIG_HOME", outside)
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
	if after := filesystemStateOutside(t, sandbox, root); !reflect.DeepEqual(after, before) {
		t.Errorf("filesystem outside Deps.Root changed:\nbefore: %v\nafter:  %v", before, after)
	}
}

type filesystemEntry struct {
	Mode           os.FileMode
	ModificationNS int64
	Content        string
}

func filesystemStateOutside(t *testing.T, sandbox, root string) map[string]filesystemEntry {
	t.Helper()
	state := map[string]filesystemEntry{}
	err := filepath.WalkDir(sandbox, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(sandbox, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := filesystemEntry{Mode: info.Mode(), ModificationNS: info.ModTime().UnixNano()}
		if info.Mode().IsRegular() {
			data, err := fs.ReadFile(os.DirFS(sandbox), filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			entry.Content = string(data)
		}
		state[rel] = entry
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
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
		osNames := map[string]bool{}
		for _, spec := range parsed.Imports {
			if spec.Path.Value != `"os"` {
				continue
			}
			switch {
			case spec.Name == nil:
				osNames["os"] = true
			case spec.Name.Name == ".":
				t.Errorf("%s dot-imports os, so uses of os.Exit cannot be excluded", path)
			case spec.Name.Name != "_":
				osNames[spec.Name.Name] = true
			}
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if ok && osNames[ident.Name] && sel.Sel.Name == "Exit" {
				t.Errorf("%s references os.Exit", path)
			}
			return true
		})
	}
}
