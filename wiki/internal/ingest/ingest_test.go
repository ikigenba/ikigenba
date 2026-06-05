package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"agentkit/job"
	"agentkit/provider"
	"agentkit/tools/write"

	"wiki/internal/db"
	"wiki/internal/search"
	"wiki/internal/store"
)

// stubProvider is a no-network provider.Client. Each Stream call replays the next
// canned event sequence, so an N-element sequence drives N agent turns. It records
// the call count so a test can prove the loop round-tripped the tools.
type stubProvider struct {
	sequences [][]provider.Event
	calls     int
}

func (s *stubProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	if s.calls >= len(s.sequences) {
		return nil, &provider.Error{Kind: provider.ErrUnknown, Msg: "stubProvider exhausted"}
	}
	evs := s.sequences[s.calls]
	s.calls++
	ch := make(chan provider.Event, len(evs))
	for _, ev := range evs {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func writeToolUse(t *testing.T, id, relPath, content string) provider.EventToolUse {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"file_path": relPath, "content": content})
	if err != nil {
		t.Fatalf("marshal write input: %v", err)
	}
	return provider.EventToolUse{ID: id, Name: write.Name, Input: raw}
}

// newCore builds a Core wired to a real on-disk store, a real BM25 index, a real
// migrated SQLite DB, and the supplied stub provider. ttl is short but ample.
func newCore(t *testing.T, stub provider.Client) (*Core, *store.Store, search.Index) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	idx := search.NewBM25Index(st.SearchIndexPath)
	t.Cleanup(func() { idx.Close() })

	conn, err := db.Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(context.Background(), conn); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	newClient := func() (provider.Client, error) { return stub, nil }
	core := New(st, idx, conn, newClient, Config{Model: "claude-sonnet-4-6", MaxTokens: 4096, JobTTL: 5 * time.Second})
	return core, st, idx
}

// awaitTerminal polls JobStatus until the job is terminal, failing on timeout.
func awaitTerminal(t *testing.T, core *Core, owner, jobID string) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		st, err := core.JobStatus(context.Background(), owner, "", jobID)
		if err == nil && st.Terminal {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for job %s to go terminal (last err=%v status=%q)", jobID, err, st.Status)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestIngestCorePath is the acceptance-gate core-path test (no network). A stub
// provider returns three tool-use turns (write a source page, update index.md,
// append log.md) then a final text turn. It drives the REAL Core.Ingest and
// asserts: the raw doc is immutable + frontmatter-stamped; the async job spawns
// and reaches succeeded; the agent's writes land INSIDE the owner+collection tree
// (confined); ReindexCollection ran so the new page is findable via the search
// index; and the job-status read reports the run terminal.
func TestIngestCorePath(t *testing.T) {
	const (
		owner   = "alice@example.com"
		content = "Otters are playful semi-aquatic mammals."
	)

	sourcePage := "---\ntype: source\nkind: chat\ntitle: Otter note\ncollection: default\n---\n# Otter note\n\nFiled from a chat snippet. See concepts/otters.md.\n"
	indexPage := "---\ntype: index\n---\n# Wiki index\n\n- [Otter note](sources/otter-note.md)\n- mentions otter\n"
	logLine := "2026-06-04 ingested otter note (1 source page, index updated)\n"

	stub := &stubProvider{sequences: [][]provider.Event{
		{writeToolUse(t, "w1", "sources/otter-note.md", sourcePage), provider.EventDone{StopReason: "tool_use"}},
		{writeToolUse(t, "w2", "index.md", indexPage), provider.EventDone{StopReason: "tool_use"}},
		{writeToolUse(t, "w3", "log.md", logLine), provider.EventDone{StopReason: "tool_use"}},
		{
			provider.EventTextDelta{Text: "Filed otter note into sources/, updated index and log."},
			provider.EventUsage{InputTokens: 100, OutputTokens: 20},
			provider.EventDone{StopReason: "end_turn"},
		},
	}}

	core, st, idx := newCore(t, stub)

	res, err := core.Ingest(context.Background(), owner, "", []byte(content), store.RawMeta{
		Title: "Otter note", Source: "chat", Tags: []string{"animals"},
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.JobID == "" {
		t.Fatal("Ingest returned empty job id")
	}
	if res.AlreadyHad {
		t.Fatal("first ingest reported AlreadyHad=true")
	}

	// Raw doc is immutable + frontmatter-stamped (sha256 keyed).
	rawBytes, err := st.ReadRaw(owner, "default", res.Sha256)
	if err != nil {
		t.Fatalf("ReadRaw: %v", err)
	}
	rawStr := string(rawBytes)
	if !strings.HasPrefix(rawStr, "---\n") {
		t.Fatalf("raw doc missing frontmatter fence:\n%s", rawStr)
	}
	for _, want := range []string{res.Sha256, "ingested_at:", "Otter note", "chat"} {
		if !strings.Contains(rawStr, want) {
			t.Fatalf("raw frontmatter missing %q:\n%s", want, rawStr)
		}
	}
	if !strings.HasSuffix(rawStr, content) {
		t.Fatalf("raw doc body is not the original content:\n%s", rawStr)
	}

	// The async job reached succeeded, and the status verb reports it terminal.
	st1 := awaitTerminal(t, core, owner, res.JobID)
	if st1.Status != string(job.StatusSucceeded) {
		t.Fatalf("job status = %q (err=%q), want succeeded", st1.Status, st1.Error)
	}
	if !st1.Terminal || st1.EndedAt == "" {
		t.Fatalf("status not terminal: %+v", st1)
	}
	if st1.UsageJSON == "" || !strings.Contains(st1.UsageJSON, "input_tokens") {
		t.Fatalf("status usage = %q, want a captured usage blob", st1.UsageJSON)
	}

	// The agent's writes landed INSIDE the owner+collection tree (confined).
	src, err := st.ReadPage(owner, "default", "sources/otter-note.md")
	if err != nil {
		t.Fatalf("ReadPage source: %v", err)
	}
	if string(src) != sourcePage {
		t.Fatalf("source page contents mismatch:\n%s", src)
	}
	logBytes, err := st.ReadPage(owner, "default", "log.md")
	if err != nil {
		t.Fatalf("ReadPage log: %v", err)
	}
	if string(logBytes) != logLine {
		t.Fatalf("log.md = %q, want %q", logBytes, logLine)
	}

	// ReindexCollection ran on success: the new source page is findable via search.
	results, err := idx.Search(context.Background(), owner, "default", "otter", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results.Hits) == 0 {
		t.Fatal("search after ingest returned no hits; reindex-on-success did not run")
	}
	found := false
	for _, h := range results.Hits {
		if h.Path == "sources/otter-note.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("search hits %v missing the filed source page", hitPaths(results))
	}
	if results.Index == nil {
		t.Fatal("search Index nil; index.md was not indexed")
	}

	// The provenance ledger recorded the ingest.
	var n int
	if err := core.db.QueryRow(`SELECT COUNT(*) FROM wiki_ingest WHERE owner=? AND sha256=?`, owner, res.Sha256).Scan(&n); err != nil {
		t.Fatalf("count wiki_ingest: %v", err)
	}
	if n != 1 {
		t.Fatalf("wiki_ingest rows = %d, want 1", n)
	}
}

func hitPaths(r search.Results) []string {
	out := make([]string, 0, len(r.Hits))
	for _, h := range r.Hits {
		out = append(out, h.Path)
	}
	return out
}

// TestReIngestIdempotent confirms re-ingesting identical bytes is a no-op on the
// raw store (AlreadyHad) and in wiki_ingest (one row), while still spawning a new
// integration job (re-running the pass over identical bytes is safe).
func TestReIngestIdempotent(t *testing.T) {
	const owner = "bob@example.com"
	body := []byte("same bytes twice")

	finalOnly := func() *stubProvider {
		return &stubProvider{sequences: [][]provider.Event{
			{provider.EventTextDelta{Text: "nothing to change"}, provider.EventDone{StopReason: "end_turn"}},
		}}
	}

	core, _, _ := newCore(t, finalOnly())
	first, err := core.Ingest(context.Background(), owner, "", body, store.RawMeta{Title: "x"})
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	awaitTerminal(t, core, owner, first.JobID)

	// Re-ingest the SAME bytes (new stub since the first is exhausted).
	core.newClient = func() (provider.Client, error) { return finalOnly(), nil }
	second, err := core.Ingest(context.Background(), owner, "", body, store.RawMeta{Title: "x"})
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if !second.AlreadyHad {
		t.Fatal("re-ingest of identical bytes did not report AlreadyHad")
	}
	if second.Sha256 != first.Sha256 {
		t.Fatalf("re-ingest sha256 %q != first %q", second.Sha256, first.Sha256)
	}
	if second.JobID == "" {
		t.Fatal("re-ingest did not spawn a new integration job")
	}
	awaitTerminal(t, core, owner, second.JobID)

	var n int
	if err := core.db.QueryRow(`SELECT COUNT(*) FROM wiki_ingest WHERE owner=? AND sha256=?`, owner, first.Sha256).Scan(&n); err != nil {
		t.Fatalf("count wiki_ingest: %v", err)
	}
	if n != 1 {
		t.Fatalf("wiki_ingest rows after re-ingest = %d, want 1 (idempotent)", n)
	}
}

// TestSingleFlightRejectsConcurrent proves a second ingest into the same
// (owner, collection) while one is still running is rejected with ErrFlightInUse.
// The first job is held running by a stub whose Stream blocks until released, so
// the second ingest is guaranteed to collide.
func TestSingleFlightRejectsConcurrent(t *testing.T) {
	const owner = "carol@example.com"

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	blocking := &blockingProvider{release: release, entered: entered}

	core, _, _ := newCore(t, blocking)

	first, err := core.Ingest(context.Background(), owner, "", []byte("doc one"), store.RawMeta{})
	if err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	// Wait until the first job's agent loop is actually in flight (Stream entered),
	// so the running row is committed before we attempt the second ingest.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first job never entered the provider Stream")
	}

	// Second ingest into the SAME owner+collection must be rejected single-flight.
	second, err := core.Ingest(context.Background(), owner, "", []byte("doc two"), store.RawMeta{})
	if !errors.Is(err, job.ErrFlightInUse) {
		t.Fatalf("second Ingest err = %v, want ErrFlightInUse", err)
	}
	if second.JobID != "" {
		t.Fatalf("rejected ingest still returned a job id %q", second.JobID)
	}
	// The bytes for doc two were still persisted immutably (raw write precedes the
	// single-flight gate), so AlreadyHad is false but sha256 is set.
	if second.Sha256 == "" {
		t.Fatal("rejected ingest did not persist the raw bytes")
	}

	// Let the first job finish so the test exits cleanly.
	close(release)
	awaitTerminal(t, core, owner, first.JobID)
}

// blockingProvider holds the first Stream call open until release is closed,
// then returns a single final text turn. It signals entered once so the test can
// synchronize on the running job before racing a second ingest.
type blockingProvider struct {
	release <-chan struct{}
	entered chan<- struct{}
	armed   bool
}

func (b *blockingProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Event, error) {
	if !b.armed {
		b.armed = true
		select {
		case b.entered <- struct{}{}:
		default:
		}
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, &provider.Error{Kind: provider.ErrUnknown, Msg: "cancelled"}
		}
	}
	ch := make(chan provider.Event, 2)
	ch <- provider.EventTextDelta{Text: "done"}
	ch <- provider.EventDone{StopReason: "end_turn"}
	close(ch)
	return ch, nil
}
