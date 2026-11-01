package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
)

func validTargetPass(p Pass) bool {
	return p.Kind == KindEnrichTarget && p.Origin == OriginEnrich && p.Trigger == TriggerEnrich && p.Scraper == "" && p.Delivery == "" && p.UserID != "" &&
		(p.EnrichKind == "" || slices.Contains(enrichers.Kinds(), p.EnrichKind)) &&
		((ValidProspectListID(p.Target) || ValidCandidateID(p.Target)) && len(p.TargetIDs) == 0 || p.Target == "" && len(p.TargetIDs) > 0)
}

func readPassDetails(ctx context.Context, tx *sql.Tx, p *Pass) error {
	if p.Status != StatusRunning {
		p.RunningKind = ""
	}
	if p.Kind != KindEnrichTarget || p.Target != "" || len(p.TargetIDs) == 0 {
		p.TargetIDs = nil
	}
	if p.Kind != KindEnrichTarget {
		p.Busy, p.Unmatched = 0, 0
	}
	entries, err := readRecords[KindEntry](ctx, tx, "pass_kinds", columns("kind:Kind new_results:NewResults not_due:NotDue not_applicable:NotApplicable failed:Failed cost:Cost"), " WHERE pass=? ORDER BY seq", p.ID)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		p.KindEntries = entries
	}
	return nil
}

// The previous release can prune a pass without its new rows. A fresh id may
// reuse that removed pass's id, so discard its inert rows before recording it.
func clearPassOrphans(ctx context.Context, tx *sql.Tx, id string) error {
	for _, table := range []string{"pass_kinds", "pass_claims"} {
		if err := deleteRecord(ctx, tx, table, "pass=?", id); err != nil {
			return err
		}
	}
	return nil
}

func runningTargetPass(ctx context.Context, tx *sql.Tx, candidate string) (string, error) {
	var id string
	err := scanSQL(ctx, tx, "SELECT passes.id FROM pass_claims JOIN passes ON passes.id=pass_claims.pass WHERE pass_claims.candidate=? AND passes.status=? LIMIT 1", []any{candidate, StatusRunning}, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// AddTargetPass records a targeted pass and atomically claims its active candidates.
func (s *Store) AddTargetPass(ctx context.Context, p Pass, ids []string) (Pass, []string, error) {
	var out Pass
	var claimed []string
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if !validTargetPass(p) {
			return ErrUnreachable
		}
		out = Pass{Kind: p.Kind, Origin: p.Origin, Trigger: p.Trigger, Target: p.Target, EnrichKind: p.EnrichKind, UserID: p.UserID, UserEmail: p.UserEmail, RequestID: p.RequestID, Status: StatusRunning}
		if len(p.TargetIDs) > 0 {
			out.TargetIDs = slices.Clone(p.TargetIDs)
		}
		claimed = []string{}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			var status string
			err := scanSQL(ctx, tx, "SELECT status FROM candidates WHERE id=?", []any{id}, &status)
			if errors.Is(err, sql.ErrNoRows) || err == nil && status != StatusActive {
				out.Unmatched++
				continue
			}
			if err != nil {
				return err
			}
			owner, err := runningTargetPass(ctx, tx, id)
			if err != nil {
				return err
			}
			if owner != "" {
				out.Busy++
				continue
			}
			claimed = append(claimed, id)
		}
		id, err := s.mintKey(ctx, tx, PassIDPrefix)
		if err != nil {
			return err
		}
		out.ID, out.Started = id, s.now()
		if err = clearPassOrphans(ctx, tx, id); err != nil {
			return err
		}
		if err = insertRecord(ctx, tx, "passes", passColumns, out, nil); err != nil {
			return err
		}
		for _, candidate := range claimed {
			if err = execSQL(ctx, tx, "INSERT INTO pass_claims(candidate,pass) VALUES(?,?)", candidate, id); err != nil {
				return err
			}
		}
		return nil
	}))
	if err != nil {
		return Pass{}, nil, err
	}
	return out, claimed, nil
}

// StartPassKind records the kind currently being processed.
func (s *Store) StartPassKind(ctx context.Context, id, kind string) (Pass, error) {
	var out Pass
	err := s.passWrite(ctx, id, func(_ *sql.Tx, r *records) error {
		p, ok := r.passes[id]
		if !ok {
			return ErrNotFound
		}
		if p.Status != StatusRunning {
			return ErrEnded
		}
		p.RunningKind = kind
		r.passes[id], out = p, p
		return nil
	})
	if err != nil {
		return Pass{}, err
	}
	return out, nil
}

// EndPassKind appends one completed kind and clears the running kind.
func (s *Store) EndPassKind(ctx context.Context, id string, k KindEntry) (Pass, error) {
	var out Pass
	err := s.passWrite(ctx, id, func(tx *sql.Tx, r *records) error {
		if k.NewResults < 0 || k.NotDue < 0 || k.NotApplicable < 0 || k.Failed < 0 || k.Cost < 0 {
			return ErrUnreachable
		}
		p, ok := r.passes[id]
		if !ok {
			return ErrNotFound
		}
		if p.Status != StatusRunning {
			return ErrEnded
		}
		if len(p.KindEntries) >= 5 {
			return ErrUnreachable
		}
		if err := execSQL(ctx, tx, "INSERT INTO pass_kinds(pass,seq,kind,new_results,not_due,not_applicable,failed,cost) VALUES(?,?,?,?,?,?,?,?)", id, len(p.KindEntries), k.Kind, k.NewResults, k.NotDue, k.NotApplicable, k.Failed, k.Cost); err != nil {
			return err
		}
		p.KindEntries = append(p.KindEntries, k)
		p.RunningKind = ""
		r.passes[id], out = p, p
		return nil
	})
	if err != nil {
		return Pass{}, err
	}
	return out, nil
}

// RunningTargetPass returns the running pass that claims a candidate.
func (s *Store) RunningTargetPass(ctx context.Context, candidate string) (string, error) {
	var out string
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error { var err error; out, err = runningTargetPass(ctx, tx, candidate); return err }))
	if err != nil {
		return "", err
	}
	return out, nil
}
