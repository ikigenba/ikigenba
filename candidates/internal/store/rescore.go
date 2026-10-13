package store

import (
	"context"
	"sort"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func assessment(r *records, c Candidate, t time.Time) ([]sources.Estimate, Assessment) {
	es := []sources.Estimate{}
	a := Assessment{Candidate: c.ID, Exclusions: []sources.Exclusion{}, Rules: sources.Rules, Scored: t}
	seen := map[string]bool{}
	exclusions := map[sources.Exclusion]bool{}
	for _, l := range linksFor(r, c.ID) {
		if l.Gone {
			continue
		}
		rd := l.Reading
		if !seen[l.Scraper] {
			seen[l.Scraper] = true
			if rd.Fleet {
				a.Fleet = true
			}
			if rd.Estimate != nil {
				es = append(es, *rd.Estimate)
			}
		}
		if rd.Filed.After(a.Filed) {
			a.Filed = rd.Filed
		}
		for _, e := range rd.Exclusions {
			if !exclusions[e] {
				exclusions[e] = true
				a.Exclusions = append(a.Exclusions, e)
			}
		}
	}
	sort.Slice(es, func(i, j int) bool { return es[i].Source < es[j].Source })
	combined := scoring.Combine(es)
	a.Low = combined.Low
	a.High = combined.High
	a.Confidence = combined.Confidence
	if m, ok := r.zips[c.Zip]; ok {
		a.Minutes = &m
	}
	a.Excluded = len(a.Exclusions) > 0
	for i := len(r.rulings) - 1; i >= 0; i-- {
		d := r.rulings[i]
		if d.Candidate != c.ID || d.Kind != KindExclude && d.Kind != KindInclude {
			continue
		}
		a.Excluded = d.Kind == KindExclude
		if a.Excluded {
			a.Exclusions = append(a.Exclusions, sources.Exclusion{Reason: sources.ExcludeRuling})
		}
		break
	}
	a.Base = scoring.Score(scoring.Point(a.Low, a.High), a.Confidence)
	return es, a
}
func rescore(r *records, ids []string, t time.Time) Counts {
	out := Counts{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		c, ok := r.candidates[id]
		if !ok || c.Status != StatusActive {
			continue
		}
		es, a := assessment(r, c, t)
		r.estimates[id] = es
		r.assessments[id] = a
		out.Rescored++
		if a.Minutes == nil {
			out.Unranged++
		}
	}
	return out
}

// Rescore replaces the scores of the active candidates named.
func (s *Store) Rescore(ctx context.Context, ids []string) (Counts, error) {
	var out Counts
	err := s.transaction(ctx, true, func(r *records) error { out = rescore(r, ids, s.now()); return nil })
	if err != nil {
		return Counts{}, err
	}
	return out, nil
}

// RescoreAll replaces every active candidate’s score.
func (s *Store) RescoreAll(ctx context.Context) (Counts, error) {
	var out Counts
	err := s.transaction(ctx, true, func(r *records) error {
		ids := []string{}
		for id, c := range r.candidates {
			if c.Status == StatusActive {
				ids = append(ids, id)
			}
		}
		out = rescore(r, ids, s.now())
		return nil
	})
	if err != nil {
		return Counts{}, err
	}
	return out, nil
}
