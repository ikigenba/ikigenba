package opsctl

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func serviceRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve service root: %v", err)
	}
	return root
}

// R-O1AD-MRKW
func TestAgentsDeclaresTestingContract(t *testing.T) {
	agentsPath := filepath.Join(serviceRoot(t), "AGENTS.md")
	contents, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read %s: %v", agentsPath, err)
	}

	allSections := string(contents)
	start := strings.Index(allSections, "## Tests")
	if start < 0 {
		t.Fatal("AGENTS.md has no Tests section")
	}
	testsSection := allSections[start:]
	if end := strings.Index(testsSection[len("## Tests"):], "\n## "); end >= 0 {
		testsSection = testsSection[:len("## Tests")+end]
	}
	testsSection = strings.Join(strings.Fields(testsSection), " ")

	wants := []string{
		"GOWORK=off go test ./...",
		"run from `opsctl/`",
		"Hermetic",
		"Manual",
		"no composed layer",
		"no live layer",
		"real `tar` binary on `PATH`",
		"project/opsctl-verification.md",
		"production build and the test gate both use `GOWORK=off`",
	}
	for _, want := range wants {
		if !strings.Contains(testsSection, want) {
			t.Errorf("AGENTS.md Tests section does not declare %q", want)
		}
	}
}

// R-O2IA-0JBL
func TestDefaultGateTestsCannotSkip(t *testing.T) {
	root := serviceRoot(t)
	needles := []string{
		"t." + "Skip",
		"t." + "Skipf",
		"t." + "SkipNow",
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
		if hasLiveBuildConstraint(contents) {
			return nil
		}
		for _, needle := range needles {
			if strings.Contains(string(contents), needle) {
				t.Errorf("default-gate test source %s contains forbidden skip call %q", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan default-gate test sources: %v", err)
	}
}

// R-2B4O-Z98N
func TestManualVerificationRunbookCoversLiveBoxChecks(t *testing.T) {
	root := serviceRoot(t)
	decisionPaths, err := filepath.Glob(filepath.Join(root, "project", "design", "D*.md"))
	if err != nil {
		t.Fatalf("glob Decision files: %v", err)
	}
	if len(decisionPaths) == 0 {
		t.Fatal("found no Decision files to scan")
	}

	idPattern := regexp.MustCompile(`R-[A-Z0-9]{4}-[A-Z0-9]{4}`)
	liveBoxIDs := make(map[string]string)
	for _, decisionPath := range decisionPaths {
		contents, err := os.ReadFile(decisionPath)
		if err != nil {
			t.Fatalf("read %s: %v", decisionPath, err)
		}
		verification := markdownSection(string(contents), "## Verification")
		for _, entry := range requirementEntries(verification, idPattern) {
			normalized := strings.ToLower(strings.Join(strings.Fields(entry), " "))
			if strings.Contains(normalized, "real-substrate") && strings.Contains(normalized, "live box") {
				id := idPattern.FindString(entry)
				liveBoxIDs[id] = decisionPath
			}
		}
	}
	if len(liveBoxIDs) == 0 {
		t.Fatal("found no real-substrate live-box Verification entries")
	}

	runbookPath := filepath.Join(root, "project", "opsctl-verification.md")
	runbookContents, err := os.ReadFile(runbookPath)
	if err != nil {
		t.Fatalf("read %s: %v", runbookPath, err)
	}
	runbook := string(runbookContents)
	for id, decisionPath := range liveBoxIDs {
		entry := markdownEntryContaining(runbook, id)
		if entry == "" {
			t.Errorf("runbook has no entry for live-box id %s from %s", id, decisionPath)
			continue
		}
		if !strings.Contains(entry, "**Positive") {
			t.Errorf("runbook entry for %s has no Positive check", id)
		}
		if !strings.Contains(entry, "**Negative") {
			t.Errorf("runbook entry for %s has no Negative check", id)
		}
	}
}

func markdownSection(document, heading string) string {
	start := strings.Index(document, heading)
	if start < 0 {
		return ""
	}
	section := document[start+len(heading):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	return section
}

func requirementEntries(section string, idPattern *regexp.Regexp) []string {
	lines := strings.Split(section, "\n")
	var entries []string
	start := -1
	for index, line := range lines {
		if idPattern.MatchString(line) {
			if start >= 0 {
				entries = append(entries, strings.Join(lines[start:index], "\n"))
			}
			start = index
		}
	}
	if start >= 0 {
		entries = append(entries, strings.Join(lines[start:], "\n"))
	}
	return entries
}

func markdownEntryContaining(document, id string) string {
	lines := strings.Split(document, "\n")
	for index, line := range lines {
		if !strings.HasPrefix(line, "### ") || !strings.Contains(line, id) {
			continue
		}
		end := len(lines)
		for next := index + 1; next < len(lines); next++ {
			if strings.HasPrefix(lines[next], "### ") {
				end = next
				break
			}
		}
		return strings.Join(lines[index:end], "\n")
	}
	return ""
}

func hasLiveBuildConstraint(contents []byte) bool {
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if line == "//go:build live" || line == "// +build live" {
			return true
		}
	}
	return false
}
