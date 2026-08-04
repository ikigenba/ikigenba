package observe_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestDependencyBoundary(t *testing.T) {
	const queriedPackage = "eventplane/observe"
	const routingPackage = "eventplane/routing"

	output, err := exec.Command(
		"go", "list",
		"-f", "{{range .Imports}}{{println .}}{{end}}",
		queriedPackage,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe imports: %v\n%s", err, output)
	}

	var internalImports []string
	for _, path := range strings.Fields(string(output)) {
		if strings.HasPrefix(path, "eventplane/") {
			internalImports = append(internalImports, path)
		}
	}
	if got, want := strings.Join(internalImports, "\n"), routingPackage; got != want {
		t.Fatalf("direct internal imports:\n%s\nwant:\n%s", got, want)
	}

	// Exercise the done-bar command verbatim. The go command includes the
	// queried package itself in -deps output, after all of its dependencies.
	output, err = exec.Command("go", "list", "-deps", queriedPackage).CombinedOutput()
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

	if got, want := strings.Join(internal, "\n"), routingPackage+"\n"+queriedPackage; got != want {
		t.Fatalf("internal packages from verbatim done-bar command:\n%s\nwant:\n%s", got, want)
	}
	if got, want := strings.Join(dependencies, "\n"), routingPackage; got != want {
		t.Fatalf("internal dependencies:\n%s\nwant:\n%s", got, want)
	}

	// Query the package's dependency set directly. Unlike `go list -deps`,
	// .Deps excludes the package being described, so this is the literal
	// dependency-boundary check intended by the done bar.
	output, err = exec.Command(
		"go", "list",
		"-f", "{{range .Deps}}{{println .}}{{end}}",
		queriedPackage,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency set: %v\n%s", err, output)
	}

	internal = internal[:0]
	for _, path := range strings.Fields(string(output)) {
		if strings.HasPrefix(path, "eventplane/") {
			internal = append(internal, path)
		}
	}
	if got, want := strings.Join(internal, "\n"), routingPackage; got != want {
		t.Fatalf("internal dependency set:\n%s\nwant:\n%s", got, want)
	}

	// Ask go list for its dependency classification as structured data. The
	// plain -deps output cannot make this distinction because it always includes
	// the package named on the command line.
	output, err = exec.Command("go", "list", "-deps", "-json", queriedPackage).CombinedOutput()
	if err != nil {
		t.Fatalf("classify observe dependencies: %v\n%s", err, output)
	}

	type listedPackage struct {
		ImportPath string
		DepOnly    bool
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	internal = internal[:0]
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		if strings.HasPrefix(pkg.ImportPath, "eventplane/") && pkg.DepOnly {
			internal = append(internal, pkg.ImportPath)
		}
	}
	if got, want := strings.Join(internal, "\n"), routingPackage; got != want {
		t.Fatalf("classified internal dependencies:\n%s\nwant:\n%s", got, want)
	}
}
