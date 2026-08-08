package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentsDeclaresRegistryTestingFacts(t *testing.T) {
	// R-O1AD-MRKW
	contents, err := os.ReadFile(filepath.Join(packageDir(t), "AGENTS.md"))
	if err != nil {
		t.Fatalf("read committed AGENTS.md: %v", err)
	}

	testsSection := markdownSection(string(contents), "Tests")
	if testsSection == "" {
		t.Fatal("AGENTS.md has no Tests section")
	}
	testsSection = strings.Join(strings.Fields(testsSection), " ")

	wants := []string{
		"default gate is `GOWORK=off go test ./...`",
		"`GOWORK=off go build ./...`",
		"Green means both commands exit 0, with no failures and no SKIP",
		"Every test is hermetic",
		"no composed layer",
		"no live layer",
		"no manual layer",
		"no environmental preconditions beyond a working Go toolchain",
		"Always force `GOWORK=off` when building and testing",
	}
	for _, want := range wants {
		if !strings.Contains(testsSection, want) {
			t.Errorf("AGENTS.md Tests section does not declare %q", want)
		}
	}
}

func TestNonLiveTestsDoNotSkip(t *testing.T) {
	// R-O2IA-0JBL
	files, err := filepath.Glob(filepath.Join(packageDir(t), "*_test.go"))
	if err != nil {
		t.Fatalf("find test sources: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("found no test sources to scan")
	}

	needles := []string{
		"t." + "Skip(",
		"t." + "Skipf(",
		"t." + "SkipNow(",
	}
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("read %s: %v", filepath.Base(file), err)
			continue
		}
		if hasLiveBuildConstraint(string(contents)) {
			continue
		}
		for _, needle := range needles {
			if strings.Contains(string(contents), needle) {
				t.Errorf("non-live test source %s contains forbidden skip call %q", filepath.Base(file), needle)
			}
		}
	}
}

func packageDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate package sources")
	}
	return filepath.Dir(filename)
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

func hasLiveBuildConstraint(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			if strings.HasPrefix(trimmed, "//go:build") && strings.Contains(trimmed, "live") {
				return true
			}
			continue
		}
		break
	}
	return false
}
