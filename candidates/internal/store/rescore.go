package store

import (
	"context"
	"database/sql"
	"math"
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
	a.Effective = scoring.Effective(a.Confidence, a.Filed, t)
	a.Score = scoring.Score(scoring.Point(a.Low, a.High), a.Effective)
	a.ScoreKey = scoreKey(a.Score)
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
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		out, err = rescoreKeys(ctx, tx, ids, s.now())
		return err
	}))
	if err != nil {
		return Counts{}, err
	}
	return out, nil
}

// RescoreAll replaces every active candidate's score.
func (s *Store) RescoreAll(ctx context.Context) (Counts, error) {
	var out Counts
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT id FROM candidates WHERE status=?", StatusActive)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		out, err = rescoreKeys(ctx, tx, ids, s.now())
		return err
	}))
	if err != nil {
		return Counts{}, err
	}
	return out, nil
}

func scoreKey(score float64) int64 {
	x := score * 1000000
	if x >= 0x1p63 {
		return math.MaxInt64
	}
	if x < -0x1p63 {
		return math.MinInt64
	}
	return int64(x)
}

// RefreshShortlist updates persisted freshness and repairs cargo lookup rows.
func (s *Store) RefreshShortlist(ctx context.Context) error {
	return unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := execSQL(ctx, tx, `UPDATE assessments SET effective = confidence * w.f, score = base * w.f, score_key = CAST(base * w.f * 1000000.0 AS INTEGER) FROM (SELECT candidate AS id, CASE WHEN filed = '' THEN 0.0 WHEN julianday(?1) <= julianday(filed, '+2 years') THEN 1.0 WHEN julianday(?1) >= julianday(filed, '+5 years') THEN 0.0 ELSE (julianday(filed, '+5 years') - julianday(?1)) / (julianday(filed, '+5 years') - julianday(filed, '+2 years')) END AS f FROM assessments) AS w WHERE assessments.candidate = w.id`, s.now().Format(time.RFC3339)); err != nil {
			return err
		}
		if err := execSQL(ctx, tx, "DELETE FROM candidate_cargo"); err != nil {
			return err
		}
		return execSQL(ctx, tx, `INSERT INTO candidate_cargo(candidate,cargo) SELECT DISTINCT candidates.id, j.value FROM candidates, json_each(candidates.cargo) AS j WHERE j.type = 'text'`)
	}))
}
