package jobstore

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"agentkit/job"

	"wiki/internal/db"
)

// newDB opens an in-process SQLite DB and runs the wiki migrations so the
// wiki_jobs table (and its partial-unique running index) exists.
func newDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := db.Migrate(context.Background(), conn); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return conn
}

func TestInsertLoadRoundTrip(t *testing.T) {
	conn := newDB(t)
	s := New(conn, "alice@example.com", "default")
	ctx := context.Background()

	started := time.Now().UTC().Truncate(time.Nanosecond)
	rec := job.Record{ID: "run-1", FlightKey: "fk-1", Status: job.StatusRunning, StartedAt: started}
	if err := s.Insert(ctx, rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := s.Load(ctx, "run-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != "run-1" || got.FlightKey != "fk-1" || got.Status != job.StatusRunning {
		t.Fatalf("Load = %+v, want id=run-1 fk-1 running", got)
	}
	if !got.StartedAt.Equal(started) {
		t.Fatalf("StartedAt = %v, want %v", got.StartedAt, started)
	}
	if !got.EndedAt.IsZero() {
		t.Fatalf("EndedAt = %v, want zero for a running record", got.EndedAt)
	}
}

func TestSingleFlight(t *testing.T) {
	conn := newDB(t)
	s := New(conn, "alice@example.com", "default")
	ctx := context.Background()

	first := job.Record{ID: "run-a", FlightKey: "shared", Status: job.StatusRunning, StartedAt: time.Now().UTC()}
	if err := s.Insert(ctx, first); err != nil {
		t.Fatalf("first Insert: %v", err)
	}

	// A second running row for the SAME flight_key must be rejected.
	second := job.Record{ID: "run-b", FlightKey: "shared", Status: job.StatusRunning, StartedAt: time.Now().UTC()}
	if err := s.Insert(ctx, second); !errors.Is(err, job.ErrFlightInUse) {
		t.Fatalf("second Insert err = %v, want ErrFlightInUse", err)
	}

	// Once the first finishes (terminal), the flight_key is free to run again.
	if err := s.UpdateTerminal(ctx, "run-a", job.StatusSucceeded, time.Now().UTC(), `{}`, ""); err != nil {
		t.Fatalf("UpdateTerminal: %v", err)
	}
	third := job.Record{ID: "run-c", FlightKey: "shared", Status: job.StatusRunning, StartedAt: time.Now().UTC()}
	if err := s.Insert(ctx, third); err != nil {
		t.Fatalf("third Insert after terminal: %v", err)
	}
}

func TestDuplicateIDRejected(t *testing.T) {
	conn := newDB(t)
	s := New(conn, "alice@example.com", "default")
	ctx := context.Background()

	rec := job.Record{ID: "dup", FlightKey: "fk", Status: job.StatusRunning, StartedAt: time.Now().UTC()}
	if err := s.Insert(ctx, rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := s.Insert(ctx, rec); !errors.Is(err, job.ErrFlightInUse) {
		t.Fatalf("duplicate id Insert err = %v, want ErrFlightInUse", err)
	}
}

func TestLoadOwnerScoped(t *testing.T) {
	conn := newDB(t)
	ctx := context.Background()

	alice := New(conn, "alice@example.com", "default")
	rec := job.Record{ID: "run-alice", FlightKey: "fk", Status: job.StatusRunning, StartedAt: time.Now().UTC()}
	if err := alice.Insert(ctx, rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Bob cannot read Alice's job — it reads as ErrNotFound.
	bob := New(conn, "bob@example.com", "default")
	if _, err := bob.Load(ctx, "run-alice"); !errors.Is(err, job.ErrNotFound) {
		t.Fatalf("foreign Load err = %v, want ErrNotFound", err)
	}
	// Alice can.
	if _, err := alice.Load(ctx, "run-alice"); err != nil {
		t.Fatalf("owner Load: %v", err)
	}
}

func TestLoadMissing(t *testing.T) {
	conn := newDB(t)
	s := New(conn, "alice@example.com", "default")
	if _, err := s.Load(context.Background(), "nope"); !errors.Is(err, job.ErrNotFound) {
		t.Fatalf("Load missing err = %v, want ErrNotFound", err)
	}
}

func TestUpdateTerminalPersists(t *testing.T) {
	conn := newDB(t)
	s := New(conn, "alice@example.com", "default")
	ctx := context.Background()

	rec := job.Record{ID: "run-t", FlightKey: "fk", Status: job.StatusRunning, StartedAt: time.Now().UTC()}
	if err := s.Insert(ctx, rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	ended := time.Now().UTC().Truncate(time.Nanosecond)
	if err := s.UpdateTerminal(ctx, "run-t", job.StatusFailed, ended, `{"usage":1}`, "boom"); err != nil {
		t.Fatalf("UpdateTerminal: %v", err)
	}
	got, err := s.Load(ctx, "run-t")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Status != job.StatusFailed || got.Error != "boom" || got.UsageJSON != `{"usage":1}` {
		t.Fatalf("terminal record = %+v, want failed/boom/usage", got)
	}
	if !got.EndedAt.Equal(ended) {
		t.Fatalf("EndedAt = %v, want %v", got.EndedAt, ended)
	}
}

func TestUpdateTerminalMissing(t *testing.T) {
	conn := newDB(t)
	s := New(conn, "alice@example.com", "default")
	if err := s.UpdateTerminal(context.Background(), "ghost", job.StatusSucceeded, time.Now().UTC(), "", ""); !errors.Is(err, job.ErrNotFound) {
		t.Fatalf("UpdateTerminal missing err = %v, want ErrNotFound", err)
	}
}

func TestSweepRunning(t *testing.T) {
	conn := newDB(t)
	ctx := context.Background()
	s := New(conn, "alice@example.com", "default")

	// Two running (crash-orphaned), one already terminal.
	for _, id := range []string{"r1", "r2"} {
		if err := s.Insert(ctx, job.Record{ID: id, FlightKey: id, Status: job.StatusRunning, StartedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("Insert %s: %v", id, err)
		}
	}
	if err := s.Insert(ctx, job.Record{ID: "done", FlightKey: "done", Status: job.StatusRunning, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("Insert done: %v", err)
	}
	if err := s.UpdateTerminal(ctx, "done", job.StatusSucceeded, time.Now().UTC(), "", ""); err != nil {
		t.Fatalf("UpdateTerminal done: %v", err)
	}

	n, err := s.SweepRunning(ctx)
	if err != nil {
		t.Fatalf("SweepRunning: %v", err)
	}
	if n != 2 {
		t.Fatalf("swept %d, want 2", n)
	}
	for _, id := range []string{"r1", "r2"} {
		got, err := s.Load(ctx, id)
		if err != nil {
			t.Fatalf("Load %s: %v", id, err)
		}
		if got.Status != job.StatusFailed || got.Error != "interrupted by restart" {
			t.Fatalf("%s after sweep = %+v, want failed/interrupted", id, got)
		}
		if got.EndedAt.IsZero() {
			t.Fatalf("%s EndedAt is zero after sweep", id)
		}
	}
	// The terminal one is untouched.
	done, _ := s.Load(ctx, "done")
	if done.Status != job.StatusSucceeded {
		t.Fatalf("done after sweep = %q, want succeeded (untouched)", done.Status)
	}
}
