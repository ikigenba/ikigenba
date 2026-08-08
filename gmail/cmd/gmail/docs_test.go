package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsDeclaresTestingFacts(t *testing.T) {
	// R-O1AD-MRKW
	body, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read committed AGENTS.md: %v", err)
	}

	testsSection := markdownSection(string(body), "Tests")
	if testsSection == "" {
		t.Fatal("AGENTS.md has no Tests section")
	}
	required := []string{
		"`cd gmail && go test ./...`",
		"Environmental preconditions beyond the Go toolchain: none.",
		"GOWORK mode: workspace (the repo-root `go.work`)",
	}
	for _, declaration := range required {
		if !strings.Contains(testsSection, declaration) {
			t.Errorf("AGENTS.md Tests section does not declare %q", declaration)
		}
	}
	const wantLayers = "- Layers present: **hermetic**, **composed**, and **live**."
	if layers := lineWithPrefix(testsSection, "- Layers present:"); layers != wantLayers {
		t.Errorf("AGENTS.md layer declaration = %q, want %q", layers, wantLayers)
	}
}

func TestDefaultGateTestSourcesDoNotSkip(t *testing.T) {
	// R-O2IA-0JBL
	moduleRoot := filepath.Clean(filepath.Join("..", ".."))
	forbidden := "t." + "Sk" + "ip"
	var offenders []string

	err := filepath.WalkDir(moduleRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasLiveBuildConstraint(string(body)) || !strings.Contains(string(body), forbidden) {
			return nil
		}
		rel, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		offenders = append(offenders, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("scan default-gate test sources: %v", err)
	}
	if len(offenders) != 0 {
		t.Fatalf("default-gate test sources call the forbidden skip methods:\n%s", strings.Join(offenders, "\n"))
	}
}

func markdownSection(document, heading string) string {
	marker := "## " + heading
	start := strings.Index(document, marker)
	if start < 0 {
		return ""
	}
	section := document[start+len(marker):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	return section
}

func lineWithPrefix(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func hasLiveBuildConstraint(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if strings.HasPrefix(line, "//go:build ") && strings.Contains(strings.TrimPrefix(line, "//go:build "), "live") {
			return true
		}
	}
	return false
}
