package enrichers_test

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func count(n int64) *int64 { return &n }

// R-3I1U-DVXM
func TestDriveMinutesDerive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		zip     string
		minutes *int64
		found   string
	}{
		{"known", "55072", count(84), `{"minutes":84}`},
		{"zero", "55072", count(0), `{"minutes":0}`},
		{"unknown", "55072", nil, `{}`},
		{"negative", "55072", count(-1), `{}`},
		{"minimum", "55072", count(math.MinInt64), `{}`},
		{"maximum", "55072", count(math.MaxInt64), `{"minutes":9223372036854775807}`},
		{"no zip", "", count(84), `{"minutes":84}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := enrichers.Subject{ID: "candidate", Email: "owner@example.test", Zip: tc.zip, Minutes: tc.minutes, Links: []enrichers.Link{{Scraper: ppp.Name}}}
			before := subjectJSON(t, s)
			e := enrichers.DriveMinutes{}
			if in := e.Inputs(s); in != tc.zip {
				t.Fatalf("inputs %q, want %q", in, tc.zip)
			}
			r := e.Derive(s)
			if r.Source != enrichers.SourceZipDrive || r.Cost != 0 || string(r.Found) != tc.found {
				t.Fatalf("result %+v, found %s, want %s", r, r.Found, tc.found)
			}
			if !bytes.Equal(before, subjectJSON(t, s)) {
				t.Fatal("subject changed")
			}
		})
	}
}

// R-IMQ1-EG3C
func TestDriveMinutesValid(t *testing.T) {
	e := enrichers.DriveMinutes{}
	valid := []string{`{}`, `{"minutes":null}`, `{"minutes":0}`, `{"minutes":84}`, `{"minutes":9223372036854775807}`, ` {"Minutes":1} `, `{"minutes":-1,"minutes":2}`}
	invalid := []string{`{"minutes":-1}`, `{"minutes":-9223372036854775808}`, `{"minutes":"84"}`, `{"minutes":1.5}`, `{"minutes":1e2}`, `{"minutes":9223372036854775808}`, `{"mins":84}`, `{"minutes":1,"x":2}`, `null`, `[]`, `1`, `"object"`, ``, ` `, `{} {}`, `{} null`, `{}x`, `{`}
	checkValidity(t, e, valid, invalid)
	for _, n := range []*int64{nil, count(-1), count(math.MinInt64), count(0), count(84), count(math.MaxInt64)} {
		if r := e.Derive(enrichers.Subject{Minutes: n}); !e.Valid(r.Found) {
			t.Fatalf("invalid derived minutes: %s", r.Found)
		}
	}
}

// R-F7H0-LOM5
func TestLinkFactsDerive(t *testing.T) {
	date := func(v string) time.Time {
		t.Helper()
		d, err := time.Parse(time.RFC3339, v)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	p := enrichers.Link{Scraper: ppp.Name, Reading: sources.Reading{KnownSince: date("2018-04-09T00:00:00Z"), Filed: date("2020-04-09T00:00:00Z"), Category: sources.CategoryConstruction}}
	f := enrichers.Link{Scraper: fmcsa.Name, Reading: sources.Reading{KnownSince: date("2015-03-01T00:00:00Z"), Filed: date("2025-06-01T00:00:00Z"), Category: sources.CategoryTrucking}}
	gone := enrichers.Link{Gone: true, Scraper: ppp.Name, Reading: sources.Reading{KnownSince: date("2010-01-01T00:00:00Z"), Filed: date("2026-01-01T00:00:00Z"), GovernmentMatch: "ignored", Category: "ignored"}}
	pg := p
	pg.Gone = true
	unrepresentable := enrichers.Link{Reading: sources.Reading{
		KnownSince: time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC),
		Filed:      time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	noScraper := enrichers.Link{Reading: sources.Reading{Category: sources.CategoryConstruction, GovernmentMatch: "x"}}
	pl := enrichers.Link{Scraper: places.Name, Reading: sources.Reading{Category: sources.CategoryRetail}}
	plGone := pl
	plGone.Gone = true
	retail := enrichers.Category{Name: sources.CategoryRetail, Source: places.Name}
	government := enrichers.Category{Name: sources.CategoryGovernment, Source: "name"}
	construction := enrichers.Category{Name: sources.CategoryConstruction, Source: ppp.Name}
	trucking := enrichers.Category{Name: sources.CategoryTrucking, Source: fmcsa.Name}
	for _, tc := range []struct {
		name                     string
		links                    []enrichers.Link
		known, filed, government string
		categories               []enrichers.Category
	}{
		{"none", nil, "", "", "", []enrichers.Category{}},
		{"places only", []enrichers.Link{pl}, "", "", "", []enrichers.Category{retail}},
		{"places duplicate", []enrichers.Link{pl, pl}, "", "", "", []enrichers.Category{retail}},
		{"places gone", []enrichers.Link{plGone}, "", "", "", []enrichers.Category{}},
		{"live and gone places", []enrichers.Link{pl, plGone}, "", "", "", []enrichers.Category{retail}},
		{"places beside ppp", []enrichers.Link{pl, p}, "2018-04-09", "2020-04-09", "", []enrichers.Category{construction}},
		{"places beside gone ppp", []enrichers.Link{pl, pg}, "", "", "", []enrichers.Category{}},
		{"gone ppp before places", []enrichers.Link{pg, pl}, "", "", "", []enrichers.Category{}},
		{"places beside unclassified fmcsa", []enrichers.Link{{Scraper: fmcsa.Name}, pl}, "", "", "", []enrichers.Category{}},
		{"places beside unknown scraper", []enrichers.Link{pl, {}}, "", "", "", []enrichers.Category{}},
		{"gone only", []enrichers.Link{gone}, "", "", "", []enrichers.Category{}},
		{"ppp", []enrichers.Link{p}, "2018-04-09", "2020-04-09", "", []enrichers.Category{construction}},
		{"mixed", []enrichers.Link{f, p}, "2015-03-01", "2025-06-01", "", []enrichers.Category{construction, trucking}},
		{"mixed reversed", []enrichers.Link{p, f}, "2015-03-01", "2025-06-01", "", []enrichers.Category{construction, trucking}},
		{"gone ppp", []enrichers.Link{f, pg}, "2015-03-01", "2025-06-01", "", []enrichers.Category{trucking}},
		{"ignore gone dates", []enrichers.Link{f, p, gone}, "2015-03-01", "2025-06-01", "", []enrichers.Category{construction, trucking}},
		{"deduplicate", []enrichers.Link{p, p}, "2018-04-09", "2020-04-09", "", []enrichers.Category{construction}},
		{"government order", []enrichers.Link{{}, {Reading: sources.Reading{GovernmentMatch: "x"}}, {Reading: sources.Reading{GovernmentMatch: "y"}}}, "", "", "x", []enrichers.Category{government}},
		{"gone government", []enrichers.Link{{}, {Gone: true, Reading: sources.Reading{GovernmentMatch: "x"}}, {Reading: sources.Reading{GovernmentMatch: "y"}}}, "", "", "y", []enrichers.Category{government}},
		{"UTC dates", []enrichers.Link{{Reading: sources.Reading{KnownSince: date("2018-04-09T23:30:00-05:00"), Filed: date("2025-06-01T00:30:00+05:00")}}}, "2018-04-10", "2025-05-31", "", []enrichers.Category{}},
		{"unrepresentable dates", []enrichers.Link{unrepresentable}, "", "", "", []enrichers.Category{}},
		{"opposite unrepresentable dates", []enrichers.Link{{Reading: sources.Reading{KnownSince: unrepresentable.Reading.Filed, Filed: unrepresentable.Reading.KnownSince}}}, "", "", "", []enrichers.Category{}},
		{"ignore unrepresentable dates", []enrichers.Link{unrepresentable, p}, "2018-04-09", "2020-04-09", "", []enrichers.Category{construction}},
		{"ignore later unrepresentable dates", []enrichers.Link{p, unrepresentable}, "2018-04-09", "2020-04-09", "", []enrichers.Category{construction}},
		{"UTC year overflow", []enrichers.Link{{Reading: sources.Reading{KnownSince: date("9999-12-31T23:00:00-05:00"), Filed: date("9999-12-31T23:00:00-05:00")}}}, "", "", "", []enrichers.Category{}},
		{"UTC year underflow", []enrichers.Link{{Reading: sources.Reading{KnownSince: date("0000-01-01T00:30:00+05:00"), Filed: date("0000-01-01T00:30:00+05:00")}}}, "", "", "", []enrichers.Category{}},
		{"date boundaries", []enrichers.Link{{Reading: sources.Reading{KnownSince: date("0000-01-01T00:00:00Z"), Filed: date("9999-12-31T00:00:00Z")}}}, "0000-01-01", "9999-12-31", "", []enrichers.Category{}},
		{"UTC enters date range", []enrichers.Link{{Reading: sources.Reading{
			KnownSince: time.Date(-1, 12, 31, 23, 0, 0, 0, time.FixedZone("west", -5*60*60)),
			Filed:      time.Date(10000, 1, 1, 0, 30, 0, 0, time.FixedZone("east", 5*60*60)),
		}}}, "0000-01-01", "9999-12-31", "", []enrichers.Category{}},
		{"no scraper", []enrichers.Link{noScraper}, "", "", "x", []enrichers.Category{government}},
		{"no scraper beside category", []enrichers.Link{noScraper, p}, "2018-04-09", "2020-04-09", "x", []enrichers.Category{construction, government}},
		{"category source order", []enrichers.Link{{Scraper: "z", Reading: sources.Reading{Category: "a"}}, {Scraper: "a", Reading: sources.Reading{Category: "a"}}, {Scraper: "a", Reading: sources.Reading{Category: "B"}}, {}}, "", "", "", []enrichers.Category{{Name: "B", Source: "a"}, {Name: "a", Source: "a"}, {Name: "a", Source: "z"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := enrichers.Subject{ID: "candidate", Zip: "55072", Minutes: count(84), Links: tc.links}
			before := s
			if s.Links != nil {
				before.Links = append([]enrichers.Link{}, s.Links...)
			}
			before.Minutes = count(*s.Minutes)
			want := enrichers.LinkFound{KnownSince: tc.known, NewestFiling: tc.filed, GovernmentMatch: tc.government, Categories: tc.categories, FleetVerdict: sources.FleetUnknown}
			assertLinkResult(t, s, want)
			if !reflect.DeepEqual(before, s) {
				t.Fatal("subject changed")
			}
		})
	}
}

// R-F7H0-LOM5
func TestLinkFactsGovernmentCategories(t *testing.T) {
	government := enrichers.Category{Name: sources.CategoryGovernment, Source: "name"}
	education := enrichers.Category{Name: sources.CategoryEducation, Source: "name"}
	trucking := enrichers.Category{Name: sources.CategoryTrucking, Source: fmcsa.Name}
	link := func(scraper, category, match string, gone bool) enrichers.Link {
		return enrichers.Link{Scraper: scraper, Gone: gone, Reading: sources.Reading{Category: category, GovernmentMatch: match}}
	}
	for _, tc := range []struct {
		name       string
		links      []enrichers.Link
		match      string
		categories []enrichers.Category
	}{
		{"county fleet", []enrichers.Link{link(fmcsa.Name, sources.CategoryTrucking, "COUNTY", false)}, "COUNTY", []enrichers.Category{government, trucking}},
		{"school fleet", []enrichers.Link{link(fmcsa.Name, sources.CategoryTrucking, "SCHOOL DISTRICT", false)}, "SCHOOL DISTRICT", []enrichers.Category{education, trucking}},
		{"university fleet", []enrichers.Link{link(fmcsa.Name, sources.CategoryTrucking, "UNIVERSITY", false)}, "UNIVERSITY", []enrichers.Category{education, trucking}},
		{"unmatched fleet", []enrichers.Link{link(fmcsa.Name, sources.CategoryTrucking, "", false)}, "", []enrichers.Category{trucking}},
		{"two government matches", []enrichers.Link{link(fmcsa.Name, "", "COUNTY", false), link(fmcsa.Name, "", "CITY OF", false)}, "COUNTY", []enrichers.Category{government}},
		{"different sources", []enrichers.Link{link(ppp.Name, sources.CategoryGovernment, "", false), link(fmcsa.Name, "", "COUNTY", false)}, "COUNTY", []enrichers.Category{government, {Name: sources.CategoryGovernment, Source: ppp.Name}}},
		{"first match wins", []enrichers.Link{link("", "", "COUNTY", false), link("", "", "UNIVERSITY", false)}, "COUNTY", []enrichers.Category{government}},
		{"gone match yields next", []enrichers.Link{link("", "", "COUNTY", true), link("", "", "UNIVERSITY", false)}, "UNIVERSITY", []enrichers.Category{education}},
		{"all matches gone", []enrichers.Link{link("", "", "COUNTY", true), link("", "", "UNIVERSITY", true)}, "", []enrichers.Category{}},
		{"deduplicate name source", []enrichers.Link{link("name", sources.CategoryGovernment, "COUNTY", false)}, "COUNTY", []enrichers.Category{government}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := enrichers.Subject{Links: tc.links}
			before := subjectJSON(t, s)
			assertLinkResult(t, s, enrichers.LinkFound{GovernmentMatch: tc.match, Categories: tc.categories, FleetVerdict: sources.FleetUnknown})
			if !bytes.Equal(before, subjectJSON(t, s)) {
				t.Fatal("subject changed")
			}
		})
	}
}

// R-FDKI-IJBM
func TestLinkFactsFleet(t *testing.T) {
	first := enrichers.Link{Scraper: fmcsa.Name, Reading: sources.Reading{FleetVerdict: sources.FleetFail, PowerUnits: count(1), Drivers: count(2)}}
	second := enrichers.Link{Scraper: fmcsa.Name, Reading: sources.Reading{FleetVerdict: sources.FleetPass, PowerUnits: count(5), Drivers: count(6)}}
	for _, tc := range []struct {
		name           string
		verdict        string
		units, drivers *int64
	}{
		{"pass", sources.FleetPass, count(3), count(4)},
		{"fail", sources.FleetFail, count(1), count(4)},
		{"unknown", sources.FleetUnknown, nil, nil},
		{"legacy", "", count(3), count(4)},
		{"unsupported verdict", "maybe", count(3), count(4)},
		{"wrong case verdict", "PASS", count(3), count(4)},
		{"negative units", sources.FleetPass, count(-1), count(4)},
		{"negative drivers", sources.FleetPass, count(3), count(-1)},
		{"zero counts", sources.FleetPass, count(0), count(0)},
		{"nil counts", sources.FleetPass, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := enrichers.Link{Scraper: fmcsa.Name, Reading: sources.Reading{Fleet: true, FleetVerdict: tc.verdict, PowerUnits: tc.units, Drivers: tc.drivers}}
			verdict := tc.verdict
			if verdict == "" || verdict == "maybe" || verdict == "PASS" {
				verdict = sources.FleetUnknown
			}
			units, drivers := tc.units, tc.drivers
			if units != nil && *units < 0 {
				units = nil
			}
			if drivers != nil && *drivers < 0 {
				drivers = nil
			}
			assertFleet(t, []enrichers.Link{l, second}, verdict, units, drivers)
		})
	}
	assertFleet(t, []enrichers.Link{{Scraper: ppp.Name, Reading: second.Reading}}, sources.FleetUnknown, nil, nil)
	assertFleet(t, []enrichers.Link{first, second}, sources.FleetFail, count(1), count(2))
	first.Gone = true
	assertFleet(t, []enrichers.Link{first, second}, sources.FleetPass, count(5), count(6))
	second.Gone = true
	assertFleet(t, []enrichers.Link{first, second}, sources.FleetUnknown, nil, nil)
}

// R-JSLE-MUH6
func TestLinkFactsValid(t *testing.T) {
	e := enrichers.LinkFacts{}
	valid := []string{`{"categories":[],"fleet_verdict":"unknown"}`, ` {"categories":[],"fleet_verdict":"pass","power_units":0,"drivers":null} `, `{"categories":[{"name":"construction","source":"ppp"}],"fleet_verdict":"fail","known_since":"2018-04-09","newest_filing":"2025-06-01","government_match":"x","power_units":9223372036854775807,"drivers":4}`, `{"categories":[],"FLEET_VERDICT":"unknown"}`, `{"categories":[],"fleet_verdict":"unknown","known_since":null}`}
	invalid := []string{`{}`, `{"fleet_verdict":"unknown"}`, `{"Categories":[],"fleet_verdict":"unknown"}`, `{"categories":null,"fleet_verdict":"unknown"}`, `{"categories":{},"fleet_verdict":"unknown"}`, `{"categories":[],"fleet_verdict":"maybe"}`, `{"categories":[]}`, `{"categories":[],"fleet_verdict":"unknown","power_units":-1}`, `{"categories":[],"fleet_verdict":"unknown","drivers":-1}`, `{"categories":[],"fleet_verdict":"unknown","power_units":1.5}`, `{"categories":[],"fleet_verdict":"unknown","drivers":"4"}`, `{"categories":[],"fleet_verdict":"unknown","known_since":"2018-4-9"}`, `{"categories":[],"fleet_verdict":"unknown","newest_filing":"2025-02-29"}`, `{"categories":[{"name":"a","source":""}],"fleet_verdict":"unknown"}`, `{"categories":[{"name":"","source":"ppp"}],"fleet_verdict":"unknown"}`, `{"categories":[null],"fleet_verdict":"unknown"}`, `{"categories":[{"name":"a","source":"ppp","x":1}],"fleet_verdict":"unknown"}`, `{"categories":[],"fleet_verdict":"unknown","x":1}`, `null`, `[]`, ``, ` `, `{"categories":[],"fleet_verdict":"unknown"} {}`, `{"categories":[],"fleet_verdict":"unknown"}x`}
	checkValidity(t, e, valid, invalid)
}

func checkValidity(t *testing.T, e enrichers.Enricher, valid, invalid []string) {
	t.Helper()
	for _, raw := range valid {
		if !e.Valid(json.RawMessage(raw)) {
			t.Errorf("rejected %s", raw)
		}
	}
	for _, raw := range invalid {
		if e.Valid(json.RawMessage(raw)) {
			t.Errorf("accepted %s", raw)
		}
	}
}

func subjectJSON(t *testing.T, s enrichers.Subject) []byte {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertLinkResult(t *testing.T, s enrichers.Subject, want enrichers.LinkFound) {
	t.Helper()
	if want.Headcounts == nil {
		want.Headcounts = []enrichers.Headcount{}
	}
	e := enrichers.LinkFacts{}
	r := e.Derive(s)
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != enrichers.SourceLinks || r.Cost != 0 || !bytes.Equal(r.Found, b) {
		t.Fatalf("result %+v, found %s, want %s", r, r.Found, b)
	}
	// R-JSLE-MUH6
	if !e.Valid(r.Found) {
		t.Fatalf("invalid derived link facts: %s", r.Found)
	}
}

func assertFleet(t *testing.T, links []enrichers.Link, verdict string, units, drivers *int64) {
	t.Helper()
	s := enrichers.Subject{Links: links}
	before := subjectJSON(t, s)
	r := (enrichers.LinkFacts{}).Derive(s)
	var got enrichers.LinkFound
	if err := json.Unmarshal(r.Found, &got); err != nil {
		t.Fatal(err)
	}
	if got.FleetVerdict != verdict || !reflect.DeepEqual(got.PowerUnits, units) || !reflect.DeepEqual(got.Drivers, drivers) {
		t.Fatalf("fleet %+v, want %q %v %v", got, verdict, units, drivers)
	}
	// R-JSLE-MUH6
	if !(enrichers.LinkFacts{}).Valid(r.Found) {
		t.Fatalf("invalid derived fleet facts: %s", r.Found)
	}
	if !bytes.Equal(before, subjectJSON(t, s)) {
		t.Fatal("subject changed")
	}
}

// R-FESE-WB2B
func TestSourceCategoriesFollowCarrierAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, class  string
		construction bool
		want         []enrichers.Category
	}{
		{"Pine County Highway Dept.", "PRIVATE PROPERTY", false, []enrichers.Category{{Name: sources.CategoryGovernment, Source: "name"}}},
		{"Pine County Highway Dept.", "LOCAL GOVERNMENT", false, []enrichers.Category{{Name: sources.CategoryGovernment, Source: "name"}}},
		{"Acme Freight", "AUTHORIZED FOR HIRE", false, []enrichers.Category{{Name: sources.CategoryTrucking, Source: fmcsa.Name}}},
		{"Acme Freight", "PRIVATE PROPERTY;AUTHORIZED FOR HIRE", false, []enrichers.Category{{Name: sources.CategoryTrucking, Source: fmcsa.Name}}},
		{"Acme Freight", "PRIVATE PROPERTY", false, []enrichers.Category{}},
		{"Acme Freight", "", false, []enrichers.Category{}},
		{"Acme Builders", "PRIVATE PROPERTY", true, []enrichers.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}}},
	} {
		raw, _ := json.Marshal(tc.class)
		reading := fmcsa.New().Read(scrapes.Row{Name: tc.name, Facts: map[string]json.RawMessage{"class": raw}})
		s := enrichers.Subject{Links: []enrichers.Link{{Scraper: fmcsa.Name, Reading: reading}}}
		if tc.construction {
			s.Links = append(s.Links, enrichers.Link{Scraper: ppp.Name, Reading: ppp.New().Read(scrapes.Row{Name: tc.name, Facts: map[string]json.RawMessage{"naics": json.RawMessage(`"236220"`)}})})
		}
		for _, gone := range []bool{false, true} {
			s.Links[0].Gone = gone
			var got enrichers.LinkFound
			if err := json.Unmarshal((enrichers.LinkFacts{}).Derive(s).Found, &got); err != nil {
				t.Fatal(err)
			}
			want := tc.want
			if gone && !tc.construction {
				want = []enrichers.Category{}
			}
			if !reflect.DeepEqual(got.Categories, want) {
				t.Fatal(tc, gone, got)
			}
			if !gone && tc.name == "Pine County Highway Dept." && got.GovernmentMatch != "COUNTY" {
				t.Fatal(got)
			}
		}
	}
}
