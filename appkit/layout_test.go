package appkit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// R-64Q2-SLQD
func TestModuleContract(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	var module, version string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "module":
			if len(fields) != 2 {
				t.Fatalf("invalid module directive %q", line)
			}
			module = fields[1]
		case "go":
			if len(fields) != 2 {
				t.Fatalf("invalid go directive %q", line)
			}
			version = fields[1]
		case "require":
			t.Fatal("go.mod must not contain a require directive")
		}
	}
	if module != "github.com/ikigenba/ikigenba/appkit" || version == "" {
		t.Fatalf("module = %q, Go version = %q", module, version)
	}
	err = filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Name() == "go.work" {
			t.Errorf("unexpected workspace file: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func goFiles(t *testing.T, includeTests bool) map[string]*ast.File {
	t.Helper()
	files := make(map[string]*ast.File)
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || (!includeTests && strings.HasSuffix(path, "_test.go")) {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		files[path] = file
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// R-Z0WH-YSGD
func TestSingleRootPackage(t *testing.T) {
	files := goFiles(t, true)
	productionCount := 0
	for path, file := range files {
		if strings.HasSuffix(path, "_test.go") {
			if file.Name.Name != "appkit" && file.Name.Name != "appkit_test" {
				t.Errorf("%s: test package %s must be appkit or appkit_test", path, file.Name.Name)
			}
			continue
		}
		productionCount++
		if filepath.Dir(path) != "." || file.Name.Name != "appkit" {
			t.Errorf("%s: package %s must be appkit at the module root", path, file.Name.Name)
		}
	}
	if productionCount == 0 {
		t.Fatal("no production Go files")
	}
}

// R-68DR-XWYG
func TestExportedSurface(t *testing.T) {
	var names []string
	for _, file := range goFiles(t, false) {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil && decl.Name.IsExported() {
					names = append(names, decl.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if spec.Name.IsExported() {
							names = append(names, spec.Name.Name)
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if name.IsExported() {
								names = append(names, name.Name)
							}
						}
					}
				}
			}
		}
	}
	slices.Sort(names)
	want := []string{"Banner", "Kit", "New", "Service", "Static", "StaticPrefix", "Templates", "User"}
	if !slices.Equal(names, want) {
		t.Fatalf("exported identifiers = %v; want %v", names, want)
	}
	kitType := reflect.TypeFor[Kit]()
	for i := range kitType.NumField() {
		if field := kitType.Field(i); field.IsExported() {
			t.Errorf("Kit exports field %s", field.Name)
		}
	}
	for _, typ := range []reflect.Type{kitType, reflect.PointerTo(kitType)} {
		for i := range typ.NumMethod() {
			if method := typ.Method(i); method.Name != "Banner" {
				t.Errorf("%v exports method %s", typ, method.Name)
			}
		}
	}
}
