package prompt

import (
	"context"
	"errors"
	"testing"
)

func finishTestRun(t *testing.T, store *Store, runID string) {
	t.Helper()
	if err := store.FinishRun(context.Background(), FinishRunInput{RunID: runID, Status: RunSucceeded, EndedAt: "2026-08-03T00:00:00Z"}); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
}

func TestExecutedSnapshotDisagreesWithUpdatedLivePrompt(t *testing.T) {
	// R-ZOYX-Y9WA
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	ctx := context.Background()
	svc, store, _, _ := newTestService(t)
	p, err := svc.Create(ctx, ownerA, ownerA, CreateInput{Name: "original", UserPrompt: "original text", Config: validConfig()})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Run(ctx, ownerA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	finishTestRun(t, store, run.ID)
	updatedConfig := Config{Provider: "anthropic", Model: testAnthropicSonnet, MaxTokens: 321}
	if _, err := svc.Update(ctx, ownerA, p.ID, UpdateInput{Name: ptr("updated"), UserPrompt: ptr("updated text"), Config: ptr(updatedConfig)}); err != nil {
		t.Fatal(err)
	}

	frozen, err := svc.RunExecuted(ctx, ownerA, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	live, err := svc.LoadFromPrompt(ctx, ownerA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.UserPrompt != "original text" || frozen.Config.Model != testAnthropicModel || frozen.Name != "original" {
		t.Fatalf("frozen execution changed: %+v", frozen)
	}
	if live.UserPrompt != "updated text" || live.Config.Model != testAnthropicSonnet || live.Config.MaxTokens != 321 || live.Name != "updated" {
		t.Fatalf("live prompt not updated: %+v", live)
	}
}

func TestDeletedPromptRunsRemainListableAndExecutableSnapshotReadable(t *testing.T) {
	// R-ZREQ-PTDO
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	ctx := context.Background()
	svc, store, _, _ := newTestService(t)
	originalConfig := Config{Provider: "anthropic", Model: testAnthropicModel, MaxTokens: 123}
	p, err := svc.Create(ctx, ownerA, ownerA, CreateInput{Name: "gone", UserPrompt: "surviving text", Config: originalConfig})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Run(ctx, ownerA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	finishTestRun(t, store, run.ID)
	if err := svc.Delete(ctx, ownerA, p.ID); err != nil {
		t.Fatal(err)
	}

	runs, err := svc.RunList(ctx, ownerA, p.ID)
	if err != nil || len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs after delete: %+v err=%v", runs, err)
	}
	frozen, err := svc.RunExecuted(ctx, ownerA, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.UserPrompt != "surviving text" || frozen.Config.Model != testAnthropicModel || frozen.Config.MaxTokens != 123 {
		t.Fatalf("snapshot after delete: %+v", frozen)
	}
	foreignRuns, err := svc.RunList(ctx, ownerB, p.ID)
	if err != nil || len(foreignRuns) != 0 {
		t.Fatalf("foreign runs: %+v err=%v", foreignRuns, err)
	}
}

func TestRunExecutedOwnerScope(t *testing.T) {
	// R-ZSMN-3L4D
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	ctx := context.Background()
	svc, _, _, _ := newTestService(t)
	p := mustCreate(t, svc, ownerA)
	run, err := svc.Run(ctx, ownerA, p.ID)
	if err != nil {
		t.Fatal(err)
	}

	foreign, err := svc.RunExecuted(ctx, ownerB, run.ID)
	if !errors.Is(err, ErrNotFound) || foreign != (Executed{}) {
		t.Fatalf("foreign RunExecuted = %+v, %v; want zero, ErrNotFound", foreign, err)
	}
	owned, err := svc.RunExecuted(ctx, ownerA, run.ID)
	if err != nil || owned.UserPrompt != "do a thing" || owned.Config.Model != testAnthropicModel {
		t.Fatalf("owned RunExecuted = %+v, %v", owned, err)
	}
}
