package server

import (
	"net/http"
	"strings"
	"testing"
)

// TestIndexServiceDirectory: the logged-in index carries only the MCP service
// directory whose rows show the local name and resource URL.
func TestIndexServiceDirectory(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "dashboard", "APP=dashboard\nMOUNT=/\nDEFAULT=true\n")
	writeManifest(t, root, "crm", "APP=crm\nMOUNT=/srv/crm/\nMCP=true\n")

	opts := newServerDeps(t).opts()
	opts.ManifestRoot = root
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sess := liveSession(t, srv)

	rec := do(t, srv, "GET", "https://int.ikigenba.com/", map[string]string{
		"Cookie":            sess.Name + "=" + sess.Value,
		"X-Forwarded-Proto": "https",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{
		"MCP Services",
		"ikigenba_crm",
		"https://int.ikigenba.com/srv/crm/mcp",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("logged-in index missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/install/claude") || strings.Contains(body, "/install/codex") || strings.Contains(body, "/install/grok") { // R-Y5OB-DAIR
		t.Errorf("logged-in index still contains install snippets:\n%s", body)
	}
}

// TestIndexConnectSectionLoggedOut: both the connect block and the LIST are
// logged-in only — a visitor with no session sees neither.
func TestIndexConnectSectionLoggedOut(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "crm", "APP=crm\nMOUNT=/srv/crm/\nMCP=true\n")

	opts := newServerDeps(t).opts()
	opts.ManifestRoot = root
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := do(t, srv, "GET", "https://int.ikigenba.com/", nil)
	body := rec.Body.String()
	if strings.Contains(body, "Connect your coding agent") {
		t.Errorf("connect block shown to logged-out visitor:\n%s", body)
	}
	if strings.Contains(body, `class="rows"`) || strings.Contains(body, "ikigenba_crm") {
		t.Errorf("services list shown to logged-out visitor:\n%s", body)
	}
}
