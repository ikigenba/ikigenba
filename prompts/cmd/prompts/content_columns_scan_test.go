package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredContentColumnsAreAbsentFromPersistenceSources(t *testing.T) {
	// R-SF8L-DKNK
	root := filepath.Join("..", "..")
	allowedWireOrLegacy := map[string]bool{
		"internal/mcp/describe.go":   true,
		"internal/mcp/tools.go":      true,
		"internal/prompt/model.go":   true,
		"internal/prompt/service.go": true,
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "project" || rel == "internal/db/migrations" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || allowedWireOrLegacy[rel] {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, retired := range []string{"user_prompt", "system_prompt", "config_json"} {
			if strings.Contains(string(body), retired) {
				t.Errorf("%s contains retired persistence identifier %q", rel, retired)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan non-test Go sources: %v", err)
	}
}
