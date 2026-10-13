package fmcsa_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func facts(v map[string]string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage)
	for k, s := range v {
		b, _ := json.Marshal(s)
		out[k] = b
	}
	return out
}

// R-FOCD-MV1S R-25HY-W0KE
func TestSource(t *testing.T) {
	var s sources.Source = fmcsa.New()
	if s.Name() != fmcsa.Name || fmcsa.Name != "fmcsa" {
		t.Fatal(s.Name())
	}
	r := sources.New(s)
	if got := r.Names(); !reflect.DeepEqual(got, []string{fmcsa.Name}) {
		t.Fatal(got)
	}
	if _, ok := r.Get("bogus"); ok {
		t.Fatal("unknown")
	}
}

// R-FUFV-JPR9 R-20MD-CXLM
func TestIdentity(t *testing.T) {
	r := scrapes.Row{Name: "Acme, Inc.", Names: []string{"Other", "Third"}, Address: scrapes.Address{Street: "1 Road", City: "Town", State: "MN", Zip: "55072", County: "115"}, People: []scrapes.Person{{Name: "Alex", Role: "owner"}}, Facts: facts(map[string]string{"phone": "+1 320 555 0123", "email": "x@ACME.COM"})}
	got := fmcsa.New().Read(r)
	want := sources.Reading{NameKey: "ACME", PhoneKey: "3205550123", DomainKey: "acme.com", Name: "Acme, Inc.", Names: []string{"Other", "Third"}, Street: "1 Road", City: "Town", State: "MN", Zip: "55072", County: "MN-115", Phone: "+1 320 555 0123", Email: "x@ACME.COM", People: []sources.Person{{Name: "Alex", Role: "owner"}}, Class: sources.ClassUnknown, Cargo: []string{}, Filed: time.Time{}, Fleet: false, Estimate: nil, Exclusions: []sources.Exclusion{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v != %+v", got, want)
	}
	empty := fmcsa.New().Read(scrapes.Row{})
	if empty.Names == nil || empty.People == nil || empty.Cargo == nil || empty.Exclusions == nil {
		t.Fatal("nil slice")
	}
}

// R-FPKA-0MSH
func TestStringFacts(t *testing.T) {
	for _, raw := range []string{"null", "14", "true", "[]", "{}", "\"\"", "bad"} {
		r := scrapes.Row{Facts: map[string]json.RawMessage{"phone": json.RawMessage(raw), "email": json.RawMessage(raw), "class": json.RawMessage(raw), "status": json.RawMessage(raw), "power_units": json.RawMessage(raw), "mcs150_date": json.RawMessage(raw)}}
		got := fmcsa.New().Read(r)
		if got.Phone != "" || got.Email != "" || got.Estimate != nil || got.Fleet || !got.Filed.IsZero() || got.Class != sources.ClassUnknown {
			t.Fatal(raw, got)
		}
	}
	r := scrapes.Row{Facts: map[string]json.RawMessage{"phone": json.RawMessage(" \n\t\"a\\tb\" \r "), "email": json.RawMessage(" \"x@HOST.COM\" ")}}
	got := fmcsa.New().Read(r)
	if got.Phone != "a\tb" || got.Email != "x@HOST.COM" {
		t.Fatal(got)
	}
}

// R-FQS6-EEJ6
func TestPowerUnits(t *testing.T) {
	for s, want := range map[string]int64{"14": 14, "014": 14, "0": 0, "36893488147419": 36893488147419, "2": 2} {
		r := scrapes.Row{Facts: facts(map[string]string{"power_units": s, "status": "A", "class": "AUTHORIZED FOR HIRE"})}
		rd := fmcsa.New().Read(r)
		if rd.Estimate == nil || rd.Estimate.Low != 150000*want || *rd.Estimate.High != 250000*want || rd.Fleet != (want >= 2) {
			t.Fatal(s, rd)
		}
	}
	for _, s := range []string{"", " 14", "14 ", "14.0", "-3", "+3", "1e2", "36893488147420", "999999999999999999999999999999", "２"} {
		rd := fmcsa.New().Read(scrapes.Row{Facts: facts(map[string]string{"power_units": s, "status": "A"})})
		if rd.Estimate != nil || rd.Fleet {
			t.Fatal(s, rd)
		}
	}
	for _, raw := range []string{"14", "null"} {
		rd := fmcsa.New().Read(scrapes.Row{Facts: map[string]json.RawMessage{"power_units": json.RawMessage(raw)}})
		if rd.Estimate != nil {
			t.Fatal(raw)
		}
	}
}

// R-FS02-S69V
func TestFiling(t *testing.T) {
	for s, want := range map[string]time.Time{"20250318": time.Date(2025, 3, 18, 0, 0, 0, 0, time.UTC), "20240229": time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC), "20250230": {}, "2025-03-18": {}, "2025031": {}, "": {}, "２０２５０３１８": {}} {
		rd := fmcsa.New().Read(scrapes.Row{Facts: facts(map[string]string{"mcs150_date": s})})
		if rd.Filed != want {
			t.Fatal(s, rd.Filed)
		}
	}
}

// R-FT7Z-5Y0K R-FWVO-B98N R-FY3K-P0ZC
func TestClassEstimate(t *testing.T) {
	for _, c := range []struct {
		class, tag string
		confidence float64
		high       bool
	}{{"PRIVATE PROPERTY", sources.ClassPrivate, .3, false}, {"AUTHORIZED FOR HIRE;EXEMPT FOR HIRE", sources.ClassForHire, .7, true}, {" exempt for hire ", sources.ClassForHire, .7, true}, {" PRIVATE PROPERTY ; authorized for hire ", sources.ClassMixed, .5, false}, {"PRIVATE PASSENGER, BUSINESS", sources.ClassUnknown, .2, false}, {"STATE GOVERNMENT", sources.ClassUnknown, .2, false}, {"", sources.ClassUnknown, .2, false}} {
		r := scrapes.Row{Facts: facts(map[string]string{"class": c.class, "power_units": "14", "mcs150_date": "20250318", "status": "I", "ignored": "extra"})}
		rd := fmcsa.New().Read(r)
		e := rd.Estimate
		if rd.Class != c.tag || e == nil {
			t.Fatal(c, rd)
		}
		want := sources.Estimate{Source: fmcsa.Name, Low: 2100000, High: nil, Confidence: c.confidence, Facts: map[string]json.RawMessage{"class": r.Facts["class"], "power_units": r.Facts["power_units"], "mcs150_date": r.Facts["mcs150_date"]}, Filed: rd.Filed, Formula: sources.Rules}
		if c.high {
			h := int64(3500000)
			want.High = &h
		}
		if !reflect.DeepEqual(*e, want) {
			t.Fatalf("%+v != %+v", *e, want)
		}
		if rd.Fleet {
			t.Fatal("inactive fleet")
		}
	}
	r := scrapes.Row{Facts: map[string]json.RawMessage{"power_units": json.RawMessage(`"14"`), "class": json.RawMessage(`null`)}}
	e := fmcsa.New().Read(r).Estimate
	if e.Facts == nil || !reflect.DeepEqual(e.Facts, r.Facts) {
		t.Fatal(e)
	}
}

// R-FVNR-XHHY
func TestFleetCargo(t *testing.T) {
	for _, c := range []struct {
		status, n string
		fleet     bool
	}{{"A", "1", false}, {"I", "40", false}, {"A", "forty", false}, {"A", "2", true}, {"a", "2", false}, {" A", "2", false}} {
		r := scrapes.Row{Facts: facts(map[string]string{"status": c.status, "power_units": c.n})}
		r.Facts["cargo"] = json.RawMessage(`["bldgmat",null,12,"genfreight",{},true,""]`)
		got := fmcsa.New().Read(r)
		if got.Fleet != c.fleet || !reflect.DeepEqual(got.Cargo, []string{"bldgmat", "genfreight", ""}) {
			t.Fatal(got)
		}
	}
	for _, raw := range []string{"null", `"cargo"`, `{}`, "12"} {
		r := scrapes.Row{Facts: map[string]json.RawMessage{"cargo": json.RawMessage(raw)}}
		if got := fmcsa.New().Read(r).Cargo; got == nil || len(got) != 0 {
			t.Fatal(raw, got)
		}
	}
}

// R-W8JS-Y2DM
func TestExclusions(t *testing.T) {
	classes := []string{"STATE GOVERNMENT", "LOCAL GOVERNMENT", "FEDERAL GOVERNMENT", "INDIAN NATION"}
	patterns := []string{"COUNTY", "CITY OF", "TOWN OF", "VILLAGE OF", "TOWNSHIP", "SCHOOL DISTRICT", "STATE OF", "DEPT", "DEPARTMENT", "UNIVERSITY", "PUBLIC WORKS"}
	r := scrapes.Row{Name: "COUNTY CITY OF TOWN OF VILLAGE OF TOWNSHIP SCHOOL DISTRICT STATE OF DEPT DEPARTMENT UNIVERSITY PUBLIC WORKS LLC", Facts: facts(map[string]string{"class": " indian nation ; local government ; state government ; federal government ; local government"})}
	want := []sources.Exclusion{}
	for _, p := range classes {
		want = append(want, sources.Exclusion{Reason: sources.ExcludeGovernmentClass, Detail: p})
	}
	for _, p := range patterns {
		want = append(want, sources.Exclusion{Reason: sources.ExcludeGovernmentName, Detail: p})
	}
	if got := fmcsa.New().Read(r).Exclusions; !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	for _, name := range []string{"COUNTYLINE TRUCKING INC", "DEPTFORD LLC", "GOVERNMENT TRANSPORT", "MUNICIPAL TRUCKING"} {
		got := fmcsa.New().Read(scrapes.Row{Name: name}).Exclusions
		if len(got) != 0 {
			t.Fatal(name, got)
		}
	}
	r = scrapes.Row{Name: "Pine County Highway Dept.", Facts: facts(map[string]string{"class": "LOCAL GOVERNMENT"})}
	want = []sources.Exclusion{{Reason: sources.ExcludeGovernmentClass, Detail: "LOCAL GOVERNMENT"}, {Reason: sources.ExcludeGovernmentName, Detail: "COUNTY"}, {Reason: sources.ExcludeGovernmentName, Detail: "DEPT"}}
	if got := fmcsa.New().Read(r).Exclusions; !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

// R-G0JD-GKGQ
func TestOwnsReading(t *testing.T) {
	r := scrapes.Row{Name: "COUNTY HAULING", Names: []string{"alias"}, People: []scrapes.Person{{Name: "Alex", Role: "owner"}}, Facts: facts(map[string]string{"power_units": "14", "class": "AUTHORIZED FOR HIRE"})}
	r.Facts["cargo"] = json.RawMessage(`["cargo"]`)
	original, _ := json.Marshal(r)
	a, b := fmcsa.New().Read(r), fmcsa.New().Read(r)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("nondeterministic")
	}
	a.Names[0] = "new"
	a.People[0].Name = "new"
	a.Cargo[0] = "new"
	a.Exclusions[0].Detail = "new"
	a.Estimate.Facts["power_units"][1] = '9'
	*a.Estimate.High = 1
	a.Estimate.Low = 1
	after, _ := json.Marshal(r)
	if string(original) != string(after) || !reflect.DeepEqual(b, fmcsa.New().Read(r)) {
		t.Fatal("shares state")
	}
	r.Names[0] = "row new"
	r.People[0].Name = "row new"
	r.Facts["class"][1] = 'X'
	if b.Names[0] != "alias" || b.People[0].Name != "Alex" || string(b.Estimate.Facts["class"]) != `"AUTHORIZED FOR HIRE"` {
		t.Fatal("shares row")
	}
}
