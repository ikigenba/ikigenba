package ikigenbasuite

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceModules(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list workspace modules: %v\n%s", err, out)
	}

	for _, dir := range strings.Fields(string(out)) {
		dir := filepath.Clean(dir)
		if dir == root {
			continue
		}
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("go", "test", "./...")
			cmd.Dir = dir
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Run(); err != nil {
				t.Fatalf("go test ./... in %s: %v\n%s", dir, err, output.String())
			}
		})
	}
}
