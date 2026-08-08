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

	contents := string(body)
	start := strings.Index(contents, "## Tests\n")
	if start < 0 {
		t.Fatal("AGENTS.md has no Tests section")
	}
	tests := contents[start+len("## Tests\n"):]
	if end := strings.Index(tests, "\n## "); end >= 0 {
		tests = tests[:end]
	}

	required := []string{
		"`cd scripts && go test ./...`",
		"hermetic and composed test layers",
		"no live layer",
		"no manual layer",
		"`python3` on `PATH`",
		"GOWORK mode is workspace",
		"repo-root `go.work`",
	}
	for _, declaration := range required {
		if !strings.Contains(tests, declaration) {
			t.Errorf("AGENTS.md Tests section does not declare %q", declaration)
		}
	}

	for _, unsupported := range []string{"-tags live", "live credentials are required", "manual invocation"} {
		if strings.Contains(tests, unsupported) {
			t.Errorf("AGENTS.md Tests section claims unsupported testing behavior %q", unsupported)
		}
	}
	for _, absentLayer := range []string{"live layer", "manual layer"} {
		if count := strings.Count(tests, absentLayer); count != 1 {
			t.Errorf("AGENTS.md Tests section mentions %q %d times; want only its no-layer declaration", absentLayer, count)
		}
	}
}

func TestDefaultGateTestsDoNotSkip(t *testing.T) {
	// R-O2IA-0JBL
	root := filepath.Join("..", "..")
	needle := "t." + "Skip"
	var findings []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := string(body)
		if strings.Contains(source, "//go:build live") {
			return nil
		}
		if strings.Contains(source, needle) {
			findings = append(findings, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan default-gate test sources: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("default-gate test sources contain forbidden skip calls: %v", findings)
	}
}
