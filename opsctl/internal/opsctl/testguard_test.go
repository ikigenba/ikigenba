package opsctl

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-E639-DSH5
func TestTestsDoNotBuildSiblingServices(t *testing.T) {
	treeRoot := filepath.Clean(filepath.Join("..", ".."))
	literalEscape := ".." + "/.." + "/.."
	joinedEscape := "filepath.Join(\"" + "..\",\"" + "..\",\"" + "..\""
	buildInvocation := "\"" + "go\",\"" + "build\""
	commandPath := "\"." + "/cmd/"

	err := filepath.WalkDir(treeRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "project" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		normalized := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(string(body))
		hasSiblingEscape := strings.Contains(string(body), literalEscape) || strings.Contains(normalized, joinedEscape)
		hasSiblingBuild := strings.Contains(normalized, buildInvocation) && strings.Contains(normalized, commandPath)
		if hasSiblingEscape && hasSiblingBuild {
			t.Errorf("%s constructs a build from a sibling service tree", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan opsctl tests: %v", err)
	}
}
