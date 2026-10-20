package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

var factsColumns = columns("candidate:Candidate known_since:KnownSince government_match:GovernmentMatch newest_filing:NewestFiling categories:Categories fleet_verdict:FleetVerdict power_units:PowerUnits drivers:Drivers website:Website website_url:WebsiteURL website_source:WebsiteSource minutes:Minutes brief:Brief")

func unknownFacts(id string) Facts {
	return Facts{Candidate: id, Categories: []sources.Category{}, Records: []sources.Category{}, FleetVerdict: sources.FleetUnknown, Website: sources.WebsiteUnknown}
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
	var b, d, l, st, w, ws string
	if err = scanSQL(ctx, tx, "SELECT brief_at,drive_minutes_at,link_facts_at,site_text_at,website_check_at,website_search_at FROM facts WHERE candidate=?", []any{id}, &b, &d, &l, &st, &w, &ws); err != nil {
		return Facts{}, err
	}
	for i, a := range []string{b, d, l, st, w, ws} {
		if a != "" {
			t, err := time.Parse(time.RFC3339, a)
			if err != nil {
				return Facts{}, err
			}
			f.Written = append(f.Written, Written{Kind: []string{enrichers.KindBrief, enrichers.KindDriveMinutes, enrichers.KindLinkFacts, enrichers.KindSiteText, sources.WebsiteCheck, enrichers.KindWebsiteSearch}[i], At: t.UTC()})
		}
	}
	f.Records, err = recordCategories(ctx, tx, id)
	return f, err
}
func projectFacts(ctx context.Context, tx *sql.Tx, id string) error {
	es, err := enrichmentRecords(ctx, tx, " WHERE candidate=? ORDER BY seq DESC", id)
	if err != nil {
		return err
	}
	if len(es) == 0 {
		return nil
	}
	f := unknownFacts(id)
	seen := map[string]bool{}
	goodBrief := false
	briefCategories := []sources.Category{}
	extra := map[string]any{"drive_minutes_at": "", "link_facts_at": "", "website_check_at": "", "site_text_at": "", "brief_at": "", "website_search_at": ""}
	for _, e := range es {
		if e.Kind == enrichers.KindBrief && !goodBrief && !(enrichers.Brief{}).Failed(e.Found) {
			var b enrichers.BriefFound
			if err = json.Unmarshal(e.Found, &b); err != nil {
				return err
			}
			f.Brief = b.Summary
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
				return err
			}
			f.Minutes = v.Minutes
		case enrichers.KindLinkFacts:
			var v enrichers.LinkFound
			if err = json.Unmarshal(e.Found, &v); err != nil {
				return err
			}
			if v.KnownSince != "" {
				f.KnownSince, err = time.Parse("2006-01-02", v.KnownSince)
				if err != nil {
					return err
				}
			}
			if v.NewestFiling != "" {
				f.NewestFiling, err = time.Parse("2006-01-02", v.NewestFiling)
				if err != nil {
					return err
				}
			}
			f.GovernmentMatch = v.GovernmentMatch
			f.FleetVerdict = v.FleetVerdict
			f.PowerUnits = v.PowerUnits
			f.Drivers = v.Drivers
			for _, c := range v.Categories {
				f.Categories = append(f.Categories, sources.Category{Name: c.Name, Source: c.Source})
			}
		}
		extra[e.Kind+"_at"] = e.At.Format(time.RFC3339)
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
	return insertRecord(ctx, tx, "facts", factsColumns, f, extra)
}

// GetFacts reads the materialized enrichment facts.
func (s *Store) GetFacts(ctx context.Context, candidate string) (Facts, error) {
	var out Facts
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error { var err error; out, err = factsKey(ctx, tx, candidate); return err }))
	return out, err
}

// FillFacts fills only missing projections from existing results.
func (s *Store) FillFacts(ctx context.Context) error {
	return unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		cs, err := readRecords[Candidate](ctx, tx, "candidates", candidateColumns, " WHERE status=? AND NOT EXISTS (SELECT 1 FROM facts WHERE facts.candidate=candidates.id)", StatusActive)
		if err != nil {
			return err
		}
		for _, c := range cs {
			if err = projectFacts(ctx, tx, c.ID); err != nil {
				return err
			}
		}
		return nil
	}))
}
func overlayFacts(a *Assessment, f Facts) {
	a.KnownSince = f.KnownSince
	a.GovernmentMatch = f.GovernmentMatch
	a.NewestFiling = f.NewestFiling
	a.Categories = append([]sources.Category{}, f.Categories...)
	a.FleetVerdict = f.FleetVerdict
	a.PowerUnits = f.PowerUnits
	a.Drivers = f.Drivers
	a.Website = f.Website
	a.WebsiteURL = f.WebsiteURL
	a.WebsiteSource = f.WebsiteSource
	a.Minutes = f.Minutes
}
func assessmentFacts(ctx context.Context, tx *sql.Tx, a *Assessment) error {
	c, err := candidateKey(ctx, tx, a.Candidate)
	if err != nil {
		return err
	}
	if c.Status != StatusActive {
		return nil
	}
	f, err := factsKey(ctx, tx, c.ID)
	if errors.Is(err, ErrNotFound) {
		f = unknownFacts(c.ID)
	} else if err != nil {
		return err
	}
	overlayFacts(a, f)
	return nil
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

// websiteFacts chooses a working check before the newest confirmed search.
func websiteFacts(es []Enrichment) (source, state, url string) {
	state = sources.WebsiteUnknown
	checkSeen := false
	var search *enrichers.WebsiteSearchFound
	for _, e := range es {
		if e.Kind == sources.WebsiteCheck && !checkSeen {
			var v website.Result
			if json.Unmarshal(e.Found, &v) == nil {
				checkSeen = true
				state = v.State
				if state == sources.WebsiteHTTPS || state == sources.WebsiteHTTP {
					return WebsiteByCheck, state, v.URL
				}
			}
		}
		if e.Kind == enrichers.KindWebsiteSearch && search == nil {
			var v enrichers.WebsiteSearchFound
			if json.Unmarshal(e.Found, &v) == nil && !v.Failed() && v.Confirmation != nil && (v.Confirmation.State == sources.WebsiteHTTPS || v.Confirmation.State == sources.WebsiteHTTP) {
				search = &v
			}
		}
	}
	if search != nil {
		return WebsiteBySearch, search.Confirmation.State, search.Confirmation.URL
	}
	return "", state, ""
}
