package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-DB16-DOCS
func TestAgentsDocumentsCurrentApexSurface(t *testing.T) {
	docBytes, err := os.ReadFile("../../AGENTS.md")
	if err != nil {
		t.Fatalf("read ../../AGENTS.md: %v", err)
	}

	doc := string(docBytes)
	lowerDoc := strings.ToLower(doc)
	normalizedDoc := strings.Join(strings.Fields(doc), " ")
	internalPackages := ""
	if start := strings.Index(doc, "- `internal/`:"); start >= 0 {
		internalPackages = doc[start:]
		if end := strings.Index(internalPackages, "\n- "); end >= 0 {
			internalPackages = internalPackages[:end]
		}
	}

	checks := []struct {
		name      string
		satisfied bool
		failure   string
	}{
		{"no single-hybrid-page rule", !strings.Contains(lowerDoc, "single hybrid page"), "AGENTS.md still mentions the obsolete single hybrid page rule"},
		{"no IAM-console rule", !strings.Contains(lowerDoc, "iam console"), "AGENTS.md still mentions the obsolete IAM console rule"},
		{"no telemetry package", !strings.Contains(lowerDoc, "telemetry"), "AGENTS.md still mentions telemetry"},
		{"four-page statement", strings.Contains(normalizedDoc, "The human web surface is four pages:"), "AGENTS.md does not state that the human web surface has four pages"},
		{"login page", strings.Contains(normalizedDoc, "logged-out **login** page"), "AGENTS.md does not name the logged-out login page"},
		{"landing page", strings.Contains(normalizedDoc, "logged-in **landing/home** page"), "AGENTS.md does not name the logged-in landing/home page"},
		{"profile page", strings.Contains(normalizedDoc, "session-gated **profile** page"), "AGENTS.md does not name the session-gated profile page"},
		{"metrics page", strings.Contains(normalizedDoc, "session-gated **metrics** page"), "AGENTS.md does not name the session-gated metrics page"},
		{"profile owns token and grant management", strings.Contains(normalizedDoc, "Personal-access-token management and OAuth-grant management live on the profile page, not the landing."), "AGENTS.md does not place personal-access-token and OAuth-grant management on profile rather than landing"},
		{"internal package list names metrics", strings.Contains(internalPackages, "`metrics`"), "AGENTS.md internal/ package list does not name metrics"},
	}

	for _, check := range checks {
		if !check.satisfied {
			t.Errorf("%s: %s", check.name, check.failure)
		}
	}
}

// R-O1AD-MRKW
func TestAgentsDeclaresDashboardTestingFacts(t *testing.T) {
	docBytes, err := os.ReadFile("../../AGENTS.md")
	if err != nil {
		t.Fatalf("read ../../AGENTS.md: %v", err)
	}

	doc := string(docBytes)
	testsStart := strings.Index(doc, "## Tests\n")
	if testsStart < 0 {
		t.Fatal("AGENTS.md has no Tests section")
	}
	testsSection := doc[testsStart:]
	if nextSection := strings.Index(testsSection[len("## Tests\n"):], "\n## "); nextSection >= 0 {
		testsSection = testsSection[:len("## Tests\n")+nextSection]
	}
	normalizedTests := strings.Join(strings.Fields(testsSection), " ")

	checks := []struct {
		name      string
		satisfied bool
		failure   string
	}{
		{"default gate", strings.Contains(normalizedTests, "default gate is `cd dashboard && go test ./...`"), "Tests section does not declare the default go test command"},
		{"green bar", strings.Contains(normalizedTests, "`go build ./...`, `go vet ./...`, a silent `gofmt -l .`, and `go test ./...`"), "Tests section does not place the default command inside the full green bar"},
		{"hermetic layer", strings.Contains(normalizedTests, "**Hermetic:**") && strings.Contains(normalizedTests, "shipped-file guards"), "Tests section does not declare the hermetic layer and its shipped-file coverage"},
		{"composed layer", strings.Contains(normalizedTests, "**Composed:**") && strings.Contains(normalizedTests, "builds the real dashboard binary"), "Tests section does not declare the composed boot smoke"},
		{"manual layer", strings.Contains(normalizedTests, "**Manual:**") && strings.Contains(normalizedTests, "interactive Google and GitHub sign-in"), "Tests section does not declare the manual interactive checks"},
		{"no live layer", strings.Contains(normalizedTests, "**No live layer:**") && strings.Contains(normalizedTests, "no `//go:build live` test files"), "Tests section does not declare that there is no live layer"},
		{"no extra environmental preconditions", strings.Contains(normalizedTests, "no environmental preconditions beyond the Go toolchain"), "Tests section does not declare the environmental preconditions"},
		{"workspace GOWORK mode", strings.Contains(normalizedTests, "**GOWORK mode: workspace**") && strings.Contains(normalizedTests, "repo-root `go.work`"), "Tests section does not declare workspace GOWORK mode"},
	}

	for _, check := range checks {
		if !check.satisfied {
			t.Errorf("%s: %s", check.name, check.failure)
		}
	}
}

// R-O2IA-0JBL
func TestTestSourcesDoNotSkipOutsideLiveLayer(t *testing.T) {
	skipVerbs := []string{"Skip", "Skip" + "f", "Skip" + "Now"}
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, walkErr error) error {
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
		source := string(contents)
		if strings.Contains(source, "//go:build live") || strings.Contains(source, "// +build live") {
			return nil
		}

		for _, verb := range skipVerbs {
			needle := "t." + verb
			if strings.Contains(source, needle) {
				t.Errorf("%s contains forbidden %s outside a live-tagged test file", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk dashboard test sources: %v", err)
	}
}
