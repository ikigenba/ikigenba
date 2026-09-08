package toolkit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

func TestGlobAndGrepAccessBlocksResolvedSearchDirectory(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	if err := os.Mkdir(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("actual", filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}

	tools := []struct {
		name string
		make func(string) (agentkit.Tool, error)
	}{
		{name: "Glob", make: func(root string) (agentkit.Tool, error) { return Glob(root) }},
		{name: "Grep", make: func(root string) (agentkit.Tool, error) { return Grep(root) }},
	}
	paths := []struct {
		name     string
		path     string
		expected string
	}{
		{name: "default", expected: root},
		{name: "relative", path: "actual", expected: actual},
		{name: "absolute", path: actual, expected: actual},
		{name: "symlink", path: "linked", expected: actual},
	}

	for _, toolCase := range tools {
		t.Run(toolCase.name, func(t *testing.T) {
			tool, err := toolCase.make(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, pathCase := range paths {
				t.Run(pathCase.name, func(t *testing.T) {
					input := json.RawMessage(`{"pattern":"match"}`)
					if pathCase.path != "" {
						input = json.RawMessage(fmt.Sprintf(`{"pattern":"match","path":%q}`, pathCase.path))
					}

					// R-DT8U-66J1: Glob and Grep block exactly the D1-resolved search directory.
					assertAccessValue(t, tool.Access(input), 1, []string{pathCase.expected})
				})
			}
		})
	}
}
