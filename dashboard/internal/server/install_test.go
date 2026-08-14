package server

import (
	"net/http"
	"strings"
	"testing"
)

func signedInInstallPage(t *testing.T, srv *http.Server) string {
	t.Helper()
	cookie := liveSession(t, srv)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/install", map[string]string{
		"Cookie":            cookie.Name + "=" + cookie.Value,
		"X-Forwarded-Proto": "https",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestInstallPageIsRegisteredSeparatelyFromScripts(t *testing.T) {
	srv := testServer(t)
	body := signedInInstallPage(t, srv)

	// R-VH1G-HX9K
	if !strings.Contains(body, `<h2>Connect your agent</h2>`) {
		t.Errorf("install page missing its composition:\n%s", body)
	}
	for _, path := range []string{"/install/claude", "/install/codex"} {
		rec := do(t, srv, "GET", "https://int.ikigenba.com"+path, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, rec.Code)
			continue
		}
		if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
			t.Errorf("GET %s Content-Type = %q, want text/plain script", path, contentType)
		}
		if !strings.HasPrefix(rec.Body.String(), "#!/usr/bin/env bash") {
			t.Errorf("GET %s no longer serves a bash script:\n%s", path, rec.Body.String())
		}
	}
}

func TestInstallPageRequiresLiveSession(t *testing.T) {
	srv := testServer(t)
	tests := []struct {
		name    string
		headers map[string]string
	}{
		{name: "missing"},
		{name: "invalid", headers: map[string]string{"Cookie": sessionCookieName + "=bogus-value"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, srv, "GET", "https://int.ikigenba.com/install", tt.headers)

			// R-VI9C-VP09
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", rec.Code)
			}
			if location := rec.Header().Get("Location"); location != "/" {
				t.Errorf("Location = %q, want /", location)
			}
			for _, secret := range []string{"Connect your agent", "IKIGENBA_TOKEN", "/install/claude"} {
				if strings.Contains(rec.Body.String(), secret) {
					t.Errorf("redirect response reveals install content %q:\n%s", secret, rec.Body.String())
				}
			}
		})
	}
}

func TestInstallPageOrdersAgentSnippetsAndCopyControls(t *testing.T) {
	body := signedInInstallPage(t, testServer(t))
	main := pageMain(t, body)
	wants := []string{
		`<h2>Connect your agent</h2>`,
		`<h3>Claude Code</h3>`,
		`/install/claude | bash</code>`,
		`aria-label="Copy Claude Code install command"`,
		`<h3>Codex</h3>`,
		`/install/codex | bash</code>`,
		`aria-label="Copy Codex install command"`,
		`<h3>Grok</h3>`,
		`/install/grok | bash</code>`,
		`aria-label="Copy Grok install command"`,
		`<h3>Antigravity</h3>`,
		`/install/agy | bash</code>`,
		`aria-label="Copy Antigravity install command"`,
	}

	// R-VVO9-365W
	previous := -1
	for _, want := range wants {
		at := strings.Index(main, want)
		if at < 0 { // R-Y4GE-ZIS2
			t.Fatalf("install page missing %q:\n%s", want, main)
		}
		if at <= previous { // R-QEFE-76XS
			t.Fatalf("install composition out of order at %q:\n%s", want, main)
		}
		previous = at
	}
}

func TestInstallPageExplainsPersonalAccessToken(t *testing.T) {
	body := signedInInstallPage(t, testServer(t))
	start := strings.Index(body, `<p class="install-lede">`)
	if start < 0 {
		t.Fatalf("install page missing lede paragraph:\n%s", body)
	}
	end := strings.Index(body[start:], `</p>`)
	if end < 0 {
		t.Fatalf("install page lede paragraph is not closed:\n%s", body[start:])
	}
	lede := body[start : start+end+len(`</p>`)]

	// R-VWW5-GXWL
	for _, want := range []string{
		"personal access token",
		`<code>IKIGENBA_TOKEN</code>`,
		`<a href="/profile">profile page</a>`,
	} {
		if !strings.Contains(lede, want) {
			t.Errorf("install lede missing %q:\n%s", want, lede)
		}
	}
}
