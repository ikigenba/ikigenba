package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionBinaryDependencyClosureExcludesAgentkit(t *testing.T) {
	// R-KDHD-V3XI
	root := filepath.Clean(filepath.Join("..", ".."))
	env := append(os.Environ(), "GOWORK=off")
	list := exec.Command("go", "list", "-deps", "./cmd/wiki")
	list.Dir, list.Env = root, env
	output, err := list.CombinedOutput()
	if err != nil {
		t.Fatalf("production-shaped dependency listing failed: %v\n%s", err, output)
	}
	for _, dependency := range strings.Split(string(output), "\n") {
		if strings.Contains(dependency, "github.com/ikigenba/"+"agentkit") {
			t.Fatalf("production binary dependency closure contains %q", dependency)
		}
	}
}
