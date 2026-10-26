package tools_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-D5XF-RMUA R-D75C-5EKZ
func TestCandidateOSHAEvidence(t *testing.T) {
	for _, reading := range []sources.Reading{
		{Name: "ACME INC", Establishment: "ACME PLANT TWO", Year: new(int64(2024)), Employees: new(int64(23)), Hours: new(int64(47840)), Headcount: &sources.Headcount{Kind: sources.HeadcountEmployees, Count: 23}},
		{Name: "ACME INC", Establishment: "ACME PLANT TWO", Year: new(int64(2024)), Employees: new(int64(23)), Hours: new(int64(1000000))},
		{Name: "ACME INC"},
	} {
		f := newFixture(t)
		_, ids, err := f.st.Attach(t.Context(), "chosen-run", false, []store.Observation{{Scraper: oshaita.Name, SourceID: "chosen-source", Digest: "chosen-digest", Reading: reading}})
		if err != nil || len(ids) != 1 {
			t.Fatal(ids, err)
		}
		counts := []enrichers.Headcount{{Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 23, Date: "2024-12-31", Hours: new(int64(47840))}, {Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, Date: "2020-04-28"}}
		found := enrichers.LinkFound{Categories: []enrichers.Category{}, FleetVerdict: sources.FleetUnknown, Headcounts: counts}
		if _, err = f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindLinkFacts, Source: enrichers.SourceLinks, Inputs: "chosen-inputs", Found: encodedJSON(found)}}); err != nil {
			t.Fatal(err)
		}
		result := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}))
		if len(result.Links) != 1 {
			t.Fatal(result.Links)
		}
		l := result.Links[0]
		if l.Establishment != reading.Establishment || !reflect.DeepEqual(l.Year, reading.Year) || !reflect.DeepEqual(l.Employees, reading.Employees) || !reflect.DeepEqual(l.Hours, reading.Hours) {
			t.Fatal(l, reading)
		}
		encoded, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err = json.Unmarshal(encoded, &object); err != nil {
			t.Fatal(err)
		}
		if _, ok := object["establishment"]; !ok {
			t.Fatal(string(encoded))
		}
		for key, pointer := range map[string]*int64{"year": reading.Year, "employees": reading.Employees, "hours": reading.Hours} {
			_, ok := object[key]
			if ok != (pointer != nil) {
				t.Fatal(string(encoded))
			}
		}
		hs := result.Enrichments.Facts.Headcounts
		want := []tools.Headcount{{Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: 23, Date: "2024-12-31", Hours: new(int64(47840))}, {Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, Date: "2020-04-28"}}
		if !reflect.DeepEqual(hs, want) {
			t.Fatal(hs, want)
		}
		b, err := json.Marshal(hs)
		if err != nil || string(b) != `[{"scraper":"osha_ita","kind":"employees","count":23,"date":"2024-12-31","hours":47840},{"scraper":"ppp","kind":"jobs","count":26,"date":"2020-04-28"}]` {
			t.Fatal(string(b), err)
		}
		found.Headcounts = counts[:1]
		if _, err = f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindLinkFacts, Source: enrichers.SourceLinks, Inputs: "chosen-single-inputs", Found: encodedJSON(found)}}); err != nil {
			t.Fatal(err)
		}
		single := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]})).Enrichments.Facts.Headcounts
		if !reflect.DeepEqual(single, want[:1]) {
			t.Fatal(single)
		}
	}
}
