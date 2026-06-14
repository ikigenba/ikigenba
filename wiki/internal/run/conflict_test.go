package run

import (
	"context"
	"errors"
	"testing"

	"wiki/internal/integrate"
)

// commitManifest builds a one-subject document manifest writing a page.
func docManifest(subjectID, body string, baseVersion int, superseded []string) *integrate.Manifest {
	return &integrate.Manifest{
		Subjects: []integrate.Subject{{
			Type: integrate.TypeEntity, Name: "Acme", Aliases: []string{"acme"},
			SubjectID: subjectID, TargetPage: subjectID, BaseVersion: baseVersion,
			PageTitle: "Acme", PageBody: body, Superseded: superseded,
			Claims: []integrate.Claim{{Text: "c", Cites: []string{"doc-1"}}},
		}},
	}
}

// TestCommitVersionGuardConflict — once a page exists at version N, a commit whose
// manifest carries a STALE base version (the lost-update race) returns a
// *ConflictError naming the subject and never advances the page.
func TestCommitVersionGuardConflict(t *testing.T) {
	s, conn := newStore(t)
	insertInbox(t, conn, "doc-1", "document", "mcp:x")
	insertInbox(t, conn, "doc-2", "document", "mcp:y")

	// First commit creates the page at version 0.
	r1, _ := s.Begin(context.Background(), "document-pass", "doc-1")
	if err := s.Commit(context.Background(), r1, "doc-1", docManifest("subj-1", "v1 body [doc-1]", 0, nil), true); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	// Update it to version 1 (another run).
	r2, _ := s.Begin(context.Background(), "document-pass", "doc-2")
	if err := s.Commit(context.Background(), r2, "doc-2", docManifest("subj-1", "v2 body [doc-1] [doc-2]", 0, nil), true); err != nil {
		t.Fatalf("second commit (version 0→1): %v", err)
	}

	// A third commit with a STALE base version 0 (page is now at 1) → conflict.
	r3, _ := s.Begin(context.Background(), "document-pass", "doc-1")
	err := s.Commit(context.Background(), r3, "doc-1", docManifest("subj-1", "stale body [doc-1] [doc-2]", 0, nil), true)
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("stale base version should yield *ConflictError, got %v", err)
	}
	if ce.Subject != "subj-1" {
		t.Fatalf("conflict subject = %q, want subj-1", ce.Subject)
	}
	// The page body is unchanged (the conflicting transaction rolled back).
	var body string
	conn.QueryRow(`SELECT body FROM pages WHERE subject='subj-1'`).Scan(&body)
	if body != "v2 body [doc-1] [doc-2]" {
		t.Fatalf("page advanced under a stale commit: %q", body)
	}
}

// TestCommitVersionGuardCorrectVersionSucceeds — a commit at the CURRENT base
// version succeeds and bumps the version (the happy update path under the guard).
func TestCommitVersionGuardCorrectVersionSucceeds(t *testing.T) {
	s, conn := newStore(t)
	insertInbox(t, conn, "doc-1", "document", "mcp:x")
	insertInbox(t, conn, "doc-2", "document", "mcp:y")

	r1, _ := s.Begin(context.Background(), "document-pass", "doc-1")
	if err := s.Commit(context.Background(), r1, "doc-1", docManifest("subj-1", "v1 [doc-1]", 0, nil), true); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Base version 0 is current → succeeds, page goes to version 1.
	r2, _ := s.Begin(context.Background(), "document-pass", "doc-2")
	if err := s.Commit(context.Background(), r2, "doc-2", docManifest("subj-1", "v2 [doc-1] [doc-2]", 0, nil), true); err != nil {
		t.Fatalf("guarded update at correct version: %v", err)
	}
	var v int
	conn.QueryRow(`SELECT version FROM pages WHERE subject='subj-1'`).Scan(&v)
	if v != 1 {
		t.Fatalf("version = %d, want 1", v)
	}
}

// TestCommitCitationGateRejectsUndeclaredLoss — the §6.1 gate fails the commit when
// the rewrite drops an old citation that is not declared superseded; the page is
// untouched (the transaction never commits).
func TestCommitCitationGateRejectsUndeclaredLoss(t *testing.T) {
	s, conn := newStore(t)
	insertInbox(t, conn, "doc-1", "document", "mcp:x")
	insertInbox(t, conn, "doc-2", "document", "mcp:y")

	r1, _ := s.Begin(context.Background(), "document-pass", "doc-1")
	if err := s.Commit(context.Background(), r1, "doc-1",
		docManifest("subj-1", "Fact A. [doc-1] Fact B. [other]", 0, nil), true); err != nil {
		t.Fatalf("create: %v", err)
	}
	// A rewrite that drops [other] without declaring it superseded → failed call.
	r2, _ := s.Begin(context.Background(), "document-pass", "doc-2")
	err := s.Commit(context.Background(), r2, "doc-2",
		docManifest("subj-1", "Fact A only. [doc-1]", 0, nil), true)
	if err == nil {
		t.Fatal("undeclared citation loss must fail the commit")
	}
	var ce *ConflictError
	if errors.As(err, &ce) {
		t.Fatalf("citation loss is a failed call, not a conflict: %v", err)
	}
	// The page still carries the original (rolled-back) body.
	var body string
	conn.QueryRow(`SELECT body FROM pages WHERE subject='subj-1'`).Scan(&body)
	if body != "Fact A. [doc-1] Fact B. [other]" {
		t.Fatalf("page mutated despite failed gate: %q", body)
	}
}

// TestCommitCitationGateDeclaredSupersededPasses — dropping a citation IS allowed
// when merge declares it in the per-page superseded carrier.
func TestCommitCitationGateDeclaredSupersededPasses(t *testing.T) {
	s, conn := newStore(t)
	insertInbox(t, conn, "doc-1", "document", "mcp:x")
	insertInbox(t, conn, "doc-2", "document", "mcp:y")

	r1, _ := s.Begin(context.Background(), "document-pass", "doc-1")
	if err := s.Commit(context.Background(), r1, "doc-1",
		docManifest("subj-1", "Old. [doc-1] New. [other]", 0, nil), true); err != nil {
		t.Fatalf("create: %v", err)
	}
	r2, _ := s.Begin(context.Background(), "document-pass", "doc-2")
	if err := s.Commit(context.Background(), r2, "doc-2",
		docManifest("subj-1", "New. [other]", 0, []string{"doc-1"}), true); err != nil {
		t.Fatalf("declared-superseded drop should commit: %v", err)
	}
	var body string
	conn.QueryRow(`SELECT body FROM pages WHERE subject='subj-1'`).Scan(&body)
	if body != "New. [other]" {
		t.Fatalf("page body = %q, want the rewritten body", body)
	}
}

// TestCountConflict — bumps runs.conflicts (the per-run collision counter §3).
func TestCountConflict(t *testing.T) {
	s, conn := newStore(t)
	insertInbox(t, conn, "doc-1", "document", "mcp:x")
	r, _ := s.Begin(context.Background(), "document-pass", "doc-1")
	for i := 0; i < 2; i++ {
		if err := s.CountConflict(context.Background(), r); err != nil {
			t.Fatalf("count conflict: %v", err)
		}
	}
	var n int
	conn.QueryRow(`SELECT conflicts FROM runs WHERE id=?`, r).Scan(&n)
	if n != 2 {
		t.Fatalf("conflicts = %d, want 2", n)
	}
}
