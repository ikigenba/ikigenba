package observe_test

import (
	"os/exec"
	"strings"
	"testing"
)

const observePackage = "eventplane/observe"

func TestDependencyBoundary(t *testing.T) {
	// -deps also lists the explicitly queried root. DepOnly selects actual
	// dependencies, which is the boundary the done bar intends to constrain.
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
