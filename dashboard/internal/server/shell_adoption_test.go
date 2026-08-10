package server

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

type renderedShellPage struct {
	name string
	path string
	body string
}

func renderedSignedInShellPages(t *testing.T) (*http.Server, serverDeps, *http.Cookie, []renderedShellPage) {
	t.Helper()
	srv, deps := patTestServer(t)
	cookie := mintSession(t, deps, "owner@int.ikigenba.com")
	headers := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}
	pages := make([]renderedShellPage, 0, 4)
	for _, page := range []struct {
		name string
		path string
	}{
		{name: "dashboard", path: "/"},
		{name: "install", path: "/install"},
		{name: "metrics", path: "/metrics"},
		{name: "profile", path: "/profile"},
	} {
		rec := do(t, srv, http.MethodGet, "https://int.ikigenba.com"+page.path, headers)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200\n%s", page.path, rec.Code, rec.Body.String())
		}
		pages = append(pages, renderedShellPage{name: page.name, path: page.path, body: rec.Body.String()})
	}
	return srv, deps, cookie, pages
}

func TestShellFooterReportsVersionOnSignedInPagesOnly(t *testing.T) {
	srv, _, _, pages := renderedSignedInShellPages(t)
	versionFooter := regexp.MustCompile(`<footer>dashboard v\d+\.\d+\.\d+[^<]*</footer>`)

	// R-VN4Y-ERZ1
	for _, page := range pages {
		if got := strings.Count(page.body, "<footer>"); got != 1 {
			t.Errorf("GET %s footer count = %d, want 1:\n%s", page.path, got, page.body)
		}
		if !versionFooter.MatchString(page.body) {
			t.Errorf("GET %s footer does not report a semantic dashboard version:\n%s", page.path, page.body)
		}
	}
	loggedOut := do(t, srv, http.MethodGet, "https://int.ikigenba.com/", nil)
	if loggedOut.Code != http.StatusOK {
		t.Fatalf("logged-out GET / status = %d, want 200", loggedOut.Code)
	}
	if strings.Contains(loggedOut.Body.String(), "<footer") {
		t.Errorf("logged-out GET / rendered a footer:\n%s", loggedOut.Body.String())
	}
}

func TestSignedInPagesShareCompleteShellHeader(t *testing.T) {
	srv, _, _, pages := renderedSignedInShellPages(t)
	logoLink := regexp.MustCompile(`<a href="/" aria-label="Dashboard home"><img src="/static/logo\.png" class="logo" alt=""></a>`)

	// R-VOCU-SJPQ
	for _, page := range pages {
		header := bannerChrome(t, page.body)
		if !logoLink.MatchString(header) {
			t.Errorf("%s header missing linked dashboard logo:\n%s", page.name, header)
		}
		for _, link := range []string{`href="/">Dashboard</a>`, `href="/install">Install</a>`, `href="/metrics">Metrics</a>`} {
			if !strings.Contains(stripAriaCurrent(header), link) {
				t.Errorf("%s header missing nav link %q:\n%s", page.name, link, header)
			}
		}
		identity := elementWithClass(t, header, "nav", "identity")
		if !strings.Contains(identity, `<form method="POST" action="/logout">`) {
			t.Errorf("%s identity controls missing POST /logout form:\n%s", page.name, identity)
		}
		avatarAt := strings.LastIndex(identity, `class="avatar"`)
		if avatarAt < 0 || strings.Contains(identity[avatarAt:], "<form") || strings.Contains(identity[avatarAt:], "<button") {
			t.Errorf("%s avatar is not the last identity control:\n%s", page.name, identity)
		}
	}
	logo := do(t, srv, http.MethodGet, "https://int.ikigenba.com/static/logo.png", nil)
	if logo.Code != http.StatusOK || logo.Body.Len() == 0 {
		t.Errorf("GET /static/logo.png = status %d, body length %d; want 200 and non-empty", logo.Code, logo.Body.Len())
	}
}

func TestShellNavMarksOnlyTheNamedPageCurrent(t *testing.T) {
	_, _, _, pages := renderedSignedInShellPages(t)
	wants := map[string]string{
		"dashboard": `href="/" aria-current="page">Dashboard</a>`,
		"install":   `href="/install" aria-current="page">Install</a>`,
		"metrics":   `href="/metrics" aria-current="page">Metrics</a>`,
	}

	// R-VQSN-K374
	for _, page := range pages {
		nav := elementWithClass(t, bannerChrome(t, page.body), "nav", "site-nav")
		want, hasCurrentNav := wants[page.name]
		wantCount := 0
		if hasCurrentNav {
			wantCount = 1
			if !strings.Contains(nav, want) {
				t.Errorf("%s nav does not mark its own link current; want %q:\n%s", page.name, want, nav)
			}
		}
		if got := strings.Count(nav, `aria-current="page"`); got != wantCount {
			t.Errorf("%s current nav-link count = %d, want %d:\n%s", page.name, got, wantCount, nav)
		}
	}
}

func TestShowOnceAndAuthorizePagesUseTheirSessionAppropriateShells(t *testing.T) {
	srv, _, cookie, _ := renderedSignedInShellPages(t)
	created := doForm(t, srv, "https://int.ikigenba.com/pat", url.Values{"label": {"Codex on laptop"}}, map[string]string{
		"Cookie": cookie.Name + "=" + cookie.Value,
		"Origin": "https://int.ikigenba.com",
	})
	if created.Code != http.StatusOK {
		t.Fatalf("POST /pat status = %d, want 200\n%s", created.Code, created.Body.String())
	}
	createdHeader := bannerChrome(t, created.Body.String())

	ts, _, client := newOAuthTest(t)
	clientID := registerClient(t, ts, client)
	resp, err := client.Get(authorizeURL(ts, clientID, nil))
	if err != nil {
		t.Fatalf("GET authorize chooser: %v", err)
	}
	defer resp.Body.Close()
	chooserBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read authorize chooser: %v", err)
	}
	chooser := string(chooserBytes)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize chooser status = %d, want 200\n%s", resp.StatusCode, chooser)
	}
	chooserHeader := bannerChrome(t, chooser)

	// R-VT8G-BMOI
	for _, want := range []string{`src="/static/logo.png"`, `class="site-nav"`, `action="/logout"`, `class="avatar"`} {
		if !strings.Contains(createdHeader, want) {
			t.Errorf("PAT show-once header missing %q:\n%s", want, createdHeader)
		}
	}
	if strings.Count(created.Body.String(), "<footer>") != 1 {
		t.Errorf("PAT show-once page does not have exactly one footer:\n%s", created.Body.String())
	}
	if !strings.Contains(chooserHeader, `src="/static/logo.png"`) {
		t.Errorf("authorize chooser header missing logo:\n%s", chooserHeader)
	}
	for _, forbidden := range []string{`class="site-nav"`, `action="/logout"`, `class="avatar"`} {
		if strings.Contains(chooserHeader, forbidden) {
			t.Errorf("authorize chooser exposes session control %q:\n%s", forbidden, chooserHeader)
		}
	}
	if strings.Count(chooser, "<footer>") != 1 {
		t.Errorf("authorize chooser does not have exactly one footer:\n%s", chooser)
	}
}

func stripAriaCurrent(s string) string {
	return strings.ReplaceAll(s, ` aria-current="page"`, "")
}

func elementWithClass(t *testing.T, body, tag, class string) string {
	t.Helper()
	start := strings.Index(body, `<`+tag+` class="`+class+`"`)
	if start < 0 {
		t.Fatalf("body missing <%s class=%q>:\n%s", tag, class, body)
	}
	end := strings.Index(body[start:], `</`+tag+`>`)
	if end < 0 {
		t.Fatalf("%s.%s is not closed:\n%s", tag, class, body[start:])
	}
	return body[start : start+end+len(tag)+3]
}
