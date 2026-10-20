package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func briefResult(t *testing.T, id, summary string, failed bool, names ...string) store.Enrichment {
	t.Helper()
	code := 0
	b := enrichers.BriefFound{Run: "brief-run", Status: "exited", ExitCode: &code, Summary: summary, Categories: []enrichers.BriefCategory{}, Labels: []string{}}
	if failed {
		b.Status = "failed"
		b.ExitCode = nil
		b.Summary = ""
	} else {
		for _, n := range names {
			b.Categories = append(b.Categories, enrichers.BriefCategory{Name: n, Why: "evidence"})
		}
	}
	raw, err := json.Marshal(b)
	contractOK(t, err)
	return store.Enrichment{Candidate: id, Kind: enrichers.KindBrief, Inputs: "page-inputs", Source: enrichers.SourcePrompts, Found: raw}
}

// R-X5MB-WZMT R-X9A1-2AUW R-NJRO-EC1G R-XE5M-LDTO
func TestBriefProjectionKeepsGoodSummaryAndNewestAttempt(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	deriveAll(t, f.s)
	_, err := f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	k := cohortCreate(t, f, "brief members")
	lf, err := json.Marshal(enrichers.LinkFound{Categories: []enrichers.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}}, FleetVerdict: sources.FleetPass})
	contractOK(t, err)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: enrichers.KindLinkFacts, Inputs: "replacement-links", Source: enrichers.SourceLinks, Found: lf}})
	contractOK(t, err)
	first := briefResult(t, id, "earlier summary", false, sources.CategoryTrucking, sources.CategoryRetail, sources.CategoryRetail, sources.CategoryConstruction)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{first})
	contractOK(t, err)
	good, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, good.Brief, "earlier summary")
	contractEqual(t, good.Categories, []sources.Category{{Name: sources.CategoryConstruction, Source: enrichers.KindBrief}, {Name: sources.CategoryRetail, Source: enrichers.KindBrief}, {Name: sources.CategoryTrucking, Source: enrichers.KindBrief}})
	var briefCategories []sources.Category
	for _, c := range good.Categories {
		if c.Source == enrichers.KindBrief {
			briefCategories = append(briefCategories, c)
		}
	}
	contractEqual(t, briefCategories, []sources.Category{{Name: sources.CategoryConstruction, Source: enrichers.KindBrief}, {Name: sources.CategoryRetail, Source: enrichers.KindBrief}, {Name: sources.CategoryTrucking, Source: enrichers.KindBrief}})
	f.now = f.now.Add(time.Hour)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, id, "", true)})
	contractOK(t, err)
	failed, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, failed.Brief, good.Brief)
	contractEqual(t, failed.Categories, good.Categories)
	contractEqual(t, failed.Written[0], store.Written{Kind: enrichers.KindBrief, At: f.now.UTC().Truncate(time.Second)})
	members := cohortRead(t, f, k).Members
	contractEqual(t, len(members), 1)
	contractEqual(t, members[0].Brief, f.now.UTC().Truncate(time.Second))
	contractEqual(t, members[0].Assessment.Categories, failed.Categories)
	filters := store.CohortFilters{Categories: []string{sources.CategoryRetail}}
	k, err = f.s.EditCohort(ctx, store.CohortEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, err)
	contractEqual(t, len(cohortRead(t, f, k).Members), 1)
	f.reopen()
	persisted, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, persisted, failed)
	f.now = f.now.Add(time.Hour)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, id, "replacement summary", false, sources.CategoryOther)})
	contractOK(t, err)
	replaced, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, replaced.Brief, "replacement summary")
	contractEqual(t, replaced.Categories, []sources.Category{{Name: sources.CategoryOther, Source: enrichers.KindBrief}})
	contractEqual(t, replaced.Written[0], store.Written{Kind: enrichers.KindBrief, At: f.now.UTC().Truncate(time.Second)})
	other := enrichmentCandidate(t, f, "only-failed", "office@failed.test")
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, other, "", true)})
	contractOK(t, err)
	only, err := f.s.GetFacts(ctx, other)
	contractOK(t, err)
	contractEqual(t, only.Brief, "")
	contractEqual(t, only.Categories, []sources.Category{})
	contractEqual(t, len(only.Written), 1)
}

// R-UADJ-LEIU
func TestSubjectCarriesNewestPagesInOriginalURLOrderAndFacts(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	subject, err := f.s.Subject(ctx, id)
	contractOK(t, err)
	contractEqual(t, subject.Pages, []enrichers.Page{})
	contractEqual(t, subject.Facts, (*enrichers.LinkFound)(nil))
	var pages []store.Page
	for _, p := range []store.Page{{URL: "https://business.test/", Title: "first", Text: "old", Size: 3}, {URL: "https://business.test/about", Title: "about", Text: "about body", Size: 10}, {URL: "https://business.test/", Title: "replacement", Text: "latest body", Size: 11}} {
		p.Candidate = id
		p.UserID = "owner"
		saved, e := f.s.AddPage(ctx, p)
		contractOK(t, e)
		pages = append(pages, saved)
	}
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, id, "summary", false, sources.CategoryRetail, sources.CategoryAgriculture)})
	contractOK(t, err)
	subject, err = f.s.Subject(ctx, id)
	contractOK(t, err)
	want := []enrichers.Page{}
	for _, i := range []int{2, 1} {
		p := pages[i]
		want = append(want, enrichers.Page{Number: p.Number, URL: p.URL, Title: p.Title, Text: p.Text, Size: p.Size})
	}
	contractEqual(t, subject.Pages, want)
	if subject.Facts == nil {
		t.Fatal("missing facts")
	}
	contractEqual(t, subject.Facts.Categories, []enrichers.Category{})
	units, drivers := int64(3), int64(8)
	lf := enrichers.LinkFound{KnownSince: "2017-04-05", NewestFiling: "2026-06-07", GovernmentMatch: "government-pattern", FleetVerdict: sources.FleetPass, PowerUnits: &units, Drivers: &drivers, Categories: []enrichers.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}}}
	raw, e := json.Marshal(lf)
	contractOK(t, e)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: enrichers.KindLinkFacts, Inputs: "subject-links", Source: enrichers.SourceLinks, Found: raw}})
	contractOK(t, err)
	subject, err = f.s.Subject(ctx, id)
	contractOK(t, err)
	lf.Categories = []enrichers.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}}
	contractEqual(t, subject.Facts, &lf)
	facts, e := f.s.GetFacts(ctx, id)
	contractOK(t, e)
	contractEqual(t, facts.Categories, []sources.Category{{Name: sources.CategoryAgriculture, Source: enrichers.KindBrief}, {Name: sources.CategoryRetail, Source: enrichers.KindBrief}})
	due, err := f.s.DueAmong(ctx, enrichers.KindBrief, []string{id}, false)
	contractOK(t, err)
	contractEqual(t, due, []store.Due{{Candidate: id, Inputs: (enrichers.Brief{}).Inputs(subject)}})
	window, err := f.s.DueEnrichments(ctx, enrichers.KindBrief, "", 10)
	contractOK(t, err)
	contractEqual(t, window, due)
}

// R-L94M-GG16
func TestBriefMigrationBytes(t *testing.T) {
	b, err := fs.ReadFile(candidates.Migrations(), "0014_brief.sql")
	contractOK(t, err)
	contractEqual(t, string(b), "ALTER TABLE facts ADD COLUMN brief TEXT NOT NULL DEFAULT '';\nALTER TABLE facts ADD COLUMN brief_at TEXT NOT NULL DEFAULT '';\n")
}

func writePreviousFacts(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO facts(candidate,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,drive_minutes_at,link_facts_at,website_check_at,site_text_at) VALUES (?,'2020-01-01T00:00:00Z','pattern','2026-01-01T00:00:00Z','[]','pass',3,2,'https','https://old.test',42,'2026-01-02T00:00:00Z','','2026-01-03T00:00:00Z','2026-01-04T00:00:00Z') ON CONFLICT(candidate) DO UPDATE SET known_since=excluded.known_since,government_match=excluded.government_match,newest_filing=excluded.newest_filing,categories=excluded.categories,fleet_verdict=excluded.fleet_verdict,power_units=excluded.power_units,drivers=excluded.drivers,website=excluded.website,website_url=excluded.website_url,minutes=excluded.minutes,drive_minutes_at=excluded.drive_minutes_at,link_facts_at=excluded.link_facts_at,website_check_at=excluded.website_check_at,site_text_at=excluded.site_text_at`, id)
	return err
}

// R-AQP9-PPHB R-ARX6-3H80
func TestBriefMigrationAndRollbackWriter(t *testing.T) {
	for _, upgrade := range []bool{true, false} {
		t.Run(map[bool]string{true: "upgrade", false: "previous-writer"}[upgrade], func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "state.db")
			m := candidates.Migrations()
			if upgrade {
				m = earlierMigrations(t, "0013_site_text.sql")
			}
			d, err := db.Open(ctx, db.Config{Path: path, Migrations: m})
			contractOK(t, err)
			id := "cand_1111111111111111"
			e := websiteResult(id, sources.WebsiteHTTPS)
			contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
				if x := writePriorCandidate(ctx, tx, id); x != nil {
					return x
				}
				if x := writePreviousFacts(ctx, tx, id); x != nil {
					return x
				}
				_, x := tx.ExecContext(ctx, `INSERT INTO enrichments(candidate,kind,inputs,found,source,at,cost) VALUES (?,?,?,?,?,?,?)`, id, e.Kind, e.Inputs, string(e.Found), e.Source, "2026-01-03T00:00:00Z", e.Cost)
				return x
			}))
			contractOK(t, d.Close())
			d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
			contractOK(t, err)
			defer func() { contractOK(t, d.Close()) }()
			s := store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }})
			facts, err := s.GetFacts(ctx, id)
			contractOK(t, err)
			contractEqual(t, facts.Brief, "")
			contractEqual(t, len(facts.Written), 3)
			contractEqual(t, facts.Written, []store.Written{{Kind: enrichers.KindDriveMinutes, At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, {Kind: enrichers.KindSiteText, At: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)}, {Kind: sources.WebsiteCheck, At: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}})
			units, drivers, minutes := int64(3), int64(2), int64(42)
			source := ""
			if upgrade {
				source = store.WebsiteByCheck
			}
			contractEqual(t, facts, store.Facts{Candidate: id, KnownSince: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), GovernmentMatch: "pattern", NewestFiling: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Categories: []sources.Category{}, Records: []sources.Category{}, FleetVerdict: sources.FleetPass, PowerUnits: &units, Drivers: &drivers, Website: sources.WebsiteHTTPS, WebsiteURL: "https://old.test", WebsiteSource: source, Minutes: &minutes, Written: []store.Written{{Kind: enrichers.KindDriveMinutes, At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}, {Kind: enrichers.KindSiteText, At: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)}, {Kind: sources.WebsiteCheck, At: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}}})
			es, err := s.Enrichments(ctx, id)
			contractOK(t, err)
			e.At = time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
			contractEqual(t, es, []store.Enrichment{e})
			_, err = s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, id, "retained summary", false, sources.CategoryRetail)})
			contractOK(t, err)
			before, err := s.GetFacts(ctx, id)
			contractOK(t, err)
			contractOK(t, d.Write(ctx, func(tx *sql.Tx) error { return writePreviousFacts(ctx, tx, id) }))
			after, err := s.GetFacts(ctx, id)
			contractOK(t, err)
			contractEqual(t, after.Brief, before.Brief)
			contractEqual(t, after.Written[0], before.Written[0])
		})
	}
}

// R-NJRO-EC1G
func TestCohortBriefUsesNewestAttemptAndIgnoresOtherKinds(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	other := enrichmentCandidate(t, f, "no-brief", "owner@no-brief.test")
	_, err := f.s.JudgeAll(ctx)
	contractOK(t, err)
	k := cohortCreate(t, f, "newest brief attempts")
	before := cohortRead(t, f, k)
	contractEqual(t, len(before.Members), 2)
	for _, failed := range []bool{false, true} {
		f.now = f.now.Add(time.Second)
		_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, id, "summary", failed)})
		contractOK(t, err)
		read := cohortRead(t, f, k)
		contractEqual(t, cohortIDs(read.Members), cohortIDs(before.Members))
		contractEqual(t, read.Matching, before.Matching)
		for _, m := range read.Members {
			if m.Candidate.ID == id {
				contractEqual(t, m.Brief, f.now.UTC().Truncate(time.Second))
			} else {
				contractEqual(t, m.Candidate.ID, other)
				contractEqual(t, m.Brief, time.Time{})
			}
		}
	}
	before = cohortRead(t, f, k)
	f.now = f.now.Add(time.Second)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{siteTextResult(t, id, enrichers.RunExited)})
	contractOK(t, err)
	read := cohortRead(t, f, k)
	contractEqual(t, read.Matching, before.Matching)
	contractEqual(t, cohortIDs(read.Members), cohortIDs(before.Members))
	for i, m := range read.Members {
		contractEqual(t, m.Brief, before.Members[i].Brief)
	}
}
