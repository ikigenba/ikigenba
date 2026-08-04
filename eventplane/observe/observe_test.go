package observe_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDependencyBoundary(t *testing.T) {
	output, err := exec.Command("go", "list", "-deps", "eventplane/observe").CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependencies: %v\n%s", err, output)
	}

	var internal []string
	for _, dependency := range strings.Fields(string(output)) {
		// go list -deps includes the package being queried. It is not one of
		// that package's dependencies, so exclude it from the boundary check.
		if strings.HasPrefix(dependency, "eventplane/") && dependency != "eventplane/observe" {
			internal = append(internal, dependency)
		}
	}

	if got, want := strings.Join(internal, "\n"), "eventplane/routing"; got != want {
		t.Fatalf("internal dependencies:\n%s\nwant:\n%s", got, want)
	}
}
