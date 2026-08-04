package observe_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const observePackage = "eventplane/observe"

func TestDependencyBoundary(t *testing.T) {
	metadata, err := exec.Command("go", "list", "-deps", "-json", observePackage).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency metadata: %v\n%s", err, metadata)
	}
	type listedPackage struct {
		ImportPath string
		Imports    []string
		DepOnly    bool
		Match      []string
	}
	decoder := json.NewDecoder(strings.NewReader(string(metadata)))
	var dependencies []string
	var root *listedPackage
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("decode observe dependency metadata: %v", err)
		}
		if pkg.DepOnly && strings.HasPrefix(pkg.ImportPath, "eventplane/") {
			dependencies = append(dependencies, pkg.ImportPath)
		}
		if pkg.ImportPath == observePackage {
			copy := pkg
			root = &copy
		}
	}
	if got, want := strings.Join(dependencies, "\n"), "eventplane/routing"; got != want {
		t.Fatalf("internal dependency set:\n%s\nwant:\n%s", got, want)
	}
	if root == nil || root.DepOnly || strings.Join(root.Match, " ") != observePackage {
		t.Fatalf("query root classification: %#v", root)
	}
	if got, want := strings.Join(root.Imports, "\n"), "context\neventplane/routing\ntime"; got != want {
		t.Fatalf("direct imports:\n%s\nwant:\n%s", got, want)
	}
}

func TestDependencyBoundaryCommandIncludesQueryRoot(t *testing.T) {
	output, err := exec.Command("go", "list", "-deps", observePackage).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency traversal: %v\n%s", err, output)
	}

	var internal []string
	for line := range strings.Lines(string(output)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "eventplane/") {
			internal = append(internal, line)
		}
	}

	// The default go list formatter emits both dependency-only packages and the
	// explicitly queried package. DepOnly metadata in TestDependencyBoundary is
	// therefore required to distinguish the actual dependency set from its root;
	// the equivalent shell gate is:
	//
	//	go list -deps -f '{{if .DepOnly}}{{.ImportPath}}{{end}}' eventplane/observe | grep '^eventplane/'
	if got, want := strings.Join(internal, "\n"), "eventplane/routing\neventplane/observe"; got != want {
		t.Fatalf("default dependency traversal (which includes its query root):\n%s\nwant:\n%s", got, want)
	}
}
