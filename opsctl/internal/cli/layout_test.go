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
		"github.com/aws/aws-sdk-go-v2/service/s3",
		"github.com/aws/aws-sdk-go-v2/service/ssm",
	}
	got := mapKeys(directRequirements(text))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("direct requirements = %v, want %v", got, want)
	}
}

func TestImportGraph(t *testing.T) {
	// R-97IE-M8BF
	const module = "github.com/ikigenba/ikigenba/opsctl"
	moduleRoot := filepath.Join("..", "..")
	filesystem, err := os.OpenRoot(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := filesystem.Close(); err != nil {
			t.Error(err)
		}
	}()
	goMod, err := filesystem.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	dependencies := allRequirements(string(goMod))
	rules := map[string]map[string]bool{
		"cmd/opsctl":           importSet(module+"/internal/cli", module+"/internal/dns", module+"/internal/dns/route53", module+"/internal/cloud", module+"/internal/cloud/aws", module+"/internal/host"),
		"internal/cli":         importSet(module+"/internal/config", module+"/internal/dns", module+"/internal/host", module+"/internal/cloud", module+"/internal/apps", module+"/internal/nginx", module+"/internal/services", module+"/internal/cert", module+"/internal/backup"),
		"internal/config":      importSet(),
		"internal/dns":         importSet(module + "/internal/config"),
		"internal/dns/route53": importSet(module + "/internal/dns"),
		"internal/host":        importSet(),
		"internal/cloud":       importSet(),
		"internal/cloud/aws":   importSet(module + "/internal/cloud"),
		"internal/apps":        importSet(module+"/internal/config", module+"/internal/host", module+"/internal/cloud"),
		"internal/nginx":       importSet(module+"/internal/config", module+"/internal/host", module+"/internal/apps"),
		"internal/services":    importSet(module+"/internal/host", module+"/internal/apps"),
		"internal/cert":        importSet(module+"/internal/config", module+"/internal/host"),
		"internal/backup":      importSet(module+"/internal/config", module+"/internal/host", module+"/internal/cloud", module+"/internal/apps"),
	}
	packages := modulePackages(t, moduleRoot)
	for name, imports := range packages {
		rule := rules[name]
		for path := range imports {
			switch {
			case strings.HasPrefix(path, module+"/"):
				if !rule[path] {
					t.Errorf("%s imports forbidden module package %s", name, path)
				}
			case isAWSImport(path):
				if !isApprovedAWSImport(name, path, dependencies) {
					t.Errorf("%s imports unapproved AWS SDK package %s", name, path)
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

func allRequirements(goMod string) map[string]bool {
	requirements := map[string]bool{}
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
		if len(fields) >= 2 {
			requirements[fields[0]] = true
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

func isAWSImport(path string) bool {
	const sdk = "github.com/aws/aws-sdk-go-v2"
	return path == sdk || strings.HasPrefix(path, sdk+"/")
}

func isApprovedAWSImport(name, path string, dependencies map[string]bool) bool {
	const sdk = "github.com/aws/aws-sdk-go-v2"
	approvedByPackage := map[string]map[string]bool{
		"internal/dns/route53": {sdk: true, sdk + "/config": true, sdk + "/service/route53": true},
		"internal/cloud/aws":   {sdk: true, sdk + "/config": true, sdk + "/service/s3": true, sdk + "/service/ssm": true},
	}
	module := ""
	for dependency := range dependencies {
		if (path == dependency || strings.HasPrefix(path, dependency+"/")) && len(dependency) > len(module) {
			module = dependency
		}
	}
	return approvedByPackage[name][module]
}
