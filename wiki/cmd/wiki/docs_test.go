package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsDeclaresTestingFacts(t *testing.T) {
	// R-O1AD-MRKW
	contents, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read committed AGENTS.md: %v", err)
	}
	tests := markdownSection(t, string(contents), "Tests")
	for _, declaration := range []string{
		"`go test ./...`",
		"Layers present: **hermetic**, **composed**, and **live**.",
		"Environmental preconditions beyond the Go toolchain: **none**.",
		"GOWORK mode: **workspace**",
	} {
		if !strings.Contains(tests, declaration) {
			t.Errorf("AGENTS.md Tests section does not declare %q", declaration)
		}
	}
	if strings.Contains(tests, "**manual**") {
		t.Error("AGENTS.md Tests section declares manual, a layer wiki does not have")
	}
}

func TestDefaultGateTestsDoNotSkip(t *testing.T) {
	// R-O2IA-0JBL
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	skipCall := strings.Join([]string{"t", ".", "Skip"}, "")
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "project" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(string(source), "//go:build live\n") {
			return nil
		}
		if strings.Contains(string(source), skipCall) {
			rel, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return relErr
			}
			t.Errorf("default-gate test %s contains a skip call", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan default-gate test sources: %v", err)
	}
}

func markdownSection(t *testing.T, document, heading string) string {
	t.Helper()
	startMarker := "## " + heading + "\n"
	start := strings.Index(document, startMarker)
	if start < 0 {
		t.Fatalf("AGENTS.md has no %q section", heading)
	}
	section := document[start+len(startMarker):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	return section
}
