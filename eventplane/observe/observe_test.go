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
		"-f", "{{.ImportPath}}|{{.DepOnly}}",
		"eventplane/observe",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependencies: %v\n%s", err, output)
	}

	var dependencies, roots []string
	for _, entry := range strings.Fields(string(output)) {
		path, depOnly, ok := strings.Cut(entry, "|")
		if !ok {
			t.Fatalf("malformed go list entry %q", entry)
		}
		if !strings.HasPrefix(path, "eventplane/") {
			continue
		}
		if depOnly == "true" {
			dependencies = append(dependencies, path)
		} else {
			roots = append(roots, path)
		}
	}

	if got, want := strings.Join(dependencies, "\n"), "eventplane/routing"; got != want {
		t.Fatalf("internal dependencies:\n%s\nwant:\n%s", got, want)
	}
	if got, want := strings.Join(roots, "\n"), "eventplane/observe"; got != want {
		t.Fatalf("queried package:\n%s\nwant:\n%s", got, want)
	}
}
