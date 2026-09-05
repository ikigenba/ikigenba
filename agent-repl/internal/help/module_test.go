package help_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
)

type moduleFile struct {
	Module  struct{ Path string }
	Go      string
	Require []moduleRequirement
}

type moduleRequirement struct {
	Path     string
	Version  string
	Indirect bool
}

// R-TYTV-5XTT
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
	if parsed.Go != "1.26" {
		t.Errorf("Go version = %q, want 1.26", parsed.Go)
	}

	direct := make(map[string]string)
	for _, requirement := range parsed.Require {
		if !requirement.Indirect {
			direct[requirement.Path] = requirement.Version
		}
	}
	if len(direct) != 2 {
		t.Errorf("direct requirements = %v, want exactly agentkit and toolkit", direct)
	}
	if version := direct["github.com/ikigenba/ikigenba/agentkit"]; !semanticVersionAtLeast(version, 0, 3, 0) {
		t.Errorf("agentkit version = %q, want v0.3.0 or later", version)
	}
	if version := direct["github.com/ikigenba/ikigenba/toolkit"]; version != "v0.1.0" {
		t.Errorf("toolkit version = %q, want v0.1.0", version)
	}
}

func semanticVersionAtLeast(version string, wantMajor, wantMinor, wantPatch int) bool {
	match := regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)([-+].*)?$`).FindStringSubmatch(version)
	if match == nil {
		return false
	}
	parts := make([]int, 3)
	for i := range parts {
		parts[i], _ = strconv.Atoi(match[i+1])
	}
	want := []int{wantMajor, wantMinor, wantPatch}
	for i := range parts {
		if parts[i] != want[i] {
			return parts[i] > want[i]
		}
	}
	return match[4] == "" || match[4][0] != '-'
}
