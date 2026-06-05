package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fixtureHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Otters &amp; Rivers</title>
  <style>body { color: red; }</style>
  <script>console.log("noise");</script>
</head>
<body>
  <nav><a href="/home">Home</a></nav>
  <h1>Otters</h1>
  <p>Otters are playful semi-aquatic mammals &mdash; they love rivers.</p>
  <p>See the <a href="https://example.org/rivers">rivers page</a> for more.</p>
  <ul>
    <li>Eurasian otter</li>
    <li>Sea otter</li>
  </ul>
  <pre>otter := NewOtter()</pre>
</body>
</html>`

// TestHTMLToMarkdown checks the extractor produces reasonable markdown: the
// heading, paragraph text, link, list items, and code survive; script/style/nav
// noise is dropped; entities are unescaped; <title> is picked up.
func TestHTMLToMarkdown(t *testing.T) {
	md, title := htmlToMarkdown([]byte(fixtureHTML))
	out := string(md)

	if title != "Otters & Rivers" {
		t.Fatalf("title = %q, want %q", title, "Otters & Rivers")
	}

	wantContains := []string{
		"# Otters",                                    // h1 → markdown heading
		"playful semi-aquatic mammals",                // paragraph body survives
		"— they love rivers",                          // &mdash; unescaped to —
		"[rivers page](https://example.org/rivers)",   // link → markdown link
		"- Eurasian otter",                            // list item
		"- Sea otter",                                 // list item
		"otter := NewOtter()",                         // <pre> code survives
	}
	for _, w := range wantContains {
		if !strings.Contains(out, w) {
			t.Fatalf("markdown missing %q:\n%s", w, out)
		}
	}

	// Noise must be dropped.
	for _, noise := range []string{"console.log", "color: red", "Home"} {
		if strings.Contains(out, noise) {
			t.Fatalf("markdown should not contain dropped noise %q:\n%s", noise, out)
		}
	}
}

// TestFetchAndExtract_HTML drives the real FetchAndExtract over an httptest
// server (no real network) serving the fixture HTML, and asserts it fetches +
// extracts to markdown with the <title>.
func TestFetchAndExtract_HTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fixtureHTML))
	}))
	defer srv.Close()

	md, title, err := FetchAndExtract(context.Background(), srv.URL+"/otters")
	if err != nil {
		t.Fatalf("FetchAndExtract: %v", err)
	}
	if title != "Otters & Rivers" {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(string(md), "# Otters") || !strings.Contains(string(md), "playful semi-aquatic") {
		t.Fatalf("extracted markdown unexpected:\n%s", md)
	}
}

// TestFetchAndExtract_PlainPassThrough confirms a non-HTML body (text/markdown)
// is passed through verbatim with a URL-derived title.
func TestFetchAndExtract_PlainPassThrough(t *testing.T) {
	const plain = "# Already markdown\n\nNothing to extract here.\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte(plain))
	}))
	defer srv.Close()

	md, title, err := FetchAndExtract(context.Background(), srv.URL+"/notes/my-note.md")
	if err != nil {
		t.Fatalf("FetchAndExtract: %v", err)
	}
	if string(md) != plain {
		t.Fatalf("plain body not passed through verbatim: %q", md)
	}
	if title != "my note.md" {
		t.Fatalf("URL-derived title = %q, want %q", title, "my note.md")
	}
}

// TestFetchAndExtract_RejectsNonHTTPScheme asserts the scheme allow-list rejects
// a file:// URL (and never touches the filesystem/network).
func TestFetchAndExtract_RejectsNonHTTPScheme(t *testing.T) {
	for _, bad := range []string{"file:///etc/passwd", "ftp://example.com/x", "data:text/html,<h1>x</h1>"} {
		_, _, err := FetchAndExtract(context.Background(), bad)
		if err == nil {
			t.Fatalf("FetchAndExtract(%q) = nil error, want scheme rejection", bad)
		}
		if !strings.Contains(err.Error(), "scheme") {
			t.Fatalf("FetchAndExtract(%q) error = %v, want a scheme-rejection error", bad, err)
		}
	}
}
