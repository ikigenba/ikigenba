package repos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSuccessPrunesItsWorktreeButFailureRetainsCrimeScene(t *testing.T) {
	// R-FWST-R7DG
	fixture := newReaperFixture(t, 24*time.Hour)
	fixture.addRepo(t, "alpha")
	now := fixture.clock.Now()
	succeeded := Session{ID: "succeeded", RepoName: "alpha", OwnerEmail: "owner@example.com", Attempt: 1, Branch: "success", Instructions: "land", Status: StatusSucceeded, CreatedAt: now.Add(-time.Hour), EndedAt: timePointer(now), LogPath: fixture.sessionFile("succeeded", "output.jsonl")}
	failed := Session{ID: "failed", RepoName: "alpha", OwnerEmail: "owner@example.com", Attempt: 1, Branch: "failure", Instructions: "inspect", Status: StatusFailed, CreatedAt: now.Add(-time.Hour), EndedAt: timePointer(now), LogPath: fixture.sessionFile("failed", "output.jsonl")}
	for _, session := range []Session{succeeded, failed} {
		fixture.addSession(t, session)
		fixture.addWorktree(t, session)
		fixture.writeDurableFiles(t, session.ID)
	}
	if err := fixture.reaper.Success(context.Background(), succeeded); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, fixture.sessionFile(succeeded.ID, "worktree"))
	assertExists(t, fixture.sessionFile(failed.ID, "worktree"))
	for _, name := range []string{"instructions.md", "output.jsonl", "check.log"} {
		assertExists(t, fixture.sessionFile(succeeded.ID, name))
		assertExists(t, fixture.sessionFile(failed.ID, name))
	}
}

func TestSupersessionAndAgeSweepRemoveOnlyEligibleWorktrees(t *testing.T) {
	// R-FY0Q-4Z45
	fixture := newReaperFixture(t, 24*time.Hour)
	fixture.addRepo(t, "alpha")
	now := fixture.clock.Now()
	issue := 8
	older := Session{ID: "older", RepoName: "alpha", OwnerEmail: "owner@example.com", IssueNumber: &issue, Attempt: 1, Branch: "issue-8", Instructions: "first", Status: StatusFailed, CreatedAt: now.Add(-2 * time.Hour), EndedAt: timePointer(now.Add(-time.Hour)), LogPath: fixture.sessionFile("older", "output.jsonl")}
	newer := Session{ID: "newer", RepoName: "alpha", OwnerEmail: "owner@example.com", IssueNumber: &issue, Attempt: 2, Branch: "issue-8-2", Instructions: "second", Status: StatusSucceeded, CreatedAt: now.Add(-time.Hour), EndedAt: timePointer(now), LogPath: fixture.sessionFile("newer", "output.jsonl")}
	for _, session := range []Session{older, newer} {
		fixture.addSession(t, session)
		fixture.addWorktree(t, session)
		fixture.writeDurableFiles(t, session.ID)
	}
	if err := fixture.reaper.Success(context.Background(), newer); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, fixture.sessionFile(older.ID, "worktree"))
	assertExists(t, fixture.sessionFile(older.ID, "output.jsonl"))

	for _, session := range []Session{
		{ID: "expired", RepoName: "alpha", OwnerEmail: "owner@example.com", Attempt: 1, Branch: "expired", Instructions: "old", Status: StatusFailed, CreatedAt: now.Add(-49 * time.Hour), EndedAt: timePointer(now.Add(-48 * time.Hour)), LogPath: fixture.sessionFile("expired", "output.jsonl")},
		{ID: "young", RepoName: "alpha", OwnerEmail: "owner@example.com", Attempt: 1, Branch: "young", Instructions: "new", Status: StatusFailed, CreatedAt: now.Add(-time.Hour), EndedAt: timePointer(now), LogPath: fixture.sessionFile("young", "output.jsonl")},
	} {
		fixture.addSession(t, session)
		fixture.addWorktree(t, session)
		fixture.writeDurableFiles(t, session.ID)
	}
	if err := fixture.reaper.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, fixture.sessionFile("expired", "worktree"))
	assertExists(t, fixture.sessionFile("expired", "output.jsonl"))
	assertExists(t, fixture.sessionFile("young", "worktree"))
	assertExists(t, fixture.sessionFile("young", "instructions.md"))
}

func TestDeleteRepoPrunesCloneAndWorktreesButRetainsSessionRecordAndTranscript(t *testing.T) {
	// R-G0GI-WILJ
	fixture := newReaperFixture(t, 24*time.Hour)
	fixture.addRepo(t, "doomed")
	now := fixture.clock.Now()
	session := Session{ID: "history", RepoName: "doomed", OwnerEmail: "owner@example.com", Attempt: 1, Branch: "history", Instructions: "remember", Status: StatusFailed, CreatedAt: now.Add(-time.Hour), EndedAt: timePointer(now), LogPath: fixture.sessionFile("history", "output.jsonl")}
	fixture.addSession(t, session)
	fixture.addWorktree(t, session)
	fixture.writeDurableFiles(t, session.ID)
	wantTranscript := []byte("{\"type\":\"text\",\"text\":\"first\"}\n{\"type\":\"text\",\"text\":\"second\"}\n")
	if err := os.WriteFile(session.LogPath, wantTranscript, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fixture.reaper.DeleteRepo(context.Background(), "doomed"); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, filepath.Join(fixture.stateRoot, "repos", "doomed"))
	assertMissing(t, fixture.sessionFile(session.ID, "worktree"))
	got, err := fixture.store.GetSession(context.Background(), session.ID)
	if err != nil || !reflect.DeepEqual(got, session) {
		t.Fatalf("retained session = %#v, %v; want %#v", got, err, session)
	}
	transcript, err := os.ReadFile(got.LogPath)
	if err != nil || !reflect.DeepEqual(transcript, wantTranscript) {
		t.Fatalf("retained transcript = %q, %v", transcript, err)
	}
	if _, err := fixture.store.GetRepo(context.Background(), "doomed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted repository lookup error = %v", err)
	}
}

type reaperFixture struct {
	store     *Store
	git       *Git
	clock     fixedClock
	stateRoot string
	reaper    *Reaper
}

func newReaperFixture(t *testing.T, ttl time.Duration) *reaperFixture {
	t.Helper()
	store, _ := migratedStore(t)
	stateRoot := t.TempDir()
	clock := fixedClock{value: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)}
	git := NewGit(filepath.Join(stateRoot, "repos"), &staticTokenSource{token: "fixture"})
	reaper, err := NewReaper(store, git, clock, stateRoot, ttl)
	if err != nil {
		t.Fatal(err)
	}
	return &reaperFixture{store: store, git: git, clock: clock, stateRoot: stateRoot, reaper: reaper}
}

func (f *reaperFixture) addRepo(t *testing.T, name string) {
	t.Helper()
	remote := newBareRemote(t)
	if err := f.git.Clone(context.Background(), fileURL(remote), name); err != nil {
		t.Fatal(err)
	}
	repo := Repo{Name: name, OwnerEmail: "owner@example.com", CloneURL: fileURL(remote), DefaultBranch: "main", CreatedAt: f.clock.Now()}
	if err := f.store.InsertRepo(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
}

func (f *reaperFixture) addSession(t *testing.T, session Session) {
	t.Helper()
	if err := f.store.InsertSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
}

func (f *reaperFixture) addWorktree(t *testing.T, session Session) {
	t.Helper()
	path := f.sessionFile(session.ID, "worktree")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.git.WorktreeAdd(context.Background(), session.RepoName, session.Branch, path, "origin/main"); err != nil {
		t.Fatal(err)
	}
}

func (f *reaperFixture) writeDurableFiles(t *testing.T, id string) {
	t.Helper()
	for _, name := range []string{"instructions.md", "output.jsonl", "check.log"} {
		if err := os.WriteFile(f.sessionFile(id, name), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *reaperFixture) sessionFile(id, name string) string {
	return filepath.Join(f.stateRoot, "sessions", id, name)
}

func timePointer(value time.Time) *time.Time { return &value }

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s to be absent, got %v", path, err)
	}
}
