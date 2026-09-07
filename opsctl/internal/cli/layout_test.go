package cli_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestGoMod(t *testing.T) {
	// R-MTHR-5KL5
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
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "require" {
			t.Fatalf("go.mod contains a require entry:\n%s", text)
		}
	}
}

func TestImportGraph(t *testing.T) {
	// R-MZL9-2FAM
	const module = "github.com/ikigenba/ikigenba/opsctl"
	cmdImports := moduleImports(t, filepath.Join("..", "..", "cmd", "opsctl"), module)
	cliImports := moduleImports(t, ".", module)
	configImports := moduleImports(t, filepath.Join("..", "config"), module)

	wantCLI := module + "/internal/cli"
	if !containsString(cmdImports, wantCLI) {
		t.Errorf("cmd/opsctl does not import %s; got %v", wantCLI, cmdImports)
	}
	for _, path := range cmdImports {
		if path != wantCLI {
			t.Errorf("cmd/opsctl imports extra module package %s", path)
		}
	}

	wantConfig := module + "/internal/config"
	if !containsString(cliImports, wantConfig) {
		t.Errorf("internal/cli does not import %s; got %v", wantConfig, cliImports)
	}
	for _, path := range cliImports {
		if path != wantConfig {
			t.Errorf("internal/cli imports extra module package %s", path)
		}
	}

	if len(configImports) != 0 {
		t.Errorf("internal/config imports module packages %v, want none", configImports)
	}
}

func moduleImports(t *testing.T, dir, module string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	prefix := module + "/"
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
			if strings.HasPrefix(importPath, prefix) {
				seen[importPath] = true
			}
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

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
