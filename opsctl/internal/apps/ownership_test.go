package apps_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
)

// R-XMCC-E2QF
func TestAppsOwnsHostLocalModelConsumedAcrossOperations(t *testing.T) {
	t.Parallel()

	validateName := apps.ValidateName
	parseManifest := apps.ParseManifest
	discover := apps.Discover

	if err := validateName("ledger"); err != nil {
		t.Fatalf("ValidateName: %v", err)
	}
	manifestData := []byte("app = \"ledger\"\n[database]\nengine = \"sqlite\"\npath = \"state/ledger.db\"\n")
	wantManifest := apps.Manifest{
		App:     "ledger",
		Secrets: []string{},
		Env:     map[string]string{},
		Database: &apps.Database{
			Engine: "sqlite",
			Path:   "state/ledger.db",
		},
	}
	parsedManifest, err := parseManifest(manifestData)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if !reflect.DeepEqual(parsedManifest, wantManifest) {
		t.Fatalf("ParseManifest = %#v, want %#v", parsedManifest, wantManifest)
	}
	root := t.TempDir()
	writeManifest(t, root, "ledger", string(manifestData))
	services, err := discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(services) != 1 || services[0].Name != "ledger" || services[0].Manifest == nil || !reflect.DeepEqual(*services[0].Manifest, wantManifest) {
		t.Fatalf("Discover(%q) = %#v, want the host-local ledger manifest", root, services)
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate internal/apps test")
	}
	internalDirectory := filepath.Dir(filepath.Dir(currentFile))
	consumers := map[string][]string{
		"cli":    {"ValidateName"},
		"nginx":  {"Discover"},
		"backup": {"Discover"},
	}
	for packageName, calls := range consumers {
		t.Run(packageName, func(t *testing.T) {
			assertCallsApps(t, filepath.Join(internalDirectory, packageName), calls)
		})
	}
}

func assertCallsApps(t *testing.T, directory string, required []string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read consumer package: %v", err)
	}
	called := make(map[string]bool, len(required))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, entry.Name()), nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", entry.Name(), parseErr)
		}
		aliases := make(map[string]bool)
		for _, spec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("decode import in %s: %v", entry.Name(), unquoteErr)
			}
			if importPath != "github.com/ikigenba/ikigenba/opsctl/internal/apps" {
				continue
			}
			alias := "apps"
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			aliases[alias] = true
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			identifier, ok := selector.X.(*ast.Ident)
			if ok && aliases[identifier.Name] {
				called[selector.Sel.Name] = true
			}
			return true
		})
	}
	for _, name := range required {
		if !called[name] {
			t.Errorf("%s does not call apps.%s", filepath.Base(directory), name)
		}
	}
}
