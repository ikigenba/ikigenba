package page

import (
	"context"
	"database/sql"
	"testing"
)

// mustExec runs a statement and fails the test on error.
func mustExec(t *testing.T, conn *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(q, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func TestOpenStaleSubjectsBatchesPerSubject(t *testing.T) {
	conn := newTestDB(t)
	s := NewStore(conn)
	ctx := context.Background()

	insertSubject(t, conn, "01A", "entity", "Acme")
	insertPage(t, conn, "01A", "Acme", "Acme body [01HX0000000000000000000001]")
	insertSubject(t, conn, "01B", "entity", "Beta")
	insertPage(t, conn, "01B", "Beta", "Beta body [01HX0000000000000000000003]")

	// Two open notes on 01A (must batch), one open on 01B, one repaired (excluded).
	mustExec(t, conn, `INSERT INTO stale_notes (id, subject, note, cites, run_id, status) VALUES
		('01N1','01A','note one','01HX0000000000000000000002','run-1','open'),
		('01N2','01A','note two','01HX0000000000000000000004','run-1','open'),
		('01N3','01B','note three','01HX0000000000000000000005','run-1','open'),
		('01N4','01A','done','x','run-0','repaired')`)

	subs, err := s.OpenStaleSubjects(ctx)
	if err != nil {
		t.Fatalf("open stale subjects: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("want 2 subjects, got %d", len(subs))
	}
	// Ordered by subject id; 01A first with its two open notes batched.
	if subs[0].SubjectID != "01A" || len(subs[0].Notes) != 2 {
		t.Fatalf("01A should batch 2 open notes, got %+v", subs[0])
	}
	if subs[0].Body == "" {
		t.Fatal("subject's current page body must be loaded")
	}
	if subs[1].SubjectID != "01B" || len(subs[1].Notes) != 1 {
		t.Fatalf("01B should have 1 open note, got %+v", subs[1])
	}
}

func TestApplyStaleRepairRewritesAndDispositions(t *testing.T) {
	conn := newTestDB(t)
	s := NewStore(conn)
	ctx := context.Background()

	insertSubject(t, conn, "01A", "entity", "Acme")
	insertPage(t, conn, "01A", "Acme", "Acme is independent. [01HX0000000000000000000001]")
	mustExec(t, conn, `INSERT INTO stale_notes (id, subject, note, cites, run_id, status) VALUES
		('01N1','01A','acquired','01HX0000000000000000000002','run-1','open'),
		('01N2','01A','obsolete','01HX0000000000000000000009','run-1','open')`)

	err := s.ApplyStaleRepair(ctx, StaleRepair{
		SubjectID: "01A",
		Title:     "Acme",
		Body:      "Acme was acquired. [01HX0000000000000000000001] [01HX0000000000000000000002]",
		Dispositions: []StaleDisposition{
			{NoteID: "01N1", Status: "repaired"},
			{NoteID: "01N2", Status: "dismissed"},
		},
	})
	if err != nil {
		t.Fatalf("apply stale repair: %v", err)
	}

	// Page rewritten + version bumped (insertPage starts at 1 → 2).
	var body string
	var version int
	if err := conn.QueryRow(`SELECT body, version FROM pages WHERE subject='01A'`).Scan(&body, &version); err != nil {
		t.Fatalf("read page: %v", err)
	}
	if version != 2 {
		t.Fatalf("version should bump to 2, got %d", version)
	}
	if body == "" || body[:4] != "Acme" {
		t.Fatalf("page not rewritten: %q", body)
	}

	// Both notes dispositioned, none left open.
	var open int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM stale_notes WHERE subject='01A' AND status='open'`).Scan(&open); err != nil {
		t.Fatalf("count open: %v", err)
	}
	if open != 0 {
		t.Fatalf("no notes should remain open, got %d", open)
	}
	var st1, st2 string
	conn.QueryRow(`SELECT status FROM stale_notes WHERE id='01N1'`).Scan(&st1)
	conn.QueryRow(`SELECT status FROM stale_notes WHERE id='01N2'`).Scan(&st2)
	if st1 != "repaired" || st2 != "dismissed" {
		t.Fatalf("dispositions wrong: 01N1=%s 01N2=%s", st1, st2)
	}

	// FTS stays in sync (the rewritten body is searchable, the old isn't).
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM pages_fts WHERE pages_fts MATCH 'acquired'`).Scan(&n); err != nil {
		t.Fatalf("fts query: %v", err)
	}
	if n != 1 {
		t.Fatalf("rewritten body should be in FTS, got %d hits", n)
	}
}

func TestApplyStaleRepairRejectsBadDisposition(t *testing.T) {
	conn := newTestDB(t)
	s := NewStore(conn)
	insertSubject(t, conn, "01A", "entity", "Acme")
	insertPage(t, conn, "01A", "Acme", "body")
	err := s.ApplyStaleRepair(context.Background(), StaleRepair{
		SubjectID:    "01A",
		Title:        "Acme",
		Body:         "new body",
		Dispositions: []StaleDisposition{{NoteID: "x", Status: "bogus"}},
	})
	if err == nil {
		t.Fatal("a bogus disposition status must fail the repair")
	}
}
