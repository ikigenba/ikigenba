package observe_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const observePackage = "eventplane/observe"

func TestDependencyBoundary(t *testing.T) {
	// Keep the check grounded in the done-bar's literal traversal. The plain
	// output contains both the sole internal dependency and the explicitly
	// named query root; this guards against mistaking the latter for an import.
	plainOutput, err := exec.Command("go", "list", "-deps", observePackage).CombinedOutput()
	if err != nil {
		t.Fatalf("list plain observe dependency traversal: %v\n%s", err, plainOutput)
	}
	var internalTraversal []string
	for _, line := range strings.Split(strings.TrimSpace(string(plainOutput)), "\n") {
		if strings.HasPrefix(line, "eventplane/") {
			internalTraversal = append(internalTraversal, line)
		}
	}
	if got, want := strings.Join(internalTraversal, "\n"), "eventplane/routing\n"+observePackage; got != want {
		t.Fatalf("plain internal traversal:\n%s\nwant:\n%s", got, want)
	}

	// DepOnly and Match distinguish the dependency from that query root in
	// machine-readable output.
	output, err := exec.Command(
		"go", "list", "-deps",
		"-json",
		observePackage,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency set: %v\n%s", err, output)
	}

	type listedPackage struct {
		ImportPath string
		DepOnly    bool
		Match      []string
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	var dependencies []string
	var root *listedPackage
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("decode go list output: %v", err)
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
}
