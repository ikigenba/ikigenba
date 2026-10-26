package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func previousSearchFacts(ctx context.Context, tx *sql.Tx, id, state, searchAt string, mixed bool) error {
	if mixed {
		_, err := tx.ExecContext(ctx, `INSERT INTO facts(candidate,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,drive_minutes_at,link_facts_at,website_check_at,site_text_at,brief,brief_at,website_search_at) VALUES (?,'2018-01-02T00:00:00Z','name-pattern','2026-03-04T00:00:00Z','[{"Name":"retail","Source":"brief"}]','pass',7,11,?,'https://old.test/',42,'2026-01-01T01:02:03Z','2026-01-02T01:02:03Z','2026-01-03T01:02:03Z','2026-01-04T01:02:03Z','old summary','2026-01-05T01:02:03Z',?) ON CONFLICT(candidate) DO UPDATE SET known_since=excluded.known_since,government_match=excluded.government_match,newest_filing=excluded.newest_filing,categories=excluded.categories,fleet_verdict=excluded.fleet_verdict,power_units=excluded.power_units,drivers=excluded.drivers,website=excluded.website,website_url=excluded.website_url,minutes=excluded.minutes,drive_minutes_at=excluded.drive_minutes_at,link_facts_at=excluded.link_facts_at,website_check_at=excluded.website_check_at,site_text_at=excluded.site_text_at,brief=excluded.brief,brief_at=excluded.brief_at,website_search_at=excluded.website_search_at`, id, state, searchAt)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO facts(candidate,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,drive_minutes_at,link_facts_at,website_check_at,site_text_at,brief,brief_at) VALUES (?,'2018-01-02T00:00:00Z','name-pattern','2026-03-04T00:00:00Z','[{"Name":"retail","Source":"brief"}]','pass',7,11,?,'https://old.test/',42,'2026-01-01T01:02:03Z','2026-01-02T01:02:03Z','2026-01-03T01:02:03Z','2026-01-04T01:02:03Z','old summary','2026-01-05T01:02:03Z') ON CONFLICT(candidate) DO UPDATE SET known_since=excluded.known_since,government_match=excluded.government_match,newest_filing=excluded.newest_filing,categories=excluded.categories,fleet_verdict=excluded.fleet_verdict,power_units=excluded.power_units,drivers=excluded.drivers,website=excluded.website,website_url=excluded.website_url,minutes=excluded.minutes,drive_minutes_at=excluded.drive_minutes_at,link_facts_at=excluded.link_facts_at,website_check_at=excluded.website_check_at,site_text_at=excluded.site_text_at,brief=excluded.brief,brief_at=excluded.brief_at`, id, state)
	return err
}

// R-J35A-41QW
func TestWebsiteSearchUpgradePreservesPreviousFactsAndResults(t *testing.T) {
	ctx := context.Background()
	for _, state := range []string{sources.WebsiteHTTPS, sources.WebsiteHTTP, sources.WebsiteNone, sources.WebsiteUnknown} {
		t.Run(state, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "upgrade.db")
			d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0014_brief.sql")})
			contractOK(t, err)
			id := "cand_1111111111111111"
			e := websiteResult(id, state)
			e.At = time.Date(2026, 1, 3, 1, 2, 3, 0, time.UTC)
			e.Cost = .25
			contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
				if x := writePriorCandidate(ctx, tx, id); x != nil {
					return x
				}
				if x := previousSearchFacts(ctx, tx, id, state, "", false); x != nil {
					return x
				}
				_, x := tx.ExecContext(ctx, `INSERT INTO enrichments(candidate,kind,inputs,found,source,at,cost) VALUES (?,?,?,?,?,?,?)`, id, e.Kind, e.Inputs, string(e.Found), e.Source, e.At.Format(time.RFC3339), e.Cost)
				return x
			}))
			contractOK(t, d.Close())
			d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
			contractOK(t, err)
			defer func() { contractOK(t, d.Close()) }()
			s := store.New(d, store.Config{})
			f, err := s.GetFacts(ctx, id)
			contractOK(t, err)
			source := ""
			if state == sources.WebsiteHTTPS || state == sources.WebsiteHTTP {
				source = store.WebsiteByCheck
			}
			units, drivers, minutes := int64(7), int64(11), int64(42)
			written := []store.Written{}
			for i, kind := range []string{enrichers.KindBrief, enrichers.KindDriveMinutes, enrichers.KindLinkFacts, enrichers.KindSiteText, sources.WebsiteCheck} {
				day := []int{5, 1, 2, 4, 3}[i]
				written = append(written, store.Written{Kind: kind, At: time.Date(2026, 1, day, 1, 2, 3, 0, time.UTC)})
			}
			contractEqual(t, f, store.Facts{Headcounts: []sources.Headcount{}, BriefLabels: []string{}, Candidate: id, KnownSince: time.Date(2018, 1, 2, 0, 0, 0, 0, time.UTC), GovernmentMatch: "name-pattern", NewestFiling: time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), Categories: []sources.Category{{Name: sources.CategoryRetail, Source: enrichers.KindBrief}}, Records: []sources.Category{}, FleetVerdict: sources.FleetPass, PowerUnits: &units, Drivers: &drivers, Website: state, WebsiteURL: "https://old.test/", WebsiteSource: source, Minutes: &minutes, MinutesSource: store.MinutesByZip, Brief: "old summary", Written: written})
			es, err := s.Enrichments(ctx, id)
			contractOK(t, err)
			contractEqual(t, es, []store.Enrichment{e})
		})
	}
}

// R-XNWT-NJR8
func TestPreviousWriterPreservesWebsiteSourceAndWritesSearchTime(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	for _, searchAt := range []string{"", "2026-01-06T01:02:03Z"} {
		id := enrichmentCandidate(t, f, "legacy-"+searchAt, "office@old.test")
		if searchAt != "" {
			_, err := f.s.AddEnrichments(ctx, []store.Enrichment{searchResult(t, id, sources.WebsiteHTTPS, "https://search.test/")})
			contractOK(t, err)
		}
		// Replace a row with a previous-release insert on the current schema.
		contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `DELETE FROM facts WHERE candidate=?`, id); err != nil {
				return err
			}
			return previousSearchFacts(ctx, tx, id, sources.WebsiteHTTPS, searchAt, true)
		}))
		facts, err := f.s.GetFacts(ctx, id)
		contractOK(t, err)
		contractEqual(t, facts.WebsiteSource, "")
		if searchAt == "" {
			contractEqual(t, len(facts.Written), 5)
		} else {
			contractEqual(t, facts.Written[5], store.Written{Kind: enrichers.KindWebsiteSearch, At: time.Date(2026, 1, 6, 1, 2, 3, 0, time.UTC)})
		}
	}
	id := contractSeed(t, f)
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{searchResult(t, id, sources.WebsiteHTTPS, "https://kept.test/")})
	contractOK(t, err)
	before, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, before.WebsiteSource, store.WebsiteBySearch)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		return previousSearchFacts(ctx, tx, id, sources.WebsiteHTTP, "2026-01-07T01:02:03Z", true)
	}))
	after, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, after.WebsiteSource, before.WebsiteSource)
	var at time.Time
	for _, w := range after.Written {
		if w.Kind == enrichers.KindWebsiteSearch {
			at = w.At
		}
	}
	contractEqual(t, at, time.Date(2026, 1, 7, 1, 2, 3, 0, time.UTC))
	f.reopen()
	kept, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, kept, after)
}
