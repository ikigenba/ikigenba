package ingest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentkit/job"
	"agentkit/provider"

	"wiki/internal/store"
)

// TestIngestURLCorePath is the Task-4.2 acceptance-gate test (no real network).
// It serves a fixture HTML page from an httptest.Server, drives the REAL
// Core.IngestURL (so the actual fetch+extract path runs against the local
// server), and asserts the URL ingest reuses the SAME async core as
// wiki_ingest_text: the fetched+extracted markdown is written to the immutable
// raw store with the URL as source provenance, the heading/body text survived the
// extraction (script/style/nav noise stripped), the async integration job ran to
// succeeded (stub provider), and reindex made the filed page searchable.
func TestIngestURLCorePath(t *testing.T) {
	const owner = "dave@example.com"

	const pageHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Beavers &amp; Dams</title>
  <script>track();</script>
  <style>.x{}</style>
</head>
<body>
  <nav><a href="/">skip me</a></nav>
  <h1>Beavers</h1>
  <p>Beavers are industrious rodents that build dams.</p>
</body>
</html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(pageHTML))
	}))
	defer srv.Close()
	pageURL := srv.URL + "/wiki/beavers"

	// The integration agent (stub provider) files one source page, updates index,
	// appends to log, then ends — mirroring TestIngestCorePath.
	sourcePage := "---\ntype: source\nkind: web\ntitle: Beavers & Dams\nsource: " + pageURL + "\ncollection: default\n---\n# Beavers\n\nFiled from a web page.\n"
	indexPage := "---\ntype: index\n---\n# Wiki index\n\n- [Beavers](sources/beavers.md)\n"
	logLine := "2026-06-04 ingested beavers page from url\n"

	stub := &stubProvider{sequences: [][]provider.Event{
		{writeToolUse(t, "w1", "sources/beavers.md", sourcePage), provider.EventDone{StopReason: "tool_use"}},
		{writeToolUse(t, "w2", "index.md", indexPage), provider.EventDone{StopReason: "tool_use"}},
		{writeToolUse(t, "w3", "log.md", logLine), provider.EventDone{StopReason: "tool_use"}},
		{
			provider.EventTextDelta{Text: "Filed beavers page."},
			provider.EventUsage{InputTokens: 80, OutputTokens: 15},
			provider.EventDone{StopReason: "end_turn"},
		},
	}}

	core, st, idx := newCore(t, stub)

	// No title/source supplied: the core defaults source to the URL and title to
	// the page <title>.
	res, err := core.IngestURL(context.Background(), owner, "", pageURL, store.RawMeta{
		Tags: []string{"web"},
	})
	if err != nil {
		t.Fatalf("IngestURL: %v", err)
	}
	if res.JobID == "" {
		t.Fatal("IngestURL returned empty job id")
	}

	// Raw doc: written immutably with the URL as source provenance, and the
	// extracted markdown (heading + body) — NOT the raw HTML / noise.
	rawBytes, err := st.ReadRaw(owner, "default", res.Sha256)
	if err != nil {
		t.Fatalf("ReadRaw: %v", err)
	}
	rawStr := string(rawBytes)
	if !strings.HasPrefix(rawStr, "---\n") {
		t.Fatalf("raw doc missing frontmatter fence:\n%s", rawStr)
	}
	for _, want := range []string{res.Sha256, "ingested_at:", pageURL, "# Beavers", "industrious rodents"} {
		if !strings.Contains(rawStr, want) {
			t.Fatalf("raw doc missing %q:\n%s", want, rawStr)
		}
	}
	// Extraction quality: HTML tags and script/style/nav noise must NOT survive.
	for _, noise := range []string{"<h1>", "<script", "track()", ".x{}", "skip me"} {
		if strings.Contains(rawStr, noise) {
			t.Fatalf("raw doc contains un-stripped noise %q:\n%s", noise, rawStr)
		}
	}

	// Source provenance defaulted to the URL; title defaulted to the page <title>.
	var gotSource, gotTitle string
	if err := core.db.QueryRow(
		`SELECT source, title FROM wiki_ingest WHERE owner=? AND sha256=?`, owner, res.Sha256,
	).Scan(&gotSource, &gotTitle); err != nil {
		t.Fatalf("read wiki_ingest provenance: %v", err)
	}
	if gotSource != pageURL {
		t.Fatalf("provenance source = %q, want the URL %q", gotSource, pageURL)
	}
	if gotTitle != "Beavers & Dams" {
		t.Fatalf("provenance title = %q, want the page <title>", gotTitle)
	}

	// The async job reached succeeded.
	st1 := awaitTerminal(t, core, owner, res.JobID)
	if st1.Status != string(job.StatusSucceeded) {
		t.Fatalf("job status = %q (err=%q), want succeeded", st1.Status, st1.Error)
	}

	// Reindex ran on success: the filed page is findable via search.
	results, err := idx.Search(context.Background(), owner, "default", "beavers", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, h := range results.Hits {
		if h.Path == "sources/beavers.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("search hits %v missing the filed source page", hitPaths(results))
	}
}

// TestIngestURL_StubFetch confirms the fetch func is injectable so IngestURL can
// be driven fully hermetically (no server at all), and that a caller-supplied
// title/source override the extractor-derived defaults.
func TestIngestURL_StubFetch(t *testing.T) {
	const owner = "erin@example.com"

	stub := &stubProvider{sequences: [][]provider.Event{
		{provider.EventTextDelta{Text: "nothing to change"}, provider.EventDone{StopReason: "end_turn"}},
	}}
	core, st, _ := newCore(t, stub)

	var gotURL string
	core.SetFetch(func(_ context.Context, rawURL string) ([]byte, string, error) {
		gotURL = rawURL
		return []byte("# Stubbed\n\nbody text\n"), "Derived Title", nil
	})

	res, err := core.IngestURL(context.Background(), owner, "", "https://example.com/x", store.RawMeta{
		Title:  "Caller Title", // explicit title wins over "Derived Title"
		Source: "custom-src",   // explicit source wins over the URL
	})
	if err != nil {
		t.Fatalf("IngestURL: %v", err)
	}
	if gotURL != "https://example.com/x" {
		t.Fatalf("fetch got url %q", gotURL)
	}
	awaitTerminal(t, core, owner, res.JobID)

	rawBytes, err := st.ReadRaw(owner, "default", res.Sha256)
	if err != nil {
		t.Fatalf("ReadRaw: %v", err)
	}
	rawStr := string(rawBytes)
	if !strings.Contains(rawStr, "body text") {
		t.Fatalf("raw doc missing stubbed body:\n%s", rawStr)
	}
	var gotSource, gotTitle string
	if err := core.db.QueryRow(
		`SELECT source, title FROM wiki_ingest WHERE owner=? AND sha256=?`, owner, res.Sha256,
	).Scan(&gotSource, &gotTitle); err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	if gotSource != "custom-src" || gotTitle != "Caller Title" {
		t.Fatalf("caller-supplied source/title did not win: source=%q title=%q", gotSource, gotTitle)
	}
}
