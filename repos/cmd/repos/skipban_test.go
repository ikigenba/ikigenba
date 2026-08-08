package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func TestDefaultGateTestsDoNotSkip(t *testing.T) {
	// R-O2IA-0JBL
	root := filepath.Join("..", "..")
	forbidden := []string{
		"t." + "Skip",
		"t." + "Skipf",
		"t." + "SkipNow",
	}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
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
		if hasLiveBuildConstraint(string(contents)) {
			return nil
		}
		for _, needle := range forbidden {
			if strings.Contains(string(contents), needle) {
				t.Errorf("default-gate test file %s contains forbidden skip call %q", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func hasLiveBuildConstraint(contents string) bool {
	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			return false
		}
		if !strings.HasPrefix(trimmed, "//go:build ") &&
			!strings.HasPrefix(trimmed, "// +build ") {
			continue
		}
		for _, word := range strings.FieldsFunc(trimmed, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
		}) {
			if word == "live" {
				return true
			}
		}
	}
	return false
}
