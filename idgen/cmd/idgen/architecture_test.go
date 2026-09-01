package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

const modulePath = "github.com/ikigenba/ikigenba/idgen"

type packageMetadata struct {
	ImportPath string
	Imports    []string
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate architecture test source")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func goOutput(t *testing.T, root string, args ...string) []byte {
	t.Helper()

	// The executable is fixed, and callers pass only test-owned Go subcommands.
	//nolint:gosec // Structured Go metadata is the behavior under test.
	command := exec.Command("go", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, output)
	}
	return output
}

func listPackage(t *testing.T, root, pattern string) packageMetadata {
	t.Helper()

	var metadata packageMetadata
	output := goOutput(t, root, "list", "-json", pattern)
	if err := json.Unmarshal(output, &metadata); err != nil {
		t.Fatalf("decode go list output for %s: %v", pattern, err)
	}
	return metadata
}

func requireDirectImport(t *testing.T, metadata packageMetadata, dependency string) {
	t.Helper()

	if !slices.Contains(metadata.Imports, dependency) {
		t.Errorf("%s direct imports = %v; want %s", metadata.ImportPath, metadata.Imports, dependency)
	}
}

func forbidDirectImport(t *testing.T, metadata packageMetadata, dependency string) {
	t.Helper()

	if slices.Contains(metadata.Imports, dependency) {
		t.Errorf("%s must not directly import %s", metadata.ImportPath, dependency)
	}
}

// R-UAKY-KVX6
func TestModuleImportGraphFlowsOneWay(t *testing.T) {
	root := moduleRoot(t)
	commandPackage := listPackage(t, root, "./cmd/idgen")
	cliPackage := listPackage(t, root, "./internal/cli")
	corePackage := listPackage(t, root, "./internal/idgen")

	cliImport := modulePath + "/internal/cli"
	coreImport := modulePath + "/internal/idgen"
	commandImport := modulePath + "/cmd/idgen"

	requireDirectImport(t, commandPackage, cliImport)
	requireDirectImport(t, cliPackage, coreImport)
	forbidDirectImport(t, corePackage, cliImport)
	forbidDirectImport(t, cliPackage, commandImport)
}

// R-UBSU-YNNV
func TestModuleRequiresOnlyStandardLibrary(t *testing.T) {
	root := moduleRoot(t)
	output := goOutput(t, root, "mod", "edit", "-json")
	definition := struct {
		Require []struct {
			Path string
		}
	}{}
	if err := json.Unmarshal(output, &definition); err != nil {
		t.Fatalf("decode go.mod metadata: %v", err)
	}

	if len(definition.Require) != 0 {
		paths := make([]string, len(definition.Require))
		for i, requirement := range definition.Require {
			paths[i] = requirement.Path
		}
		t.Errorf("go.mod requires non-standard-library modules: %v", paths)
	}
}
