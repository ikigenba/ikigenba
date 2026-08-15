package server

import (
	"dashboard/ui"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"testing"
)

func TestServedTokensCSSUsesWikiSystemFontStacks(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/static/tokens.css", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// R-VKP5-N8HN
	for _, declaration := range []string{
		`--font-display: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;`,
		`--font-body:    system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;`,
		`--font-mono:    ui-monospace, "SFMono-Regular", "Cascadia Code", "Liberation Mono", Menlo, Consolas, monospace;`,
	} {
		if !strings.Contains(body, declaration) {
			t.Errorf("tokens CSS missing system-font declaration %q:\n%s", declaration, body)
		}
	}
	for _, forbidden := range []string{"@font-face", "url("} {
		if strings.Contains(body, forbidden) {
			t.Errorf("tokens CSS contains retired font-loading text %q:\n%s", forbidden, body)
		}
	}
}

func TestEmbeddedUIAndRenderedHeadsContainNoFontLoads(t *testing.T) {
	// R-VLX2-108C
	err := fs.WalkDir(ui.Files, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(path.Ext(name), ".woff2") {
			t.Errorf("embedded UI contains retired web font %q", name)
		}
		if !entry.IsDir() && (path.Ext(name) == ".html" || path.Ext(name) == ".tmpl") {
			body, readErr := fs.ReadFile(ui.Files, name)
			if readErr != nil {
				return readErr
			}
			assertNoFontPreload(t, name, string(body))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded UI: %v", err)
	}

	srv := testServer(t)
	sess := liveSession(t, srv)
	signedIn := map[string]string{"Cookie": sess.Name + "=" + sess.Value}
	for _, tc := range []struct {
		name    string
		target  string
		headers map[string]string
	}{
		{name: "logged-out home", target: "https://int.ikigenba.com/"},
		{name: "logged-in home", target: "https://int.ikigenba.com/", headers: signedIn},
		{name: "metrics", target: "https://int.ikigenba.com/metrics", headers: signedIn},
		{name: "profile", target: "https://int.ikigenba.com/profile", headers: signedIn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, srv, "GET", tc.target, tc.headers)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			assertNoFontPreload(t, tc.name, documentHead(t, rec.Body.String()))
		})
	}
}

func TestServedAppCSSDefinesStableWikiShellColumn(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/static/app.css", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// R-VS0J-XUXT
	htmlRule := cssRule(t, body, "html")
	if !strings.Contains(htmlRule, "scrollbar-gutter: stable;") {
		t.Errorf("html rule does not reserve a stable scrollbar gutter:\n%s", htmlRule)
	}
	shellRule := cssRule(t, body, "header,\nmain,\nfooter")
	for _, declaration := range []string{
		"max-width: var(--layout-max-width);",
		"padding-right: var(--layout-gutter);",
		"padding-left: var(--layout-gutter);",
	} {
		if !strings.Contains(shellRule, declaration) {
			t.Errorf("shared header/main/footer rule missing %q:\n%s", declaration, shellRule)
		}
	}
}

func TestStaticRouteServesWikiLogo(t *testing.T) {
	srv := testServer(t)
	rec := do(t, srv, "GET", "https://int.ikigenba.com/static/logo.png", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("logo response body is empty")
	}
}

func assertNoFontPreload(t *testing.T, surface, head string) {
	t.Helper()
	lower := strings.ToLower(head)
	if strings.Contains(lower, `rel="preload"`) && strings.Contains(lower, `as="font"`) {
		t.Errorf("%s contains a font preload:\n%s", surface, head)
	}
}

func documentHead(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<head>")
	if start < 0 {
		t.Fatalf("body missing <head>:\n%s", body)
	}
	end := strings.Index(body[start:], "</head>")
	if end < 0 {
		t.Fatalf("body missing </head>:\n%s", body[start:])
	}
	return body[start : start+end+len("</head>")]
}
