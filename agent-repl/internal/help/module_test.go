package help_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

type moduleFile struct {
	Module  struct{ Path string }
	Go      string
	Require []moduleRequirement
}

type moduleRequirement struct {
	Path     string
	Indirect bool
}

// R-JUQZ-ZDMZ
func TestModuleContract(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// Ask the Go toolchain to parse go.mod, avoiding assumptions about whether
	// requirements use block or single-line syntax.
	root := filepath.Join(filepath.Dir(filename), "..", "..")
	command := exec.Command("go", "mod", "edit", "-json")
	command.Dir = root
	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var parsed moduleFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Module.Path != "github.com/ikigenba/ikigenba/agent-repl" {
		t.Fatalf("module path = %q", parsed.Module.Path)
	}
	if parsed.Go == "" {
		t.Error("go.mod does not specify a Go version")
	}

	direct := make(map[string]struct{})
	for _, requirement := range parsed.Require {
		if !requirement.Indirect {
			direct[requirement.Path] = struct{}{}
		}
	}
	if len(direct) != 2 {
		t.Errorf("direct requirements = %v, want exactly agentkit and toolkit", direct)
	}
	if _, ok := direct["github.com/ikigenba/ikigenba/agentkit"]; !ok {
		t.Error("go.mod does not directly require github.com/ikigenba/ikigenba/agentkit")
	}
	if _, ok := direct["github.com/ikigenba/ikigenba/toolkit"]; !ok {
		t.Error("go.mod does not directly require github.com/ikigenba/ikigenba/toolkit")
	}
}
