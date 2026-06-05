// Package jobstore implements agentkit/job.Store over the wiki_jobs table
// (Task 3.1 migration 002_wiki.sql). It is the persistence half of the agentkit
// job seam: agentkit owns the async lifecycle (spawn / cancel / TTL / sweep);
// this package owns where the run records live (the suite's single-writer
// SQLite DB).
//
// The generic Record columns (id, flight_key, status, started_at, ended_at,
// usage_json, error) map 1:1 onto job.Record. owner + collection are the
// consumer-side scoping the agentkit seam deliberately keeps out of Record:
// Insert stamps them from the per-job context; Load can be owner-scoped so a
// foreign-owned id reads as job.ErrNotFound.
//
// Single-flight rests on the partial-unique index wiki_jobs_flight_running
// (UNIQUE(flight_key) WHERE status='running') under db.Open's single connection:
// a second Insert for an already-running flight_key violates that index, which
// Store maps to job.ErrFlightInUse — the exact contract job.Runner.Spawn reacts
// to.
package jobstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"agentkit/job"
)

// timeFmt is the on-disk timestamp format. RFC3339Nano keeps sub-second order
// stable across rapid spawns; "" round-trips as the zero time.
const timeFmt = time.RFC3339Nano

// Store is a SQLite-backed job.Store over the wiki_jobs table. It carries the
// owner+collection for a job alongside the generic Record fields. A Store is
// created per ingest (it pins the owner/collection that Insert stamps and Load
// scopes by); the underlying *sql.DB is shared (single-writer).
type Store struct {
	db         *sql.DB
	owner      string
	collection string
}

// Ensure Store satisfies the agentkit seam.
var _ job.Store = (*Store)(nil)

// New returns a Store bound to (owner, collection) over db. owner/collection are
// stamped on Insert and enforced on Load (a row owned by a different owner reads
// as job.ErrNotFound). db must be the service's single-writer handle.
func New(db *sql.DB, owner, collection string) *Store {
	return &Store{db: db, owner: owner, collection: collection}
}

// Insert persists rec in StatusRunning, stamping this Store's owner+collection.
// It returns job.ErrFlightInUse when another row with the same flight_key is
// already running (the partial-unique index violation), which is the
// single-flight gate job.Runner.Spawn relies on.
func (s *Store) Insert(ctx context.Context, rec job.Record) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO wiki_jobs
		   (id, flight_key, status, started_at, ended_at, usage_json, error, owner, collection)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID,
		rec.FlightKey,
		string(rec.Status),
		fmtTime(rec.StartedAt),
		fmtTime(rec.EndedAt),
		rec.UsageJSON,
		rec.Error,
		s.owner,
		s.collection,
	)
	if err != nil {
		if isUniqueViolation(err) {
			// Either the partial-unique running index (a second in-flight run for
			// this flight_key) or the PK (a duplicate id) — both are the
			// single-flight conflict the seam signals with ErrFlightInUse.
			return job.ErrFlightInUse
		}
		return fmt.Errorf("jobstore: insert %s: %w", rec.ID, err)
	}
	return nil
}

// Load returns the record by id, scoped to this Store's owner: a row owned by a
// different owner (or absent) reads as job.ErrNotFound.
func (s *Store) Load(ctx context.Context, id string) (job.Record, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, flight_key, status, started_at, ended_at, usage_json, error
		   FROM wiki_jobs
		  WHERE id = ? AND owner = ?`,
		id, s.owner,
	)
	return scanRecord(row)
}

// UpdateTerminal writes the run's end state. It is intentionally NOT owner-scoped
// in its WHERE: the runner calls it from its own goroutine with the id it minted,
// on a fresh background context, and the id is a ULID — owner-scoping here would
// only risk dropping a legitimate terminal write if the runner outlived the
// Store's owner binding.
func (s *Store) UpdateTerminal(ctx context.Context, id string, status job.Status, endedAt time.Time, usageJSON, errMsg string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE wiki_jobs
		    SET status = ?, ended_at = ?, usage_json = ?, error = ?
		  WHERE id = ?`,
		string(status), fmtTime(endedAt), usageJSON, errMsg, id,
	)
	if err != nil {
		return fmt.Errorf("jobstore: update terminal %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("jobstore: rows affected for %s: %w", id, err)
	}
	if n == 0 {
		return job.ErrNotFound
	}
	return nil
}

// SweepRunning is boot-time crash recovery: every row still 'running' (orphaned
// by a crash mid-run) is flipped to 'failed' with an endedAt + "interrupted by
// restart" error, transactionally, returning the count swept. It is NOT
// owner-scoped — a crash orphans every owner's runs, so recovery must sweep them
// all. Call it once on boot before accepting new Spawns.
func (s *Store) SweepRunning(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("jobstore: begin sweep tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE wiki_jobs
		    SET status = ?, ended_at = ?, error = ?
		  WHERE status = ?`,
		string(job.StatusFailed),
		fmtTime(time.Now().UTC()),
		"interrupted by restart",
		string(job.StatusRunning),
	)
	if err != nil {
		return 0, fmt.Errorf("jobstore: sweep: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("jobstore: sweep rows affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("jobstore: commit sweep: %w", err)
	}
	return int(n), nil
}

// scanRecord reads one wiki_jobs row into a job.Record, mapping sql.ErrNoRows to
// job.ErrNotFound.
func scanRecord(row *sql.Row) (job.Record, error) {
	var (
		rec                job.Record
		status             string
		startedAt, endedAt string
	)
	err := row.Scan(&rec.ID, &rec.FlightKey, &status, &startedAt, &endedAt, &rec.UsageJSON, &rec.Error)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return job.Record{}, job.ErrNotFound
	case err != nil:
		return job.Record{}, fmt.Errorf("jobstore: scan: %w", err)
	}
	rec.Status = job.Status(status)
	rec.StartedAt = parseTime(startedAt)
	rec.EndedAt = parseTime(endedAt)
	return rec, nil
}

// fmtTime renders t as RFC3339Nano, or "" for the zero time (matching the
// column's ” default for an unset ended_at).
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeFmt)
}

// parseTime is fmtTime's inverse; an empty or unparseable value yields the zero
// time (so a still-running row's ended_at reads as zero, per Record's contract).
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(timeFmt, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// isUniqueViolation reports whether err is a SQLite UNIQUE/PK constraint failure.
// modernc.org/sqlite surfaces these in the error message ("UNIQUE constraint
// failed" / "constraint failed (2067)"); we match on the stable text since the
// driver does not export a typed sentinel.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") ||
		strings.Contains(msg, "constraint failed") && strings.Contains(msg, "2067")
}
