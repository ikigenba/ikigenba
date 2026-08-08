package main

import (
	"io/fs"
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

	section := markdownSection(string(contents), "Tests")
	if section == "" {
		t.Fatal("AGENTS.md has no Tests section")
	}
	section = strings.Join(strings.Fields(section), " ")

	required := []string{
		"cd sites && go build ./...",
		"cd sites && go vet ./...",
		"cd sites && gofmt -l . # must print nothing",
		"cd sites && go test ./...",
		"exactly two test layers: **hermetic** and **composed**",
		"There is no live layer and no manual layer",
		"`google-chrome` binary must be on `PATH`",
		"its absence is a hard failure, never a skip",
		"`go` binary must also be on `PATH` in the test process's environment",
		"module cache already resolving sites' `replace` siblings",
		"its absence is likewise a hard failure, never a skip",
		"**workspace** GOWORK mode through the repository-root",
		"`go.work`",
		"production build's `GOWORK=off` mode is not part of the gate",
	}
	for _, declaration := range required {
		declaration = strings.Join(strings.Fields(declaration), " ")
		if !strings.Contains(section, declaration) {
			t.Errorf("AGENTS.md Tests section does not declare %q", declaration)
		}
	}
}

func TestTestsOutsideLiveLayerDoNotSkip(t *testing.T) {
	// R-O2IA-0JBL
	root := filepath.Join("..", "..")
	liveConstraint := "//go:build " + "live"
	needles := []string{
		"t." + "SkipNow",
		"t." + "Skipf",
		"t." + "Skip",
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
		source := string(contents)
		if strings.Contains(source, liveConstraint) {
			return nil
		}
		for _, needle := range needles {
			if strings.Contains(source, needle) {
				t.Errorf("non-live test file %s contains banned call %q", path, needle)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan test sources: %v", err)
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
