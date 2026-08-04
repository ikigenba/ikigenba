package observe_test

import (
	"os/exec"
	"strings"
	"testing"
)

const observePackage = "eventplane/observe"

func TestDependencyBoundary(t *testing.T) {
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
