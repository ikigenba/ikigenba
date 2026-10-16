package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

var factsColumns = columns("candidate:Candidate known_since:KnownSince government_match:GovernmentMatch newest_filing:NewestFiling categories:Categories fleet_verdict:FleetVerdict power_units:PowerUnits drivers:Drivers website:Website website_url:WebsiteURL minutes:Minutes")

func unknownFacts(id string) Facts {
	return Facts{Candidate: id, Categories: []sources.Category{}, FleetVerdict: sources.FleetUnknown, Website: sources.WebsiteUnknown}
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
	var d, l, w string
	if err = scanSQL(ctx, tx, "SELECT drive_minutes_at,link_facts_at,website_check_at FROM facts WHERE candidate=?", []any{id}, &d, &l, &w); err != nil {
		return Facts{}, err
	}
	for i, a := range []string{d, l, w} {
		if a != "" {
			t, err := time.Parse(time.RFC3339, a)
			if err != nil {
				return Facts{}, err
			}
			f.Written = append(f.Written, Written{Kind: enrichers.Kinds()[i], At: t.UTC()})
		}
	}
	return f, nil
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
	extra := map[string]any{"drive_minutes_at": "", "link_facts_at": "", "website_check_at": ""}
	for _, e := range es {
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
		case sources.WebsiteCheck:
			var v website.Result
			if err = json.Unmarshal(e.Found, &v); err != nil {
				return err
			}
			f.Website = v.State
			if v.State == sources.WebsiteHTTPS || v.State == sources.WebsiteHTTP {
				f.WebsiteURL = v.URL
			}
		}
		extra[e.Kind+"_at"] = e.At.Format(time.RFC3339)
	}
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
