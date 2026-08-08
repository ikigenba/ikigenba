package eventplane

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Dir(sourceFile)
}

func TestAgentsDeclaresTestingFacts(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(moduleRoot(t), "AGENTS.md"))
	if err != nil {
		t.Fatalf("read committed AGENTS.md: %v", err)
	}

	testsSection, found := strings.CutPrefix(string(contents), "# eventplane")
	if !found {
		t.Fatal("AGENTS.md does not describe eventplane")
	}
	_, testsSection, found = strings.Cut(testsSection, "## Tests\n")
	if !found {
		t.Fatal("AGENTS.md has no Tests section")
	}
	if end := strings.Index(testsSection, "\n## "); end >= 0 {
		testsSection = testsSection[:end]
	}
	testsSection = strings.Join(strings.Fields(testsSection), " ")

	requiredFacts := map[string][]string{
		"default gate":               {"go test ./..."},
		"testing layers":             {"hermetic", "no composed layer", "no live layer", "no manual layer"},
		"environmental precondition": {"`go` binary on `PATH`", "module cache", "hard test failure"},
		"GOWORK mode":                {"workspace mode", "go.work", "GOWORK=off"},
	}

	// R-O1AD-MRKW: each assertion checks a required fact in the real AGENTS.md Tests section.
	for fact, phrases := range requiredFacts {
		for _, phrase := range phrases {
			if !strings.Contains(testsSection, phrase) {
				t.Errorf("Tests section does not declare %s with %q", fact, phrase)
			}
		}
	}
}

func TestDefaultTestsDoNotSkip(t *testing.T) {
	root := moduleRoot(t)
	var violations []string
	skipMethods := []string{
		"Skip",
		"Skipf",
		"SkipNow",
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "project" || entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hasLiveBuildConstraint(string(contents)) {
			return nil
		}
		for _, method := range skipMethods {
			needle := "t." + method
			if strings.Contains(string(contents), needle) {
				relativePath, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				violations = append(violations, relativePath+": "+needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan test sources: %v", err)
	}

	// R-O2IA-0JBL: the default test sources must contain zero skip calls.
	if len(violations) != 0 {
		t.Errorf("default test sources contain forbidden skip calls: %s", strings.Join(violations, ", "))
	}
}

func hasLiveBuildConstraint(contents string) bool {
	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			return false
		}
		if strings.HasPrefix(trimmed, "//go:build ") {
			expression := strings.TrimPrefix(trimmed, "//go:build ")
			for _, token := range strings.FieldsFunc(expression, func(r rune) bool {
				return r != '_' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z')
			}) {
				if token == "live" {
					return true
				}
			}
		}
	}
	return false
}
