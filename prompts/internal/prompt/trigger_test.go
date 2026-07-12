package prompt

import (
	"context"
	"errors"
	"testing"
)

func TestValidateTriggerCanonicalFamilies(t *testing.T) {
	// R-6KLN-PNOI
	for _, filter := range []string{"create/bills/**", ":create/**", "*:create/**", "drop*:create/**", "github:push/**"} {
		if _, err := validateTrigger(filter); !errors.Is(err, ErrValidation) {
			t.Errorf("validateTrigger(%q) = %v, want ErrValidation", filter, err)
		}
	}
	if source, err := validateTrigger("dropbox:create/bills/**"); err != nil || source != "dropbox" {
		t.Fatalf("valid filter = %q, %v", source, err)
	}
	// R-6LTK-3FF7
	for _, filter := range []string{"dropbox:create/bills/**/*.pdf", "dropbox:*", "cron:tick/some-schedule-nobody-declared"} {
		if _, err := validateTrigger(filter); err != nil {
			t.Errorf("validateTrigger(%q) = %v", filter, err)
		}
	}
	for _, filter := range []string{"dropbox:nosuchkind/**", "dropbox:create/["} {
		if _, err := validateTrigger(filter); !errors.Is(err, ErrValidation) {
			t.Errorf("validateTrigger(%q) = %v, want ErrValidation", filter, err)
		}
	}
}

// TestStore_SetTrigger_MultiSource asserts the composite-key contract: a prompt
// may hold many canonical filter bindings; a repeat is an upsert.
// key is an upsert (no duplicate row), while distinct keys insert distinct rows.
func TestStore_SetTrigger_MultiSource(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	p := seedPrompt(t, store, "o@example.com")

	mustSet(t, store, p.ID, "cron", "cron.nightly")
	mustSet(t, store, p.ID, "dropbox", "file.created")
	mustSet(t, store, p.ID, "scripts", "scripts.succeeded")
	// Repeat of an existing composite key — upsert, not a new row.
	mustSet(t, store, p.ID, "cron", "cron.nightly")

	got, err := store.ListTriggers(ctx, p.ID)
	if err != nil {
		t.Fatalf("ListTriggers: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 distinct bindings, got %d: %+v", len(got), got)
	}

	var n int
	if err := store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM prompt_triggers WHERE prompt_id = ?`, p.ID,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected exactly 3 trigger rows, got %d", n)
	}
}

// TestStore_PromptsForEvent_FanOut asserts the (source, type) fan-out returns
// every prompt whose binding matches and excludes others — including a glob
// binding and a same-type-different-source non-match.
func TestStore_PromptsForEvent_FanOut(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	a := seedPrompt(t, store, "o@example.com")
	b := seedPrompt(t, store, "o@example.com")
	c := seedPrompt(t, store, "o@example.com")
	d := seedPrompt(t, store, "o@example.com")

	mustSet(t, store, a.ID, "dropbox", "file.created")
	mustSet(t, store, b.ID, "dropbox", "file.*") // glob matches file.created
	mustSet(t, store, c.ID, "dropbox", "file.deleted")
	mustSet(t, store, d.ID, "crm", "contact.created")

	got, err := store.PromptsForEvent(ctx, "dropbox", "file.created")
	if err != nil {
		t.Fatalf("PromptsForEvent: %v", err)
	}
	ids := map[string]bool{}
	for _, id := range got {
		ids[id] = true
	}
	if !ids[a.ID] || !ids[b.ID] {
		t.Fatalf("expected a (exact) and b (glob) to match file.created, got %v", got)
	}
	if ids[c.ID] {
		t.Fatalf("file.deleted binding leaked into file.created fan-out")
	}
	if ids[d.ID] {
		t.Fatalf("crm binding leaked into a dropbox fan-out")
	}
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 matches, got %d: %v", len(got), got)
	}
}

// TestStore_ClearTrigger removes one binding (leaving the others) and a no-match
// is ErrNotFound.
func TestStore_ClearTrigger(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	p := seedPrompt(t, store, "o@example.com")
	mustSet(t, store, p.ID, "cron", "cron.nightly")
	mustSet(t, store, p.ID, "dropbox", "file.created")

	if err := store.ClearTrigger(ctx, p.ID, "cron", "cron.nightly"); err != nil {
		t.Fatalf("ClearTrigger: %v", err)
	}
	got, err := store.ListTriggers(ctx, p.ID)
	if err != nil {
		t.Fatalf("ListTriggers: %v", err)
	}
	if len(got) != 1 || got[0].Source != "dropbox" {
		t.Fatalf("expected only the dropbox binding to remain, got %+v", got)
	}
	// Clearing an absent binding is ErrNotFound.
	if err := store.ClearTrigger(ctx, p.ID, "cron", "cron.nightly"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound clearing absent binding, got %v", err)
	}
}

// TestStore_DeleteTriggers removes ALL of a prompt's bindings and is a no-op
// (no error) when the prompt has none.
func TestStore_DeleteTriggers(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	p := seedPrompt(t, store, "o@example.com")
	mustSet(t, store, p.ID, "cron", "cron.nightly")
	mustSet(t, store, p.ID, "dropbox", "file.created")

	if err := store.DeleteTriggers(ctx, p.ID); err != nil {
		t.Fatalf("DeleteTriggers: %v", err)
	}
	got, err := store.ListTriggers(ctx, p.ID)
	if err != nil {
		t.Fatalf("ListTriggers: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no bindings after DeleteTriggers, got %+v", got)
	}
	// No-op on a prompt with no bindings.
	if err := store.DeleteTriggers(ctx, p.ID); err != nil {
		t.Fatalf("DeleteTriggers on empty must be a no-op, got %v", err)
	}
}

func mustSet(t *testing.T, store *Store, promptID, source, eventFilter string) {
	t.Helper()
	if err := store.SetTrigger(context.Background(), Trigger{
		PromptID: promptID, Source: source, EventFilter: eventFilter,
	}); err != nil {
		t.Fatalf("SetTrigger: %v", err)
	}
}
