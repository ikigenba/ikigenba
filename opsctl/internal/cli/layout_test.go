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
	// R-EL9M-CEGL
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
	want := []string{
		"github.com/aws/aws-sdk-go-v2",
		"github.com/aws/aws-sdk-go-v2/config",
		"github.com/aws/aws-sdk-go-v2/service/route53",
	}
	got := mapKeys(directRequirements(text))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("direct requirements = %v, want %v", got, want)
	}
}

func TestImportGraph(t *testing.T) {
	// R-5FBZ-KZIB
	const module = "github.com/ikigenba/ikigenba/opsctl"
	moduleRoot := filepath.Join("..", "..")
	rules := map[string]map[string]bool{
		"cmd/opsctl":           importSet(module+"/internal/cli", module+"/internal/dns", module+"/internal/dns/route53", module+"/internal/host"),
		"internal/cli":         importSet(module+"/internal/config", module+"/internal/dns", module+"/internal/host", module+"/internal/cloud", module+"/internal/apps", module+"/internal/nginx", module+"/internal/cert", module+"/internal/backup"),
		"internal/config":      importSet(),
		"internal/dns":         importSet(module + "/internal/config"),
		"internal/dns/route53": importSet(module + "/internal/dns"),
		"internal/host":        importSet(),
		"internal/cloud":       importSet(),
		"internal/apps":        importSet(module+"/internal/config", module+"/internal/host", module+"/internal/cloud"),
		"internal/nginx":       importSet(module+"/internal/config", module+"/internal/host", module+"/internal/apps"),
		"internal/cert":        importSet(module+"/internal/config", module+"/internal/host"),
		"internal/backup":      importSet(module+"/internal/config", module+"/internal/host", module+"/internal/cloud", module+"/internal/apps"),
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
		for path := range imports {
			switch {
			case strings.HasPrefix(path, module+"/"):
				if !rule[path] {
					t.Errorf("%s imports forbidden module package %s", name, path)
				}
			case isExternalImport(path):
				if name != "internal/dns/route53" || !isApprovedAWSImport(path) {
					t.Errorf("%s imports external package %s", name, path)
				}
			}
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

func isApprovedAWSImport(path string) bool {
	for _, approved := range []string{"github.com/aws/aws-sdk-go-v2/aws", "github.com/aws/aws-sdk-go-v2/config", "github.com/aws/aws-sdk-go-v2/service/route53"} {
		if path == approved || strings.HasPrefix(path, approved+"/") {
			return true
		}
	}
	return false
}
