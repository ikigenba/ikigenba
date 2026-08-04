package observe_test

import (
	"os/exec"
	"strings"
	"testing"
)

const observePackage = "eventplane/observe"

func internalPackages(output []byte) string {
	var paths []string
	for _, path := range strings.Fields(string(output)) {
		if strings.HasPrefix(path, "eventplane/") {
			paths = append(paths, path)
		}
	}
	return strings.Join(paths, "\n")
}

func TestDependencyBoundary(t *testing.T) {
	// DepOnly distinguishes dependencies from the package named on the command
	// line. Keep the check on the same -deps traversal as the done-bar command,
	// but do not misclassify the query root as one of its own dependencies.
	output, err := exec.Command(
		"go", "list", "-deps",
		"-f", "{{if .DepOnly}}{{.ImportPath}}{{end}}",
		observePackage,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency set: %v\n%s", err, output)
	}

	if got, want := internalPackages(output), "eventplane/routing"; got != want {
		t.Fatalf("internal dependency set:\n%s\nwant:\n%s", got, want)
	}
}

func TestDependencyTraversalIncludesQueryRoot(t *testing.T) {
	// Exercise the done-bar command verbatim. The final package is the query
	// root, not one of its own dependencies: `go list -deps` emits both the
	// dependencies and every package explicitly named on its command line.
	output, err := exec.Command(
		"go", "list", "-deps",
		"-f", "{{.ImportPath}} {{.DepOnly}}",
		observePackage,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency traversal: %v\n%s", err, output)
	}

	var internal []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.HasPrefix(line, "eventplane/") {
			internal = append(internal, line)
		}
	}
	if got, want := strings.Join(internal, "\n"), "eventplane/routing true\n"+observePackage+" false"; got != want {
		t.Fatalf("internal packages and dependency classification:\n%s\nwant:\n%s", got, want)
	}
}
