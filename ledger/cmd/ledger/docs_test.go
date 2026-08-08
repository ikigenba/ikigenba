package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsDeclaresTestingFacts(t *testing.T) {
	// R-O1AD-MRKW
	committed, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read committed AGENTS.md: %v", err)
	}

	_, testsSection, found := strings.Cut(string(committed), "## Tests")
	if !found {
		t.Fatal("AGENTS.md has no Tests section")
	}
	if end := strings.Index(testsSection, "\n## "); end >= 0 {
		testsSection = testsSection[:end]
	}

	required := []string{
		"cd ledger && go test ./...",
		"hermetic and composed",
		"no live layer",
		"no manual layer",
		"Environmental preconditions beyond the Go toolchain: none",
		"workspace GOWORK mode",
		"cd ledger && GOWORK=off go build ./...",
	}
	for _, fact := range required {
		if !strings.Contains(testsSection, fact) {
			t.Errorf("AGENTS.md Tests section does not declare %q", fact)
		}
	}
}

func TestTestFilesContainNoSkipCalls(t *testing.T) {
	// R-O2IA-0JBL
	root := filepath.Join("..", "..")
	forbidden := []string{
		"t." + "Skip",
		"t." + "Skipf",
		"t." + "SkipNow",
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasLiveBuildConstraint(string(content)) {
			return nil
		}
		for _, call := range forbidden {
			if strings.Contains(string(content), call) {
				t.Errorf("%s contains forbidden test skip call %q", path, call)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan test files: %v", err)
	}
}

func hasLiveBuildConstraint(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "//go:build ") {
			continue
		}
		for _, field := range strings.FieldsFunc(line, func(r rune) bool {
			return r != '_' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z')
		}) {
			if field == "live" {
				return true
			}
		}
	}
	return false
}
