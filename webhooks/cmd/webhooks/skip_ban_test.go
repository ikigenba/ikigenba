package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-O2IA-0JBL
func TestNonLiveTestsDoNotSkip(t *testing.T) {
	root := moduleRoot(t)
	self := filepath.Clean("cmd/webhooks/skip_ban_test.go")
	needles := []string{
		"t." + "Skip",
		"t." + "Skipf",
		"t." + "SkipNow",
	}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == self {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "//go:build live") {
			return nil
		}
		for _, needle := range needles {
			if strings.Contains(string(body), needle) {
				t.Errorf("%s contains the forbidden skip call %q", filepath.ToSlash(rel), needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan test sources under %s: %v", root, err)
	}
}
