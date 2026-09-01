package options_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// R-E98W-U3WF
func TestInternalPackageImportBoundaries(t *testing.T) {
	t.Parallel()

	const (
		modulePrefix = "github.com/ikigenba/ikigenba/oauth/"
		oauthImport  = modulePrefix + "internal/oauth"
	)

	cases := []struct {
		name          string
		directory     string
		allowedImport string
	}{
		{name: "oauth", directory: "../oauth"},
		{name: "callback", directory: "../callback"},
		{name: "browser", directory: "../browser"},
		{name: "options", directory: ".", allowedImport: oauthImport},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			entries, err := os.ReadDir(testCase.directory)
			if err != nil {
				t.Fatalf("read source directory %q: %v", testCase.directory, err)
			}

			productionFiles := 0
			for _, entry := range entries {
				if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
					continue
				}
				productionFiles++

				path := filepath.Join(testCase.directory, entry.Name())
				parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					t.Fatalf("read or parse source file %q: %v", path, err)
				}

				for _, spec := range parsed.Imports {
					importPath, err := strconv.Unquote(spec.Path.Value)
					if err != nil {
						t.Fatalf("decode import in %q: %v", path, err)
					}
					if strings.HasPrefix(importPath, modulePrefix) && importPath != testCase.allowedImport {
						t.Errorf("source file %q imports forbidden module-local package %q", path, importPath)
					}
				}
			}

			if productionFiles == 0 {
				t.Errorf("source directory %q contains no production Go files", testCase.directory)
			}
		})
	}
}
