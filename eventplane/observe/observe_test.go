package observe_test

import (
	"os/exec"
	"strings"
	"testing"
)

const observePackage = "eventplane/observe"

func TestDependencyBoundary(t *testing.T) {
	// Keep the check on the done-bar's -deps traversal. DepOnly distinguishes
	// actual dependencies from the package explicitly named on the command line.
	output, err := exec.Command(
		"go", "list", "-deps",
		"-f", "{{.ImportPath}} {{.DepOnly}}",
		observePackage,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency set: %v\n%s", err, output)
	}

	var internal []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.HasPrefix(line, "eventplane/") {
			internal = append(internal, line)
		}
	}
	if got, want := strings.Join(internal, "\n"), "eventplane/routing true\n"+observePackage+" false"; got != want {
		t.Fatalf("internal dependency traversal:\n%s\nwant:\n%s", got, want)
	}
}
