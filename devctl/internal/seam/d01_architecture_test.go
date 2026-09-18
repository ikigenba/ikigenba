package seam_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/ikigenba/ikigenba/devctl"

type sourceFile struct {
	path       string
	pkgPath    string
	file       *ast.File
	importPath map[string]string
}

func moduleSources(t *testing.T) []sourceFile {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate D01 architecture test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	var files []sourceFile
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			relDir, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			imports := make(map[string]string)
			for _, spec := range parsed.Imports {
				imported, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				name := filepath.Base(imported)
				if spec.Name != nil {
					name = spec.Name.Name
				}
				imports[name] = imported
			}
			files = append(files, sourceFile{
				path:       path,
				pkgPath:    modulePath + "/" + filepath.ToSlash(relDir),
				file:       parsed,
				importPath: imports,
			})
			return nil
		})
		if err != nil {
			t.Fatalf("read %s sources: %v", top, err)
		}
	}
	return files
}

// R-BU3V-ZE1N
func TestCommandsUseExplicitRootsAndProcessDirectories(t *testing.T) {
	filesystemCalls := map[string]bool{
		"Create": true, "DirFS": true, "Mkdir": true, "MkdirAll": true,
		"Open": true, "OpenFile": true, "OpenRoot": true, "ReadDir": true,
		"ReadFile": true, "Readlink": true, "Remove": true, "RemoveAll": true,
		"Rename": true, "Stat": true, "WriteFile": true,
	}
	for _, source := range moduleSources(t) {
		if strings.HasSuffix(source.path, "_test.go") {
			continue
		}
		ast.Inspect(source.file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.CompositeLit:
				if !isCmdType(value.Type, source) {
					return true
				}
				foundDir := false
				for _, element := range value.Elts {
					field, ok := element.(*ast.KeyValueExpr)
					if !ok {
						t.Errorf("%s: seam.Cmd must use keyed fields and explicitly name Dir", source.path)
						continue
					}
					name, ok := field.Key.(*ast.Ident)
					if ok && name.Name == "Dir" {
						foundDir = true
						if literal, ok := field.Value.(*ast.BasicLit); ok && literal.Kind == token.STRING && literal.Value == `""` {
							t.Errorf("%s: seam.Cmd Dir is explicitly empty", source.path)
						}
					}
				}
				if !foundDir {
					t.Errorf("%s: seam.Cmd does not explicitly supply Dir", source.path)
				}
			case *ast.CallExpr:
				selector, ok := value.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := selector.X.(*ast.Ident)
				if !ok || source.importPath[ident.Name] != "os" {
					return true
				}
				if selector.Sel.Name == "UserHomeDir" {
					t.Errorf("%s: command source discovers the developer home directory", source.path)
				}
				if !filesystemCalls[selector.Sel.Name] || len(value.Args) == 0 {
					return true
				}
				literal, ok := value.Args[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				path, err := strconv.Unquote(literal.Value)
				if err == nil && path != "" && !filepath.IsAbs(path) {
					t.Errorf("%s: os.%s uses relative literal path %q instead of a dependency root", source.path, selector.Sel.Name, path)
				}
			}
			return true
		})
	}
}

func isCmdType(expr ast.Expr, source sourceFile) bool {
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name == "Cmd" && source.pkgPath == modulePath+"/internal/seam"
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Cmd" {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && source.importPath[ident.Name] == modulePath+"/internal/seam"
}

// R-YHTO-8IAI
func TestCommandAndSharedHelperOwnership(t *testing.T) {
	requiredFunctions := map[string]map[string]bool{
		modulePath + "/internal/hostsetup": {
			"Latest": false, "InstallLatest": false, "Upgrade": false, "Version": false, "Configure": false,
		},
		modulePath + "/internal/spaceinit": {"Run": false},
		modulePath + "/internal/spaceapps": {"Run": false},
		modulePath + "/internal/remove":    {"Run": false},
		modulePath + "/internal/appref": {
			"ValidName": false, "ValidVersion": false, "VersionForTag": false, "ParseFile": false,
		},
	}
	commandPackages := map[string]bool{
		modulePath + "/internal/build":       true,
		modulePath + "/internal/deploy":      true,
		modulePath + "/internal/remove":      true,
		modulePath + "/internal/restore":     true,
		modulePath + "/internal/secrets":     true,
		modulePath + "/internal/spaceapps":   true,
		modulePath + "/internal/spacecreate": true,
		modulePath + "/internal/spaceinit":   true,
	}
	helperPackages := map[string]bool{
		modulePath + "/internal/appref":    true,
		modulePath + "/internal/hostsetup": true,
	}
	spaceAppsGrammar := map[string]bool{"restart": false, "logs": false}

	for _, source := range moduleSources(t) {
		if strings.HasSuffix(source.path, "_test.go") {
			continue
		}
		if helperPackages[source.pkgPath] {
			for _, imported := range source.importPath {
				if imported == modulePath+"/internal/cli" || commandPackages[imported] {
					t.Errorf("%s: shared helper imports command package %q", source.path, imported)
				}
			}
		}
		for _, declaration := range source.file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			if functions := requiredFunctions[source.pkgPath]; functions != nil {
				if _, required := functions[function.Name.Name]; required {
					functions[function.Name.Name] = true
				}
			}
		}
		if source.pkgPath == modulePath+"/internal/spaceapps" {
			ast.Inspect(source.file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err == nil {
					if _, required := spaceAppsGrammar[value]; required {
						spaceAppsGrammar[value] = true
					}
				}
				return true
			})
		}
	}

	for pkg, functions := range requiredFunctions {
		for function, found := range functions {
			if !found {
				t.Errorf("%s does not own required function %s", pkg, function)
			}
		}
	}
	for command, found := range spaceAppsGrammar {
		if !found {
			t.Errorf("internal/spaceapps does not own %q command grammar", command)
		}
	}
}
