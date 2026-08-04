package observe_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDependencyBoundary(t *testing.T) {
	const queriedPackage = "eventplane/observe"

	// Exercise the done-bar command verbatim. The go command includes the
	// queried package itself in -deps output, after all of its dependencies.
	output, err := exec.Command("go", "list", "-deps", queriedPackage).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependencies: %v\n%s", err, output)
	}

	var internal, dependencies []string
	for _, path := range strings.Fields(string(output)) {
		if !strings.HasPrefix(path, "eventplane/") {
			continue
		}
		internal = append(internal, path)
		if path != queriedPackage {
			dependencies = append(dependencies, path)
		}
	}

	if got, want := strings.Join(internal, "\n"), "eventplane/routing\neventplane/observe"; got != want {
		t.Fatalf("internal packages from verbatim done-bar command:\n%s\nwant:\n%s", got, want)
	}
	if got, want := strings.Join(dependencies, "\n"), "eventplane/routing"; got != want {
		t.Fatalf("internal dependencies:\n%s\nwant:\n%s", got, want)
	}

	// Ask go list for its dependency classification as a separate assertion.
	// The plain -deps output cannot make this distinction because it always
	// includes the package named on the command line.
	output, err = exec.Command(
		"go", "list", "-deps",
		"-f", "{{if .DepOnly}}{{.ImportPath}}{{end}}",
		queriedPackage,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("classify observe dependencies: %v\n%s", err, output)
	}

	internal = internal[:0]
	for _, path := range strings.Fields(string(output)) {
		if strings.HasPrefix(path, "eventplane/") {
			internal = append(internal, path)
		}
	}
	if got, want := strings.Join(internal, "\n"), "eventplane/routing"; got != want {
		t.Fatalf("classified internal dependencies:\n%s\nwant:\n%s", got, want)
	}
}
