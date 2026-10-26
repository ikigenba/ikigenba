package pages

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-PKHU-DTGN
func TestOSHALinkView(t *testing.T) {
	for _, tc := range []struct {
		scraper                               string
		reading                               sources.Reading
		establishment, year, employees, hours string
	}{
		{oshaita.Name, sources.Reading{Establishment: "ACME HAULING CHAMPLIN", Year: new(int64(2024)), Employees: new(int64(23)), Hours: new(int64(47840))}, "ACME HAULING CHAMPLIN", "2024", "23", "47840"},
		{oshaita.Name, sources.Reading{Employees: new(int64(1200)), Hours: new(int64(2400000))}, "", "", "1200", "2400000"},
		{ppp.Name, sources.Reading{}, "", "", "", ""},
	} {
		v := linkView(store.Link{Scraper: tc.scraper, Reading: tc.reading}, "")
		if v.Establishment != tc.establishment || v.Year != tc.year || v.Employees != tc.employees || v.Hours != tc.hours {
			t.Fatal(v, tc)
		}
	}
}

// R-PMXN-5CY1
func TestOSHAHeadcountViews(t *testing.T) {
	facts := store.Facts{Headcounts: []sources.Headcount{{Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 23, AsOf: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC), Hours: new(int64(47840))}, {Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, AsOf: time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)}}}
	want := []HeadcountRow{{oshaita.Name, sources.HeadcountEmployees, "23", "47840", "2024-12-31", "2024-12-31"}, {ppp.Name, sources.HeadcountJobs, "26", "", "2020-04-28", "2020-04-28"}}
	if v := factsView(facts, ""); !reflect.DeepEqual(v.Headcounts, want) {
		t.Fatal(v)
	}
	v := factsView(store.Facts{Headcounts: []sources.Headcount{{Count: 600, Hours: new(int64(1250000))}}}, "")
	if v.Headcounts[0].Count != "600" || v.Headcounts[0].Hours != "1250000" {
		t.Fatal(v)
	}
	cfg, _ := pageFixture(t)
	_, ids, err := cfg.Store.Attach(t.Context(), "chosen-run", false, []store.Observation{{Scraper: oshaita.Name, SourceID: "chosen-source", Digest: "chosen-digest", Reading: sources.Reading{Name: "chosen-business"}}})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	found := enrichers.LinkFound{Categories: []enrichers.Category{}, FleetVerdict: sources.FleetUnknown, Headcounts: []enrichers.Headcount{{Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 23, Date: "2024-12-31", Hours: new(int64(47840))}}}
	if _, err = cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindLinkFacts, Source: enrichers.SourceLinks, Inputs: "chosen-inputs", Found: encodedJSON(found)}}); err != nil {
		t.Fatal(err)
	}
	got := capturedCandidate(t, cfg, ids[0]).Enrichments.Facts.Headcounts
	if len(got) != 1 || got[0].Hours != "47840" {
		t.Fatal(got)
	}
}

// R-PO5J-J4OQ
func TestOSHAEvidenceTemplate(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, setValues := range []bool{false, true} {
		set, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		link := LinkView{}
		h := HeadcountRow{}
		values := []string{}
		if setValues {
			link.Establishment = "chosen-establishment"
			link.Year = "chosen-year"
			link.Employees = "chosen-employees"
			link.Hours = "chosen-link-hours"
			h.Hours = "chosen-facts-hours"
			values = []string{link.Establishment, link.Year, link.Employees, link.Hours, h.Hours}
		}
		var b bytes.Buffer
		err = set.templates.ExecuteTemplate(&b, "candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Links: []LinkView{link, {}}, Enrichments: &EnrichmentsView{Facts: &FactsView{Headcounts: []HeadcountRow{h, {}}}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range values {
			if !strings.Contains(b.String(), v) {
				t.Fatalf("missing supplied value %q", v)
			}
		}
	}
}
