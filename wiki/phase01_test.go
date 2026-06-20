package wiki_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionShapedBuildCompilesWithPublishedAgentkit(t *testing.T) {
	// R-MV3L-QS7I
	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	modText := string(goMod)
	if !strings.Contains(modText, "github.com/ikigenba/agentkit v0.1.0") {
		t.Fatalf("go.mod does not pin published agentkit v0.1.0:\n%s", modText)
	}
	if strings.Contains(modText, "replace github.com/ikigenba/agentkit") {
		t.Fatalf("go.mod must not replace published agentkit:\n%s", modText)
	}

	cmd := exec.Command("go", "build", "./cmd/wiki")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("GOWORK=off go build ./cmd/wiki failed: %v\n%s", err, out)
	}
}

func TestWorkspaceIncludesWikiModule(t *testing.T) {
	goWork, err := os.ReadFile(filepath.Join("..", "go.work"))
	if err != nil {
		t.Fatalf("read ../go.work: %v", err)
	}
	if !strings.Contains(string(goWork), "./wiki") {
		t.Fatalf("../go.work does not include ./wiki:\n%s", goWork)
	}
}
