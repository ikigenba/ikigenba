package cli_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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
	rules := []struct {
		name          string
		dir           string
		moduleImports []string
		allowExternal bool
	}{
		{"cmd/opsctl", filepath.Join("..", "..", "cmd", "opsctl"), []string{module + "/internal/cli", module + "/internal/dns", module + "/internal/dns/route53"}, false},
		{"internal/cli", ".", []string{module + "/internal/config", module + "/internal/dns"}, false},
		{"internal/dns", filepath.Join("..", "dns"), []string{module + "/internal/config"}, false},
		{"internal/dns/route53", filepath.Join("..", "dns", "route53"), []string{module + "/internal/dns"}, true},
		{"internal/config", filepath.Join("..", "config"), nil, false},
	}
	for _, rule := range rules {
		allowed := make(map[string]bool, len(rule.moduleImports))
		for _, path := range rule.moduleImports {
			allowed[path] = true
		}
		for _, path := range packageImports(t, rule.dir) {
			switch {
			case strings.HasPrefix(path, module+"/"):
				if !allowed[path] {
					t.Errorf("%s imports forbidden module package %s", rule.name, path)
				}
			case isExternalImport(path):
				if !rule.allowExternal {
					t.Errorf("%s imports external package %s", rule.name, path)
				}
			}
		}
	}
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

func packageImports(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	seen := map[string]bool{}
	productionFiles := 0
	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		productionFiles++
		path := filepath.Join(dir, entry.Name())
		parsed, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		for _, spec := range parsed.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("unquote import in %s: %v", path, unquoteErr)
			}
			seen[importPath] = true
		}
	}
	if productionFiles == 0 {
		t.Fatalf("no production Go files in %s", dir)
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	return out
}

func isExternalImport(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return strings.Contains(first, ".")
}
