package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsDeclaresNotifyTestingFacts(t *testing.T) {
	// R-O1AD-MRKW
	moduleRoot := filepath.Join("..", "..")
	contents, err := os.ReadFile(filepath.Join(moduleRoot, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}

	testsSection, found := strings.CutPrefix(string(contents), "# notify")
	if !found {
		t.Fatal("AGENTS.md does not begin with the notify heading")
	}
	_, testsSection, found = strings.Cut(testsSection, "## Tests\n")
	if !found {
		t.Fatal("AGENTS.md does not contain a Tests section")
	}
	if nextHeading := strings.Index(testsSection, "\n## "); nextHeading >= 0 {
		testsSection = testsSection[:nextHeading]
	}
	testsSection = strings.Join(strings.Fields(testsSection), " ")

	required := []string{
		"`cd notify && go test ./...`",
		"Layers present: **hermetic** and **composed**",
		"**no live layer**",
		"**no manual layer**",
		"Environmental preconditions beyond the Go toolchain: **none**",
		"do not read or change behavior based on ambient `NTFY_TOPIC` or `NTFY_API_KEY`",
		"GOWORK mode: **workspace**",
	}
	for _, declaration := range required {
		if !strings.Contains(testsSection, declaration) {
			t.Errorf("AGENTS.md Tests section does not declare %q", declaration)
		}
	}
}

func TestNonLiveTestsDoNotSkip(t *testing.T) {
	// R-O2IA-0JBL
	moduleRoot := filepath.Join("..", "..")
	self := filepath.Clean(filepath.Join(moduleRoot, "cmd", "notify", "docs_test.go"))
	liveConstraint := []byte("//go:build " + "live")
	forbidden := [][]byte{
		[]byte("t." + "Skip" + "("),
		[]byte("t." + "Skipf" + "("),
		[]byte("t." + "SkipNow" + "("),
	}

	err := filepath.WalkDir(moduleRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") || filepath.Clean(path) == self {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(src, liveConstraint) {
			return nil
		}
		for _, needle := range forbidden {
			if bytes.Contains(src, needle) {
				return fmt.Errorf("%s contains forbidden test skip call %q", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan non-live tests under %s: %v", moduleRoot, err)
	}
}
