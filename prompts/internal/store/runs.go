package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

func validUsage(u Usage) bool {
	return u.Calls >= 0 && u.ToolCalls >= 0 && u.InputTokens >= 0 && u.CachedTokens >= 0 && u.OutputTokens >= 0 && u.ReasoningTokens >= 0 && u.CostNanos >= 0
}
func validReason(r string) bool { return r == ReasonStartFailed || r == ReasonQueueAbandoned }
func validRun(r Run) bool {
	if !ValidRunID(r.ID) || r.Prompt == "" || r.Model == "" || r.User == "" || stamp(r.Started).IsZero() || r.StdoutBytes < 0 || r.StderrBytes < 0 || r.Usage != (Usage{}) || r.ExitCode != 0 {
		return false
	}
	if (r.Trigger != TriggerManual || r.Event != "") && (r.Trigger != TriggerEvent || r.Event == "") {
		return false
	}
	switch r.Status {
	case StatusRunning, StatusQueued:
		return r.Finished.IsZero() && r.StdoutBytes == 0 && r.StderrBytes == 0 && !r.Truncated() && r.Reason == ""
	case StatusFailed:
		return validReason(r.Reason) && !stamp(r.Finished).IsZero() && !stamp(r.Finished).Before(stamp(r.Started))
	default:
		return false
	}
}
func validEnding(e Ending) bool {
	if stamp(e.Finished).IsZero() || e.StdoutBytes < 0 || e.StderrBytes < 0 || !validUsage(e.Usage) {
		return false
	}
	switch e.Status {
	case StatusExited:
		return e.ExitCode >= 0 && e.ExitCode <= 255 && e.Reason == ""
	case StatusKilled, StatusTimedOut:
		return e.ExitCode == 0 && e.Reason == ""
	case StatusFailed:
		return e.ExitCode == 0 && validReason(e.Reason) && e.Usage == (Usage{})
	default:
		return false
	}
}
func fits(e Ending, r Run) bool {
	if r.Status == StatusRunning {
		return e.Status == StatusExited || e.Status == StatusKilled || e.Status == StatusTimedOut
	}
	return (e.Status == StatusKilled || e.Status == StatusFailed) && e.StdoutBytes == 0 && e.StderrBytes == 0 && !e.StdoutTruncated && !e.StderrTruncated && e.Usage == (Usage{})
}
func runArgs(r Run) []any {
	var finished any
	if !r.Finished.IsZero() {
		finished = r.Finished.Format(time.RFC3339)
	}
	return []any{r.ID, r.Prompt, r.Model, r.User, r.RequestID, r.Trigger, r.Event, r.Status, r.ExitCode, r.Started.Format(time.RFC3339), finished, r.StdoutBytes, r.StderrBytes, r.StdoutTruncated, r.StderrTruncated, r.Reason, r.Usage.Calls, r.Usage.ToolCalls, r.Usage.InputTokens, r.Usage.CachedTokens, r.Usage.OutputTokens, r.Usage.ReasoningTokens, r.Usage.CostNanos}
}

// AddRun records a new run and its event delivery if applicable.
func (s *Store) AddRun(ctx context.Context, r Run) (out Run, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		if !validRun(r) {
			return errInvalid
		}
		occupied, e := exists(tx, "SELECT 1 FROM runs WHERE id=?", r.ID)
		if e != nil {
			return e
		}
		if occupied {
			return errInvalid
		}
		if e = requirePrompt(tx, r.Prompt); e != nil {
			return e
		}
		if r.Trigger == TriggerEvent {
			delivered, e := exists(tx, "SELECT 1 FROM event_runs WHERE prompt=? AND event=?", r.Prompt, r.Event)
			if e != nil {
				return e
			}
			if delivered {
				return ErrDelivered
			}
			if _, e := tx.Exec("INSERT INTO event_runs VALUES (?,?)", r.Prompt, r.Event); e != nil {
				return e
			}
		}
		r.Started = stamp(r.Started)
		if !r.Finished.IsZero() {
			r.Finished = stamp(r.Finished)
		}
		_, e = tx.Exec("INSERT INTO runs VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", runArgs(r)...)
		out = r
		return e
	})
	if err != nil {
		out = Run{}
	}
	return
}

// StartRun moves a queued run to running.
func (s *Store) StartRun(ctx context.Context, id string) (out Run, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		r, err := queryRun(tx, "SELECT "+runColumns+" FROM runs WHERE id=?", id)
		if err != nil {
			return err
		}
		if r.Status != StatusQueued {
			return ErrEnded
		}
		r.Status = StatusRunning
		_, e := tx.Exec("UPDATE runs SET status=? WHERE id=?", r.Status, id)
		out = r
		return e
	})
	if err != nil {
		out = Run{}
	}
	return
}

// FinishRun ends a queued or running run exactly once.
func (s *Store) FinishRun(ctx context.Context, id string, e Ending) (out Run, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		if !validEnding(e) {
			return errInvalid
		}
		r, err := queryRun(tx, "SELECT "+runColumns+" FROM runs WHERE id=?", id)
		if err != nil {
			return err
		}
		if r.Status != StatusRunning && r.Status != StatusQueued {
			return ErrEnded
		}
		if !fits(e, r) || stamp(e.Finished).Before(r.Started) {
			return errInvalid
		}
		r.Status = e.Status
		r.ExitCode = e.ExitCode
		r.Finished = stamp(e.Finished)
		r.StdoutBytes = e.StdoutBytes
		r.StderrBytes = e.StderrBytes
		r.StdoutTruncated = e.StdoutTruncated
		r.StderrTruncated = e.StderrTruncated
		r.Reason = e.Reason
		r.Usage = e.Usage
		args := runArgs(r)[7:]
		args = append(args, id)
		_, err = tx.Exec("UPDATE runs SET status=?,exit_code=?,started=?,finished=?,stdout_bytes=?,stderr_bytes=?,stdout_truncated=?,stderr_truncated=?,reason=?,calls=?,tool_calls=?,input_tokens=?,cached_tokens=?,output_tokens=?,reasoning_tokens=?,cost_nanos=? WHERE id=?", args...)
		out = r
		return err
	})
	if err != nil {
		out = Run{}
	}
	return
}

func scanRun(row scanner) (r Run, err error) {
	var started string
	var finished sql.NullString
	err = row.Scan(&r.ID, &r.Prompt, &r.Model, &r.User, &r.RequestID, &r.Trigger, &r.Event, &r.Status, &r.ExitCode, &started, &finished, &r.StdoutBytes, &r.StderrBytes, &r.StdoutTruncated, &r.StderrTruncated, &r.Reason, &r.Usage.Calls, &r.Usage.ToolCalls, &r.Usage.InputTokens, &r.Usage.CachedTokens, &r.Usage.OutputTokens, &r.Usage.ReasoningTokens, &r.Usage.CostNanos)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return
	}
	r.Started, err = time.Parse(time.RFC3339, started)
	if err == nil && finished.Valid {
		r.Finished, err = time.Parse(time.RFC3339, finished.String)
	}
	return
}
func queryRun(tx *sql.Tx, query string, args ...any) (Run, error) {
	return scanRun(tx.QueryRow(query, args...))
}
func queryRuns(tx *sql.Tx, query string, args ...any) ([]Run, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	rs := []Run{}
	for rows.Next() {
		r, e := scanRun(rows)
		if e != nil {
			err = e
			break
		}
		rs = append(rs, r)
	}
	if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return rs, nil
}

// RunByID finds a run of any owner.
func (s *Store) RunByID(ctx context.Context, id string) (r Run, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		r, err = queryRun(tx, "SELECT "+runColumns+" FROM runs WHERE id=?", id)
		return err
	})
	return
}

// FindRun finds a run belonging to the given owner.
func (s *Store) FindRun(ctx context.Context, owner, id string) (r Run, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		if owner == "" {
			return ErrNotFound
		}
		r, err = queryRun(tx, "SELECT "+runColumns+" FROM runs WHERE id=? AND EXISTS (SELECT 1 FROM prompts WHERE prompts.id=runs.prompt AND owner_id=?)", id, owner)
		return err
	})
	return
}

// Runs lists a bounded window of a prompt's runs newest first.
func (s *Store) Runs(ctx context.Context, prompt, started, id string, n int) (rs []Run, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			rs = []Run{}
			return nil
		}
		query := "SELECT " + runColumns + " FROM runs WHERE prompt=?"
		args := []any{prompt}
		if id != "" {
			query += " AND (started,id)<(?,?)"
			args = append(args, started, id)
		}
		query += " ORDER BY started DESC,id DESC LIMIT ?"
		args = append(args, n)
		rs, err = queryRuns(tx, query, args...)
		return err
	})
	if err != nil {
		rs = nil
	}
	return
}

// RunCount returns the total number of a prompt's runs.
func (s *Store) RunCount(ctx context.Context, prompt string) (n int, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow("SELECT count(*) FROM runs WHERE prompt=?", prompt).Scan(&n)
	})
	return
}
func (s *Store) status(ctx context.Context, status string) (rs []Run, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		rs, err = queryRuns(tx, "SELECT "+runColumns+" FROM runs WHERE status=? ORDER BY id", status)
		return err
	})
	return
}

// Running lists running records by ID.
func (s *Store) Running(ctx context.Context) ([]Run, error) { return s.status(ctx, StatusRunning) }

// Queued lists queued records by ID.
func (s *Store) Queued(ctx context.Context) ([]Run, error) { return s.status(ctx, StatusQueued) }

// PastKeeping lists terminal runs beyond both retention bounds.
func (s *Store) PastKeeping(ctx context.Context, now time.Time, days, count int64) (rs []Run, err error) {
	err = s.read(ctx, func(tx *sql.Tx) error {
		if days < 1 || count < 1 {
			return errInvalid
		}
		if days > math.MaxInt64/int64(24*time.Hour) {
			rs = []Run{}
			return nil
		}
		// Ascending rank is equivalent to descending rank after subtracting it
		// from the partition size, and follows the retention index directly.
		cutoff := now.UTC().Add(-time.Duration(days) * 24 * time.Hour)
		if cutoff.Nanosecond() != 0 {
			cutoff = cutoff.Truncate(time.Second).Add(time.Second)
		}
		rs, err = queryRuns(tx, "SELECT "+runColumns+" FROM (SELECT "+runColumns+", count(*) OVER (PARTITION BY prompt) - row_number() OVER (PARTITION BY prompt ORDER BY started,id) AS position FROM runs) WHERE position>=? AND status NOT IN (?,?) AND started<?", count, StatusRunning, StatusQueued, cutoff.Format(time.RFC3339))
		return err
	})
	if err != nil {
		rs = nil
	}
	return
}

// DeleteRun removes a run while preserving event delivery memory.
func (s *Store) DeleteRun(ctx context.Context, id string) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		result, err := tx.Exec("DELETE FROM runs WHERE id=?", id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
