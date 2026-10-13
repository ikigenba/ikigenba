package store

import (
	"context"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// GetCandidate reads one candidate, including merged candidates.
func (s *Store) GetCandidate(ctx context.Context, id string) (Candidate, error) {
	var out Candidate
	err := s.transaction(ctx, false, func(r *records) error {
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
		if !a.Reading.Filed.Equal(b.Reading.Filed) {
			return a.Reading.Filed.After(b.Reading.Filed)
		}
		if !a.LastSeen.Equal(b.LastSeen) {
			return a.LastSeen.After(b.LastSeen)
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
	err := s.transaction(ctx, false, func(r *records) error {
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
	err := s.transaction(ctx, false, func(r *records) error {
		if _, ok := r.candidates[id]; !ok {
			return ErrNotFound
		}
		out = append([]sources.Estimate{}, r.estimates[id]...)
		return nil
	})
	return out, err
}

// GetAssessment reads a candidate’s last assessment.
func (s *Store) GetAssessment(ctx context.Context, id string) (Assessment, error) {
	var out Assessment
	err := s.transaction(ctx, false, func(r *records) error {
		var ok bool
		out, ok = r.assessments[id]
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
	err := s.transaction(ctx, false, func(r *records) error {
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
func matchesCandidate(r *records, c Candidate, f CandidateFilter) bool {
	if f.Zip != "" && f.Zip != c.Zip {
		return false
	}
	if f.Name != "" {
		ok := false
		for _, n := range c.Names {
			if strings.Contains(strings.ToLower(n), strings.ToLower(f.Name)) {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	if f.Phone != "" {
		k := sources.PhoneKey(f.Phone)
		if k == "" {
			return false
		}
		ok := false
		for _, l := range r.links {
			if l.Candidate == c.ID && l.Reading.PhoneKey == k {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// ListCandidates reads a filtered window of active candidates.
func (s *Store) ListCandidates(ctx context.Context, f CandidateFilter, offset, limit int64) ([]Candidate, error) {
	var out []Candidate
	err := s.transaction(ctx, false, func(r *records) error {
		if !validWindow(offset, limit) {
			return ErrUnreachable
		}
		out = []Candidate{}
		for _, c := range r.candidates {
			if c.Status == StatusActive && matchesCandidate(r, c, f) {
				out = append(out, c)
			}
		}
		sort.Slice(out, func(i, j int) bool {
			a, b := out[i], out[j]
			if !a.Created.Equal(b.Created) {
				return a.Created.After(b.Created)
			}
			return a.ID > b.ID
		})
		out = window(out, offset, limit)
		return nil
	})
	return out, err
}

// Shortlist ranks eligible candidates at the supplied clock time.
func (s *Store) Shortlist(ctx context.Context, f ShortlistFilter, offset, limit int64) ([]Ranked, error) {
	var out []Ranked
	err := s.transaction(ctx, false, func(r *records) error {
		if !validWindow(offset, limit) {
			return ErrUnreachable
		}
		t := s.cfg.Now()
		out = []Ranked{}
		for _, c := range r.candidates {
			a, ok := r.assessments[c.ID]
			if !ok || c.Status != StatusActive || a.Minutes == nil || *a.Minutes > f.MaxMinutes || a.High != nil && *a.High < f.MinRevenue || !a.Fleet || a.Excluded || f.Class != "" && f.Class != c.Class {
				continue
			}
			if f.Cargo != "" {
				found := false
				for _, v := range c.Cargo {
					if v == f.Cargo {
						found = true
					}
				}
				if !found {
					continue
				}
			}
			e := scoring.Effective(a.Confidence, a.Filed, t)
			if e <= 0 {
				continue
			}
			out = append(out, Ranked{c, a, e, scoring.Score(scoring.Point(a.Low, a.High), e)})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Score != out[j].Score {
				return out[i].Score > out[j].Score
			}
			return out[i].Candidate.ID < out[j].Candidate.ID
		})
		out = window(out, offset, limit)
		return nil
	})
	return out, err
}
