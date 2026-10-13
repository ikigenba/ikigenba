package store

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"reflect"
	"sort"
)

func (s *Store) mint(r *records, prefix string) (string, error) {
	for range 8 {
		var b [8]byte
		if s.cfg.Rand == nil {
			return "", ErrUnreachable
		}
		if _, err := io.ReadFull(s.cfg.Rand, b[:]); err != nil {
			return "", ErrUnreachable
		}
		id := prefix + hex.EncodeToString(b[:])
		if prefix == CandidateIDPrefix {
			if _, ok := r.candidates[id]; !ok {
				return id, nil
			}
		} else {
			if _, ok := r.passes[id]; !ok {
				return id, nil
			}
		}
	}
	return "", ErrUnreachable
}
func validPass(p Pass) bool {
	return p.Trigger != "" && (p.Trigger == TriggerRebuild && p.Delivery == "" && p.UserID != "" || p.Trigger != TriggerRebuild && p.Delivery != "")
}

// AddPass records a running pass, enforcing delivery uniqueness and exclusivity.
func (s *Store) AddPass(ctx context.Context, p Pass) (Pass, error) {
	var out Pass
	err := s.transaction(ctx, true, func(r *records) error {
		if !validPass(p) {
			return ErrUnreachable
		}
		for _, q := range r.passes {
			if p.Delivery != "" && p.Delivery == q.Delivery || p.Trigger != TriggerRebuild && p.Trigger == q.Trigger {
				return ErrDelivered
			}
		}
		for _, q := range r.passes {
			if q.Status == StatusRunning {
				return ErrBusy
			}
		}
		id, e := s.mint(r, PassIDPrefix)
		if e != nil {
			return e
		}
		out = Pass{ID: id, Trigger: p.Trigger, Status: StatusRunning, Started: s.now(), UserID: p.UserID, UserEmail: p.UserEmail, RequestID: p.RequestID, Delivery: p.Delivery}
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
	err := s.transaction(ctx, true, func(r *records) error {
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
		p.Rescored = c.Rescored
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
	err := s.transaction(ctx, true, func(r *records) error {
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
	err := s.transaction(ctx, false, func(r *records) error {
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
func window[T any](a []T, offset, limit int64) []T {
	if offset >= int64(len(a)) {
		return []T{}
	}
	a = a[offset:]
	if limit < int64(len(a)) {
		a = a[:limit]
	}
	return a
}
func validWindow(offset, limit int64) bool { return offset >= 0 && limit >= 1 }

// ListPasses reads a window in newest-first pass order.
func (s *Store) ListPasses(ctx context.Context, offset, limit int64) ([]Pass, error) {
	var out []Pass
	err := s.transaction(ctx, false, func(r *records) error {
		if !validWindow(offset, limit) {
			return ErrUnreachable
		}
		out = window(orderedPasses(r), offset, limit)
		return nil
	})
	return out, err
}

// RunningPasses reads all unfinished passes.
func (s *Store) RunningPasses(ctx context.Context) ([]Pass, error) {
	var out []Pass
	err := s.transaction(ctx, false, func(r *records) error {
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
	err := s.transaction(ctx, false, func(r *records) error {
		var ok bool
		out, ok = r.zips[zip]
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	return out, err
}
