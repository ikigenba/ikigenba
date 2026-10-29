package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-R0AE-AZTH
func TestFactSourcesMigrationBytes(t *testing.T) {
	got, err := fs.ReadFile(candidates.Migrations(), "0026_fact_sources.sql")
	contractOK(t, err)
	want := "ALTER TABLE candidates ADD COLUMN name_source TEXT NOT NULL DEFAULT '';\n" +
		"ALTER TABLE candidates ADD COLUMN name_record TEXT NOT NULL DEFAULT '';\n" +
		"ALTER TABLE candidates ADD COLUMN address_source TEXT NOT NULL DEFAULT '';\n" +
		"ALTER TABLE candidates ADD COLUMN address_record TEXT NOT NULL DEFAULT '';\n" +
		"ALTER TABLE candidates ADD COLUMN phone_source TEXT NOT NULL DEFAULT '';\n" +
		"ALTER TABLE candidates ADD COLUMN phone_record TEXT NOT NULL DEFAULT '';\n" +
		"ALTER TABLE candidates ADD COLUMN email_source TEXT NOT NULL DEFAULT '';\n" +
		"ALTER TABLE candidates ADD COLUMN email_record TEXT NOT NULL DEFAULT '';\n" +
		"ALTER TABLE candidates ADD COLUMN other_names TEXT NOT NULL DEFAULT '[]';\n" +
		"ALTER TABLE candidates ADD COLUMN other_addresses TEXT NOT NULL DEFAULT '[]';\n" +
		"ALTER TABLE candidates ADD COLUMN other_phones TEXT NOT NULL DEFAULT '[]';\n" +
		"ALTER TABLE candidates ADD COLUMN other_emails TEXT NOT NULL DEFAULT '[]';\n" +
		"ALTER TABLE facts ADD COLUMN known_since_source TEXT NOT NULL DEFAULT '';\n" +
		"ALTER TABLE facts ADD COLUMN newest_filing_source TEXT NOT NULL DEFAULT '';\n" +
		"ALTER TABLE facts ADD COLUMN website_checked_url TEXT NOT NULL DEFAULT '';\n"
	contractEqual(t, string(got), want)
}

// These are the complete column lists the previous store wrote. They are
// deliberately independent of the current store's record encoders.
const factSourcesPriorCandidateColumns = "id,status,merged_into,name,names,names_lower,street,city,state,zip,county,phone,email,people,class,cargo,created,updated"
const factSourcesPriorFactsColumns = "candidate,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,drive_minutes_at,link_facts_at,website_check_at,site_text_at,brief,brief_at,website_source,website_search_at,minutes_source,miles,route_address,drive_route_at,headcounts,brief_labels"

func factSourcesPriorInsertCandidate(ctx context.Context, tx *sql.Tx, args []any) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO candidates("+factSourcesPriorCandidateColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", args...)
	return err
}

func factSourcesPriorInsertFacts(ctx context.Context, tx *sql.Tx, args []any) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO facts("+factSourcesPriorFactsColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", args...)
	return err
}

func factSourcesPriorUpdateCandidate(ctx context.Context, tx *sql.Tx, args []any) error {
	_, err := tx.ExecContext(ctx, "UPDATE candidates SET id=?,status=?,merged_into=?,name=?,names=?,names_lower=?,street=?,city=?,state=?,zip=?,county=?,phone=?,email=?,people=?,class=?,cargo=?,created=?,updated=? WHERE id=?", append(args, args[0])...)
	return err
}

func factSourcesPriorUpdateFacts(ctx context.Context, tx *sql.Tx, args []any) error {
	_, err := tx.ExecContext(ctx, "UPDATE facts SET candidate=?,known_since=?,government_match=?,newest_filing=?,categories=?,fleet_verdict=?,power_units=?,drivers=?,website=?,website_url=?,minutes=?,drive_minutes_at=?,link_facts_at=?,website_check_at=?,site_text_at=?,brief=?,brief_at=?,website_source=?,website_search_at=?,minutes_source=?,miles=?,route_address=?,drive_route_at=?,headcounts=?,brief_labels=? WHERE candidate=?", append(args, args[0])...)
	return err
}

func factSourcesPriorCandidate(id string, populated bool) (store.Candidate, []any) {
	stamp := time.Date(2026, 8, 9, 10, 11, 12, 0, time.UTC)
	c := store.Candidate{ID: id, Status: store.StatusActive, Names: []string{}, People: []sources.Sourced[sources.Person]{}, Cargo: []string{}, Created: stamp, Updated: stamp.Add(time.Hour), OtherNames: []sources.Sourced[string]{}, OtherAddresses: []sources.Sourced[sources.Address]{}, OtherPhones: []sources.Sourced[string]{}, OtherEmails: []sources.Sourced[string]{}}
	people := []sources.Person{}
	if populated {
		c.Name, c.Names = "Legacy business", []string{"Legacy business", "Legacy trade name"}
		c.Street, c.City, c.State, c.Zip, c.County = "22 Legacy Road", "Legacy city", "MN", "55072", "MN-115"
		c.Phone, c.Email, c.Class, c.Cargo = "320-555-0122", "legacy@business.test", sources.ClassForHire, []string{"grain", "paper"}
		people = []sources.Person{{Name: "First person", Role: "president"}, {Name: "Second person", Role: "agent"}}
		for _, p := range people {
			c.People = append(c.People, sources.Sourced[sources.Person]{Value: p})
		}
	}
	names, _ := json.Marshal(c.Names)
	peopleJSON, _ := json.Marshal(people)
	cargo, _ := json.Marshal(c.Cargo)
	return c, []any{c.ID, c.Status, c.MergedInto, c.Name, string(names), strings.ToLower(strings.Join(c.Names, "\n")), c.Street, c.City, c.State, c.Zip, c.County, c.Phone, c.Email, string(peopleJSON), c.Class, string(cargo), c.Created.Format(time.RFC3339), c.Updated.Format(time.RFC3339)}
}

func factSourcesPriorFacts(id string, populated bool) (store.Facts, []any) {
	f := store.Facts{Candidate: id, Categories: []sources.Category{}, Records: []sources.Category{}, FleetVerdict: sources.FleetUnknown, Headcounts: []sources.Headcount{}, Website: sources.WebsiteUnknown, BriefLabels: []string{}}
	var known, newest any = "", ""
	var units, drivers, minutes, miles any
	ats := []string{"", "", "", "", "", "", ""}
	if populated {
		f.KnownSince = time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
		f.NewestFiling = time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
		f.GovernmentMatch, f.FleetVerdict = "legacy-match", sources.FleetPass
		f.Categories = []sources.Category{{Name: "legacy-category", Source: "ppp"}}
		f.PowerUnits, f.Drivers, f.Minutes = ptrInt(7), ptrInt(9), ptrInt(43)
		mi := 28.5
		f.Miles = &mi
		f.Website, f.WebsiteURL, f.WebsiteSource = sources.WebsiteHTTPS, "https://legacy.test/", store.WebsiteByCheck
		f.MinutesSource, f.RouteAddress, f.Brief = store.MinutesByRoute, "22 Legacy Road, Legacy city, MN 55072", "Legacy summary"
		f.Headcounts = []sources.Headcount{{Scraper: "ppp", Kind: sources.HeadcountJobs, Count: 17, AsOf: f.KnownSince, Hours: ptrInt(1200)}}
		f.BriefLabels = []string{"legacy-label", "second-label"}
		known, newest, units, drivers, minutes, miles = f.KnownSince.Format(time.RFC3339), f.NewestFiling.Format(time.RFC3339), *f.PowerUnits, *f.Drivers, *f.Minutes, *f.Miles
		kinds := []string{enrichers.KindBrief, enrichers.KindDriveMinutes, enrichers.KindDriveRoute, enrichers.KindLinkFacts, enrichers.KindSiteText, sources.WebsiteCheck, enrichers.KindWebsiteSearch}
		for i, kind := range kinds {
			at := time.Date(2026, 7, i+1, 2, 3, 4, 0, time.UTC)
			ats[i] = at.Format(time.RFC3339)
			f.Written = append(f.Written, store.Written{Kind: kind, At: at})
		}
	}
	categories, _ := json.Marshal(f.Categories)
	headcounts := "[]"
	if populated {
		headcounts = `[{"scraper":"ppp","kind":"jobs","count":17,"date":"2020-01-02T00:00:00Z","hours":1200}]`
	}
	labels, _ := json.Marshal(f.BriefLabels)
	return f, []any{id, known, f.GovernmentMatch, newest, string(categories), f.FleetVerdict, units, drivers, f.Website, f.WebsiteURL, minutes, ats[1], ats[3], ats[5], ats[4], f.Brief, ats[0], f.WebsiteSource, ats[6], f.MinutesSource, miles, f.RouteAddress, ats[2], headcounts, string(labels)}
}

// R-R1IA-ORK6
func TestFactSourcesUpgradePreservesPreviousCandidateAndFacts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	now := func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) }
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0025_search_cases.sql"), Now: now})
	contractOK(t, err)
	ids := []string{"cand_1111111111111111", "cand_2222222222222222"}
	wantCandidates := []store.Candidate{}
	wantFacts := []store.Facts{}
	for i, id := range ids {
		c, ca := factSourcesPriorCandidate(id, i == 0)
		f, fa := factSourcesPriorFacts(id, i == 0)
		wantCandidates, wantFacts = append(wantCandidates, c), append(wantFacts, f)
		contractOK(t, d.Write(ctx, func(tx *sql.Tx) error {
			if e := factSourcesPriorInsertCandidate(ctx, tx, ca); e != nil {
				return e
			}
			if e := factSourcesPriorInsertFacts(ctx, tx, fa); e != nil {
				return e
			}
			_, e := tx.ExecContext(ctx, "INSERT INTO enrichments(seq,candidate,kind,found,source,at,cost,inputs) VALUES (?,?,?,?,?,?,?,?)", i+1, id, sources.WebsiteCheck, string(websiteFound(website.Result{State: sources.WebsiteHTTPS, URL: "https://legacy.test/"})), "legacy.test", "2026-07-06T02:03:04Z", .25, "legacy.test")
			return e
		}))
	}
	contractOK(t, d.Close())
	d, err = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations(), Now: now})
	contractOK(t, err)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{Now: now})
	for i, id := range ids {
		got, e := s.GetCandidate(ctx, id)
		contractOK(t, e)
		contractEqual(t, got, wantCandidates[i])
		facts, e := s.GetFacts(ctx, id)
		contractOK(t, e)
		contractEqual(t, facts, wantFacts[i])
	}
}

// R-R2Q7-2JAV
func TestFactSourcesPreviousWriterPreservesProvenanceAndReadsPeople(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := contractObservation("current-one", "Current business", f.now)
	a.Scraper = "fmcsa"
	b := contractObservation("current-two", "Alternative business", f.now)
	b.Scraper, b.Reading.NameKey = "fmcsa", a.Reading.NameKey
	b.Reading.Street, b.Reading.Phone, b.Reading.Email = "33 Other Road", "320-555-0133", "other@business.test"
	id := contractAttach(t, f, "current-run", a, b)[0]
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{
		{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "business.test", Source: "business.test", Found: websiteFound(website.Result{State: sources.WebsiteHTTPS, URL: "https://business.test/", From: "https://business.test"})},
		{Candidate: id, Kind: enrichers.KindLinkFacts, Inputs: "current-links", Found: json.RawMessage(`{"known_since":"2020-01-02","known_since_source":"ppp","newest_filing":"2026-01-03","newest_filing_source":"fmcsa","categories":[],"fleet_verdict":"unknown"}`)},
	})
	contractOK(t, err)
	before, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	factsBefore, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	if before.NameSource == "" || before.AddressSource == "" || len(before.People) == 0 || before.People[0].Source == "" || factsBefore.WebsiteCheckedURL == "" || factsBefore.KnownSinceSource == "" || factsBefore.NewestFilingSource == "" {
		t.Fatal("fixture must hold current candidate and facts provenance")
	}
	if len(before.OtherNames) == 0 || len(before.OtherAddresses) == 0 || len(before.OtherPhones) == 0 || len(before.OtherEmails) == 0 {
		t.Fatal("fixture must hold alternatives for every candidate fact")
	}
	// The rollback reader selects every earlier column, including the people
	// written by the current encoder, and ignores its additional JSON members.
	readPeople := func() []sources.Person {
		t.Helper()
		var people []sources.Person
		contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
			values := make([]any, len(strings.Split(factSourcesPriorCandidateColumns, ",")))
			dest := make([]any, len(values))
			for i := range values {
				dest[i] = &values[i]
			}
			if e := tx.QueryRowContext(ctx, "SELECT "+factSourcesPriorCandidateColumns+" FROM candidates WHERE id=?", id).Scan(dest...); e != nil {
				return e
			}
			return json.Unmarshal([]byte(values[13].(string)), &people)
		}))
		return people
	}
	wantPeople := []sources.Person{}
	for _, p := range before.People {
		wantPeople = append(wantPeople, p.Value)
	}
	contractEqual(t, readPeople(), wantPeople)
	inserted := []string{"cand_1111111111111111", "cand_2222222222222222"}
	for i, added := range inserted {
		_, ca := factSourcesPriorCandidate(added, i == 0)
		_, fa := factSourcesPriorFacts(added, i == 0)
		contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
			if e := factSourcesPriorInsertCandidate(ctx, tx, ca); e != nil {
				return e
			}
			return factSourcesPriorInsertFacts(ctx, tx, fa)
		}))
	}
	_, ca := factSourcesPriorCandidate(id, true)
	peopleJSON, err := json.Marshal(wantPeople)
	contractOK(t, err)
	ca[13] = string(peopleJSON)
	_, fa := factSourcesPriorFacts(id, true)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		if e := factSourcesPriorUpdateCandidate(ctx, tx, ca); e != nil {
			return e
		}
		return factSourcesPriorUpdateFacts(ctx, tx, fa)
	}))
	contractEqual(t, readPeople(), wantPeople)
	for _, added := range inserted {
		got, e := f.s.GetCandidate(ctx, added)
		contractOK(t, e)
		assertFactSourcesCandidateProvenance(t, got, store.Candidate{})
	}
	after, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	assertFactSourcesCandidateProvenance(t, after, before)
	factsAfter, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, factsAfter.KnownSinceSource, factsBefore.KnownSinceSource)
	contractEqual(t, factsAfter.NewestFilingSource, factsBefore.NewestFilingSource)
	contractEqual(t, factsAfter.WebsiteCheckedURL, factsBefore.WebsiteCheckedURL)
}

func assertFactSourcesCandidateProvenance(t *testing.T, got, want store.Candidate) {
	t.Helper()
	contractEqual(t, []string{got.NameSource, got.NameRecord, got.AddressSource, got.AddressRecord, got.PhoneSource, got.PhoneRecord, got.EmailSource, got.EmailRecord}, []string{want.NameSource, want.NameRecord, want.AddressSource, want.AddressRecord, want.PhoneSource, want.PhoneRecord, want.EmailSource, want.EmailRecord})
	if len(got.OtherNames) != len(want.OtherNames) || len(got.OtherAddresses) != len(want.OtherAddresses) || len(got.OtherPhones) != len(want.OtherPhones) || len(got.OtherEmails) != len(want.OtherEmails) {
		t.Fatal("candidate alternatives changed")
	}
	if len(want.OtherNames) > 0 {
		contractEqual(t, got.OtherNames, want.OtherNames)
	}
	if len(want.OtherAddresses) > 0 {
		contractEqual(t, got.OtherAddresses, want.OtherAddresses)
	}
	if len(want.OtherPhones) > 0 {
		contractEqual(t, got.OtherPhones, want.OtherPhones)
	}
	if len(want.OtherEmails) > 0 {
		contractEqual(t, got.OtherEmails, want.OtherEmails)
	}
}
