package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// GetCandidate reads one candidate, including merged candidates.
func (s *Store) GetCandidate(ctx context.Context, id string) (Candidate, error) {
	var out Candidate
	err := s.readScope(ctx, "candidate", id, func(r *records) error {
		var ok bool
		out, ok = r.candidates[id]
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	return out, err
}
func linksFor(r *records, id string) []Link {
	out := []Link{}
	for _, l := range r.links {
		if l.Candidate == id {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Gone != b.Gone {
			return !a.Gone
		}
		if a.Reading.Filed.IsZero() != b.Reading.Filed.IsZero() {
			return !a.Reading.Filed.IsZero()
		}
		if !a.Reading.Filed.Equal(b.Reading.Filed) {
			return a.Reading.Filed.After(b.Reading.Filed)
		}
		if a.Scraper != b.Scraper {
			return a.Scraper < b.Scraper
		}
		return a.SourceID < b.SourceID
	})
	return out
}

// Links reads a candidate’s evidence in display order.
func (s *Store) Links(ctx context.Context, id string) ([]Link, error) {
	var out []Link
	err := s.readScope(ctx, "links", id, func(r *records) error {
		if _, ok := r.candidates[id]; !ok {
			return ErrNotFound
		}
		out = linksFor(r, id)
		return nil
	})
	return out, err
}

// Estimates reads a candidate’s stored source estimates.
func (s *Store) Estimates(ctx context.Context, id string) ([]sources.Estimate, error) {
	var out []sources.Estimate
	err := s.readScope(ctx, "estimates", id, func(r *records) error {
		if _, ok := r.candidates[id]; !ok {
			return ErrNotFound
		}
		out = append([]sources.Estimate{}, r.estimates[id]...)
		return nil
	})
	return out, err
}

// GetJudgments reads a candidate’s last judgments.
func (s *Store) GetJudgments(ctx context.Context, id string) (Judgments, error) {
	var out Judgments
	err := s.readScope(ctx, "judgments", id, func(r *records) error {
		var ok bool
		out, ok = r.judgments[id]
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	return out, err
}

// Rulings reads a candidate’s decisions in reverse recording order.
func (s *Store) Rulings(ctx context.Context, id string) ([]Ruling, error) {
	var out []Ruling
	err := s.readScope(ctx, "rulings", id, func(r *records) error {
		if _, ok := r.candidates[id]; !ok {
			return ErrNotFound
		}
		out = []Ruling{}
		for i := len(r.rulings) - 1; i >= 0; i-- {
			d := r.rulings[i]
			if d.Candidate == id || d.Other == id {
				out = append(out, d)
			}
		}
		return nil
	})
	return out, err
}

// ListCandidates reads a filtered keyset window of active candidates.
func (s *Store) ListCandidates(ctx context.Context, f CandidateFilter, created, id string, n int) ([]Candidate, error) {
	var out []Candidate
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			return ErrUnreachable
		}
		name := strings.ToLower(f.Name)
		if strings.Contains(name, "\n") {
			out = []Candidate{}
			return nil
		}
		phone := sources.PhoneKey(f.Phone)
		if f.Phone != "" && phone == "" {
			out = []Candidate{}
			return nil
		}
		query := CandidatesFirst
		args := []any{name, f.Zip, phone, n}
		if id != "" {
			query = CandidatesAfter
			args = []any{name, f.Zip, phone, created, id, n}
		}
		var err error
		out, err = readQueryRecords[Candidate](ctx, tx, candidateColumns, query, args...)
		return err
	}))
	return out, err
}

func rankedWindow(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Ranked, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, key)
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	out := []Ranked{}
	for _, key := range ids {
		c, err := candidateKey(ctx, tx, key)
		if err != nil {
			return nil, err
		}
		a, err := readRecords[Judgments](ctx, tx, "judgments", judgmentsColumns, " WHERE candidate=?", key)
		if err != nil {
			return nil, err
		}
		member := Ranked{Candidate: c, Judgments: a[0]}
		f, err := factsKey(ctx, tx, key)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if errors.Is(err, ErrNotFound) {
			f = unknownFacts(key)
		}
		es, err := enrichmentRecords(ctx, tx, " WHERE candidate=? ORDER BY seq DESC", key)
		if err != nil {
			return nil, err
		}
		member.Basis = BasisOf(es)
		member.Facts = f
		seen := map[string]bool{}
		for _, e := range es {
			if seen[e.Kind] {
				continue
			}
			seen[e.Kind] = true
			if e.Kind == enrichers.KindBrief {
				member.Brief = e.At
			}
			if e.Kind == enrichers.KindSiteText {
				member.SiteText = e.At
			}
		}
		out = append(out, member)
	}
	return out, nil
}
