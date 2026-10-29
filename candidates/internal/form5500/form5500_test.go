package form5500_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/form5500"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func row(f map[string]string) scrapes.Row {
	r := scrapes.Row{Facts: map[string]json.RawMessage{}}
	for k, v := range f {
		b, _ := json.Marshal(v)
		r.Facts[k] = b
	}
	return r
}
func read(f map[string]string) sources.Reading { return form5500.New().Read(row(f)) }

// R-2CTJ-O6QC R-DGYG-8492 R-0PRE-CG2Z
func TestSource(t *testing.T) {
	var s sources.Source = form5500.New()
	if s.Name() != form5500.Name || form5500.Name != "form5500" {
		t.Fatal(s.Name())
	}
	if got := sources.New(fmcsa.New(), ppp.New(), s).Names(); !reflect.DeepEqual(got, []string{fmcsa.Name, ppp.Name, form5500.Name}) {
		t.Fatal(got)
	}
}

// R-DPHQ-WIFX R-DUDC-FLEP R-DRXJ-O1XB R-DVL8-TD5E
func TestIdentityAndFacts(t *testing.T) {
	r := row(map[string]string{"phone": "3205550100", "email": "ops@example.com", "ein": "411234567", "received": "2025-06-12", "plan_effective": "1995-01-01", "business_code": "238900", "class": "AUTHORIZED FOR HIRE", "status": "A", "power_units": "14", "total_drivers": "4", "active_participants_eoy": "21", "net_assets_eoy": "1000000", "employer_contributions": "50000"})
	r.Facts["cargo"] = json.RawMessage(`["genfreight"]`)
	r.Name = "Hjort Excavating, Inc."
	r.Names = []string{r.Name}
	r.Address = scrapes.Address{Street: "1 Street", City: "Town", State: "MN", Zip: "55072", County: "HENNEPIN"}
	r.People = []scrapes.Person{{Name: "Alex", Role: "owner"}, {Name: "Sam", Role: "contact"}}
	got := form5500.New().Read(r)
	want := sources.Reading{TradeNames: []string{}, Mailing: true, NameKey: "HJORT EXCAVATING", NameKeys: []string{"HJORT EXCAVATING"}, EINKey: "411234567", PhoneKey: "3205550100", Name: r.Name, Names: []string{r.Name}, Street: "1 Street", City: "Town", State: "MN", Zip: "55072", Phone: "3205550100", People: []sources.Person{{Name: "Alex", Role: "owner"}, {Name: "Sam", Role: "contact"}}, Cargo: []string{}, FleetVerdict: sources.FleetUnknown, Filed: time.Date(2025, 6, 12, 0, 0, 0, 0, time.UTC), KnownSince: time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC), Category: sources.CategoryConstruction, Evidence: []sources.Evidence{{Name: sources.EvidenceForm5500Filing, Source: form5500.Name, Weight: .2, Description: sources.DescribeForm5500Filing}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v != %+v", got, want)
	}
	for _, facts := range []map[string]string{nil, {"form": "5500", "entity": "1"}, {"final_filing": "1"}} {
		g := read(facts)
		if !reflect.DeepEqual(g.Evidence, want.Evidence) {
			t.Fatal(g)
		}
	}
	for _, raw := range []string{`"000000000"`, `"41123456"`, `" 411234567"`, `""`, `411234567`, `null`} {
		r.Facts["ein"] = json.RawMessage(raw)
		if form5500.New().Read(r).EINKey != "" {
			t.Fatal(raw)
		}
	}
	r.Facts = nil
	r.Name = "CITY OF EDINA"
	g := form5500.New().Read(r)
	if g.EINKey != "" || g.Phone != "" || g.GovernmentMatch != "CITY OF" || g.Category != "" {
		t.Fatal(g)
	}
	for _, raw := range []string{`""`, `"N/A"`, `238900`, `null`} {
		r.Facts = map[string]json.RawMessage{"business_code": json.RawMessage(raw)}
		if form5500.New().Read(r).Category != "" {
			t.Fatal(raw)
		}
	}
}

// R-DI6C-LVZR R-DUDC-FLEP
func TestDates(t *testing.T) {
	for _, key := range []string{"received", "plan_effective", "plan_year_end", "plan_year_begin"} {
		for _, value := range []string{"2024-12-31", "2024-02-29", "2023-02-29", "2024-13-01", "2024-1-31", "12/31/2024", "20241231", " 2024-12-31", "2024-12-31 ", "2024-12-31T00:00:00Z", ""} {
			f := map[string]string{"form": "5500", "entity": "2", "active_participants_eoy": "21"}
			if key == "plan_year_begin" {
				f["active_participants_eoy"] = "0"
				f["participants_boy"] = "36"
			}
			f[key] = value
			g := read(f)
			got := g.Filed
			switch key {
			case "plan_effective":
				got = g.KnownSince
			case "plan_year_end", "plan_year_begin":
				got = g.Headcount.AsOf
			}
			want := time.Time{}
			if value == "2024-12-31" {
				want = time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
			}
			if value == "2024-02-29" {
				want = time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
			}
			if got != want {
				t.Fatal(key, value, got)
			}
		}
		for _, raw := range []string{`20241231`, `null`, `true`} {
			r := row(map[string]string{"form": "5500", "entity": "2", "active_participants_eoy": "21", "participants_boy": "36"})
			if key == "plan_year_begin" {
				delete(r.Facts, "active_participants_eoy")
			}
			r.Facts[key] = json.RawMessage(raw)
			g := form5500.New().Read(r)
			if !g.Filed.IsZero() || !g.KnownSince.IsZero() || !g.Headcount.AsOf.IsZero() {
				t.Fatal(key, raw, g)
			}
		}
	}
}

// R-DKM5-DFH5 R-DLU1-R77U R-0VUW-9ASG
func TestHeadcounts(t *testing.T) {
	for _, v := range []string{"21", "021", "0", "36893488147419", "", " 21", "21 ", "21.0", "-3", "1e2", "1,200", "36893488147420"} {
		for _, field := range []string{"active_participants_eoy", "participants_boy"} {
			f := map[string]string{"form": "5500-SF", "entity": "1", "plan_year_end": "2024-12-31", "plan_year_begin": "2024-01-01"}
			f[field] = v
			g := read(f)
			n := int64(0)
			switch v {
			case "21", "021":
				n = 21
			case "36893488147419":
				n = 36893488147419
			}
			if n == 0 {
				if g.Headcount != nil {
					t.Fatal(v, g)
				}
			} else {
				d := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
				if field == "participants_boy" {
					d = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
				}
				if g.Headcount == nil || *g.Headcount != (sources.Headcount{Scraper: form5500.Name, Kind: sources.HeadcountParticipants, Count: n, AsOf: d}) {
					t.Fatal(field, v, g)
				}
			}
		}
	}
	for _, pair := range [][2]string{{"5500", "2"}, {"5500-SF", "1"}} {
		g := read(map[string]string{"form": pair[0], "entity": pair[1], "active_participants_eoy": "21", "participants_boy": "36", "plan_year_end": "2024-12-31", "plan_year_begin": "2024-01-01"})
		want := sources.Headcount{Scraper: form5500.Name, Kind: sources.HeadcountParticipants, Count: 21, AsOf: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)}
		if g.Headcount == nil || *g.Headcount != want {
			t.Fatal(pair, g)
		}
	}
	for _, pair := range [][2]string{{"5500", "2"}, {"5500-SF", "1"}, {"5500", "1"}, {"5500", "3"}, {"5500", "4"}, {"5500-SF", "2"}, {"5500-sf", "1"}, {"5500", " 2"}, {"5500", ""}, {"", "2"}} {
		g := read(map[string]string{"form": pair[0], "entity": pair[1], "active_participants_eoy": "21"})
		want := pair == [2]string{"5500", "2"} || pair == [2]string{"5500-SF", "1"}
		if (g.Headcount != nil) != want {
			t.Fatal(pair, g)
		}
	}
	for _, raw := range []string{`21`, `null`, `true`} {
		r := row(map[string]string{"form": "5500", "entity": "2"})
		r.Facts["active_participants_eoy"] = json.RawMessage(raw)
		if g := form5500.New().Read(r); g.Headcount != nil {
			t.Fatal(raw, g)
		}
	}
	r := row(map[string]string{"form": "5500", "active_participants_eoy": "21"})
	r.Facts["entity"] = json.RawMessage(`2`)
	if form5500.New().Read(r).Headcount != nil {
		t.Fatal("numeric entity")
	}
	for _, bad := range []string{"", "0", "21.0", "36893488147420"} {
		g := read(map[string]string{"form": "5500", "entity": "2", "active_participants_eoy": bad, "participants_boy": "36", "plan_year_begin": "2024-01-01", "plan_year_end": "2024-12-31"})
		if g.Headcount == nil || g.Headcount.Count != 36 || g.Headcount.AsOf != time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) {
			t.Fatal(g)
		}
	}
}

// R-DN1Y-4YYJ R-DQPN-AA6M
func TestDBANames(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want []string
	}{
		{"BROWN HAWK LLC DBA NORTHERN LIGHTS STEEL FAB", []string{"BROWN HAWK LLC", "NORTHERN LIGHTS STEEL FAB"}},
		{"Acme Inc d/b/a Acme Paving", []string{"Acme Inc", "Acme Paving"}},
		{"SMITH CO.  D.B.A. SMITH TRUCKING", []string{"SMITH CO.", "SMITH TRUCKING"}},
		{"A DBA B D/B/A C", []string{"A", "B D/B/A C"}}, {"A D/B/A B DBA C", []string{"A", "B DBA C"}}, {"SMITH DBA ", []string{"SMITH"}},
		{"DBA HOLDINGS LLC", nil}, {"ADBA SERVICES", nil}, {"SMITH DBAX INC", nil}, {"SMITH-DBA-JONES", nil}, {"HEGGIES PIZZA, LLC", nil}, {"", nil},
	} {
		g := read(map[string]string{"sponsor_name": tc.s})
		want := append([]string{}, tc.want...)
		if !reflect.DeepEqual(g.Names, want) {
			t.Fatal(tc, g.Names)
		}
	}
}

// R-0UMZ-VJ1R R-DQPN-AA6M
func TestPlanNames(t *testing.T) {
	for _, tc := range [][2]string{
		{"NORTHERN LIGHTS STEEL FABRICATION 401(K) PROFIT SHARING PLAN", "NORTHERN LIGHTS STEEL FABRICATION"}, {"HEGGIES SLICE OF THE PIE", "HEGGIES SLICE OF THE PIE"}, {"Acme Paving, Inc. 401(k) Plan", "Acme Paving, Inc."}, {"ACME HEALTH AND 401K PLAN", "ACME"}, {"SMITH & JONES AND 401K PLAN", "SMITH & JONES"}, {"Smith and 401k Plan", "Smith"}, {"Acme And Sons Retirement Plan", "Acme And Sons"}, {"ACME & - PENSION PLAN", "ACME"}, {"ACME CO, RETIREMENT SAVINGS PLAN", "ACME CO"}, {"ACME EMPLOYEES' BENEFIT TRUST", "ACME"}, {"ACME LIFEWAY SEC. 125 PLAN", "ACME LIFEWAY"}, {"ACME 401KPLUS TRUST", "ACME 401KPLUS"}, {"PLANET FITNESS 401K", "PLANET FITNESS"}, {"BRAND 401K PLAN", "BRAND"}, {"UPLAND 401K PLAN", "UPLAND"}, {"HIGHLAND PLAN", "HIGHLAND"}, {"401(K) PLAN", ""}, {"AND PLAN", ""}, {"", ""},
	} {
		g := read(map[string]string{"plan_name": tc[0]})
		want := []string{}
		if tc[1] != "" {
			want = append(want, tc[1])
		}
		if !reflect.DeepEqual(g.Names, want) {
			t.Fatal(tc, g.Names)
		}
	}
	for _, word := range []string{"401(K)", "401K", "403(B)", "403B", "PROFIT SHARING", "PROFIT-SHARING", "RETIREMENT", "PENSION", "SAVINGS", "THRIFT", "INCENTIVE", "EMPLOYEE STOCK", "STOCK OWNERSHIP", "ESOP", "CASH BALANCE", "DEFINED BENEFIT", "DEFINED CONTRIBUTION", "MONEY PURCHASE", "HEALTH", "WELFARE", "BENEFIT", "BENEFITS", "CAFETERIA", "FLEXIBLE", "SECTION 125", "SEC 125", "SEC. 125", "DENTAL", "VISION", "LIFE", "DISABILITY", "MEDICAL", "INSURANCE", "EMPLOYEES'", "EMPLOYEES", "EMPLOYEE", "PLAN", "TRUST"} {
		g := read(map[string]string{"plan_name": "Acme " + word + " Fund"})
		if !reflect.DeepEqual(g.Names, []string{"Acme"}) {
			t.Fatal(word, g.Names)
		}
	}
}

// R-DQPN-AA6M R-DWT5-74W3
func TestNameOrderingAndIsolation(t *testing.T) {
	for _, tc := range []struct {
		names         []string
		sponsor, plan string
		want          []string
	}{
		{[]string{"SMITH TRUCKING"}, "SMITH CO DBA SMITH TRUCKING", "", []string{"SMITH TRUCKING", "SMITH CO"}},
		{[]string{"ACME PAVING INC"}, "", "Acme Paving, Inc. 401(k) Plan", []string{"ACME PAVING INC"}},
		{[]string{"SMITH CO DBA ACME PAVING"}, "SMITH CO DBA ACME PAVING", "ACME PAVING 401K PLAN", []string{"SMITH CO DBA ACME PAVING", "SMITH CO", "ACME PAVING"}},
		{[]string{"SMITH DBA"}, "SMITH DBA ", "401(K) PLAN", []string{"SMITH DBA", "SMITH"}},
		{nil, "", "", []string{}},
	} {
		r := row(map[string]string{"sponsor_name": tc.sponsor, "plan_name": tc.plan})
		r.Names = tc.names
		g := form5500.New().Read(r)
		if !reflect.DeepEqual(g.Names, tc.want) || !reflect.DeepEqual(g.NameKeys, sources.NameKeys(tc.want)) {
			t.Fatal(g)
		}
	}
	r := row(map[string]string{"form": "5500", "entity": "2", "active_participants_eoy": "21"})
	r.Names = []string{"Acme"}
	r.People = []scrapes.Person{{Name: "Alex", Role: "owner"}}
	before, _ := json.Marshal(r)
	a, b := form5500.New().Read(r), form5500.New().Read(r)
	if !reflect.DeepEqual(a, b) {
		t.Fatal(a, b)
	}
	afterRead, _ := json.Marshal(r)
	if string(before) != string(afterRead) {
		t.Fatal("Read changed input")
	}
	savedB, _ := json.Marshal(b)
	a.Names[0] = "changed"
	a.NameKeys[0] = "changed"
	a.People[0].Name = "changed"
	a.Evidence[0].Weight = 1
	a.Headcount.Count = 99
	a.Cargo = append(a.Cargo, "changed")
	after, _ := json.Marshal(r)
	afterB, _ := json.Marshal(b)
	if string(before) != string(after) || string(savedB) != string(afterB) {
		t.Fatal("aliased input or output")
	}
	if !reflect.DeepEqual(form5500.New().Read(r), b) {
		t.Fatal("subsequent Read changed output")
	}
}

// R-DZ8X-YODH R-1PBY-SGWN
func TestDescription(t *testing.T) {
	const description string = sources.DescribeForm5500Filing
	if description == "" || strings.Contains(description, "\n") {
		t.Fatal(description)
	}
	for _, other := range []string{sources.DescribeFMCSAActive, sources.DescribeFMCSAActivity, sources.DescribePPPLoan, sources.DescribeOwnDomain, sources.DescribeWebsiteLive} {
		if description == other {
			t.Fatal(description)
		}
	}
}
