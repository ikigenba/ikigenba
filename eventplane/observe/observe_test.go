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
