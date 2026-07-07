package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestNoHardcodedLoopbackServicePortInGoSource(t *testing.T) {
	// R-JEJ9-8S5K
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	barePort := regexp.MustCompile(`(^|[^0-9])3001([^0-9]|$)`)

	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel == "project" || strings.HasPrefix(rel, "project"+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(src)
		if strings.Contains(text, "127.0.0.1:30") {
			t.Errorf("%s contains hardcoded loopback service port prefix", rel)
		}
		if barePort.MatchString(text) {
			t.Errorf("%s contains bare wiki service port literal", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Go source: %v", err)
	}
}
