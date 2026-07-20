package prompt

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	appkitdb "appkit/db"

	"prompts/internal/db"
	"prompts/internal/ids"
)

func openMigratedTestDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "prompts.db"))
	if err != nil {
		t.Fatalf("appkitdb.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	migs, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatalf("appkitdb.LoadMigrations: %v", err)
	}
	if err := appkitdb.Migrate(ctx, conn, migs); err != nil {
		t.Fatalf("appkitdb.Migrate: %v", err)
	}
	return conn
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	conn := openMigratedTestDB(t, context.Background())
	return NewStore(conn)
}

func seedPrompt(t *testing.T, store *Store, owner string) Prompt {
	t.Helper()
	now := store.nowStr()
	sess := Prompt{
		ID:         ids.NewULID(),
		OwnerEmail: owner,
		Name:       "n",
		UserPrompt: "p",
		Config:     Config{Provider: "anthropic", Model: "claude-haiku-4-5"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := store.InsertPrompt(context.Background(), sess); err != nil {
		t.Fatalf("InsertPrompt: %v", err)
	}
	return sess
}

func TestStoreGetNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := seedPrompt(t, store, ownerA)

	if _, err := store.GetPrompt(ctx, "nobody@example.com", sess.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign owner: want ErrNotFound, got %v", err)
	}
	if _, err := store.GetPrompt(ctx, ownerA, "nonexistent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing id: want ErrNotFound, got %v", err)
	}

	got, err := store.GetPrompt(ctx, ownerA, sess.ID)
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if got.Config.Model != "claude-haiku-4-5" {
		t.Fatalf("config round-trip: %+v", got.Config)
	}
}

func TestStoreLatestRunNilWhenNone(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	sess := seedPrompt(t, store, ownerA)

	last, err := store.GetLatestRun(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetLatestRun: %v", err)
	}
	if last != nil {
		t.Fatalf("want nil, got %+v", last)
	}
}

func TestStoreLatestRunNewest(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	sess := seedPrompt(t, store, ownerA)

	older := Run{ID: ids.NewULID(), PromptID: sess.ID, Status: RunSucceeded, StartedAt: "2026-01-01T00:00:00Z", LogPath: "a"}
	newer := Run{ID: ids.NewULID(), PromptID: sess.ID, Status: RunRunning, StartedAt: "2026-02-01T00:00:00Z", LogPath: "b"}
	if err := store.InsertRun(ctx, older); err != nil {
		t.Fatalf("InsertRun older: %v", err)
	}
	if err := store.InsertRun(ctx, newer); err != nil {
		t.Fatalf("InsertRun newer: %v", err)
	}

	last, err := store.GetLatestRun(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetLatestRun: %v", err)
	}
	if last == nil || last.ID != newer.ID {
		t.Fatalf("want newest %s, got %+v", newer.ID, last)
	}
}

func TestBrowsePromptsFiltersOrdersPaginatesAndCounts(t *testing.T) {
	// R-ZZVE-DK4O
	store := newTestStore(t)
	ctx := context.Background()
	seed := func(id, owner, name, updated string) {
		t.Helper()
		if err := store.InsertPrompt(ctx, Prompt{
			ID: id, OwnerEmail: owner, Name: name, UserPrompt: "body",
			Config:    Config{Provider: "anthropic", Model: "claude-haiku-4-5"},
			CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: updated,
		}); err != nil {
			t.Fatalf("InsertPrompt %s: %v", id, err)
		}
	}
	seed("p1", "first@example.com", "Alpha report", "2026-01-01T01:00:00Z")
	seed("p2", "alpha.owner@example.com", "Quarterly", "2026-01-01T02:00:00Z")
	seed("p3", "other@example.com", "ALPHABET", "2026-01-01T03:00:00Z")
	seed("p4", "other@example.com", "Unrelated", "2026-01-01T04:00:00Z")

	got, total, err := store.BrowsePrompts(ctx, BrowseFilter{Q: "aLpHa", Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("BrowsePrompts: %v", err)
	}
	if total != 3 {
		t.Fatalf("filtered total = %d, want 3", total)
	}
	if ids := promptIDs(got); !reflect.DeepEqual(ids, []string{"p2", "p1"}) {
		t.Fatalf("paged prompt ids = %v, want [p2 p1]", ids)
	}
}

func TestBrowseRunsFiltersOrdersPaginatesAndCounts(t *testing.T) {
	// R-013A-RBVD
	store := newTestStore(t)
	ctx := context.Background()
	runs := []Run{
		{ID: "r1", PromptID: "prompt-a", OwnerEmail: "one@example.com", PromptName: "Alpha", Status: RunSucceeded, StartedAt: "2026-01-01T01:00:00Z", LogPath: "1"},
		{ID: "r2", PromptID: "prompt-a", OwnerEmail: "alice@example.com", PromptName: "Beta", Status: RunFailed, StartedAt: "2026-01-01T02:00:00Z", LogPath: "2"},
		{ID: "r3", PromptID: "prompt-b", OwnerEmail: "other@example.com", PromptName: "Gamma", Status: RunSucceeded, StartedAt: "2026-01-01T03:00:00Z", LogPath: "3"},
		{ID: "r4", PromptID: "prompt-a", OwnerEmail: "alice@example.com", PromptName: "Alpha Plus", Status: RunSucceeded, StartedAt: "2026-01-01T04:00:00Z", LogPath: "4"},
	}
	for _, run := range runs {
		if err := store.InsertRun(ctx, run); err != nil {
			t.Fatalf("InsertRun %s: %v", run.ID, err)
		}
	}

	checks := []struct {
		name string
		f    BrowseFilter
		want []string
	}{
		{"q", BrowseFilter{Q: "ALIce"}, []string{"r4", "r2"}},
		{"status", BrowseFilter{Status: RunFailed}, []string{"r2"}},
		{"prompt", BrowseFilter{PromptID: "prompt-b"}, []string{"r3"}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			got, total, err := store.BrowseRuns(ctx, check.f)
			if err != nil {
				t.Fatalf("BrowseRuns: %v", err)
			}
			if ids := runIDs(got); !reflect.DeepEqual(ids, check.want) || total != len(check.want) {
				t.Fatalf("ids/total = %v/%d, want %v/%d", ids, total, check.want, len(check.want))
			}
		})
	}

	got, total, err := store.BrowseRuns(ctx, BrowseFilter{
		Q: "alpha", Status: RunSucceeded, PromptID: "prompt-a", Limit: 1, Offset: 1,
	})
	if err != nil {
		t.Fatalf("combined BrowseRuns: %v", err)
	}
	if ids := runIDs(got); !reflect.DeepEqual(ids, []string{"r1"}) || total != 2 {
		t.Fatalf("combined paged ids/total = %v/%d, want [r1]/2", ids, total)
	}
}

func promptIDs(prompts []Prompt) []string {
	result := make([]string, len(prompts))
	for i, prompt := range prompts {
		result[i] = prompt.ID
	}
	return result
}

func runIDs(runs []Run) []string {
	result := make([]string, len(runs))
	for i, run := range runs {
		result[i] = run.ID
	}
	return result
}

func TestStoreUpdateRunTerminal(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	sess := seedPrompt(t, store, ownerA)
	run := Run{ID: ids.NewULID(), PromptID: sess.ID, Status: RunRunning, StartedAt: store.nowStr(), LogPath: "x"}
	if err := store.InsertRun(ctx, run); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	if err := store.UpdateRunTerminal(ctx, run.ID, RunSucceeded, store.nowStr(), `{"tokens":5}`, ""); err != nil {
		t.Fatalf("UpdateRunTerminal: %v", err)
	}
	got, err := store.GetLatestRun(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetLatestRun: %v", err)
	}
	if got.Status != RunSucceeded || got.UsageJSON != `{"tokens":5}` || got.EndedAt == "" {
		t.Fatalf("terminal not applied: %+v", got)
	}
}

func TestStoreDeleteIsTombstone(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	sess := seedPrompt(t, store, ownerA)
	run := Run{ID: ids.NewULID(), PromptID: sess.ID, OwnerEmail: ownerA, Status: RunSucceeded, StartedAt: store.nowStr(), LogPath: "x"}
	if err := store.InsertRun(ctx, run); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	if err := store.DeletePrompt(ctx, ownerA, sess.ID); err != nil {
		t.Fatalf("DeletePrompt: %v", err)
	}
	// Tombstone (A3): the prompt is gone but its run survives — there is no
	// cascade. The run is still addressable by run_id.
	if _, err := store.GetPrompt(ctx, ownerA, sess.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("prompt should be gone, got err=%v", err)
	}
	got, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun after tombstone: %v", err)
	}
	if got.ID != run.ID || got.OwnerEmail != ownerA {
		t.Fatalf("run should survive tombstone, got %+v", got)
	}
	last, err := store.GetLatestRun(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetLatestRun: %v", err)
	}
	if last == nil || last.ID != run.ID {
		t.Fatalf("latest run should survive tombstone, got %+v", last)
	}
}

func TestSweepRunning(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	withRun := seedPrompt(t, store, ownerA)

	run := Run{ID: ids.NewULID(), PromptID: withRun.ID, Status: RunRunning, StartedAt: store.nowStr(), LogPath: "x"}
	if err := store.InsertRun(ctx, run); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	n, err := store.SweepRunning(ctx)
	if err != nil {
		t.Fatalf("SweepRunning: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept count: want 1, got %d", n)
	}

	// The orphaned running run is marked failed. There is no prompt status to
	// flip — SweepRunning touches runs only.
	got, err := store.GetLatestRun(ctx, withRun.ID)
	if err != nil {
		t.Fatalf("GetLatestRun: %v", err)
	}
	if got.Status != RunFailed || got.Error != "interrupted by restart" || got.EndedAt == "" {
		t.Fatalf("swept run: %+v", got)
	}
}
