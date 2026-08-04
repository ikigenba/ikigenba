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
	// .Deps is the dependency set of the described package and, unlike the
	// packages visited by -deps, does not include the described package itself.
	output, err := exec.Command(
		"go", "list",
		"-f", "{{range .Deps}}{{println .}}{{end}}",
		observePackage,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency set: %v\n%s", err, output)
	}

	if got, want := internalPackages(output), "eventplane/routing"; got != want {
		t.Fatalf("internal dependency set:\n%s\nwant:\n%s", got, want)
	}
}

func TestGoListDepsIncludesQueriedPackage(t *testing.T) {
	// The done-bar command is intentionally exercised verbatim. Per `go help
	// list`, -deps visits the named package as well as its dependencies; only
	// packages not named on the command line have DepOnly set. Consequently the
	// final line here is the queried package, not an additional dependency.
	output, err := exec.Command("go", "list", "-deps", observePackage).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe and its dependencies: %v\n%s", err, output)
	}

	want := "eventplane/routing\n" + observePackage
	if got := internalPackages(output); got != want {
		t.Fatalf("internal packages from verbatim done-bar command:\n%s\nwant:\n%s", got, want)
	}
}
