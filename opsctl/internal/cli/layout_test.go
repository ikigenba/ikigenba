package cli_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestGoMod(t *testing.T) {
	// R-LV0Z-17L3
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "module github.com/ikigenba/ikigenba/opsctl\n") {
		t.Fatalf("go.mod module declaration:\n%s", text)
	}
	if !regexp.MustCompile(`(?m)^go \d+\.\d+`).MatchString(text) {
		t.Fatalf("go.mod has no Go version:\n%s", text)
	}
	want := map[string]string{
		"github.com/aws/aws-sdk-go-v2":                 "v1.46.0",
		"github.com/aws/aws-sdk-go-v2/config":          "v1.33.3",
		"github.com/aws/aws-sdk-go-v2/service/route53": "v1.69.0",
	}
	got := directRequirements(text)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("direct requirements = %v, want %v", got, want)
	}
}

func TestImportGraph(t *testing.T) {
	// R-LYOO-6IT6
	const module = "github.com/ikigenba/ikigenba/opsctl"
	moduleRoot := filepath.Join("..", "..")
	rules := map[string]struct {
		moduleImports map[string]bool
		allowExternal bool
	}{
		"cmd/opsctl":           {importSet(module+"/internal/cli", module+"/internal/dns", module+"/internal/dns/route53"), false},
		"internal/cli":         {importSet(module+"/internal/config", module+"/internal/dns"), false},
		"internal/dns":         {importSet(module + "/internal/config"), false},
		"internal/dns/route53": {importSet(module + "/internal/dns"), true},
		"internal/config":      {importSet(), false},
	}
	packages := modulePackages(t, moduleRoot)
	if len(packages) != len(rules) {
		t.Errorf("module packages = %v, want exactly %v", mapKeys(packages), mapKeys(rules))
	}
	for name, imports := range packages {
		rule, ok := rules[name]
		if !ok {
			t.Errorf("unexpected module package %s", name)
			continue
		}
		gotModuleImports := map[string]bool{}
		for path := range imports {
			switch {
			case strings.HasPrefix(path, module+"/"):
				gotModuleImports[path] = true
			case isExternalImport(path):
				if !rule.allowExternal {
					t.Errorf("%s imports external package %s", name, path)
				}
			}
		}
		if !reflect.DeepEqual(gotModuleImports, rule.moduleImports) {
			t.Errorf("%s module imports = %v, want exactly %v", name, mapKeys(gotModuleImports), mapKeys(rule.moduleImports))
		}
	}
	for name := range rules {
		if _, ok := packages[name]; !ok {
			t.Errorf("required module package %s is missing", name)
		}
	}
}

func importSet(paths ...string) map[string]bool {
	set := make(map[string]bool, len(paths))
	for _, path := range paths {
		set[path] = true
	}
	return set
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func directRequirements(goMod string) map[string]string {
	requirements := map[string]string{}
	inBlock := false
	for _, line := range strings.Split(goMod, "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case "require (":
			inBlock = true
			continue
		case ")":
			inBlock = false
			continue
		}
		if strings.HasPrefix(line, "require ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		} else if !inBlock {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && !strings.Contains(line, "// indirect") {
			requirements[fields[0]] = fields[1]
		}
	}
	return requirements
}

func modulePackages(t *testing.T, root string) map[string]map[string]bool {
	t.Helper()
	packages := map[string]map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == "vendor" || entry.Name() == "testdata" || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		relDir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relDir)
		if name == "." {
			name = ""
		}
		imports := packages[name]
		if imports == nil {
			imports = map[string]bool{}
			packages[name] = imports
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			imports[importPath] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan module packages: %v", err)
	}
	return packages
}

func isExternalImport(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return strings.Contains(first, ".")
}
