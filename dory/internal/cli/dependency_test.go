package cli_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const internalImportPrefix = "github.com/ikigenba/ikigenba/dory/internal/"

type sourceImport struct {
	file string
	path string
}

// R-HF1L-0NWH
func TestInternalPackagesRespectDependencyBoundary(t *testing.T) {
	allowedImports := map[string]map[string]bool{
		"options": {},
		"model":   {},
		"store":   {},
		"render":  {},
		"agent": {
			internalImportPrefix + "model": true,
			internalImportPrefix + "store": true,
		},
	}

	for packageName, allowed := range allowedImports {
		for _, imported := range productionInternalImports(t, packageName) {
			if !allowed[imported.path] {
				t.Errorf("internal/%s production file %s imports forbidden module package %q", packageName, imported.file, imported.path)
			}
		}
	}
}

func productionInternalImports(t *testing.T, packageName string) []sourceImport {
	t.Helper()

	packageDir := filepath.Join("..", packageName)
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("read governed package directory internal/%s: %v", packageName, err)
	}

	var imports []sourceImport
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		filename := filepath.Join(packageDir, entry.Name())
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parse production Go file internal/%s/%s: %v", packageName, entry.Name(), parseErr)
		}
		for _, spec := range parsed.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("unquote import in internal/%s/%s: %v", packageName, entry.Name(), unquoteErr)
			}
			if strings.HasPrefix(importPath, internalImportPrefix) {
				imports = append(imports, sourceImport{file: entry.Name(), path: importPath})
			}
		}
	}

	return imports
}
