package opsctl

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func serviceRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve service root: %v", err)
	}
	return root
}

// R-O1AD-MRKW
func TestAgentsDeclaresTestingContract(t *testing.T) {
	agentsPath := filepath.Join(serviceRoot(t), "AGENTS.md")
	contents, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read %s: %v", agentsPath, err)
	}

	allSections := string(contents)
	start := strings.Index(allSections, "## Tests")
	if start < 0 {
		t.Fatal("AGENTS.md has no Tests section")
	}
	testsSection := allSections[start:]
	if end := strings.Index(testsSection[len("## Tests"):], "\n## "); end >= 0 {
		testsSection = testsSection[:len("## Tests")+end]
	}
	testsSection = strings.Join(strings.Fields(testsSection), " ")

	wants := []string{
		"GOWORK=off go test ./...",
		"run from `opsctl/`",
		"Hermetic",
		"Manual",
		"no composed layer",
		"no live layer",
		"real `tar` binary on `PATH`",
		"project/opsctl-verification.md",
		"production build and the test gate both use `GOWORK=off`",
	}
	for _, want := range wants {
		if !strings.Contains(testsSection, want) {
			t.Errorf("AGENTS.md Tests section does not declare %q", want)
		}
	}
}

// R-O2IA-0JBL
func TestDefaultGateTestsCannotSkip(t *testing.T) {
	root := serviceRoot(t)
	needles := []string{
		"t." + "Skip",
		"t." + "Skipf",
		"t." + "SkipNow",
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasLiveBuildConstraint(contents) {
			return nil
		}
		for _, needle := range needles {
			if strings.Contains(string(contents), needle) {
				t.Errorf("default-gate test source %s contains forbidden skip call %q", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan default-gate test sources: %v", err)
	}
}

func hasLiveBuildConstraint(contents []byte) bool {
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if line == "//go:build live" || line == "// +build live" {
			return true
		}
	}
	return false
}
