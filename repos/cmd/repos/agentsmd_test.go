package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsMDDeclaresRepositoryTestingFacts(t *testing.T) {
	// R-O1AD-MRKW
	contents, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	testsSection, found := markdownSection(string(contents), "Tests")
	if !found {
		t.Fatal("AGENTS.md has no Tests section")
	}
	testsSection = strings.Join(strings.Fields(testsSection), " ")

	for fact, requiredText := range map[string][]string{
		"default gate": {
			"from `repos/` with `go test ./...`",
			"`go build ./...`",
			"`go vet ./...`",
			"`gofmt -l .`",
		},
		"test layers": {
			"**hermetic**",
			"**composed**",
			"no live layer",
			"no tree-local manual runbook",
		},
		"environmental preconditions": {
			"real `git` binary must be on `PATH`",
			"absence is a hard failure",
			"`go` binary must be on `PATH` in the test process's environment",
			"module cache resolving repos' `replace` siblings",
			"real `google-chrome` binary must also be on `PATH`",
			"failure to launch is a hard failure, never a skip",
		},
		"GOWORK mode": {
			"workspace mode through the repo-root `go.work`",
			"production build forces `GOWORK=off`",
			"not part of the default gate",
		},
	} {
		t.Run(fact, func(t *testing.T) {
			for _, text := range requiredText {
				if !strings.Contains(testsSection, text) {
					t.Errorf("Tests section does not declare %q", text)
				}
			}
		})
	}
}

func markdownSection(document, name string) (string, bool) {
	heading := "## " + name
	start := strings.Index(document, heading)
	if start < 0 {
		return "", false
	}
	section := document[start+len(heading):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	return section, true
}
