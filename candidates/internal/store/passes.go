package store

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
)

func validPass(p Pass) bool {
	return p.Trigger != "" && (p.Kind == KindRebuild && p.Origin == OriginRebuild && p.Trigger == TriggerRebuild && p.Delivery == "" && p.UserID != "" || p.Kind == KindIngest && p.Origin == OriginRun && p.Trigger != TriggerRebuild && p.Delivery != "" || p.Kind == KindJudge && p.Origin == OriginCron && p.Trigger != TriggerRebuild && p.Delivery != "")
}

// AddPass records a running pass, enforcing delivery uniqueness and exclusivity.
func (s *Store) AddPass(ctx context.Context, p Pass) (Pass, error) {
	var out Pass
	where := " WHERE status=?"
	args := []any{StatusRunning}
	if p.Delivery != "" {
		where += " OR delivery=?"
		args = append(args, p.Delivery)
	}
	if p.Origin == OriginRun {
		where += ` OR (origin='run' AND "trigger"=?)`
		args = append(args, p.Trigger)
	}
	err := s.passWrite(ctx, where, args, func(tx *sql.Tx, r *records) error {
		if !validPass(p) {
			return ErrUnreachable
		}
		for _, q := range r.passes {
			if p.Delivery != "" && p.Delivery == q.Delivery || p.Origin == OriginRun && q.Origin == OriginRun && p.Trigger == q.Trigger {
				return ErrDelivered
			}
		}
		for _, q := range r.passes {
			if q.Status == StatusRunning {
				return ErrBusy
			}
		}
		id, e := s.mintKey(ctx, tx, PassIDPrefix)
		if e != nil {
			return e
		}
		out = Pass{ID: id, Kind: p.Kind, Origin: p.Origin, Trigger: p.Trigger, Status: StatusRunning, Started: s.now(), UserID: p.UserID, UserEmail: p.UserEmail, RequestID: p.RequestID, Delivery: p.Delivery}
		r.passes[id] = out
		return nil
	})
	if err != nil {
		return Pass{}, err
	}
	return out, nil
}
func nonnegative(c Counts) bool {
	v := reflect.ValueOf(c)
	for i := range v.NumField() {
		if v.Field(i).Int() < 0 {
			return false
		}
	}
	return true
}

// FinishPass ends a running pass with its work counts.
func (s *Store) FinishPass(ctx context.Context, id string, c Counts) (Pass, error) {
	var out Pass
	err := s.passWrite(ctx, " WHERE id=?", []any{id}, func(_ *sql.Tx, r *records) error {
		if !nonnegative(c) {
			return ErrUnreachable
		}
		p, ok := r.passes[id]
		if !ok {
			return ErrNotFound
		}
		if p.Status != StatusRunning {
			return ErrEnded
		}
		p.Status = StatusDone
		p.Finished = s.now()
		p.Rows = c.Rows
		p.New = c.New
		p.Changed = c.Changed
		p.Unchanged = c.Unchanged
		p.Gone = c.Gone
		p.Created = c.Created
		p.Linked = c.Linked
		p.Judged = c.Judged
		p.Unranged = c.Unranged
		r.passes[id] = p
		out = p
		return nil
	})
	if err != nil {
		return Pass{}, err
	}
	return out, nil
}

// FailPass ends a running pass with a failure reason.
func (s *Store) FailPass(ctx context.Context, id, reason string) (Pass, error) {
	var out Pass
	err := s.passWrite(ctx, " WHERE id=?", []any{id}, func(_ *sql.Tx, r *records) error {
		if reason != ReasonScrapes && reason != ReasonInterrupted && reason != ReasonStore {
			return ErrUnreachable
		}
		p, ok := r.passes[id]
		if !ok {
			return ErrNotFound
		}
		if p.Status != StatusRunning {
			return ErrEnded
		}
		p.Status = StatusFailed
		p.Reason = reason
		p.Finished = s.now()
		r.passes[id] = p
		out = p
		return nil
	})
	if err != nil {
		return Pass{}, err
	}
	return out, nil
}

// GetPass reads one pass.
func (s *Store) GetPass(ctx context.Context, id string) (Pass, error) {
	var out Pass
	err := s.readScope(ctx, "pass", id, func(r *records) error {
		var ok bool
		out, ok = r.passes[id]
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	return out, err
}
func orderedPasses(r *records) []Pass {
	out := []Pass{}
	for _, p := range r.passes {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.Started.Equal(b.Started) {
			return a.Started.After(b.Started)
		}
		return a.ID > b.ID
	})
	return out
}

// ListPasses reads a keyset window in newest-first pass order.
func (s *Store) ListPasses(ctx context.Context, started, id string, n int) ([]Pass, error) {
	var out []Pass
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			return ErrUnreachable
		}
		query := StartedFirst
		args := []any{n}
		if id != "" {
			query = StartedAfter
			args = []any{started, id, n}
		}
		var err error
		out, err = readQueryRecords[Pass](ctx, tx, passColumns, query, args...)
		return err
	}))
	return out, err
}

// FindDelivered finds the newest pass carrying a delivery or run.
func (s *Store) FindDelivered(ctx context.Context, delivery, run string) (Pass, error) {
	var out Pass
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		ps, err := readRecords[Pass](ctx, tx, "passes", passColumns, ` WHERE (delivery<>'' AND delivery=?1) OR (?2<>'' AND origin=?3 AND "trigger"=?2) ORDER BY started DESC,id DESC LIMIT 1`, delivery, run, OriginRun)
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			return ErrNotFound
		}
		out = ps[0]
		return nil
	}))
	return out, err
}

// RunningPasses reads all unfinished passes.
func (s *Store) RunningPasses(ctx context.Context) ([]Pass, error) {
	var out []Pass
	err := s.readScope(ctx, "running", "", func(r *records) error {
		out = []Pass{}
		for _, p := range r.passes {
			if p.Status == StatusRunning {
				out = append(out, p)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return nil
	})
	return out, err
}

// PrunePasses removes old completed passes independently by status.
func (s *Store) PrunePasses(ctx context.Context, keep int64) ([]Pass, error) {
	var out []Pass
	err := s.transaction(ctx, true, func(r *records) error {
		if keep < 1 {
			return ErrUnreachable
		}
		out = []Pass{}
		counts := map[string]int64{}
		for _, p := range orderedPasses(r) {
			if p.Status == StatusRunning {
				continue
			}
			counts[p.Status]++
			if counts[p.Status] > keep {
				out = append(out, p)
				delete(r.passes, p.ID)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ReplaceZipDrive replaces the ZIP drive-time table.
func (s *Store) ReplaceZipDrive(ctx context.Context, table map[string]int64) error {
	return s.transaction(ctx, true, func(r *records) error {
		for z, n := range table {
			if !validZip(z) || n < 0 {
				return fmt.Errorf("invalid ZIP entry")
			}
		}
		r.zips = map[string]int64{}
		for z, n := range table {
			r.zips[z] = n
		}
		return nil
	})
}

// ZipMinutes reads the drive time for one ZIP.
func (s *Store) ZipMinutes(ctx context.Context, zip string) (int64, error) {
	var out int64
	err := s.readScope(ctx, "zip", zip, func(r *records) error {
		var ok bool
		out, ok = r.zips[zip]
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	return out, err
}

// ListPassesReversed reads preceding passes nearest first in reversed pass order.
func (s *Store) ListPassesReversed(ctx context.Context, started, id string, n int) ([]Pass, error) {
	var out []Pass
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			return ErrUnreachable
		}
		query := StartedLast
		args := []any{n}
		if id != "" {
			query = StartedPreceding
			args = []any{started, id, n}
		}
		var err error
		out, err = readQueryRecords[Pass](ctx, tx, passColumns, query, args...)
		return err
	}))
	return out, err
}
