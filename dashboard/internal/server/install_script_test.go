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
		"echo \"Installing 2 MCP\"",
		"claude mcp remove --scope user ikigenba_crm >/dev/null 2>&1 || true",
		"if claude mcp add --scope user --transport http ikigenba_crm https://int.ikigenba.com/srv/crm/mcp --header 'Authorization: Bearer ${IKIGENBA_TOKEN}' >/dev/null 2>&1; then",
		"echo \"🟢 ikigenba_crm\"",
		"echo \"🔴 ikigenba_crm\"",
		"claude mcp remove --scope user ikigenba_ledger >/dev/null 2>&1 || true",
		"if claude mcp add --scope user --transport http ikigenba_ledger https://int.ikigenba.com/srv/ledger/mcp --header 'Authorization: Bearer ${IKIGENBA_TOKEN}' >/dev/null 2>&1; then",
		"echo \"${ok} of 2 successfully installed.\"",
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
		"echo \"Installing 2 MCP\"",
		"codex mcp remove ikigenba_crm >/dev/null 2>&1 || true",
		"if codex mcp add ikigenba_crm --url https://int.ikigenba.com/srv/crm/mcp --bearer-token-env-var IKIGENBA_TOKEN >/dev/null 2>&1; then",
		"echo \"🟢 ikigenba_crm\"",
		"echo \"🔴 ikigenba_crm\"",
		"codex mcp remove ikigenba_ledger >/dev/null 2>&1 || true",
		"if codex mcp add ikigenba_ledger --url https://int.ikigenba.com/srv/ledger/mcp --bearer-token-env-var IKIGENBA_TOKEN >/dev/null 2>&1; then",
		"echo \"${ok} of 2 successfully installed.\"",
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

// TestInstallScriptGrok drives the public Grok route through New's registered
// route table and checks the native Grok CLI command shape for every MCP service.
func TestInstallScriptGrok(t *testing.T) {
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

	rec := do(t, srv, "GET", "https://int.ikigenba.com/install/grok",
		map[string]string{"X-Forwarded-Proto": "https"})
	if rec.Code != http.StatusOK { // R-Y6W7-R29G
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "#!/usr/bin/env bash") {
		t.Errorf("script does not start with bash shebang:\n%s", body)
	}

	wantLines := []string{
		`if [ -z "${IKIGENBA_TOKEN:-}" ]; then`,
		`echo "Installing 2 MCP"`,
		"grok mcp remove --scope user ikigenba_crm >/dev/null 2>&1 || true",
		"grok mcp add --scope user --transport http ikigenba_crm https://int.ikigenba.com/srv/crm/mcp --header 'Authorization: Bearer ${IKIGENBA_TOKEN}'",
		"grok mcp remove --scope user ikigenba_ledger >/dev/null 2>&1 || true",
		"grok mcp add --scope user --transport http ikigenba_ledger https://int.ikigenba.com/srv/ledger/mcp --header 'Authorization: Bearer ${IKIGENBA_TOKEN}'",
		`echo "${ok} of 2 successfully installed."`,
		"Restart Grok",
	}
	for _, want := range wantLines {
		if !strings.Contains(body, want) { // R-Y844-4U05
			t.Errorf("Grok script missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"claude mcp", "codex mcp", "--bearer-token-env-var"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("Grok script contains forbidden command shape %q:\n%s", forbidden, body)
		}
	}
}

func TestInstallScriptAgy(t *testing.T) {
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

	rec := do(t, srv, "GET", "https://int.ikigenba.com/install/agy",
		map[string]string{"X-Forwarded-Proto": "https"})
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" || !strings.HasPrefix(rec.Body.String(), "#!/usr/bin/env bash") { // R-QFNA-KYOH
		t.Fatalf("route response = status %d, Content-Type %q, body %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}

	body := rec.Body.String()
	wants := []string{
		`if [ -z "${IKIGENBA_TOKEN:-}" ]; then`,
		`echo "Installing 2 MCP"`,
		`"name": "ikigenba"`,
		`"description": "Ikigenba MCP tools"`,
		`"ikigenba_crm": {`,
		`"serverUrl": "https://int.ikigenba.com/srv/crm/mcp"`,
		`"Authorization": "Bearer ${IKIGENBA_TOKEN}"`,
		`"ikigenba_ledger": {`,
		`"serverUrl": "https://int.ikigenba.com/srv/ledger/mcp"`,
		`agy plugin install "${TMP_DIR}"`,
		`🟢 ikigenba_crm`,
		`🟢 ikigenba_ledger`,
		`echo "${ok} of 2 successfully installed."`,
		`Restart agy`,
	}
	for _, want := range wants {
		if !strings.Contains(body, want) { // R-QGV6-YQF6
			t.Errorf("agy script missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"claude mcp", "codex mcp", "grok mcp", "--bearer-token-env-var", "dashboard"} {
		if strings.Contains(body, forbidden) { // R-QGV6-YQF6
			t.Errorf("agy script contains forbidden content %q:\n%s", forbidden, body)
		}
	}
}
