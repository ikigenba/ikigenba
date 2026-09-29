package apps_test

import (
	"bytes"
	"embed"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

//go:embed *.go
var appsSources embed.FS

// R-8XR7-K2DV
func TestIconAndServicesConstants(t *testing.T) {
	const icon, path, environment = apps.IconPath, apps.ServicesPath, apps.ServicesEnv
	if icon != "share/icon.svg" || path != "/var/lib/ikigenba/services.json" || environment != "IKIGENBA_SERVICES" {
		t.Fatalf("icon and services constants: %q, %q, %q", icon, path, environment)
	}
}

// R-8YZ3-XU4K
func TestIconSentinelErrors(t *testing.T) {
	if apps.ErrIconNotSVG.Error() != "share/icon.svg is not an SVG image" {
		t.Errorf("ErrIconNotSVG: %q", apps.ErrIconNotSVG)
	}
	if apps.ErrIconTooLarge.Error() != "share/icon.svg is larger than 64 KiB" {
		t.Errorf("ErrIconTooLarge: %q", apps.ErrIconTooLarge)
	}
}

// R-9070-BLV9
var _ func([]byte) error = apps.CheckIcon

// R-91EW-PDLY
func TestCheckIconSizeAndSentinelResults(t *testing.T) {
	maximum := append([]byte("<svg>"), bytes.Repeat([]byte{' '}, 65536-len("<svg></svg>"))...)
	maximum = append(maximum, []byte("</svg>")...)
	if err := apps.CheckIcon(maximum); err != nil {
		t.Fatalf("65536-byte SVG: %v", err)
	}
	if err := apps.CheckIcon(append(maximum, ' ')); !reflect.ValueOf(err).Equal(reflect.ValueOf(apps.ErrIconTooLarge)) {
		t.Errorf("65537-byte SVG: %v", err)
	}
	if err := apps.CheckIcon(bytes.Repeat([]byte{'x'}, 65537)); !reflect.ValueOf(err).Equal(reflect.ValueOf(apps.ErrIconTooLarge)) {
		t.Errorf("oversize non-SVG: %v", err)
	}
	if err := apps.CheckIcon([]byte("not SVG")); !reflect.ValueOf(err).Equal(reflect.ValueOf(apps.ErrIconNotSVG)) {
		t.Errorf("non-SVG: %v", err)
	}
}

// R-M1NV-I2WL
func TestCheckIconSVGDefinition(t *testing.T) {
	valid := []string{
		"<svg/>",
		"\xef\xbb\xbf<svg/>",
		" \t\r\n<?xml version=\"1.0\"?>\n<!-- before --><svg><g/></svg><!-- after -->\n",
		"<!DOCTYPE svg><a:svg xmlns:a=\"urn:example\"/>",
		"<svg xmlns:a=\"urn:a\" xmlns:b=\"urn:b\" a:key=\"1\" b:key=\"2\"/>",
	}
	for _, data := range valid {
		if err := apps.CheckIcon([]byte(data)); err != nil {
			t.Errorf("valid SVG %q: %v", data, err)
		}
	}
	invalid := []string{
		"", "plain text", "\x89PNG\r\n", "\xff<svg/>",
		"<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><svg/>",
		"<svg><g></svg>", "<svg>", "<svg>&undefined;</svg>",
		"<svg x=\"1\" x=\"2\"/>",
		"<svg xmlns:a=\"urn:one\" xmlns:b=\"urn:one\" a:x=\"1\" b:x=\"2\"/>",
		"<html/>", "<svg/><svg/>", "<svg/>after", "\xef\xbb\xbf\xef\xbb\xbf<svg/>",
		"\u00a0<svg/>",
	}
	for _, data := range invalid {
		if err := apps.CheckIcon([]byte(data)); !errors.Is(err, apps.ErrIconNotSVG) {
			t.Errorf("invalid SVG %q: %v", data, err)
		}
	}
}

// R-93UP-GX3C
func TestCheckIconDependsOnlyOnInput(t *testing.T) {
	entries, err := appsSources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	type sourceFunction struct {
		declaration *ast.FuncDecl
		imports     map[string]string
	}
	functions := make(map[string]sourceFunction)
	globals := make(map[string]bool)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, err := appsSources.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), content, 0)
		if err != nil {
			t.Fatal(err)
		}
		imports := make(map[string]string)
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			alias := path.Base(importPath)
			if imported.Name != nil {
				alias = imported.Name.Name
			}
			imports[alias] = importPath
		}
		for _, decl := range file.Decls {
			switch declaration := decl.(type) {
			case *ast.GenDecl:
				if declaration.Tok != token.VAR {
					continue
				}
				for _, spec := range declaration.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						if name.Name != "_" {
							globals[name.Name] = true
						}
					}
				}
			case *ast.FuncDecl:
				if declaration.Recv == nil {
					functions[declaration.Name.Name] = sourceFunction{declaration, imports}
				}
			}
		}
	}
	if _, ok := functions["CheckIcon"]; !ok {
		t.Fatal("CheckIcon declaration not found")
	}
	visited := make(map[string]bool)
	var inspectFunction func(string)
	inspectFunction = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		function := functions[name]
		selectorNames := make(map[*ast.Ident]bool)
		ast.Inspect(function.declaration.Body, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok {
				selectorNames[selector.Sel] = true
				if receiver, ok := selector.X.(*ast.Ident); ok {
					importPath := function.imports[receiver.Name]
					if isExternalEffectPackage(importPath) {
						t.Errorf("%s references effectful package %s", name, importPath)
					}
				}
			}
			return true
		})
		ast.Inspect(function.declaration.Body, func(node ast.Node) bool {
			switch expression := node.(type) {
			case *ast.Ident:
				if selectorNames[expression] || expression.Name == "ErrIconNotSVG" || expression.Name == "ErrIconTooLarge" {
					break
				}
				if globals[expression.Name] {
					t.Errorf("%s accesses mutable package global %s", name, expression.Name)
				}
			case *ast.CallExpr:
				if called, ok := expression.Fun.(*ast.Ident); ok {
					if _, exists := functions[called.Name]; exists {
						inspectFunction(called.Name)
					} else if called.Name == "print" || called.Name == "println" {
						t.Errorf("%s writes process output", name)
					}
				}
			}
			return true
		})
	}
	inspectFunction("CheckIcon")
}

func isExternalEffectPackage(importPath string) bool {
	for _, prefix := range []string{"os", "net", "time", "syscall", "runtime", "unsafe", "crypto/rand", "math/rand"} {
		if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
			return true
		}
	}
	return false
}
