package page

import (
	"context"
	"database/sql"
	"fmt"
)

// The lint-stale read/write surface (design §6, §6.1, P9c): the open-notes work
// list grouped per subject, the per-subject page evidence the stale-repair call
// reads, and the single per-subject repair transaction that rewrites the page and
// records each note's disposition. Like lint-dups (lint.go) these are distinct
// from the integration write surface (write.go): lint-stale runs ONE TRANSACTION
// PER SUBJECT (per-subject recovery via the open-notes queue itself — design §6),
// not the end-of-run manifest commit, so its writes open and own their own
// *sql.Tx here.

// StaleNote is one open staleness note the lint-stale work list yields (design §6
// / §12 #4). Id addresses the note for its per-note disposition; Note is what the
// writer observed; Cites is the inbox id(s) of the new evidence that makes the
// repair legal (the citation(s) the rewritten page must fold in).
type StaleNote struct {
	ID    string
	Note  string
	Cites string
}

// StaleSubject is one subject's full repair work item (design §6): the subject's
// current page (title + body, the prose to rewrite) plus its open notes batched
// together — "one tool-less call per subject batching its open notes" (P9c). Body
// drives the §6.1 citation-preservation gate (the rewritten page must preserve or
// declare every citation the old body carries). A subject with no page yet has an
// empty Title/Body; its notes still queue (the repair builds a fresh page).
type StaleSubject struct {
	SubjectID string
	Title     string
	Body      string
	Notes     []StaleNote
}

// OpenStaleSubjects returns the open stale_notes batched per subject (design §6):
// every subject with at least one status='open' note, each carrying its current
// page title/body and the list of its open notes. The batching is the design's
// "one tool-less call per subject batching its open notes" — per-subject
// duplicates are merged at repair time (there is no UNIQUE on stale_notes).
// Subjects are ordered by id and notes by id within a subject so a run walks the
// queue deterministically (the property the per-job test relies on).
func (s *Store) OpenStaleSubjects(ctx context.Context) ([]StaleSubject, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.subject, n.id, n.note, n.cites
		FROM stale_notes n
		WHERE n.status = 'open'
		ORDER BY n.subject, n.id`)
	if err != nil {
		return nil, fmt.Errorf("page: open stale subjects: %w", err)
	}
	defer rows.Close()

	var out []StaleSubject
	idx := map[string]int{} // subject id → index into out
	for rows.Next() {
		var subject string
		var n StaleNote
		if err := rows.Scan(&subject, &n.ID, &n.Note, &n.Cites); err != nil {
			return nil, fmt.Errorf("page: open stale subjects scan: %w", err)
		}
		i, ok := idx[subject]
		if !ok {
			idx[subject] = len(out)
			i = len(out)
			out = append(out, StaleSubject{SubjectID: subject})
		}
		out[i].Notes = append(out[i].Notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fill each subject's current page (one read per subject; the stale queue is a
	// rare-cadence batch, so a per-subject page read is acceptable). A subject with
	// no page yet keeps empty Title/Body.
	for i := range out {
		var title, body string
		err := s.db.QueryRowContext(ctx,
			`SELECT title, body FROM pages WHERE subject = ?`, out[i].SubjectID,
		).Scan(&title, &body)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("page: open stale subject page %q: %w", out[i].SubjectID, err)
		}
		out[i].Title, out[i].Body = title, body
	}
	return out, nil
}

// StaleDisposition is the per-note outcome the stale-repair call returns (design
// §6): each open note is either 'repaired' (the rewrite folded its evidence in)
// or 'dismissed' (the note no longer applies). The note id addresses the row; a
// disposition for an id not in the subject's open set is ignored at write time.
type StaleDisposition struct {
	NoteID string
	Status string // 'repaired' | 'dismissed'
}

// StaleRepair is the decided, ready-to-apply per-subject repair (design §6): the
// rewritten page (Title/Body), the §6.1 superseded declarations, the per-note
// dispositions, and the lint-stale run id stamped nowhere on stale_notes (the
// status carries the outcome; Run scopes the repair for provenance/logging). A
// subject with no page yet is created at version 0.
type StaleRepair struct {
	SubjectID    string
	Title        string
	Body         string
	Dispositions []StaleDisposition
}

// ApplyStaleRepair writes one subject's stale repair in ONE transaction (design
// §6): it rewrites the subject's page (body + title, version-bumped, pages_fts
// kept in sync) and sets each note's disposition status. The §6.1 citation gate
// is enforced by the CALLER (lint package) against the OLD body before this is
// invoked, so a failed repair never reaches the transaction. The transaction is
// all-or-nothing: a failure rolls the whole subject's repair back and its notes
// stay open for a later run (per-subject recovery via the queue). A note id in
// Dispositions that is not an open note for this subject is left untouched.
func (s *Store) ApplyStaleRepair(ctx context.Context, r StaleRepair) error {
	if r.SubjectID == "" {
		return fmt.Errorf("page: stale repair has no subject")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("page: begin stale repair tx: %w", err)
	}
	defer tx.Rollback()

	// Rewrite the page (reuses the un-version-guarded folded-page writer: lint-stale
	// runs serially per subject and the repair body is the authoritative content,
	// exactly as lint-dups' fold does).
	if err := s.writeFoldedPage(ctx, tx, r.SubjectID, r.Title, r.Body); err != nil {
		return err
	}

	for _, d := range r.Dispositions {
		status := d.Status
		if status != "repaired" && status != "dismissed" {
			return fmt.Errorf("page: stale disposition %q invalid (must be repaired|dismissed)", status)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE stale_notes SET status = ?
			  WHERE id = ? AND subject = ? AND status = 'open'`,
			status, d.NoteID, r.SubjectID,
		); err != nil {
			return fmt.Errorf("page: set stale disposition %q: %w", d.NoteID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("page: commit stale repair: %w", err)
	}
	return nil
}
