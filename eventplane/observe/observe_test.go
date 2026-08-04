package observe_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const observePackage = "eventplane/observe"

func TestDependencyBoundary(t *testing.T) {
	metadata, err := exec.Command("go", "list", "-deps", "-json", observePackage).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency metadata: %v\n%s", err, metadata)
	}
	type listedPackage struct {
		ImportPath string
		Imports    []string
		DepOnly    bool
		Match      []string
	}
	decoder := json.NewDecoder(strings.NewReader(string(metadata)))
	var dependencies []string
	var root *listedPackage
	for decoder.More() {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("decode observe dependency metadata: %v", err)
		}
		if pkg.DepOnly && strings.HasPrefix(pkg.ImportPath, "eventplane/") {
			dependencies = append(dependencies, pkg.ImportPath)
		}
		if pkg.ImportPath == observePackage {
			copy := pkg
			root = &copy
		}
	}
	if got, want := strings.Join(dependencies, "\n"), "eventplane/routing"; got != want {
		t.Fatalf("internal dependency set:\n%s\nwant:\n%s", got, want)
	}
	if root == nil || root.DepOnly || strings.Join(root.Match, " ") != observePackage {
		t.Fatalf("query root classification: %#v", root)
	}
	if got, want := strings.Join(root.Imports, "\n"), "context\neventplane/routing\ntime"; got != want {
		t.Fatalf("direct imports:\n%s\nwant:\n%s", got, want)
	}
}

func TestDependencyBoundaryCommand(t *testing.T) {
	output, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}} {{.DepOnly}}", observePackage).CombinedOutput()
	if err != nil {
		t.Fatalf("list observe dependency traversal: %v\n%s", err, output)
	}

	var dependencies []string
	var queryRoots []string
	for line := range strings.Lines(string(output)) {
		line = strings.TrimSpace(line)
		path, depOnly, ok := strings.Cut(line, " ")
		if !ok || !strings.HasPrefix(path, "eventplane/") {
			continue
		}
		switch depOnly {
		case "true":
			dependencies = append(dependencies, path)
		case "false":
			queryRoots = append(queryRoots, path)
		default:
			t.Fatalf("unexpected DepOnly value in %q", line)
		}
	}

	if got, want := strings.Join(dependencies, "\n"), "eventplane/routing"; got != want {
		t.Fatalf("internal dependencies:\n%s\nwant:\n%s", got, want)
	}
	if got, want := strings.Join(queryRoots, "\n"), observePackage; got != want {
		t.Fatalf("explicit query roots:\n%s\nwant:\n%s", got, want)
	}
}
