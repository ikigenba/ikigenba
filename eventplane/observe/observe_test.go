package observe_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const observePackage = "eventplane/observe"

func TestDependencyBoundary(t *testing.T) {
	metadata, err := exec.Command("go", "list", "-json", observePackage).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe package metadata: %v\n%s", err, metadata)
	}
	var pkg struct {
		ImportPath string
		Imports    []string
		Deps       []string
	}
	if err := json.Unmarshal(metadata, &pkg); err != nil {
		t.Fatalf("decode observe package metadata: %v", err)
	}
	if pkg.ImportPath != observePackage {
		t.Fatalf("query root = %q, want %q", pkg.ImportPath, observePackage)
	}
	if got, want := strings.Join(pkg.Imports, "\n"), "context\neventplane/routing\ntime"; got != want {
		t.Fatalf("direct imports:\n%s\nwant:\n%s", got, want)
	}
	var internal []string
	for _, dependency := range pkg.Deps {
		if strings.HasPrefix(dependency, "eventplane/") {
			internal = append(internal, dependency)
		}
	}
	if got, want := strings.Join(internal, "\n"), "eventplane/routing"; got != want {
		t.Fatalf("internal dependency set:\n%s\nwant:\n%s", got, want)
	}
}
