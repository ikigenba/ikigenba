package cli_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type listedPackage struct {
	ImportPath string
	Imports    []string
}

// R-U3PG-P0SL
func TestLeafPackagesRespectModuleDependencyBoundaries(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	command := exec.Command("go", "list", "-json", "./internal/help", "./internal/session", "./internal/render", "./internal/options")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list leaf packages: %v", err)
	}

	decoder := json.NewDecoder(strings.NewReader(string(output)))
	packages := make(map[string]listedPackage)
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		packages[pkg.ImportPath] = pkg
	}

	const module = "github.com/ikigenba/ikigenba/agent-repl"
	allowed := map[string]map[string]bool{
		module + "/internal/help":    {},
		module + "/internal/session": {},
		module + "/internal/render":  {},
		module + "/internal/options": {module + "/internal/help": true},
	}
	for packagePath, permitted := range allowed {
		pkg, found := packages[packagePath]
		if !found {
			t.Errorf("go list did not return %s", packagePath)
			continue
		}
		for _, imported := range pkg.Imports {
			if strings.HasPrefix(imported, module+"/") && !permitted[imported] {
				t.Errorf("%s imports disallowed module package %s", packagePath, imported)
			}
		}
	}
}
