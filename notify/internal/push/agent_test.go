package push_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"eventplane/consumer"
	"eventplane/outbox"

	"notify/internal/push"

	_ "modernc.org/sqlite"
)

// TestAgentHandlerPushesOnSucceeded asserts run.succeeded fires one best-effort
// push (Title "Run succeeded", body = session_name) and the handler returns nil so
// the engine advances agent's cursor.
func TestAgentHandlerPushesOnSucceeded(t *testing.T) {
	ntfy := newNtfyMock(t)
	discard := slog.New(slog.NewJSONHandler(io.Discard, nil))
	client := push.NewClient(ntfy.srv.URL, "topic", "tok", discard)
	h := push.AgentHandler(client, discard)

	ev := consumer.Event{
		Type:    "run.succeeded",
		ID:      "01JRUNOK",
		Source:  "agent",
		Payload: json.RawMessage(`{"session_id":"s1","session_name":"nightly scan","trigger_event":"cron.nightly","scheduled_for":"2026-06-06T08:00:00Z"}`),
	}
	if err := h(context.Background(), ev); err != nil {
		t.Fatalf("run.succeeded returned %v, want nil", err)
	}
	waitFor(t, "one push", func() bool { return len(ntfy.snapshot()) == 1 })
	p := ntfy.snapshot()[0]
	if p.title != "Run succeeded" {
		t.Errorf("Title = %q, want %q", p.title, "Run succeeded")
	}
	if p.body != "nightly scan" {
		t.Errorf("body = %q, want %q", p.body, "nightly scan")
	}
}

// TestAgentHandlerPushesOnFailed asserts run.failed fires one push (Title "Run
// failed", body = session_name + error) and returns nil.
func TestAgentHandlerPushesOnFailed(t *testing.T) {
	ntfy := newNtfyMock(t)
	discard := slog.New(slog.NewJSONHandler(io.Discard, nil))
	client := push.NewClient(ntfy.srv.URL, "topic", "tok", discard)
	h := push.AgentHandler(client, discard)

	ev := consumer.Event{
		Type:    "run.failed",
		ID:      "01JRUNBAD",
		Source:  "agent",
		Payload: json.RawMessage(`{"session_id":"s1","session_name":"nightly scan","trigger_event":"cron.nightly","scheduled_for":"2026-06-06T08:00:00Z","error":"run TTL exceeded"}`),
	}
	if err := h(context.Background(), ev); err != nil {
		t.Fatalf("run.failed returned %v, want nil", err)
	}
	waitFor(t, "one push", func() bool { return len(ntfy.snapshot()) == 1 })
	p := ntfy.snapshot()[0]
	if p.title != "Run failed" {
		t.Errorf("Title = %q, want %q", p.title, "Run failed")
	}
	if p.body != "nightly scan: run TTL exceeded" {
		t.Errorf("body = %q, want %q", p.body, "nightly scan: run TTL exceeded")
	}
}

// TestAgentHandlerMalformedPayloadSkips asserts an undecodable run outcome payload
// is poison: the handler returns an ErrSkip-wrapped error (engine logs loud +
// advances) and fires no push.
func TestAgentHandlerMalformedPayloadSkips(t *testing.T) {
	ntfy := newNtfyMock(t)
	discard := slog.New(slog.NewJSONHandler(io.Discard, nil))
	client := push.NewClient(ntfy.srv.URL, "topic", "tok", discard)
	h := push.AgentHandler(client, discard)

	ev := consumer.Event{
		Type:    "run.succeeded",
		ID:      "01JRUNPOISON",
		Source:  "agent",
		Payload: json.RawMessage(`{"session_name": `), // truncated JSON
	}
	err := h(context.Background(), ev)
	if err == nil {
		t.Fatal("malformed payload returned nil, want an ErrSkip-wrapped error")
	}
	if !errors.Is(err, consumer.ErrSkip) {
		t.Fatalf("malformed payload error does not satisfy errors.Is(err, ErrSkip): %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if got := ntfy.snapshot(); len(got) != 0 {
		t.Fatalf("malformed payload fired %d pushes, want 0", len(got))
	}
}

// TestAgentHandlerNonMatchingTypeAdvances asserts a non run.* event returns nil
// (the engine advances; it is not ours) with no push.
func TestAgentHandlerNonMatchingTypeAdvances(t *testing.T) {
	ntfy := newNtfyMock(t)
	discard := slog.New(slog.NewJSONHandler(io.Discard, nil))
	client := push.NewClient(ntfy.srv.URL, "topic", "tok", discard)
	h := push.AgentHandler(client, discard)

	ev := consumer.Event{Type: "run.cancelled", ID: "01JOTHER", Source: "agent", Payload: json.RawMessage(`{}`)}
	if err := h(context.Background(), ev); err != nil {
		t.Fatalf("non-matching type returned %v, want nil", err)
	}
	time.Sleep(20 * time.Millisecond)
	if got := ntfy.snapshot(); len(got) != 0 {
		t.Fatalf("non-matching type fired %d pushes, want 0", len(got))
	}
}

// TestAgentHandlerPushFailureReturnsNil asserts the best-effort contract under a
// FAILING ntfy sink: a non-2xx (or dead) ntfy must NOT stall the feed — the
// handler returns nil regardless of the push outcome (the push is detached and
// swallows its failure).
func TestAgentHandlerPushFailureReturnsNil(t *testing.T) {
	discard := slog.New(slog.NewJSONHandler(io.Discard, nil))
	// A sink that always 500s — the push attempt is made and fails.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)
	client := push.NewClient(failing.URL, "topic", "tok", discard)
	h := push.AgentHandler(client, discard)

	ev := consumer.Event{
		Type:    "run.failed",
		ID:      "01JRUNFAIL",
		Source:  "agent",
		Payload: json.RawMessage(`{"session_id":"s1","session_name":"task","error":"boom"}`),
	}
	if err := h(context.Background(), ev); err != nil {
		t.Fatalf("push failure must not stall: handler returned %v, want nil", err)
	}
	// Give the detached push goroutine a moment to fail; the handler already
	// returned nil, which is the whole point.
	time.Sleep(20 * time.Millisecond)
}

// agentCursor reads the feed_offset cursor for a given source.
func cursorFor(t *testing.T, db *sql.DB, source string) sql.NullString {
	t.Helper()
	var cur sql.NullString
	err := db.QueryRow(`SELECT cursor FROM feed_offset WHERE source=?`, source).Scan(&cur)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("read feed_offset(%s): %v", source, err)
	}
	return cur
}

// TestTwoFeedOffsetsAdvanceIndependently is the critical correctness check for P9:
// two consumer.Run loops sharing ONE notify DB but keyed by different sources
// ("crm" and "agent") advance their OWN feed_offset rows without ever clobbering
// each other. The crm loop draining its feed must not move agent's cursor, and
// vice versa.
func TestTwoFeedOffsetsAdvanceIndependently(t *testing.T) {
	dir := t.TempDir()
	discard := slog.New(slog.NewJSONHandler(io.Discard, nil))

	// crm producer.
	crmDB := openDB(t, filepath.Join(dir, "crm.db"), outbox.SchemaSQL)
	crmOb, err := outbox.New(crmDB, outbox.Options{
		Source: "crm", DBPath: filepath.Join(dir, "crm.db"),
		GenerationPath: filepath.Join(dir, "crmgen"), Logger: discard,
	})
	if err != nil {
		t.Fatalf("crm outbox.New: %v", err)
	}
	crmFeed := httptest.NewServer(crmOb.FeedHandler())
	t.Cleanup(crmFeed.Close)

	// agent producer.
	agentDB := openDB(t, filepath.Join(dir, "agent.db"), outbox.SchemaSQL)
	agentOb, err := outbox.New(agentDB, outbox.Options{
		Source: "agent", DBPath: filepath.Join(dir, "agent.db"),
		GenerationPath: filepath.Join(dir, "agentgen"), Logger: discard,
	})
	if err != nil {
		t.Fatalf("agent outbox.New: %v", err)
	}
	agentFeed := httptest.NewServer(agentOb.FeedHandler())
	t.Cleanup(agentFeed.Close)

	// ONE shared notify consumer DB (the real deployment: both loops, one DB).
	cdb := openDB(t, filepath.Join(dir, "notify.db"), consumer.SchemaSQL)

	ntfy := newNtfyMock(t)
	client := push.NewClient(ntfy.srv.URL, "topic", "tok", discard)

	// Seed one event on each feed.
	emit(t, crmOb, crmDB, "contact.created", `{"id":"c1","display_name":"Alice"}`)
	emit(t, agentOb, agentDB, "run.succeeded", `{"session_id":"s1","session_name":"nightly"}`)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	go func() {
		done <- consumer.Run(ctx, consumer.Config{
			FeedURL: crmFeed.URL + "/feed", From: "earliest", DB: cdb,
			Source: "crm", ConsumerID: "notify", Logger: discard,
		}, push.Handler(client, discard))
	}()
	go func() {
		done <- consumer.Run(ctx, consumer.Config{
			FeedURL: agentFeed.URL + "/feed", From: "earliest", DB: cdb,
			Source: "agent", ConsumerID: "notify", Logger: discard,
		}, push.AgentHandler(client, discard))
	}()
	t.Cleanup(func() {
		cancel()
		for i := 0; i < 2; i++ {
			if err := <-done; err != nil {
				t.Errorf("consumer.Run returned non-nil: %v", err)
			}
		}
	})

	// Both cursors become non-null (each loop drained its own one event).
	waitFor(t, "crm cursor set", func() bool { return cursorFor(t, cdb, "crm").Valid })
	waitFor(t, "agent cursor set", func() bool { return cursorFor(t, cdb, "agent").Valid })
	crm1 := cursorFor(t, cdb, "crm").String
	agent1 := cursorFor(t, cdb, "agent").String

	// Advance ONLY the crm feed. agent's cursor must NOT move.
	emit(t, crmOb, crmDB, "contact.created", `{"id":"c2","display_name":"Bob"}`)
	waitFor(t, "crm cursor advances", func() bool {
		c := cursorFor(t, cdb, "crm")
		return c.Valid && c.String != crm1
	})
	if got := cursorFor(t, cdb, "agent"); !got.Valid || got.String != agent1 {
		t.Fatalf("agent cursor moved when only crm advanced: %v (want %q)", got, agent1)
	}

	// Now advance ONLY agent. crm's cursor must NOT move.
	crm2 := cursorFor(t, cdb, "crm").String
	emit(t, agentOb, agentDB, "run.failed", `{"session_id":"s1","session_name":"nightly","error":"boom"}`)
	waitFor(t, "agent cursor advances", func() bool {
		c := cursorFor(t, cdb, "agent")
		return c.Valid && c.String != agent1
	})
	if got := cursorFor(t, cdb, "crm"); !got.Valid || got.String != crm2 {
		t.Fatalf("crm cursor moved when only agent advanced: %v (want %q)", got, crm2)
	}
}
