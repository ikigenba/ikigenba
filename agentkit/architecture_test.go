package agentkit

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoWarningOrCategorySpecificErrorTypesAreExported(t *testing.T) {
	// R-2K5Z-AIWY
	// R-2V52-QGL7
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	exportedErrorTypes := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpecification := specification.(*ast.TypeSpec)
				name := typeSpecification.Name.Name
				if name == "Warning" {
					t.Fatal("exported Warning type exists; invalid configuration must fail loudly")
				}
				if ast.IsExported(name) && strings.HasSuffix(name, "Error") {
					exportedErrorTypes = append(exportedErrorTypes, name)
				}
			}
		}
	}
	if len(exportedErrorTypes) != 1 || exportedErrorTypes[0] != "Error" {
		t.Fatalf("exported error types = %v, want only Error", exportedErrorTypes)
	}
}

type listedPackage struct {
	ImportPath string
	Imports    []string
}

func TestDependencyDirection(t *testing.T) {
	// R-1WZW-0VTR
	command := exec.Command("go", "list", "-json", "./...")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	const module = "github.com/ikigenba/ikigenba/agentkit"
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		if pkg.ImportPath == module {
			for _, imported := range pkg.Imports {
				if strings.HasPrefix(imported, module+"/") {
					t.Fatalf("root Conversation package imports lower layer %q", imported)
				}
			}
			continue
		}
		if !strings.HasPrefix(pkg.ImportPath, module+"/") {
			continue
		}
		for _, imported := range pkg.Imports {
			if imported == module {
				t.Fatalf("lower package %q imports back up to Conversation", pkg.ImportPath)
			}
			if strings.Contains(imported, "/anthropic") || strings.Contains(imported, "/openai") || strings.Contains(imported, "/gemini") {
				t.Fatalf("lower package %q imports vendor package %q", pkg.ImportPath, imported)
			}
		}
	}
}
