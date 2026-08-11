package completion

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ikigenba/agentkit"

	"prompts/internal/admit"
	"prompts/internal/calls"
	"prompts/internal/prompt"
)

func TestStoreEnsureInboxAndAckLifecycle(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	store := NewStore(database, func() time.Time { return now })
	first, created, err := store.Ensure(t.Context(), Item{Consumer: "service:wiki", Key: "same", Origin: "service:wiki", Name: "wiki.extract", Request: `{}`})
	if err != nil || !created {
		t.Fatalf("first Ensure = %#v, %v, %v", first, created, err)
	}
	existing, created, err := store.Ensure(t.Context(), Item{Consumer: "service:wiki", Key: "same", Origin: "service:other", Name: "other.work", Request: `{"different":true}`})
	if err != nil || created || existing.ID != first.ID || existing.Origin != first.Origin {
		t.Fatalf("idempotent Ensure = %#v, %v, %v", existing, created, err)
	}
	claimed, err := store.Claim(t.Context())
	if err != nil || claimed.ID != first.ID || claimed.Status != StatusRunning {
		t.Fatalf("Claim = %#v, %v", claimed, err)
	}
	if err := store.Complete(t.Context(), first.ID, `{"ok":true}`, `{}`, 0.5); err != nil {
		t.Fatal(err)
	}
	inbox, err := store.Inbox(t.Context(), "service:wiki")
	if err != nil || len(inbox) != 1 || inbox[0].Result != `{"ok":true}` {
		t.Fatalf("Inbox = %#v, %v", inbox, err)
	}
	if err := store.Ack(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(t.Context(), first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Ack = %v, want not found", err)
	}
}

// R-JIPJ-VP27
func TestStoreBootRequeuesRunningItemForReexecution(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	store := NewStore(database, func() time.Time { return now })
	request, _ := json.Marshal(Request{Model: completionTestModel, Messages: []Message{{Role: "user", Text: "work"}}})
	item, _, err := store.Ensure(t.Context(), Item{Consumer: "service:wiki", Key: "restart", Origin: "service:wiki", Name: "wiki.extract", Request: string(request)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(t.Context()); err != nil {
		t.Fatal(err)
	}
	count, err := store.RequeueRunning(t.Context())
	if err != nil || count != 1 {
		t.Fatalf("RequeueRunning = %d, %v", count, err)
	}
	requeued, err := store.Get(t.Context(), item.ID)
	if err != nil || requeued.Status != StatusQueued || requeued.Error != "" {
		t.Fatalf("requeued item = %#v, %v", requeued, err)
	}

	provider := &scriptedProvider{results: []*agentkit.RoundTrip{testRoundTrip(`{"status":"ok","result":"recovered"}`, agentkit.Usage{Total: 1}, nil)}}
	build := func(prompt.Config, func(string) string) (agentkit.Provider, error) { return provider, nil }
	executor := NewExecutor(store, calls.NewStore(database), admit.New(2, 1), build, func(string) string { return "test-key" }, func() bool { return false }, time.Hour)
	didWork, err := executor.ExecuteNext(t.Context())
	if err != nil || !didWork {
		t.Fatalf("ExecuteNext = %v, %v", didWork, err)
	}
	terminal, err := store.Get(t.Context(), item.ID)
	if err != nil || terminal.Status != StatusDone || terminal.Result != `"recovered"` {
		t.Fatalf("re-executed item = %#v, %v", terminal, err)
	}
}

// R-JOT1-SJRO
func TestStoreSweepDeletesOnlyExpiredTerminalItems(t *testing.T) {
	database := openCompletionTestDB(t)
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store := NewStore(database, func() time.Time { return now })
	seed := func(id, status string, finished time.Time) {
		t.Helper()
		finishedAt := ""
		if !finished.IsZero() {
			finishedAt = formatTime(finished)
		}
		_, err := database.ExecContext(t.Context(), `INSERT INTO completions
			(id,consumer,origin,key,name,request,status,created_at,started_at,finished_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, id, "service:wiki", "service:wiki", id, "wiki.extract", `{}`, status,
			formatTime(now.Add(-10*24*time.Hour)), formatTime(now.Add(-9*24*time.Hour)), finishedAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	seed("expired-done", StatusDone, now.Add(-RetentionTTL-time.Second))
	seed("young-failed", StatusFailed, now.Add(-RetentionTTL+time.Second))
	seed("old-queued", StatusQueued, time.Time{})
	seed("old-running", StatusRunning, time.Time{})

	deleted, err := store.Sweep(t.Context())
	if err != nil || deleted != 1 {
		t.Fatalf("Sweep = %d, %v", deleted, err)
	}
	for _, id := range []string{"young-failed", "old-queued", "old-running"} {
		if _, err := store.Get(t.Context(), id); err != nil {
			t.Errorf("%s was swept: %v", id, err)
		}
	}
	if _, err := store.Get(t.Context(), "expired-done"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired terminal survived: %v", err)
	}
}

func TestStoreInboxIsPartitionedTerminalOldestFirstAndCapped(t *testing.T) {
	database := openCompletionTestDB(t)
	store := NewStore(database, time.Now)
	for i := range 102 {
		id := fmt.Sprintf("item-%03d", i)
		consumer := "service:wiki"
		if i == 101 {
			consumer = "service:other"
		}
		_, err := database.ExecContext(t.Context(), `INSERT INTO completions
			(id,consumer,origin,key,name,request,status,created_at,finished_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			id, consumer, "service:wiki", id, "wiki.extract", `{}`, StatusDone,
			formatTime(time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC)), formatTime(time.Now()))
		if err != nil {
			t.Fatal(err)
		}
	}
	inbox, err := store.Inbox(t.Context(), "service:wiki")
	if err != nil || len(inbox) != 100 || inbox[0].ID != "item-000" || inbox[99].ID != "item-099" {
		t.Fatalf("Inbox = len %d, %v", len(inbox), err)
	}
}
