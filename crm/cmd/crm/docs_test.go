package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsDeclaresTestingFacts(t *testing.T) {
	// R-O1AD-MRKW
	doc, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read committed AGENTS.md: %v", err)
	}

	testsSection := section(string(doc), "## Tests")
	if testsSection == "" {
		t.Fatal("AGENTS.md has no Tests section")
	}
	for _, declaration := range []string{
		"cd crm && go test ./...",
		"Layers present: **hermetic** and **composed**",
		"**no live layer**",
		"**no manual layer**",
		"Environmental preconditions beyond the Go toolchain: **none**",
		"GOWORK mode: **workspace**",
	} {
		if !strings.Contains(testsSection, declaration) {
			t.Errorf("AGENTS.md Tests section does not declare %q", declaration)
		}
	}
}

func TestTestsDoNotSkipOutsideLiveLayer(t *testing.T) {
	// R-O2IA-0JBL
	root := filepath.Join("..", "..")
	skipPrefix := "t." + "Skip"
	needles := []string{skipPrefix + "Now", skipPrefix + "f", skipPrefix}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(source), "//go:build live") {
			return nil
		}
		for _, needle := range needles {
			if strings.Contains(string(source), needle) {
				t.Errorf("%s contains banned skip call %q outside a live-tagged file", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan crm test files: %v", err)
	}
}

func section(doc, heading string) string {
	start := strings.Index(doc, heading+"\n")
	if start < 0 {
		return ""
	}
	rest := doc[start+len(heading)+1:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}
	return rest
}
