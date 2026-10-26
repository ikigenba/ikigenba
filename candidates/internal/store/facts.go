package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

var factsColumns = columns("candidate:Candidate known_since:KnownSince government_match:GovernmentMatch newest_filing:NewestFiling categories:Categories fleet_verdict:FleetVerdict power_units:PowerUnits drivers:Drivers headcounts:Headcounts website:Website website_url:WebsiteURL website_source:WebsiteSource minutes:Minutes minutes_source:MinutesSource miles:Miles route_address:RouteAddress brief:Brief brief_labels:BriefLabels")

func unknownFacts(id string) Facts {
	return Facts{Candidate: id, Categories: []sources.Category{}, Records: []sources.Category{}, Headcounts: []sources.Headcount{}, BriefLabels: []string{}, FleetVerdict: sources.FleetUnknown, Website: sources.WebsiteUnknown}
}
func factsKey(ctx context.Context, tx *sql.Tx, id string) (Facts, error) {
	fs, err := readRecords[Facts](ctx, tx, "facts", factsColumns, " WHERE candidate=?", id)
	if err != nil {
		return Facts{}, err
	}
	if len(fs) == 0 {
		return Facts{}, ErrNotFound
	}
	f := fs[0]
	var b, d, dr, l, st, w, ws string
	if err = scanSQL(ctx, tx, "SELECT brief_at,drive_minutes_at,drive_route_at,link_facts_at,site_text_at,website_check_at,website_search_at FROM facts WHERE candidate=?", []any{id}, &b, &d, &dr, &l, &st, &w, &ws); err != nil {
		return Facts{}, err
	}
	for i, a := range []string{b, d, dr, l, st, w, ws} {
		if a != "" {
			t, err := time.Parse(time.RFC3339, a)
			if err != nil {
				return Facts{}, err
			}
			f.Written = append(f.Written, Written{Kind: []string{enrichers.KindBrief, enrichers.KindDriveMinutes, enrichers.KindDriveRoute, enrichers.KindLinkFacts, enrichers.KindSiteText, sources.WebsiteCheck, enrichers.KindWebsiteSearch}[i], At: t.UTC()})
		}
	}
	f.Records, err = recordCategories(ctx, tx, id)
	return f, err
}
func (s *Store) projectFacts(ctx context.Context, tx *sql.Tx, id string) error {
	f, extra, err := s.projectedFacts(ctx, tx, id)
	if err != nil || f.Candidate == "" {
		return err
	}
	return insertRecord(ctx, tx, "facts", factsColumns, f, extra)
}
func (s *Store) projectedFacts(ctx context.Context, tx *sql.Tx, id string) (Facts, map[string]any, error) {
	es, err := enrichmentRecords(ctx, tx, " WHERE candidate=? ORDER BY seq DESC", id)
	if err != nil {
		return Facts{}, nil, err
	}
	if len(es) == 0 {
		return Facts{}, nil, nil
	}
	f := unknownFacts(id)
	seen := map[string]bool{}
	goodBrief := false
	briefCategories := []sources.Category{}
	extra := map[string]any{"drive_minutes_at": "", "drive_route_at": "", "link_facts_at": "", "website_check_at": "", "site_text_at": "", "brief_at": "", "website_search_at": ""}
	for _, e := range es {
		if e.Kind == enrichers.KindBrief && !goodBrief && !(enrichers.Brief{}).Failed(e.Found) {
			var b enrichers.BriefFound
			if err = json.Unmarshal(e.Found, &b); err != nil {
				return Facts{}, nil, err
			}
			f.Brief = b.Summary
			f.BriefLabels = append(f.BriefLabels, b.Labels...)
			for _, c := range b.Categories {
				briefCategories = append(briefCategories, sources.Category{Name: c.Name, Source: enrichers.KindBrief})
			}
			goodBrief = true
		}
		if seen[e.Kind] {
			continue
		}
		seen[e.Kind] = true
		switch e.Kind {
		case enrichers.KindDriveMinutes:
			var v enrichers.MinutesFound
			if err = json.Unmarshal(e.Found, &v); err != nil {
				return Facts{}, nil, err
			}
			f.Minutes = v.Minutes
		case enrichers.KindLinkFacts:
			var v enrichers.LinkFound
			if err = json.Unmarshal(e.Found, &v); err != nil {
				return Facts{}, nil, err
			}
			if v.KnownSince != "" {
				f.KnownSince, err = time.Parse("2006-01-02", v.KnownSince)
				if err != nil {
					return Facts{}, nil, err
				}
			}
			if v.NewestFiling != "" {
				f.NewestFiling, err = time.Parse("2006-01-02", v.NewestFiling)
				if err != nil {
					return Facts{}, nil, err
				}
			}
			f.GovernmentMatch = v.GovernmentMatch
			f.FleetVerdict = v.FleetVerdict
			f.PowerUnits = v.PowerUnits
			f.Drivers = v.Drivers
			for _, h := range v.Headcounts {
				asOf := time.Time{}
				if h.Date != "" {
					asOf, err = time.Parse("2006-01-02", h.Date)
					if err != nil {
						return Facts{}, nil, err
					}
				}
				f.Headcounts = append(f.Headcounts, sources.Headcount{Scraper: h.Scraper, Kind: h.Kind, Count: h.Count, AsOf: asOf, Hours: h.Hours})
			}
			for _, c := range v.Categories {
				f.Categories = append(f.Categories, sources.Category{Name: c.Name, Source: c.Source})
			}
		}
		extra[e.Kind+"_at"] = e.At.Format(time.RFC3339)
	}
	if f.Minutes != nil {
		f.MinutesSource = MinutesByZip
	}
	// The address is needed only when a recorded route could decide the drive.
	if seen[enrichers.KindDriveRoute] && s.cfg.Origin != "" {
		c, err := candidateKey(ctx, tx, id)
		if err != nil {
			return Facts{}, nil, err
		}
		inputs := (enrichers.DriveRoute{}).Inputs(enrichers.Subject{Street: c.Street, City: c.City, State: c.State, Zip: c.Zip, Origin: s.cfg.Origin})
		for _, e := range es {
			if inputs == "" || e.Kind != enrichers.KindDriveRoute || e.Inputs != inputs {
				continue
			}
			var g enrichers.RouteFound
			if json.Unmarshal(e.Found, &g) == nil && !g.Failed() && g.Minutes != nil {
				f.Minutes, f.MinutesSource, f.Miles, f.RouteAddress = g.Minutes, MinutesByRoute, g.Miles, g.Address
				break
			}
		}
	}
	f.WebsiteSource, f.Website, f.WebsiteURL = websiteFacts(es)
	if len(briefCategories) > 0 {
		f.Categories = briefCategories
	}
	sort.Slice(f.Categories, func(i, j int) bool {
		a, b := f.Categories[i], f.Categories[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Source < b.Source
	})
	unique := f.Categories[:0]
	for _, c := range f.Categories {
		if len(unique) == 0 || unique[len(unique)-1] != c {
			unique = append(unique, c)
		}
	}
	f.Categories = unique
	return f, extra, nil
}

// GetFacts reads the materialized enrichment facts.
func (s *Store) GetFacts(ctx context.Context, candidate string) (Facts, error) {
	var out Facts
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error { var err error; out, err = factsKey(ctx, tx, candidate); return err }))
	return out, err
}

// FillFacts repairs projections and refreshes the headcount and label columns.
func (s *Store) FillFacts(ctx context.Context) error {
	return unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		cs, err := readRecords[Candidate](ctx, tx, "candidates", candidateColumns, " WHERE status=? AND EXISTS (SELECT 1 FROM enrichments WHERE enrichments.candidate=candidates.id)", StatusActive)
		if err != nil {
			return err
		}
		for _, c := range cs {
			old, err := factsKey(ctx, tx, c.ID)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
			missing := errors.Is(err, ErrNotFound)
			var routes int64
			if err = scanSQL(ctx, tx, "SELECT COUNT(*) FROM enrichments WHERE candidate=? AND kind=?", []any{c.ID, enrichers.KindDriveRoute}, &routes); err != nil {
				return err
			}
			f, extra, err := s.projectedFacts(ctx, tx, c.ID)
			if err != nil {
				return err
			}
			repair := missing || (old.Minutes == nil && old.MinutesSource != "") || (old.Minutes != nil && old.MinutesSource == "") || routes > 0
			if !repair {
				if reflect.DeepEqual(old.Headcounts, f.Headcounts) && reflect.DeepEqual(old.BriefLabels, f.BriefLabels) {
					continue
				}
				headcounts, err := encodeValue(reflect.ValueOf(f.Headcounts))
				if err != nil {
					return err
				}
				labels, err := json.Marshal(f.BriefLabels)
				if err != nil {
					return err
				}
				if err = execSQL(ctx, tx, "UPDATE facts SET headcounts=?,brief_labels=? WHERE candidate=?", headcounts, string(labels), c.ID); err != nil {
					return err
				}
				continue
			}
			projected := f
			projected.Records = old.Records
			for _, kind := range []string{enrichers.KindBrief, enrichers.KindDriveMinutes, enrichers.KindDriveRoute, enrichers.KindLinkFacts, enrichers.KindSiteText, sources.WebsiteCheck, enrichers.KindWebsiteSearch} {
				if at, ok := extra[kind+"_at"].(string); ok && at != "" {
					parsed, err := time.Parse(time.RFC3339, at)
					if err != nil {
						return err
					}
					projected.Written = append(projected.Written, Written{Kind: kind, At: parsed})
				}
			}
			if !missing && reflect.DeepEqual(old, projected) {
				continue
			}
			if err = insertRecord(ctx, tx, "facts", factsColumns, f, extra); err != nil {
				return err
			}
		}
		return nil
	}))
}

// ClassifiedBy identifies the evidence that supplies the categories.
func ClassifiedBy(categories []sources.Category) string {
	if len(categories) == 0 {
		return ""
	}
	for _, c := range categories {
		if c.Source != enrichers.KindBrief {
			return enrichers.SourceLinks
		}
	}
	return enrichers.KindBrief
}

// RecordsDiffer compares a brief classification with record category names.
func RecordsDiffer(categories, records []sources.Category) bool {
	if ClassifiedBy(categories) != enrichers.KindBrief {
		return false
	}
	names := map[string]bool{}
	for _, c := range categories {
		names[c.Name] = true
	}
	other := map[string]bool{}
	for _, c := range records {
		other[c.Name] = true
	}
	if len(names) != len(other) {
		return true
	}
	for name := range names {
		if !other[name] {
			return true
		}
	}
	return false
}

func recordCategories(ctx context.Context, tx *sql.Tx, id string) ([]sources.Category, error) {
	es, err := enrichmentRecords(ctx, tx, " WHERE candidate=? AND kind=? ORDER BY seq DESC LIMIT 1", id, enrichers.KindLinkFacts)
	if err != nil {
		return nil, err
	}
	out := []sources.Category{}
	if len(es) == 0 {
		return out, nil
	}
	var found enrichers.LinkFound
	if err = json.Unmarshal(es[0].Found, &found); err != nil {
		return nil, err
	}
	for _, c := range found.Categories {
		out = append(out, sources.Category{Name: c.Name, Source: c.Source})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Source < out[j].Source
	})
	unique := out[:0]
	for _, c := range out {
		if len(unique) == 0 || unique[len(unique)-1] != c {
			unique = append(unique, c)
		}
	}
	return unique, nil
}

// websiteFacts chooses a working newest check, then a confirmed search,
// then a search that found no site. A failed check alone leaves it unknown.
func websiteFacts(es []Enrichment) (source, state, url string) {
	state = sources.WebsiteUnknown
	checkSeen := false
	var search *enrichers.WebsiteSearchFound
	unfound := false
	for _, e := range es {
		if e.Kind == sources.WebsiteCheck && !checkSeen {
			var v website.Result
			if json.Unmarshal(e.Found, &v) == nil {
				checkSeen = true
				if v.State == sources.WebsiteHTTPS || v.State == sources.WebsiteHTTP {
					return WebsiteByCheck, v.State, v.URL
				}
			}
		}
		if e.Kind == enrichers.KindWebsiteSearch && search == nil {
			var v enrichers.WebsiteSearchFound
			if json.Unmarshal(e.Found, &v) == nil && !v.Failed() {
				if v.Confirmation != nil && (v.Confirmation.State == sources.WebsiteHTTPS || v.Confirmation.State == sources.WebsiteHTTP) {
					search = &v
				}
				if v.Answer.URL == "" || v.Confirmation != nil && v.Confirmation.State == sources.WebsiteNone {
					unfound = true
				}
			}
		}
	}
	if search != nil {
		return WebsiteBySearch, search.Confirmation.State, search.Confirmation.URL
	}
	if unfound {
		return "", sources.WebsiteNone, ""
	}
	return "", state, ""
}

// MinutesByRoute and MinutesByZip identify the deciding drive result.
const (
	MinutesByRoute = "route"
	MinutesByZip   = "zip"
)
