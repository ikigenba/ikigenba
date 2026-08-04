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

	plainOutput, err := exec.Command("go", "list", "-deps", observePackage).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency traversal: %v\n%s", err, plainOutput)
	}
	var traversal []string
	for _, packagePath := range strings.Fields(string(plainOutput)) {
		if strings.HasPrefix(packagePath, "eventplane/") {
			traversal = append(traversal, packagePath)
		}
	}
	// The literal done-bar command includes the explicitly queried root after
	// its dependencies. Keep that behavior visible so it cannot be mistaken
	// for an observe import.
	if got, want := strings.Join(traversal, "\n"), "eventplane/routing\n"+observePackage; got != want {
		t.Fatalf("internal traversal:\n%s\nwant:\n%s", got, want)
	}

	// DepOnly selects actual dependencies and excludes that query root.
	output, err := exec.Command(
		"go", "list", "-deps",
		"-f", `{{if .DepOnly}}{{.ImportPath}}{{end}}`,
		observePackage,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency set: %v\n%s", err, output)
	}
	var internal []string
	for _, dependency := range strings.Fields(string(output)) {
		if strings.HasPrefix(dependency, "eventplane/") {
			internal = append(internal, dependency)
		}
	}
	if got, want := strings.Join(internal, "\n"), "eventplane/routing"; got != want {
		t.Fatalf("internal dependency set:\n%s\nwant:\n%s", got, want)
	}
}
