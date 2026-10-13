package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"

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

// GetAssessment reads a candidate’s last assessment.
func (s *Store) GetAssessment(ctx context.Context, id string) (Assessment, error) {
	var out Assessment
	err := s.readScope(ctx, "assessment", id, func(r *records) error {
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
		var err error
		out, err = readRecords[Candidate](ctx, tx, "candidates", candidateColumns, ` WHERE status=?1 AND (?2='' OR instr(names_lower,?2)>0) AND (?3='' OR zip=?3) AND (?4='' OR (?5<>'' AND id IN (SELECT candidate FROM links WHERE phone_key=?5))) AND (?6='' OR created<?7 OR (created=?7 AND id<?6)) ORDER BY created DESC,id DESC LIMIT ?8`, StatusActive, name, f.Zip, f.Phone, phone, id, created, n)
		return err
	}))
	return out, err
}

// Shortlist reads a window of persisted eligible scores in score-key order.
func (s *Store) Shortlist(ctx context.Context, f ShortlistFilter, scoreKey int64, id string, n int) ([]Ranked, error) {
	var out []Ranked
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			return ErrUnreachable
		}
		rows, err := tx.QueryContext(ctx, `SELECT a.candidate FROM assessments AS a INDEXED BY assessments_score JOIN candidates AS c ON c.id=a.candidate WHERE c.status=?1 AND a.minutes IS NOT NULL AND a.minutes<=?2 AND (a.high IS NULL OR a.high>=?3) AND a.fleet=1 AND a.excluded=0 AND a.effective>0 AND (?4='' OR c.class=?4) AND (?5='' OR c.id IN (SELECT candidate FROM candidate_cargo WHERE cargo=?5)) AND (?6='' OR a.score_key<?7 OR (a.score_key=?7 AND a.candidate>?6)) ORDER BY a.score_key DESC,a.candidate ASC LIMIT ?8`, StatusActive, f.MaxMinutes, f.MinRevenue, f.Class, f.Cargo, id, scoreKey, n)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var key string
			if err = rows.Scan(&key); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, key)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		out = []Ranked{}
		for _, key := range ids {
			c, err := candidateKey(ctx, tx, key)
			if err != nil {
				return err
			}
			a, err := readRecords[Assessment](ctx, tx, "assessments", assessmentColumns, " WHERE candidate=?", key)
			if err != nil {
				return err
			}
			out = append(out, Ranked{Candidate: c, Assessment: a[0]})
		}
		return nil
	}))
	return out, err
}
