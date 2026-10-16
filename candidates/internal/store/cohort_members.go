package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func cohortCondition(k Cohort, now time.Time) (string, []any) {
	parts := []string{`c.status='active'`, `NOT EXISTS (SELECT 1 FROM rejects AS r WHERE r.cohort=? AND r.candidate=c.id AND r.kind='reject' AND NOT EXISTS (SELECT 1 FROM rejects AS n WHERE n.cohort=r.cohort AND n.candidate=r.candidate AND n.seq>r.seq))`}
	args := []any{k.ID}
	f := k.Filters
	add := func(q string, a ...any) { parts = append(parts, q); args = append(args, a...) }
	if !f.ShowExcluded {
		add(`a.excluded=0`)
	}
	if f.RevenueMin != nil || f.RevenueMax != nil {
		lo := int64(0)
		if f.RevenueMin != nil {
			lo = *f.RevenueMin
		}
		add(`((a.revenue_low>=? AND (? IS NULL OR a.revenue_low<=?)) OR (a.revenue_low IS NULL AND ?))`, lo, f.RevenueMax, f.RevenueMax, f.RevenueUnknown)
	}
	if f.MaxMinutes != nil {
		add(`(a.minutes<=? OR (a.minutes IS NULL AND ?))`, *f.MaxMinutes, f.MinutesUnknown)
	}
	if len(f.Categories) > 0 {
		b, _ := json.Marshal(f.Categories)
		add(`(EXISTS (SELECT 1 FROM json_each(a.categories) AS ccat JOIN json_each(?) AS wanted ON json_extract(ccat.value,'$.Name')=wanted.value) OR ((a.categories IS NULL OR json_array_length(a.categories)=0) AND ?))`, string(b), f.CategoryUnknown)
	}
	if f.Fleet != "" {
		add(`(a.fleet_verdict=? OR (a.fleet_verdict=? AND ?))`, f.Fleet, sources.FleetUnknown, f.FleetUnknown)
	}
	if f.Government != nil {
		if *f.Government {
			add(`coalesce(a.government_match,'')<>''`)
		} else {
			add(`coalesce(a.government_match,'')=''`)
		}
	}
	if f.MaxFilingAgeDays != nil {
		add(`((coalesce(a.newest_filing,'')<>'' AND julianday(?)-julianday(substr(a.newest_filing,1,10))<=?) OR (coalesce(a.newest_filing,'')='' AND ?))`, now.UTC().Format("2006-01-02"), *f.MaxFilingAgeDays, f.FilingUnknown)
	}
	if f.Class != "" {
		add(`(c.class=? OR ((c.class='' OR c.class=?) AND ?))`, f.Class, sources.ClassUnknown, f.ClassUnknown)
	}
	return strings.Join(parts, " AND "), args
}
func cohortOrdering(o []CohortOrder) string {
	fields := map[string][2]string{
		OrderOperatingConfidence: {"a.operating", "a.operating_key"}, OrderRevenueLow: {"a.revenue_low", "a.revenue_low"}, OrderRevenueConfidence: {"a.revenue_confidence", "a.revenue_confidence"}, OrderDriveMinutes: {"a.minutes", "a.minutes"}, OrderNewestFiling: {"nullif(a.newest_filing,'')", "a.newest_filing"}, OrderKnownSince: {"nullif(a.known_since,'')", "a.known_since"},
	}
	parts := []string{}
	for _, term := range normalOrder(o) {
		f := fields[term.Field]
		direction := "ASC"
		if term.Direction == DirectionDesc {
			direction = "DESC"
		}
		parts = append(parts, f[0]+" IS NULL ASC", f[1]+" "+direction)
	}
	return strings.Join(append(parts, "c.id ASC"), ",")
}

// ReadCohort computes a saved search's present members and count.
func (s *Store) ReadCohort(ctx context.Context, id string) (CohortRead, error) {
	var out CohortRead
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		k, err := cohortKey(ctx, tx, id)
		if err != nil {
			return err
		}
		condition, args := cohortCondition(k, s.cfg.Now())
		base := ` FROM assessments AS a JOIN candidates AS c ON c.id=a.candidate WHERE ` + condition
		var count int64
		if err = tx.QueryRowContext(ctx, `SELECT count(*)`+base, args...).Scan(&count); err != nil {
			return err
		}
		members, err := rankedWindow(ctx, tx, `SELECT c.id`+base+` ORDER BY `+cohortOrdering(k.Order)+` LIMIT ?`, append(args, k.Size)...)
		if err != nil {
			return err
		}
		rejects, err := standingRejects(ctx, tx, id)
		if err != nil {
			return err
		}
		out = CohortRead{Cohort: k, Matching: count, Members: members, Rejects: rejects}
		return nil
	}))
	if err != nil {
		return CohortRead{}, err
	}
	return out, nil
}
