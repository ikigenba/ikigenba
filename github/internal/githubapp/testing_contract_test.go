package githubapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
}

// R-O1AD-MRKW
func TestAgentsDeclaresTestingFacts(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(repositoryRoot(t), "AGENTS.md"))
	if err != nil {
		t.Fatalf("read committed AGENTS.md: %v", err)
	}

	text := string(contents)
	testsStart := strings.Index(text, "## Tests\n")
	if testsStart < 0 {
		t.Fatal("AGENTS.md has no Tests section")
	}
	tests := text[testsStart:]
	if nextSection := strings.Index(tests[len("## Tests\n"):], "\n## "); nextSection >= 0 {
		tests = tests[:len("## Tests\n")+nextSection]
	}
	tests = strings.Join(strings.Fields(tests), " ")

	requiredDeclarations := []string{
		"GOWORK=off go test ./...",
		"GOWORK=off go build ./...",
		"GOWORK=off go vet ./...",
		"gofmt -l .",
		"hermetic layer",
		"composed layer",
		"manual layer",
		"project/github-verification.md",
		"no live layer",
		"`go` binary",
		"on `PATH`",
		"module cache",
		"hard failure",
		"never a skip",
		"`GOWORK=off` is the tree's required mode",
	}
	for _, declaration := range requiredDeclarations {
		if !strings.Contains(tests, declaration) {
			t.Errorf("AGENTS.md Tests section does not declare %q", declaration)
		}
	}
}

// R-O2IA-0JBL
func TestNonLiveTestsDoNotSkip(t *testing.T) {
	root := repositoryRoot(t)
	variants := []string{"Skip", "Skipf", "SkipNow"}

	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if path != root && info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasLiveBuildConstraint(string(contents)) {
			return nil
		}
		for _, variant := range variants {
			needle := "t." + variant
			if strings.Contains(string(contents), needle) {
				relativePath, relErr := filepath.Rel(root, path)
				if relErr != nil {
					return relErr
				}
				t.Errorf("non-live test %s contains banned skip call %s", relativePath, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan test sources: %v", err)
	}
}

func hasLiveBuildConstraint(contents string) bool {
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if !strings.HasPrefix(line, "//go:build") {
			continue
		}
		for _, term := range strings.FieldsFunc(line, func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_'
		}) {
			if term == "live" {
				return true
			}
		}
	}
	return false
}
