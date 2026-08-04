package observe_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDependencyBoundary(t *testing.T) {
	// -deps also lists the package named on the command line. DepOnly is Go's
	// documented distinction between that package and its actual dependencies.
	output, err := exec.Command(
		"go", "list", "-deps",
		"-f", "{{if .DepOnly}}{{.ImportPath}}{{end}}",
		"eventplane/observe",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependencies: %v\n%s", err, output)
	}

	var internal []string
	for _, dependency := range strings.Fields(string(output)) {
		if strings.HasPrefix(dependency, "eventplane/") {
			internal = append(internal, dependency)
		}
	}

	if got, want := strings.Join(internal, "\n"), "eventplane/routing"; got != want {
		t.Fatalf("internal dependencies:\n%s\nwant:\n%s", got, want)
	}
}
