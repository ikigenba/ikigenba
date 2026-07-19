package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestProductionBuildHasNoRetiredInferenceModule(t *testing.T) {
	// R-0UOV-2HFX
	root := filepath.Clean(filepath.Join("..", ".."))
	env := append(os.Environ(), "GOWORK=off")
	build := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "wiki"), "./cmd/wiki")
	build.Dir, build.Env = root, env
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("production-shaped build failed: %v\n%s", err, output)
	}
	graph := exec.Command("go", "mod", "graph")
	graph.Dir, graph.Env = root, env
	output, err := graph.CombinedOutput()
	if err != nil {
		t.Fatalf("go mod graph failed: %v\n%s", err, output)
	}
	retired := "github.com/ikigenba/" + "agentkit"
	if strings.Contains(string(output), retired) {
		t.Fatalf("module graph contains retired module %q", retired)
	}
}

func TestModuleFileHasOnlySuiteSiblingReplacements(t *testing.T) {
	// R-0VWR-G96M
	root := filepath.Clean(filepath.Join("..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(go.mod): %v", err)
	}
	text := string(raw)
	retired := "github.com/ikigenba/" + "agentkit"
	if strings.Contains(text, retired) {
		t.Fatalf("go.mod contains retired module %q", retired)
	}
	re := regexp.MustCompile(`(?m)^replace\s+(\S+)\s+=>\s+(\S+)\s*$`)
	matches := re.FindAllStringSubmatch(text, -1)
	want := map[string]string{"appkit": "../appkit", "eventplane": "../eventplane", "registry": "../registry"}
	if len(matches) != len(want) {
		t.Fatalf("replace directives = %v, want exactly %v", matches, want)
	}
	for _, match := range matches {
		if want[match[1]] != match[2] {
			t.Fatalf("replace %s => %s, want suite siblings %v", match[1], match[2], want)
		}
	}
}
