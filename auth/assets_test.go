package auth

import (
	"embed"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestEmbeddedAssetsDeclaration(t *testing.T) {
	// R-1YHM-2HK1
	if reflect.TypeFor[embed.FS]() != reflect.TypeOf(Assets) {
		t.Fatal("Assets is not embed.FS")
	}
	// R-20XE-U11F
	info, err := fs.Stat(Assets, "assets/theme.css")
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("theme.css: %v, %v", info, err)
	}
	// R-UC3D-XIHZ
	root := sourceRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, entry.Name()), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if ast.IsExported(d.Name.Name) {
					t.Errorf("unexpected root export %s", d.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch v := spec.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(v.Name.Name) {
							t.Errorf("unexpected root type %s", v.Name.Name)
						}
					case *ast.ValueSpec:
						for _, name := range v.Names {
							if ast.IsExported(name.Name) && name.Name != "Assets" {
								t.Errorf("unexpected root export %s", name.Name)
							}
						}
					}
				}
			}
		}
	}
	// Its embedded copy includes exactly the directly maintained files and bytes.
	disk, err := os.ReadDir(filepath.Join(root, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := Assets.ReadDir("assets")
	if err != nil {
		t.Fatal(err)
	}
	if len(disk) != len(embedded) {
		t.Fatalf("embedded entries=%d, disk=%d", len(embedded), len(disk))
	}
	diskRoot, err := os.OpenRoot(filepath.Join(root, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = diskRoot.Close() }()
	for _, entry := range disk {
		if entry.IsDir() {
			continue
		}
		want, err := diskRoot.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		got, err := Assets.ReadFile("assets/" + entry.Name())
		if err != nil || string(got) != string(want) {
			t.Fatalf("embedded copy %s: %v", entry.Name(), err)
		}
	}
}

func sourceRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Dir(file)
}

func TestPackageLayoutAndImportDirection(t *testing.T) {
	// R-1W1T-AY2N R-1TM0-JEL9
	const module = "github.com/ikigenba/ikigenba/auth"
	allowed := map[string]map[string]bool{
		".":                {},
		"cmd/auth":         {"internal/cli": true},
		"internal/cli":     {"internal/server": true, "internal/store": true, "internal/google": true, "internal/idcodec": true, "internal/version": true},
		"internal/server":  {".": true, "internal/store": true, "internal/google": true, "internal/idcodec": true, "internal/version": true},
		"internal/store":   {"internal/idcodec": true, "internal/version": true},
		"internal/google":  {"internal/idcodec": true, "internal/version": true},
		"internal/idcodec": {},
		"internal/version": {},
	}
	root := sourceRoot(t)
	seen := map[string]bool{}
	cliImport := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		permits, ok := allowed[rel]
		if !ok {
			t.Errorf("unexpected Go directory %s", rel)
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		seen[rel] = true
		expected := filepath.Base(rel)
		if rel == "." {
			expected = "auth"
		}
		if rel == "cmd/auth" {
			expected = "main"
		}
		if f.Name.Name != expected {
			t.Errorf("%s package %s, want %s", rel, f.Name.Name, expected)
		}
		for _, imp := range f.Imports {
			value, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if value != module && !strings.HasPrefix(value, module+"/") {
				continue
			}
			target := strings.TrimPrefix(value, module+"/")
			if value == module {
				target = "."
			}
			if !permits[target] {
				t.Errorf("forbidden import %s -> %s", rel, target)
			}
			if rel == "cmd/auth" && target == "internal/cli" {
				cliImport = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != len(allowed) {
		t.Fatalf("packages=%v", seen)
	}
	if !cliImport {
		t.Fatal("main does not import cli")
	}
}
