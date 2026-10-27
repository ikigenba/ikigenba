package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func prospectListCondition(k ProspectList, now time.Time) (string, []any) {
	parts := []string{`c.status='active'`, `NOT EXISTS (SELECT 1 FROM rejects AS r WHERE r.prospect_list=? AND r.candidate=c.id AND r.kind='reject' AND NOT EXISTS (SELECT 1 FROM rejects AS n WHERE n.prospect_list=r.prospect_list AND n.candidate=r.candidate AND n.seq>r.seq))`}
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
		add(`((a.revenue_low>=? AND (? IS NULL OR a.revenue_high<=? OR (a.revenue_high IS NULL AND ?))) OR (a.revenue_low IS NULL AND ?))`, lo, f.RevenueMax, f.RevenueMax, f.RevenueUnknown, f.RevenueUnknown)
	}
	if f.MaxMinutes != nil {
		add(`(f.minutes<=? OR (f.minutes IS NULL AND ?))`, *f.MaxMinutes, f.MinutesUnknown)
	}
	if len(f.Categories) > 0 {
		b, _ := json.Marshal(f.Categories)
		add(`(EXISTS (SELECT 1 FROM json_each(f.categories) AS ccat JOIN json_each(?) AS wanted ON json_extract(ccat.value,'$.Name')=wanted.value) OR ((f.categories IS NULL OR json_array_length(f.categories)=0) AND ?))`, string(b), f.CategoryUnknown)
	} else if f.CategoryUnknown {
		add(`(f.categories IS NULL OR json_array_length(f.categories)=0)`)
	}
	if f.Fleet != "" {
		add(`(coalesce(f.fleet_verdict,'unknown')=? OR (coalesce(f.fleet_verdict,'unknown')=? AND ?))`, f.Fleet, sources.FleetUnknown, f.FleetUnknown)
	}
	if len(f.Website) > 0 {
		b, _ := json.Marshal(f.Website)
		add(`(f.website IN (SELECT value FROM json_each(?)) OR (coalesce(f.website,'unknown')=? AND ?))`, string(b), sources.WebsiteUnknown, f.WebsiteUnknown)
	}
	if len(f.Trend) > 0 {
		b, _ := json.Marshal(f.Trend)
		add(`(a.trend IN (SELECT value FROM json_each(?)) OR (a.trend=? AND ?))`, string(b), judging.TrendUnknown, f.TrendUnknown)
	}
	if f.Government != nil {
		if *f.Government {
			add(`coalesce(f.government_match,'')<>''`)
		} else {
			add(`coalesce(f.government_match,'')=''`)
		}
	}
	if f.MaxFilingAgeDays != nil {
		add(`((coalesce(f.newest_filing,'')<>'' AND julianday(?)-julianday(substr(f.newest_filing,1,10))<=?) OR (coalesce(f.newest_filing,'')='' AND ?))`, now.UTC().Format("2006-01-02"), *f.MaxFilingAgeDays, f.FilingUnknown)
	}
	if f.MinCampaignAgeDays != nil && *f.MinCampaignAgeDays > 0 {
		add(`NOT EXISTS (SELECT 1 FROM campaign_uses AS u WHERE u.candidate=c.id AND julianday(?)-julianday(substr(CASE WHEN u.added<>'' THEN u.added WHEN u.rejected='' THEN u.delivered WHEN u.delivered='' THEN u.rejected WHEN u.rejected<u.delivered THEN u.rejected ELSE u.delivered END,1,10))<?)`, now.UTC().Format("2006-01-02"), *f.MinCampaignAgeDays)
	}
	if f.Class != "" {
		add(`(c.class=? OR ((c.class='' OR c.class=?) AND ?))`, f.Class, sources.ClassUnknown, f.ClassUnknown)
	}
	if len(f.Basis) > 0 {
		b, _ := json.Marshal(f.Basis)
		add(basisExpression+` IN (SELECT value FROM json_each(?))`, string(b))
	}
	return strings.Join(parts, " AND "), args
}
func prospectListOrdering(o []ProspectListOrder) string {
	fields := map[string][2]string{
		OrderTrend:               {"a.trend_change", "a.trend_change"},
		OrderOperatingConfidence: {"a.operating", "a.operating_key"}, OrderRevenueLow: {"a.revenue_low", "a.revenue_low"}, OrderRevenueConfidence: {"a.revenue_confidence", "a.revenue_confidence"}, OrderDriveMinutes: {"f.minutes", "f.minutes"}, OrderNewestFiling: {"nullif(f.newest_filing,'')", "f.newest_filing"}, OrderKnownSince: {"nullif(f.known_since,'')", "f.known_since"},
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

// ReadProspectList computes a saved search's present members and count.
func (s *Store) ReadProspectList(ctx context.Context, id string) (ProspectListRead, error) {
	var out ProspectListRead
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, _, err = s.readProspectList(ctx, tx, id, s.cfg.Now())
		return err
	}))
	if err != nil {
		return ProspectListRead{}, err
	}
	return out, nil
}

const basisExpression = `(CASE WHEN EXISTS (SELECT 1 FROM enrichments e WHERE e.candidate=c.id AND e.kind='brief' AND json_extract(e.found,'$.status')='exited' AND json_extract(e.found,'$.exit_code')=0 AND coalesce(json_extract(e.found,'$.summary'),'')<>'') AND EXISTS (SELECT 1 FROM enrichments e WHERE e.candidate=c.id AND e.kind='drive_route' AND json_extract(e.found,'$.minutes') IS NOT NULL) THEN 'enriched' WHEN EXISTS (SELECT 1 FROM enrichments e WHERE e.candidate=c.id AND e.kind IN ('website_search','site_text','brief','drive_route')) THEN 'partial' ELSE 'records' END)`

func (s *Store) readProspectList(ctx context.Context, tx *sql.Tx, id string, now time.Time) (ProspectListRead, []string, error) {
	k, err := prospectListKey(ctx, tx, id)
	if err != nil {
		return ProspectListRead{}, nil, err
	}
	condition, args := prospectListCondition(k, now)
	base := ` FROM judgments AS a JOIN candidates AS c ON c.id=a.candidate LEFT JOIN facts AS f ON f.candidate=c.id WHERE ` + condition
	rows, err := tx.QueryContext(ctx, strings.Join([]string{`SELECT c.id`, base, ` ORDER BY `, prospectListOrdering(k.Order)}, ""), args...)
	if err != nil {
		return ProspectListRead{}, nil, err
	}
	passing := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		passing = append(passing, id)
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return ProspectListRead{}, nil, err
	}
	members, err := rankedWindow(ctx, tx, `SELECT c.id`+base+` ORDER BY `+prospectListOrdering(k.Order)+` LIMIT ?`, append(args, k.Size)...)
	if err != nil {
		return ProspectListRead{}, nil, err
	}
	rejects, err := standingRejects(ctx, tx, id)
	if err != nil {
		return ProspectListRead{}, nil, err
	}
	out := ProspectListRead{ProspectList: k, Matching: int64(len(passing)), Members: members, Rejects: rejects}
	for _, m := range members {
		if ClassifiedBy(m.Facts.Categories) == enrichers.KindBrief {
			out.Classified++
		}
		switch m.Basis {
		case BasisRecords:
			out.ByBasis.Records++
		case BasisPartial:
			out.ByBasis.Partial++
		case BasisEnriched:
			out.ByBasis.Enriched++
		}
	}
	out.Changes, err = prospectListChanges(ctx, tx, k, members, now)
	return out, passing, err
}
