package store

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"time"
)

var openColumns = columns("seq:Seq prospect_list:ProspectList at:At user_id:UserID user_email:UserEmail filters:Filters ordering:Order size:Size members:Members passing:Passing")

func openRecords(ctx context.Context, tx *sql.Tx, id string, limit bool) ([]Open, error) {
	q := " WHERE prospect_list=? ORDER BY seq DESC"
	if limit {
		q += " LIMIT 1"
	}
	out, err := readRecords[Open](ctx, tx, "opens", openColumns, q, id)
	for i := range out {
		out[i].Filters = normalFilters(out[i].Filters)
		out[i].Order = normalOrder(out[i].Order)
		if out[i].Members == nil {
			out[i].Members = []OpenMember{}
		}
		if out[i].Passing == nil {
			out[i].Passing = []string{}
		}
	}
	return out, err
}

// Opens returns immutable snapshots, newest first, including deleted lists.
func (s *Store) Opens(ctx context.Context, prospectList string) ([]Open, error) {
	var out []Open
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = openRecords(ctx, tx, prospectList, false)
		return err
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// OpenProspectList reads a list and records its complete opening atomically.
func (s *Store) OpenProspectList(ctx context.Context, id, userID, userEmail string) (ProspectListRead, error) {
	var out ProspectListRead
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if userID == "" {
			return ErrUnreachable
		}
		now := s.cfg.Now()
		var passing []string
		var err error
		out, passing, err = s.readProspectList(ctx, tx, id, now)
		if err != nil {
			return err
		}
		o := Open{ProspectList: id, At: now.UTC().Truncate(time.Second), UserID: userID, UserEmail: userEmail, Filters: out.ProspectList.Filters, Order: out.ProspectList.Order, Size: out.ProspectList.Size, Members: []OpenMember{}, Passing: passing}
		for _, m := range out.Members {
			o.Members = append(o.Members, OpenMember{Candidate: m.Candidate.ID, Basis: m.Basis})
		}
		return insertRecord(ctx, tx, "opens", openColumns[1:], o, nil)
	}))
	if err != nil {
		return ProspectListRead{}, err
	}
	return out, nil
}

func prospectListChanges(ctx context.Context, tx *sql.Tx, k ProspectList, members []Ranked, now time.Time) (Changes, error) {
	out := Changes{DefinitionChanged: []string{}, Joined: []Joined{}, Left: []Left{}}
	os, err := openRecords(ctx, tx, k.ID, true)
	if err != nil {
		return Changes{}, err
	}
	if len(os) == 0 {
		return out, nil
	}
	p := os[0]
	out.Since = p.At
	if !reflect.DeepEqual(p.Filters, k.Filters) {
		out.DefinitionChanged = append(out.DefinitionChanged, DefinitionFilters)
	}
	if !reflect.DeepEqual(p.Order, k.Order) {
		out.DefinitionChanged = append(out.DefinitionChanged, DefinitionOrder)
	}
	if p.Size != k.Size {
		out.DefinitionChanged = append(out.DefinitionChanged, DefinitionSize)
	}
	previous := map[string]bool{}
	current := map[string]bool{}
	for _, m := range p.Members {
		previous[m.Candidate] = true
	}
	for _, m := range members {
		id := m.Candidate.ID
		current[id] = true
		if previous[id] {
			continue
		}
		why := WhyPasses
		if m.Candidate.Created.After(p.At) {
			why = WhyNew
		} else if slices.Contains(p.Passing, id) {
			why = WhyMovedUp
		}
		out.Joined = append(out.Joined, Joined{Candidate: id, Name: m.Candidate.Name, Basis: m.Basis, Why: why})
	}
	rejects, err := standingRejects(ctx, tx, k.ID)
	if err != nil {
		return Changes{}, err
	}
	rejected := map[string]bool{}
	for _, r := range rejects {
		rejected[r.Candidate] = true
	}
	for _, old := range p.Members {
		if current[old.Candidate] {
			continue
		}
		c, err := candidateKey(ctx, tx, old.Candidate)
		if err != nil {
			return Changes{}, err
		}
		left := Left{Candidate: c.ID, Name: c.Name, BasisThen: old.Basis, Why: WhyGone}
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM judgments WHERE candidate=?", c.ID).Scan(&count); err != nil {
			return Changes{}, err
		}
		if c.Status == StatusActive && count != 0 {
			ms, err := rankedWindow(ctx, tx, "SELECT id FROM candidates WHERE id=?", c.ID)
			if err != nil {
				return Changes{}, err
			}
			m := ms[0]
			left.BasisNow = m.Basis
			switch {
			case !k.Filters.ShowExcluded && m.Judgments.Excluded:
				left.Why = WhyExcluded
			case rejected[c.ID]:
				left.Why = WhyRejected
			default:
				filter, err := firstFailingFilter(ctx, tx, k.Filters, c.ID, now)
				if err != nil {
					return Changes{}, err
				}
				if filter != "" {
					left.Why = WhyFilter
					left.Filter = filter
				} else {
					left.Why = WhyCut
				}
			}
		}
		out.Left = append(out.Left, left)
	}
	return out, nil
}
func firstFailingFilter(ctx context.Context, tx *sql.Tx, f ProspectListFilters, id string, now time.Time) (string, error) {
	// Each independent filter uses the membership predicate, in the documented precedence.
	revenue := ProspectListFilters{RevenueMin: f.RevenueMin, RevenueMax: f.RevenueMax, RevenueUnknown: f.RevenueUnknown}
	tests := []struct {
		name   string
		filter ProspectListFilters
	}{
		{"revenue_max", revenue},
		{"max_minutes", ProspectListFilters{MaxMinutes: f.MaxMinutes, MinutesUnknown: f.MinutesUnknown}},
		{"categories", ProspectListFilters{Categories: f.Categories, CategoryUnknown: f.CategoryUnknown}},
		{"fleet", ProspectListFilters{Fleet: f.Fleet, FleetUnknown: f.FleetUnknown}},
		{"website", ProspectListFilters{Website: f.Website, WebsiteUnknown: f.WebsiteUnknown}},
		{"government", ProspectListFilters{Government: f.Government}},
		{"max_filing_age_days", ProspectListFilters{MaxFilingAgeDays: f.MaxFilingAgeDays, FilingUnknown: f.FilingUnknown}},
		{"min_campaign_age_days", ProspectListFilters{MinCampaignAgeDays: f.MinCampaignAgeDays}},
		{"class", ProspectListFilters{Class: f.Class, ClassUnknown: f.ClassUnknown}},
		{"basis", ProspectListFilters{Basis: f.Basis}},
	}
	for _, test := range tests {
		test.filter.ShowExcluded = true
		cond, args := prospectListCondition(ProspectList{Filters: test.filter}, now)
		var n int
		err := tx.QueryRowContext(ctx, `SELECT count(*) FROM judgments a JOIN candidates c ON c.id=a.candidate LEFT JOIN facts f ON f.candidate=c.id WHERE `+cond+` AND c.id=?`, append(args, id)...).Scan(&n)
		if err != nil {
			return "", err
		}
		if n != 0 {
			continue
		}
		if test.name == "revenue_max" && f.RevenueMin != nil {
			var low sql.NullInt64
			if err = tx.QueryRowContext(ctx, "SELECT revenue_low FROM judgments WHERE candidate=?", id).Scan(&low); err != nil {
				return "", err
			}
			if !low.Valid || low.Int64 < *f.RevenueMin {
				return "revenue_min", nil
			}
		}
		if test.name == "categories" && len(f.Categories) == 0 {
			return "category_unknown", nil
		}
		return test.name, nil
	}
	return "", nil
}
