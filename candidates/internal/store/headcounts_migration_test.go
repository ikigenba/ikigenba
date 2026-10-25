package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-LSOS-R0OJ
func TestHeadcountsMigrationBytes(t *testing.T) {
	b, err := fs.ReadFile(candidates.Migrations(), "0020_headcounts.sql")
	contractOK(t, err)
	contractEqual(t, string(b), "ALTER TABLE facts ADD COLUMN headcounts TEXT NOT NULL DEFAULT '[]';\nALTER TABLE facts ADD COLUMN brief_labels TEXT NOT NULL DEFAULT '[]';\nALTER TABLE judgments ADD COLUMN headcount_low INTEGER;\nALTER TABLE judgments ADD COLUMN headcount_high INTEGER;\nALTER TABLE judgments ADD COLUMN headcount_basis TEXT NOT NULL DEFAULT '';\n")
}

func headcountsPriorFacts(ctx context.Context, tx *sql.Tx, id, brief string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO facts(candidate,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,drive_minutes_at,link_facts_at,website_check_at,site_text_at,brief,brief_at,website_source,website_search_at,minutes_source,miles,route_address,drive_route_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(candidate) DO UPDATE SET known_since=excluded.known_since,government_match=excluded.government_match,newest_filing=excluded.newest_filing,categories=excluded.categories,fleet_verdict=excluded.fleet_verdict,power_units=excluded.power_units,drivers=excluded.drivers,website=excluded.website,website_url=excluded.website_url,minutes=excluded.minutes,drive_minutes_at=excluded.drive_minutes_at,link_facts_at=excluded.link_facts_at,website_check_at=excluded.website_check_at,site_text_at=excluded.site_text_at,brief=excluded.brief,brief_at=excluded.brief_at,website_source=excluded.website_source,website_search_at=excluded.website_search_at,minutes_source=excluded.minutes_source,miles=excluded.miles,route_address=excluded.route_address,drive_route_at=excluded.drive_route_at`,
		id, "2020-01-02T00:00:00Z", "prior-match", "2026-01-01T00:00:00Z", `[{"Name":"prior-category","Source":"ppp"}]`, "yes", int64(3), int64(5), "https", "https://prior.test", int64(42), "", "", "", "", brief, "", "check", "", "route", 31.5, "prior-address", "")
	return err
}

func headcountsPriorJudgment(ctx context.Context, tx *sql.Tx, id string, populated bool, calculated string) error {
	var operating, evidence, low, high, confidence any
	key := int64(-1)
	if populated {
		operating, key, evidence = 0.73, 730000, `[{"Name":"prior-evidence","Source":"ppp","Weight":0.25,"Description":"provided-description"}]`
		low, high, confidence = int64(230), int64(470), 0.61
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO judgments(candidate,operating,operating_key,evidence,revenue_low,revenue_high,revenue_confidence,excluded,rules,calculated) VALUES (?,?,?,?,?,?,?,?,?,?) ON CONFLICT(candidate) DO UPDATE SET operating=excluded.operating,operating_key=excluded.operating_key,evidence=excluded.evidence,revenue_low=excluded.revenue_low,revenue_high=excluded.revenue_high,revenue_confidence=excluded.revenue_confidence,excluded=excluded.excluded,rules=excluded.rules,calculated=excluded.calculated`, id, operating, key, evidence, low, high, confidence, populated, int64(314), calculated)
	return err
}

func headcountsAssertPrior(t *testing.T, s *store.Store, id, brief string, populated bool, calculated string, counts []sources.Headcount, labels []string, low, high *int64, basis string) {
	t.Helper()
	f, err := s.GetFacts(context.Background(), id)
	contractOK(t, err)
	contractEqual(t, f.Headcounts, counts)
	contractEqual(t, f.BriefLabels, labels)
	contractEqual(t, f.Brief, brief)
	contractEqual(t, f.Candidate, id)
	contractEqual(t, f.GovernmentMatch, "prior-match")
	contractEqual(t, f.KnownSince, time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC))
	contractEqual(t, f.NewestFiling, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	contractEqual(t, f.Categories, []sources.Category{{Name: "prior-category", Source: "ppp"}})
	contractEqual(t, f.FleetVerdict, "yes")
	contractEqual(t, f.PowerUnits, ptrInt(3))
	contractEqual(t, f.Drivers, ptrInt(5))
	contractEqual(t, f.Website, "https")
	contractEqual(t, f.WebsiteURL, "https://prior.test")
	contractEqual(t, f.WebsiteSource, "check")
	contractEqual(t, f.Minutes, ptrInt(42))
	contractEqual(t, f.MinutesSource, "route")
	miles := 31.5
	contractEqual(t, f.Miles, &miles)
	contractEqual(t, f.RouteAddress, "prior-address")
	got, err := s.GetJudgments(context.Background(), id)
	contractOK(t, err)
	at, err := time.Parse(time.RFC3339, calculated)
	contractOK(t, err)
	want := store.Judgments{Candidate: id, OperatingKey: -1, Evidence: []sources.Evidence{}, Rules: 314, Calculated: at.UTC(), HeadcountLow: low, HeadcountHigh: high, HeadcountBasis: basis}
	if populated {
		op, confidence := 0.73, 0.61
		want.Operating, want.OperatingKey, want.RevenueConfidence = &op, 730000, &confidence
		want.RevenueLow, want.RevenueHigh, want.Excluded = ptrInt(230), ptrInt(470), true
		want.Evidence = []sources.Evidence{{Name: "prior-evidence", Source: "ppp", Weight: 0.25, Description: "provided-description"}}
	}
	contractEqual(t, got, want)
}

// R-LTWP-4SF8
func TestHeadcountsUpgradeReadsPreviousFactsAndJudgments(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0019_opens.sql")})
	contractOK(t, err)
	at := "2026-04-03T06:07:08+02:00"
	ids := []string{"prior-populated", "prior-null"}
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		for i, id := range ids {
			if e := writePriorCandidate(ctx, tx, id); e != nil {
				return e
			}
			if e := headcountsPriorFacts(ctx, tx, id, "previous-brief"); e != nil {
				return e
			}
			if e := headcountsPriorJudgment(ctx, tx, id, i == 0, at); e != nil {
				return e
			}
		}
		return nil
	}))
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	for i, id := range ids {
		headcountsAssertPrior(t, store.New(d, store.Config{}), id, "previous-brief", i == 0, at, []sources.Headcount{}, []string{}, nil, nil, "")
	}
}

// R-LV4L-IK5X
func TestHeadcountsPreviousWriterDefaultsAndPreservesAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rollback.db")
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	old, added := "current-row", "rollback-row"
	before := "2026-04-03T06:07:08Z"
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range []string{old, added} {
			if e := writePriorCandidate(ctx, tx, id); e != nil {
				return e
			}
		}
		if e := headcountsPriorFacts(ctx, tx, old, "current-brief"); e != nil {
			return e
		}
		if e := headcountsPriorJudgment(ctx, tx, old, false, before); e != nil {
			return e
		}
		if _, e := tx.ExecContext(ctx, `UPDATE facts SET headcounts='[{"scraper":"ppp","kind":"jobs","count":26,"date":"2020-04-28T00:00:00Z"},{"scraper":"fmcsa","kind":"drivers","count":9}]',brief_labels='["label-one","label-two"]' WHERE candidate=?`, old); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `UPDATE judgments SET headcount_low=24,headcount_high=30,headcount_basis='prior-headcount' WHERE candidate=?`, old)
		return e
	}))
	counts := []sources.Headcount{{Scraper: "ppp", Kind: "jobs", Count: 26, AsOf: time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)}, {Scraper: "fmcsa", Kind: "drivers", Count: 9}}
	labels := []string{"label-one", "label-two"}
	s := store.New(d, store.Config{})
	headcountsAssertPrior(t, s, old, "current-brief", false, before, counts, labels, ptrInt(24), ptrInt(30), "prior-headcount")
	after := "2026-05-06T07:08:09-05:00"
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range []string{old, added} {
			if e := headcountsPriorFacts(ctx, tx, id, "rollback-brief"); e != nil {
				return e
			}
			if e := headcountsPriorJudgment(ctx, tx, id, true, after); e != nil {
				return e
			}
		}
		return nil
	}))
	check := func() {
		headcountsAssertPrior(t, s, old, "rollback-brief", true, after, counts, labels, ptrInt(24), ptrInt(30), "prior-headcount")
		headcountsAssertPrior(t, s, added, "rollback-brief", true, after, []sources.Headcount{}, []string{}, nil, nil, "")
	}
	check()
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s = store.New(d, store.Config{})
	check()
}
