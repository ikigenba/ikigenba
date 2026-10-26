package store

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func judgments(r *records, c Candidate, f Facts, t time.Time) ([]sources.Estimate, Judgments) {
	t = t.UTC().Truncate(time.Second)
	a := Judgments{Candidate: c.ID, Evidence: []sources.Evidence{}, Rules: sources.Rules, Calculated: t}
	estimates := map[string]sources.Estimate{}
	seen := map[string]bool{}
	evidence := map[string]sources.Evidence{}
	for _, l := range linksFor(r, c.ID) {
		if l.Gone {
			continue
		}
		rd := l.Reading
		if !seen[l.Scraper] {
			seen[l.Scraper] = true
			if rd.Estimate != nil {
				estimates[l.Scraper] = *rd.Estimate
			}
		}
		for _, e := range rd.Evidence {
			e.Weight = judging.Weight(e, rd.Filed, t)
			if e.Weight <= 0 {
				continue
			}
			old, ok := evidence[e.Name]
			if !ok || e.Weight > old.Weight {
				evidence[e.Name] = e
			}
		}
	}
	if (f.Website == sources.WebsiteHTTPS || f.Website == sources.WebsiteHTTP) && f.WebsiteSource != WebsiteBySearch {
		e := sources.Evidence{Name: sources.EvidenceWebsiteLive, Source: sources.WebsiteCheck, Weight: .5, Description: sources.DescribeWebsiteLive}
		if old, ok := evidence[e.Name]; !ok || e.Weight > old.Weight {
			evidence[e.Name] = e
		}
	}
	if e := judging.HeadcountEstimate(f.Headcounts, f.BriefLabels, f.Categories); e != nil {
		estimates[e.Source] = *e
	}
	if staff := judging.PickHeadcount(f.Headcounts); staff != nil {
		a.HeadcountLow, a.HeadcountHigh, a.HeadcountBasis = &staff.Low, &staff.High, staff.Basis
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
	a.Operating = judging.Operating(a.Evidence)
	a.OperatingKey = judging.OperatingKey(a.Operating)
	if len(es) > 0 {
		combined := judging.Combine(es)
		a.RevenueLow = &combined.Low
		a.RevenueHigh = combined.High
		a.RevenueConfidence = &combined.Confidence
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

// JudgeAll replaces one page's judgments at the caller's time.
func (s *Store) JudgeAll(ctx context.Context, after string, n int, at time.Time) (string, Counts, error) {
	var out Counts
	var last string
	err := unreachable(s.retryUnit(ctx, func(ctx context.Context, tx *sql.Tx) error {
		out, last = Counts{}, ""
		if n < 1 || at.IsZero() {
			return ErrUnreachable
		}
		rows, err := querySQL(ctx, tx, "SELECT id FROM candidates INDEXED BY sqlite_autoindex_candidates_1 WHERE status=? AND id>? ORDER BY id LIMIT ?", StatusActive, after, n)
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
			last = id
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		out, err = judgeKeys(ctx, tx, ids, at)
		return err
	}))
	if err != nil {
		return "", Counts{}, err
	}
	return last, out, nil
}
