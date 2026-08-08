package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R-O1AD-MRKW
func TestAgentsMdDeclaresTestingFacts(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(moduleRoot(t), "AGENTS.md"))
	if err != nil {
		t.Fatalf("read committed AGENTS.md: %v", err)
	}

	testsSection := strings.Join(strings.Fields(markdownSection(t, string(body), "## Tests")), " ")
	required := []string{
		"`go test ./...` from `webhooks/`",
		"`go build ./...`",
		"`go vet ./...`",
		"`gofmt -l .`",
		"**hermetic**",
		"**composed**",
		"no live layer",
		"no tree-local manual runbook",
		"`go` binary on",
		"`PATH`",
		"module cache already resolving webhooks' `replace` siblings",
		"POSIX `bash` with `grep`",
		"hard failure, never a skip",
		"workspace mode through the repo-root `go.work`",
		"`GOWORK=off`",
		"not part of the default gate",
	}
	for _, fact := range required {
		if !strings.Contains(testsSection, fact) {
			t.Errorf("AGENTS.md Tests section does not declare %q", fact)
		}
	}
}

func markdownSection(t *testing.T, document, heading string) string {
	t.Helper()

	start := strings.Index(document, heading+"\n")
	if start < 0 {
		t.Fatalf("AGENTS.md does not contain %s", heading)
	}
	section := document[start+len(heading)+1:]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	return section
}
