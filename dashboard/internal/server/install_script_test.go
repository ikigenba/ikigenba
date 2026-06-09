package server

import (
	"net/http"
	"strings"
	"testing"
)

// TestInstallScript: GET /install/claude serves a text/plain bash script that
// emits an idempotent, user-scoped `claude mcp add` for every MCP=true service on
// the box (crm + ledger), self-templated to the request host, and omits the
// dashboard (no MCP=true). It is public — no session cookie is supplied.
func TestInstallScript(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "dashboard", "APP=dashboard\nMOUNT=/\nDEFAULT=true\n")
	writeManifest(t, root, "crm", "APP=crm\nMOUNT=/srv/crm/\nMCP=true\n")
	writeManifest(t, root, "ledger", "APP=ledger\nMOUNT=/srv/ledger/\nMCP=true\n")

	opts := newServerDeps(t).opts()
	opts.ManifestRoot = root
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := do(t, srv, "GET", "https://int.ikigenba.com/install/claude",
		map[string]string{"X-Forwarded-Proto": "https"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	body := rec.Body.String()

	// Registration handles are namespaced "ikigenba_<svc>"; the resource URLs stay
	// the bare /srv/<svc>/mcp endpoints (not prefixed).
	wantLines := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"claude mcp remove --scope user ikigenba_crm >/dev/null 2>&1 || true",
		"claude mcp add --scope user --transport http ikigenba_crm https://int.ikigenba.com/srv/crm/mcp --header 'Authorization: Bearer ${IKIGENBA_TOKEN}'",
		"claude mcp remove --scope user ikigenba_ledger >/dev/null 2>&1 || true",
		"claude mcp add --scope user --transport http ikigenba_ledger https://int.ikigenba.com/srv/ledger/mcp --header 'Authorization: Bearer ${IKIGENBA_TOKEN}'",
		// Missing-token guard (progressive-discovery moment).
		`if [ -z "${IKIGENBA_TOKEN:-}" ]; then`,
		"exit 1",
		`export IKIGENBA_TOKEN=`,
	}
	for _, want := range wantLines {
		if !strings.Contains(body, want) {
			t.Errorf("script missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "ikigenba_crm/mcp") {
		t.Errorf("resource URL must not be prefixed:\n%s", body)
	}
	if strings.Contains(body, "dashboard") {
		t.Errorf("dashboard (no MCP) leaked into install script:\n%s", body)
	}
}

// TestInstallScriptCodex: GET /install/codex serves the Codex variant of the
// one-paste script — `codex mcp add <name> --url <resource>` (no --scope, no
// --transport) for every MCP=true service, with a guarded `codex mcp remove`
// ahead of each. Mirrors TestInstallScript's manifest setup.
func TestInstallScriptCodex(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "dashboard", "APP=dashboard\nMOUNT=/\nDEFAULT=true\n")
	writeManifest(t, root, "crm", "APP=crm\nMOUNT=/srv/crm/\nMCP=true\n")
	writeManifest(t, root, "ledger", "APP=ledger\nMOUNT=/srv/ledger/\nMCP=true\n")

	opts := newServerDeps(t).opts()
	opts.ManifestRoot = root
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := do(t, srv, "GET", "https://int.ikigenba.com/install/codex",
		map[string]string{"X-Forwarded-Proto": "https"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	body := rec.Body.String()

	wantLines := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"codex mcp remove ikigenba_crm >/dev/null 2>&1 || true",
		"codex mcp add ikigenba_crm --url https://int.ikigenba.com/srv/crm/mcp --bearer-token-env-var IKIGENBA_TOKEN",
		"codex mcp remove ikigenba_ledger >/dev/null 2>&1 || true",
		"codex mcp add ikigenba_ledger --url https://int.ikigenba.com/srv/ledger/mcp --bearer-token-env-var IKIGENBA_TOKEN",
		"Restart Codex",
		// Missing-token guard (progressive-discovery moment).
		`if [ -z "${IKIGENBA_TOKEN:-}" ]; then`,
		"exit 1",
		`export IKIGENBA_TOKEN=`,
	}
	for _, want := range wantLines {
		if !strings.Contains(body, want) {
			t.Errorf("script missing %q:\n%s", want, body)
		}
	}
	// Codex uses neither the Claude transport flag nor a scope flag.
	if strings.Contains(body, "--transport") {
		t.Errorf("Codex script must not carry --transport:\n%s", body)
	}
	if strings.Contains(body, "--scope") {
		t.Errorf("Codex script must not carry --scope:\n%s", body)
	}
	if strings.Contains(body, "dashboard") {
		t.Errorf("dashboard (no MCP) leaked into install script:\n%s", body)
	}
}
