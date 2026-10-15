package store

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func assessment(r *records, c Candidate, t time.Time) ([]sources.Estimate, Assessment) {
	t = t.UTC().Truncate(time.Second)
	a := Assessment{Candidate: c.ID, Evidence: []sources.Evidence{}, Categories: []sources.Category{}, Rules: sources.Rules, Calculated: t}
	estimates := map[string]sources.Estimate{}
	seen := map[string]bool{}
	evidence := map[string]sources.Evidence{}
	categories := map[sources.Category]bool{}
	for _, l := range linksFor(r, c.ID) {
		if l.Gone {
			continue
		}
		rd := l.Reading
		if !seen[l.Scraper] {
			seen[l.Scraper] = true
			a.Fleet = a.Fleet || rd.Fleet
			if rd.Estimate != nil {
				estimates[l.Scraper] = *rd.Estimate
			}
		} else if l.Scraper == ppp.Name && rd.Estimate != nil {
			old, ok := estimates[l.Scraper]
			if !ok || rd.Estimate.Low > old.Low {
				estimates[l.Scraper] = *rd.Estimate
			}
		}
		if !rd.KnownSince.IsZero() && (a.KnownSince.IsZero() || rd.KnownSince.Before(a.KnownSince)) {
			a.KnownSince = rd.KnownSince
		}
		if rd.Filed.After(a.NewestFiling) {
			a.NewestFiling = rd.Filed
		}
		if a.GovernmentMatch == "" {
			a.GovernmentMatch = rd.GovernmentMatch
		}
		if rd.Category != "" {
			categories[sources.Category{Name: rd.Category, Source: l.Scraper}] = true
		}
		for _, e := range rd.Evidence {
			e.Weight = scoring.Weight(e, rd.Filed, t)
			if e.Weight <= 0 {
				continue
			}
			old, ok := evidence[e.Name]
			if !ok || e.Weight > old.Weight {
				evidence[e.Name] = e
			}
		}
	}
	es := []sources.Estimate{}
	for _, e := range estimates {
		es = append(es, e)
	}
	sort.Slice(es, func(i, j int) bool { return es[i].Source < es[j].Source })
	for _, e := range evidence {
		a.Evidence = append(a.Evidence, e)
	}
	sort.Slice(a.Evidence, func(i, j int) bool {
		if a.Evidence[i].Weight != a.Evidence[j].Weight {
			return a.Evidence[i].Weight > a.Evidence[j].Weight
		}
		return a.Evidence[i].Name < a.Evidence[j].Name
	})
	for c := range categories {
		a.Categories = append(a.Categories, c)
	}
	sort.Slice(a.Categories, func(i, j int) bool {
		if a.Categories[i].Name != a.Categories[j].Name {
			return a.Categories[i].Name < a.Categories[j].Name
		}
		return a.Categories[i].Source < a.Categories[j].Source
	})
	a.Operating = scoring.Operating(a.Evidence)
	a.OperatingKey = scoring.OperatingKey(a.Operating)
	if len(es) > 0 {
		combined := scoring.Combine(es)
		a.RevenueLow = &combined.Low
		a.RevenueHigh = combined.High
		a.RevenueConfidence = &combined.Confidence
	}
	if m, ok := r.zips[c.Zip]; ok {
		a.Minutes = &m
	}
	for i := len(r.rulings) - 1; i >= 0; i-- {
		d := r.rulings[i]
		if d.Candidate == c.ID && (d.Kind == KindExclude || d.Kind == KindInclude) {
			a.Excluded = d.Kind == KindExclude
			break
		}
	}
	return es, a
}

// Judge replaces the judgments of the active candidates named by ids.
func (s *Store) Judge(ctx context.Context, ids []string) (Counts, error) {
	var out Counts
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		out, err = judgeKeys(ctx, tx, ids, s.now())
		return err
	}))
	if err != nil {
		return Counts{}, err
	}
	return out, nil
}

// JudgeAll replaces every active candidate's judgments and repairs cargo lookups.
func (s *Store) JudgeAll(ctx context.Context) (Counts, error) {
	var out Counts
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := querySQL(ctx, tx, "SELECT id FROM candidates WHERE status=?", StatusActive)
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
		out, err = judgeKeys(ctx, tx, ids, s.now())
		if err != nil {
			return err
		}
		if err = execSQL(ctx, tx, "DELETE FROM candidate_cargo"); err != nil {
			return err
		}
		return execSQL(ctx, tx, `INSERT INTO candidate_cargo(candidate,cargo) SELECT DISTINCT candidates.id,j.value FROM candidates,json_each(candidates.cargo) AS j WHERE j.type='text'`)
	}))
	if err != nil {
		return Counts{}, err
	}
	return out, nil
}
