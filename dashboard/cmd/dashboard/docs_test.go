package main

import (
	"os"
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
