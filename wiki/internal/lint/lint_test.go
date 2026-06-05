package lint

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"agentkit/job"
	"agentkit/provider"
	"agentkit/tools/edit"
	"agentkit/tools/write"

	"wiki/internal/db"
	"wiki/internal/ingest"
	"wiki/internal/search"
	"wiki/internal/store"
)

// stubProvider is a no-network provider.Client. Each Stream call replays the next
// canned event sequence, so an N-element sequence drives N agent turns. Mirrors
// the ingest test's stub.
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

func editToolUse(t *testing.T, id, relPath, oldStr, newStr string) provider.EventToolUse {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"file_path": relPath, "old_string": oldStr, "new_string": newStr})
	if err != nil {
		t.Fatalf("marshal edit input: %v", err)
	}
	return provider.EventToolUse{ID: id, Name: edit.Name, Input: raw}
}

// newLinter builds a Linter wired to a real on-disk store, a real BM25 index, a
// real migrated SQLite DB, and the supplied stub provider. ttl is short but ample.
// It returns the Linter, the store, and the index so tests can seed a fixture
// tree and assert on the post-lint state.
func newLinter(t *testing.T, stub provider.Client) (*Linter, *store.Store, search.Index) {
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
	l := New(st, idx, conn, newClient, Config{Model: "claude-sonnet-4-6", MaxTokens: 4096, JobTTL: 5 * time.Second})
	return l, st, idx
}

// awaitTerminal polls JobStatus until the job is terminal, failing on timeout.
func awaitTerminal(t *testing.T, l *Linter, owner, jobID string) ingest.Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		st, err := l.JobStatus(context.Background(), owner, "", jobID)
		if err == nil && st.Terminal {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for lint job %s to go terminal (last err=%v status=%q)", jobID, err, st.Status)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// seedFixtureTree lays down a small collection with a duplicate/synonymous page
// and an orphan, so the lint pass has something realistic to consolidate. The
// pages are written through the store (not through the agent) — this is the
// pre-existing tree lint operates over.
func seedFixtureTree(t *testing.T, st *store.Store, owner string) {
	t.Helper()
	const col = "default"
	if _, err := st.EnsureLayout(owner, col); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	pages := map[string]string{
		// Two synonymous concept pages (duplicates) — the canonical merge target
		// and the duplicate the lint pass should fold into it.
		"concepts/otters.md":         "---\ntype: concept\ntitle: Otters\nsource: sources/otter-note.md\ncollection: default\n---\n# Otters\n\nPlayful semi-aquatic mammals.\n",
		"concepts/river-otters.md":   "---\ntype: concept\ntitle: River otters\nsource: sources/otter-note.md\ncollection: default\n---\n# River otters\n\nA kind of otter. Duplicate of Otters.\n",
		"sources/otter-note.md":      "---\ntype: source\nkind: chat\ntitle: Otter note\ncollection: default\n---\n# Otter note\n\nFiled from a chat snippet.\n",
		// An orphan: nothing links to it and it links to nothing.
		"concepts/lonely-orphan.md":  "---\ntype: concept\ntitle: Lonely orphan\ncollection: default\n---\n# Lonely orphan\n\nNo page references this and it references none.\n",
		"index.md":                   "---\ntype: index\n---\n# Wiki index\n\n- [Otters](concepts/otters.md)\n- [River otters](concepts/river-otters.md)\n",
	}
	for rel, body := range pages {
		if err := st.WritePage(owner, col, rel, []byte(body)); err != nil {
			t.Fatalf("seed WritePage %s: %v", rel, err)
		}
	}
}

// TestLintCorePath is the acceptance-gate stub-provider test (no network). It
// seeds a fixture tree (a duplicate/synonymous page + an orphan), then drives the
// REAL Linter.Lint with a stub provider returning a few canned tool-uses
// (consolidate the duplicate into the canonical page, edit index.md to drop the
// merged entry + link the orphan, append to log.md) followed by a final text turn.
//
// It asserts: the lint job spawns and reaches terminal `succeeded`; the agent's
// writes landed INSIDE the owner+collection tree (confined); raw/ was untouched;
// ReindexCollection ran so the lint's writes are searchable; and the job-status
// path reports it terminal.
func TestLintCorePath(t *testing.T) {
	const owner = "alice@example.com"

	// What the lint agent "decides" to do, replayed as canned tool-uses:
	mergedCanonical := "---\ntype: concept\ntitle: Otters\nsource: sources/otter-note.md\ncollection: default\n---\n# Otters\n\nPlayful semi-aquatic mammals. River otters are one kind (merged from concepts/river-otters.md).\n"
	stubDuplicate := "---\ntype: concept\ntitle: River otters\nsource: sources/otter-note.md\ncollection: default\n---\n# River otters\n\nSuperseded — merged into [Otters](otters.md).\n"
	newIndex := "---\ntype: index\n---\n# Wiki index\n\n- [Otters](concepts/otters.md) (includes river otters)\n- [Lonely orphan](concepts/lonely-orphan.md)\n"
	logLine := "2026-06-04 lint: merged river-otters into otters; linked orphan from index\n"

	stub := &stubProvider{sequences: [][]provider.Event{
		// 1. write the consolidated canonical page.
		{writeToolUse(t, "w1", "concepts/otters.md", mergedCanonical), provider.EventDone{StopReason: "tool_use"}},
		// 2. supersede the duplicate with a stub pointing to the canonical (append-don't-destroy).
		{writeToolUse(t, "w2", "concepts/river-otters.md", stubDuplicate), provider.EventDone{StopReason: "tool_use"}},
		// 3. rewrite index.md (drop the merged duplicate's standalone entry; link the orphan).
		{writeToolUse(t, "w3", "index.md", newIndex), provider.EventDone{StopReason: "tool_use"}},
		// 4. append to log.md (append-only) via the edit tool is overkill; lint writes log via write here.
		{writeToolUse(t, "w4", "log.md", logLine), provider.EventDone{StopReason: "tool_use"}},
		// 5. final text turn.
		{
			provider.EventTextDelta{Text: "Merged river-otters into otters, linked the orphan, updated index and log."},
			provider.EventUsage{InputTokens: 200, OutputTokens: 40},
			provider.EventDone{StopReason: "end_turn"},
		},
	}}

	l, st, idx := newLinter(t, stub)
	seedFixtureTree(t, st, owner)

	res, err := l.Lint(context.Background(), owner, "")
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if res.JobID == "" {
		t.Fatal("Lint returned empty job id")
	}

	// The async job reached succeeded, and the status verb reports it terminal.
	st1 := awaitTerminal(t, l, owner, res.JobID)
	if st1.Status != string(job.StatusSucceeded) {
		t.Fatalf("lint job status = %q (err=%q), want succeeded", st1.Status, st1.Error)
	}
	if !st1.Terminal || st1.EndedAt == "" {
		t.Fatalf("status not terminal: %+v", st1)
	}
	if st1.UsageJSON == "" || !strings.Contains(st1.UsageJSON, "input_tokens") {
		t.Fatalf("status usage = %q, want a captured usage blob", st1.UsageJSON)
	}

	// The agent's writes landed INSIDE the owner+collection tree (confined).
	gotCanonical, err := st.ReadPage(owner, "default", "concepts/otters.md")
	if err != nil {
		t.Fatalf("ReadPage canonical: %v", err)
	}
	if string(gotCanonical) != mergedCanonical {
		t.Fatalf("canonical page not the merged content:\n%s", gotCanonical)
	}
	gotDup, err := st.ReadPage(owner, "default", "concepts/river-otters.md")
	if err != nil {
		t.Fatalf("ReadPage duplicate: %v", err)
	}
	if !strings.Contains(string(gotDup), "Superseded") {
		t.Fatalf("duplicate not superseded (append-don't-destroy):\n%s", gotDup)
	}
	gotLog, err := st.ReadPage(owner, "default", "log.md")
	if err != nil {
		t.Fatalf("ReadPage log: %v", err)
	}
	if string(gotLog) != logLine {
		t.Fatalf("log.md = %q, want %q", gotLog, logLine)
	}

	// raw/ must be untouched by lint (immutable raw invariant): the fixture wrote
	// no raw doc, and lint must not have created one.
	if rawPages, err := st.ListPages(owner, "default", "raw"); err == nil && len(rawPages) > 0 {
		t.Fatalf("lint touched raw/: %v", rawPages)
	}

	// ReindexCollection ran on success: the merged page is findable via search.
	results, err := idx.Search(context.Background(), owner, "default", "otters", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := false
	for _, h := range results.Hits {
		if h.Path == "concepts/otters.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("search after lint missing the merged canonical page; hits=%v", hitPaths(results))
	}
	if results.Index == nil {
		t.Fatal("search Index nil; index.md was not re-indexed after lint")
	}
}

func hitPaths(r search.Results) []string {
	out := make([]string, 0, len(r.Hits))
	for _, h := range r.Hits {
		out = append(out, h.Path)
	}
	return out
}

// TestLintEditToolPath confirms the lint agent's edit tool (amend-in-place) is in
// the toolset and dispatches under confinement: a single edit on a seeded page
// lands, and the job succeeds. This exercises the edit path the merge/flag
// instructions rely on (the core path test uses write; this proves edit too).
func TestLintEditToolPath(t *testing.T) {
	const owner = "dave@example.com"
	stub := &stubProvider{sequences: [][]provider.Event{
		{editToolUse(t, "e1", "concepts/otters.md", "Playful semi-aquatic mammals.", "Playful semi-aquatic mammals. (See also river otters.)"), provider.EventDone{StopReason: "tool_use"}},
		{provider.EventTextDelta{Text: "Added a cross-reference."}, provider.EventDone{StopReason: "end_turn"}},
	}}
	l, st, _ := newLinter(t, stub)
	seedFixtureTree(t, st, owner)

	res, err := l.Lint(context.Background(), owner, "")
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	s := awaitTerminal(t, l, owner, res.JobID)
	if s.Status != string(job.StatusSucceeded) {
		t.Fatalf("lint job status = %q (err=%q), want succeeded", s.Status, s.Error)
	}
	got, err := st.ReadPage(owner, "default", "concepts/otters.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if !strings.Contains(string(got), "See also river otters") {
		t.Fatalf("edit did not land in the confined tree:\n%s", got)
	}
}

// TestLintSingleFlightWithIngest proves lint and ingest serialize over the SAME
// per-(owner, collection) flight key: a lint started while an ingest job is still
// running for the same collection is rejected with job.ErrFlightInUse (and the
// converse — see the inline note). The ingest job is held running by a blocking
// stub provider so the collision is deterministic (no sleeps).
func TestLintSingleFlightWithIngest(t *testing.T) {
	const owner = "carol@example.com"

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

	// Share the ONE db + store + index between the ingest core and the linter, so
	// the single-flight gate (the global partial-unique running index on wiki_jobs)
	// is actually exercised across the two packages.
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	blocking := &blockingProvider{release: release, entered: entered}

	ingestCore := ingest.New(st, idx, conn,
		func() (provider.Client, error) { return blocking, nil },
		ingest.Config{Model: "claude-sonnet-4-6", MaxTokens: 4096, JobTTL: 5 * time.Second})

	// A linter sharing the same db; its own provider never gets a chance to run
	// because the spawn is rejected single-flight before the goroutine launches.
	linter := New(st, idx, conn,
		func() (provider.Client, error) { return &stubProvider{}, nil },
		Config{Model: "claude-sonnet-4-6", MaxTokens: 4096, JobTTL: 5 * time.Second})

	// Start an ingest; hold it running.
	ing, err := ingestCore.Ingest(context.Background(), owner, "", []byte("doc one"), store.RawMeta{})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("ingest job never entered the provider Stream")
	}

	// A lint into the SAME owner+collection must be rejected single-flight: it
	// shares ingest's flight key (ingest.FlightKey), so the running ingest row
	// blocks the lint Insert.
	lr, err := linter.Lint(context.Background(), owner, "")
	if !errors.Is(err, job.ErrFlightInUse) {
		t.Fatalf("lint while ingest running: err = %v, want ErrFlightInUse", err)
	}
	if lr.JobID != "" {
		t.Fatalf("rejected lint still returned a job id %q", lr.JobID)
	}

	// Let the ingest finish, then a lint must now succeed (the flight key is free)
	// — proving the gate is per-flight-in-progress, not a permanent block, and
	// confirming the converse direction is unblocked once the write-pass clears.
	close(release)
	// Poll the ingest job to terminal via its own core (shared db, owner-scoped).
	deadline := time.Now().Add(3 * time.Second)
	for {
		s, err := ingestCore.JobStatus(context.Background(), owner, "", ing.JobID)
		if err == nil && s.Terminal {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ingest job %s never went terminal (err=%v)", ing.JobID, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	lr2, err := linter.Lint(context.Background(), owner, "")
	if err != nil {
		t.Fatalf("lint after ingest cleared: %v", err)
	}
	if lr2.JobID == "" {
		t.Fatal("post-ingest lint returned empty job id")
	}
	awaitTerminal(t, linter, owner, lr2.JobID)
}

// blockingProvider holds the first Stream call open until release is closed, then
// returns a single final text turn. It signals entered once so the test can
// synchronize on the running job before racing a second write-pass. Mirrors the
// ingest test's blockingProvider.
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
