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

// R-FUFV-JPR9
func TestIdentity(t *testing.T) {
	r := scrapes.Row{Name: "Acme, Inc.", Names: []string{"Other", "Third"}, Address: scrapes.Address{Street: "1 Road", City: "Town", State: "MN", Zip: "55072", County: "115"}, People: []scrapes.Person{{Name: "Alex", Role: "owner"}}, Facts: facts(map[string]string{"phone": "+1 320 555 0123", "email": "x@ACME.COM"})}
	got := fmcsa.New().Read(r)
	want := sources.Reading{NameKey: "ACME", NameKeys: []string{"OTHER", "THIRD"}, PhoneKey: "3205550123", DomainKey: "acme.com", Name: "Acme, Inc.", Names: []string{"Other", "Third"}, Street: "1 Road", City: "Town", State: "MN", Zip: "55072", County: "MN-115", Phone: "+1 320 555 0123", Email: "x@ACME.COM", People: []sources.Person{{Name: "Alex", Role: "owner"}}, Class: sources.ClassUnknown, Cargo: []string{}, Filed: time.Time{}, Fleet: false, FleetVerdict: sources.FleetFail, Estimate: nil, Evidence: []sources.Evidence{{Name: sources.EvidenceOwnDomain, Source: fmcsa.Name, Weight: .2, Description: sources.DescribeOwnDomain}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v != %+v", got, want)
	}
	empty := fmcsa.New().Read(scrapes.Row{})
	if empty.Names == nil || empty.People == nil || empty.Cargo == nil || empty.Evidence == nil {
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

// R-FS02-S69V R-EAXM-4HCO
func TestFiling(t *testing.T) {
	for s, want := range map[string]time.Time{"20250318": time.Date(2025, 3, 18, 0, 0, 0, 0, time.UTC), "20240229": time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC), "20250230": {}, "2025-03-18": {}, "2025031": {}, "": {}, "２０２５０３１８": {}} {
		rd := fmcsa.New().Read(scrapes.Row{Facts: facts(map[string]string{"mcs150_date": s})})
		if rd.Filed != want {
			t.Fatal(s, rd.Filed)
		}
	}
}

// R-FT7Z-5Y0K R-EAXM-4HCO R-47BO-U40T R-48JL-7VRI R-49RH-LNI7
func TestClassEstimate(t *testing.T) {
	for _, c := range []struct {
		class, tag string
		confidence float64
		high       bool
	}{{"PRIVATE PROPERTY", sources.ClassPrivate, .3, false}, {"AUTHORIZED FOR HIRE;EXEMPT FOR HIRE", sources.ClassForHire, .7, true}, {" exempt for hire ", sources.ClassForHire, .7, true}, {" PRIVATE PROPERTY ; authorized for hire ", sources.ClassMixed, .5, false}, {"PRIVATE PASSENGER, BUSINESS", sources.ClassUnknown, .2, false}, {"STATE GOVERNMENT", sources.ClassUnknown, .2, false}, {"", sources.ClassUnknown, .2, false}} {
		r := scrapes.Row{Facts: facts(map[string]string{"class": c.class, "power_units": "14", "mcs150_date": "20250318", "status": "I", "ignored": "extra"})}
		rd := fmcsa.New().Read(r)
		e := rd.Estimate
		if rd.Class != c.tag {
			t.Fatal(c, rd)
		}
		if c.tag == sources.ClassPrivate || c.tag == sources.ClassUnknown {
			if e != nil {
				t.Fatal(e)
			}
			continue
		}
		if e == nil {
			t.Fatal(rd)
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
	if e := fmcsa.New().Read(scrapes.Row{Facts: map[string]json.RawMessage{"power_units": json.RawMessage(`"14"`), "class": json.RawMessage(`null`)}}).Estimate; e != nil {
		t.Fatal(e)
	}
}

// R-EAXM-4HCO
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

// R-C1P7-BDXD
func TestDriverCount(t *testing.T) {
	for _, tc := range []struct {
		name, units, raw string
		confidence       float64
		missing          bool
	}{
		{"four", "201", `"4"`, 0, false},
		{"leading zeros", "9866", `"011335"`, .7, false},
		{"leading zeros reported", "201", `"04"`, 0, false},
		{"reported zero", "51", `"0"`, 0, false},
		{"ceiling", "36893488147419", `"36893488147419"`, .7, false},
		{"absent", "510", "", .7, true},
		{"empty", "510", `""`, .7, false},
		{"space", "510", `" 4"`, .7, false},
		{"trailing space", "510", `"4 "`, .7, false},
		{"fraction", "510", `"4.0"`, .7, false},
		{"negative", "510", `"-1"`, .7, false},
		{"positive sign", "510", `"+4"`, .7, false},
		{"exponent", "510", `"1e2"`, .7, false},
		{"non ASCII", "510", `"４"`, .7, false},
		{"over ceiling", "510", `"36893488147420"`, .7, false},
		{"overflow", "510", `"99999999999999999999999999"`, .7, false},
		{"number", "510", `4`, .7, false},
		{"null", "510", `null`, .7, false},
		{"boolean", "510", `true`, .7, false},
		{"array", "510", `[]`, .7, false},
		{"object", "510", `{}`, .7, false},
		{"JSON whitespace", "201", " \n\t\"4\" \r", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := scrapes.Row{Facts: facts(map[string]string{"power_units": tc.units, "class": "AUTHORIZED FOR HIRE"})}
			if !tc.missing {
				r.Facts["total_drivers"] = json.RawMessage(tc.raw)
			}
			got := fmcsa.New().Read(r)
			if tc.confidence == 0 {
				if got.Estimate != nil {
					t.Fatal(got.Estimate)
				}
			} else if got.Estimate == nil || got.Estimate.Confidence != tc.confidence {
				t.Fatal(got.Estimate)
			}
		})
	}
}

// R-E16F-2BF4
func TestMileage(t *testing.T) {
	for _, raw := range []string{`"847225359"`, `"00012"`, `"36893488147419"`, " \n\t\"12\" \r", `"0"`, `"000"`, `""`, `"n/a"`, `"1,200"`, `" 12"`, `"12 "`, `"12.0"`, `"-1"`, `"+12"`, `"1e2"`, `"１２"`, `"36893488147420"`, `"9999999999999999999999999"`, `0`, `12`, `null`, `true`, `[]`, `{}`, "bad", ""} {
		t.Run(raw, func(t *testing.T) {
			r := scrapes.Row{Facts: facts(map[string]string{"power_units": "201", "total_drivers": "4", "class": "AUTHORIZED FOR HIRE"})}
			if raw != "" {
				r.Facts["mileage"] = json.RawMessage(raw)
			}
			positive := raw == `"847225359"` || raw == `"00012"` || raw == `"36893488147419"` || raw == " \n\t\"12\" \r"
			got := fmcsa.New().Read(r)
			if positive {
				if got.Estimate == nil || got.Estimate.Confidence != .35 {
					t.Fatal(got.Estimate)
				}
			} else if got.Estimate != nil {
				t.Fatal(got.Estimate)
			}
		})
	}
}

// R-E4U4-7MN7 R-E79W-Z64L R-EAXM-4HCO R-47BO-U40T R-48JL-7VRI
func TestPlausibility(t *testing.T) {
	for _, tc := range []struct {
		units, drivers, mileage string
		confidence              float64
	}{
		{"199999", "4", "0", 0}, {"199999", "4", "", 0},
		{"201", "4", "0", 0}, {"51", "0", "", 0},
		{"36893488147419", "1", "0", 0},
		{"36893488147419", "36893488147419", "0", .7},
		{"200", "4", "0", .35}, {"50", "0", "", .35},
		{"199999", "4", "12", .35}, {"510", "", "0", .7},
		{"1828", "1", "19390412", .35}, {"41", "4", "0", .35},
		{"11", "0", "", .35}, {"40", "4", "", .7},
		{"10", "0", "", .7}, {"2", "1", "", .7},
		{"9866", "11335", "847225359", .7}, {"forty", "4", "0", 0},
		{"", "4", "12", 0}, {"0", "0", "0", .7},
	} {
		t.Run(tc.units+"/"+tc.drivers+"/"+tc.mileage, func(t *testing.T) {
			f := map[string]string{"power_units": tc.units, "status": "A", "class": "AUTHORIZED FOR HIRE"}
			if tc.drivers != "" {
				f["total_drivers"] = tc.drivers
			}
			if tc.mileage != "" {
				f["mileage"] = tc.mileage
			}
			got := fmcsa.New().Read(scrapes.Row{Facts: facts(f)})
			if tc.confidence == 0 {
				if got.Estimate != nil || got.Fleet {
					t.Fatal(got)
				}
				return
			}
			e := got.Estimate
			if e == nil || e.Confidence != tc.confidence || got.Fleet != (tc.units != "0") {
				t.Fatal(got)
			}
			var units int64
			if err := json.Unmarshal([]byte(tc.units), &units); err != nil {
				t.Fatal(err)
			}
			if e.Source != fmcsa.Name || e.Low != 150000*units || e.High == nil || *e.High != 250000*units || e.Filed != got.Filed || e.Formula != sources.Rules {
				t.Fatal(e)
			}
			if tc.confidence == .35 && string(e.Facts["plausibility"]) != `"suspicious"` {
				t.Fatal(e.Facts)
			}
			if tc.confidence == .7 {
				if _, ok := e.Facts["plausibility"]; ok {
					t.Fatal(e.Facts)
				}
			}
		})
	}
}

// R-E16F-2BF4 R-E4U4-7MN7 R-E79W-Z64L R-EAXM-4HCO R-47BO-U40T R-49RH-LNI7
func TestMileageUsesRowKey(t *testing.T) {
	for _, tc := range []struct {
		name, mileage, legacy, year string
		estimate                    bool
	}{
		{"legacy only", "", `"847225359"`, "", false},
		{"year only", "", "", `"2024"`, false},
		{"zero with legacy", `"0"`, `"847225359"`, `"2024"`, false},
		{"unreadable with legacy", `null`, `"847225359"`, `"2024"`, false},
		{"positive with zero legacy", `"19390412"`, `"0"`, `"2024"`, true},
		{"positive with conflicting legacy", `"19390412"`, `"5"`, `"2024"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := scrapes.Row{Facts: facts(map[string]string{"power_units": "1828", "total_drivers": "1", "status": "A", "class": "AUTHORIZED FOR HIRE"})}
			for key, raw := range map[string]string{"mileage": tc.mileage, "mcs150_mileage": tc.legacy, "mileage_year": tc.year} {
				if raw != "" {
					r.Facts[key] = json.RawMessage(raw)
				}
			}
			got := fmcsa.New().Read(r)
			if !tc.estimate {
				if got.Estimate != nil || got.Fleet {
					t.Fatal(got)
				}
				return
			}
			want := map[string]json.RawMessage{"class": r.Facts["class"], "power_units": r.Facts["power_units"], "total_drivers": r.Facts["total_drivers"], "mileage": r.Facts["mileage"], "plausibility": json.RawMessage(`"suspicious"`)}
			if got.Estimate == nil || got.Estimate.Confidence != .35 || !got.Fleet || !reflect.DeepEqual(got.Estimate.Facts, want) {
				t.Fatal(got)
			}
		})
	}
}

// R-48JL-7VRI R-49RH-LNI7
func TestSuspiciousEstimate(t *testing.T) {
	for _, tc := range []struct {
		class      string
		confidence float64
		high       bool
	}{{"AUTHORIZED FOR HIRE", .35, true}, {"PRIVATE PROPERTY;AUTHORIZED FOR HIRE", .25, false}} {
		r := scrapes.Row{Facts: facts(map[string]string{"power_units": "1828", "total_drivers": "1", "mileage": "19390412", "class": tc.class, "mcs150_date": "20250318", "status": "I", "other": "extra", "plausibility": "from row"})}
		r.Facts["total_drivers"] = json.RawMessage(" \n\"1\" \t")
		got := fmcsa.New().Read(r)
		want := sources.Estimate{Source: fmcsa.Name, Low: 274200000, Confidence: tc.confidence, Filed: time.Date(2025, 3, 18, 0, 0, 0, 0, time.UTC), Formula: sources.Rules, Facts: map[string]json.RawMessage{}}
		for _, key := range []string{"power_units", "class", "mcs150_date", "total_drivers", "mileage"} {
			want.Facts[key] = r.Facts[key]
		}
		want.Facts["plausibility"] = json.RawMessage(`"suspicious"`)
		if tc.high {
			h := int64(457000000)
			want.High = &h
		}
		if got.Estimate == nil || !reflect.DeepEqual(*got.Estimate, want) || got.Fleet {
			t.Fatalf("got %+v, want %+v", got.Estimate, want)
		}
	}
	r := scrapes.Row{Facts: map[string]json.RawMessage{"class": json.RawMessage(`"AUTHORIZED FOR HIRE"`), "power_units": json.RawMessage(`"1828"`), "total_drivers": json.RawMessage(`"1"`), "mileage": json.RawMessage(`"19390412"`)}}
	want := map[string]json.RawMessage{"class": r.Facts["class"], "power_units": r.Facts["power_units"], "total_drivers": r.Facts["total_drivers"], "mileage": r.Facts["mileage"], "plausibility": json.RawMessage(`"suspicious"`)}
	if got := fmcsa.New().Read(r).Estimate; got == nil || !reflect.DeepEqual(got.Facts, want) {
		t.Fatal(got)
	}
}

// R-49RH-LNI7
func TestEstimateCopiesUnreadableFacts(t *testing.T) {
	r := scrapes.Row{Facts: map[string]json.RawMessage{"power_units": json.RawMessage(`"14"`), "class": json.RawMessage(`"AUTHORIZED FOR HIRE"`), "mcs150_date": json.RawMessage(`{}`), "total_drivers": json.RawMessage(`4`), "mileage": json.RawMessage(" \nnull\t"), "mcs150_mileage": json.RawMessage(`"847225359"`), "mileage_year": json.RawMessage(`"2024"`), "plausibility": json.RawMessage(`"from row"`)}}
	want := map[string]json.RawMessage{}
	for _, key := range []string{"power_units", "class", "mcs150_date", "total_drivers", "mileage"} {
		want[key] = r.Facts[key]
	}
	if got := fmcsa.New().Read(r).Estimate; got == nil || !reflect.DeepEqual(got.Facts, want) {
		t.Fatal(got)
	}
}

// R-JJZ1-365H R-JL6X-GXW6 R-ZXNG-SEEH
func TestAddedCategoryGovernment(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want time.Time
	}{
		{`"20040115"`, time.Date(2004, 1, 15, 0, 0, 0, 0, time.UTC)},
		{`"20250230"`, time.Time{}}, {`"2004-01-15"`, time.Time{}}, {`"2004011"`, time.Time{}}, {`""`, time.Time{}}, {`20040115`, time.Time{}}, {`null`, time.Time{}}, {``, time.Time{}},
	} {
		r := scrapes.Row{Name: "Pine County Highway Dept.", Facts: facts(map[string]string{"class": "LOCAL GOVERNMENT", "power_units": "199999", "total_drivers": "4", "mileage": "0"})}
		if tc.raw != "" {
			r.Facts["added"] = json.RawMessage(tc.raw)
		}
		for _, implausible := range []bool{false, true} {
			if !implausible {
				delete(r.Facts, "power_units")
			} else {
				r.Facts["power_units"] = json.RawMessage(`"199999"`)
			}
			got := fmcsa.New().Read(r)
			if got.KnownSince != tc.want || got.Category != "" || got.GovernmentMatch != "COUNTY" {
				t.Fatal(got)
			}
		}
	}
	if got := fmcsa.New().Read(scrapes.Row{Name: "COUNTYLINE TRUCKING INC"}); !got.KnownSince.IsZero() || got.GovernmentMatch != "" {
		t.Fatal(got)
	}
}

// R-ZYVD-6656
func TestEvidence(t *testing.T) {
	active := sources.Evidence{Name: sources.EvidenceFMCSAActive, Source: fmcsa.Name, Weight: .6, Description: sources.DescribeFMCSAActive}
	activity := sources.Evidence{Name: sources.EvidenceFMCSAActivity, Source: fmcsa.Name, Weight: .3, Description: sources.DescribeFMCSAActivity}
	domain := sources.Evidence{Name: sources.EvidenceOwnDomain, Source: fmcsa.Name, Weight: .2, Description: sources.DescribeOwnDomain}
	for _, tc := range []struct {
		status, units, drivers, mileage, email string
		want                                   []sources.Evidence
	}{
		{"A", "14", "20", "500000", "dispatch@acmetrucking.com", []sources.Evidence{active, activity, domain}},
		{"I", "14", "20", "500000", "joe@gmail.com", []sources.Evidence{activity}},
		{"A", "14", "0", "500000", "", []sources.Evidence{active}},
		{"A", "14", "", "500000", "", []sources.Evidence{active}},
		{"A", "forty", "3", "100", "", []sources.Evidence{active, activity}},
		{"A", "199999", "4", "0", "dispatch@acmetrucking.com", []sources.Evidence{}},
		{"", "", "", "", "", []sources.Evidence{}},
	} {
		got := fmcsa.New().Read(scrapes.Row{Facts: facts(map[string]string{"status": tc.status, "power_units": tc.units, "total_drivers": tc.drivers, "mileage": tc.mileage, "email": tc.email})})
		if !reflect.DeepEqual(got.Evidence, tc.want) {
			t.Fatal(got.Evidence, tc.want)
		}
	}
}

// R-4AZD-ZF8W
func TestOwnsReading(t *testing.T) {
	r := scrapes.Row{Name: "COUNTY HAULING", Names: []string{"alias"}, People: []scrapes.Person{{Name: "Alex", Role: "owner"}}, Facts: facts(map[string]string{"power_units": "14", "class": "AUTHORIZED FOR HIRE", "total_drivers": "1", "mileage": "12"})}
	r.Facts["cargo"] = json.RawMessage(`["cargo"]`)
	original, _ := json.Marshal(r)
	a, b := fmcsa.New().Read(r), fmcsa.New().Read(r)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("nondeterministic")
	}
	a.Names[0] = "new"
	a.People[0].Name = "new"
	a.Cargo[0] = "new"
	a.Evidence[0].Description = "new"
	for _, key := range []string{"power_units", "total_drivers", "mileage", "plausibility"} {
		a.Estimate.Facts[key][1] = '9'
	}
	*a.Estimate.High = 1
	a.Headcount.Count = 999
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

// R-45BN-FM63 R-46JJ-TDWS
func TestFleetVerdictAndReportedCounts(t *testing.T) {
	for _, tc := range []struct {
		status, units, drivers, mileage, verdict string
		power, driver                            *int64
	}{
		{"A", "3", "4", "0", sources.FleetPass, number(3), number(4)},
		{"A", "2", "1", "0", sources.FleetPass, number(2), number(1)},
		{"A", "1828", "1", "19390412", sources.FleetPass, number(1828), number(1)},
		{"A", "1", "1", "0", sources.FleetFail, number(1), number(1)},
		{"A", "0", "0", "0", sources.FleetFail, number(0), number(0)},
		{"I", "40", "4", "0", sources.FleetFail, number(40), number(4)},
		{"I", "forty", "3", "0", sources.FleetFail, nil, number(3)},
		{"", "14", "20", "0", sources.FleetFail, number(14), number(20)},
		{"A", "forty", "3", "0", sources.FleetUnknown, nil, number(3)},
		{"A", "", "", "", sources.FleetUnknown, nil, nil},
		{"A", "199999", "4", "0", sources.FleetUnknown, nil, nil},
		{"A", "510", "", "", sources.FleetPass, number(510), nil},
		{"A", "36893488147420", "4", "", sources.FleetUnknown, nil, number(4)},
	} {
		got := fmcsa.New().Read(scrapes.Row{Facts: facts(map[string]string{"status": tc.status, "power_units": tc.units, "total_drivers": tc.drivers, "mileage": tc.mileage})})
		if got.FleetVerdict != tc.verdict || got.Fleet != (tc.verdict == sources.FleetPass) || !reflect.DeepEqual(got.PowerUnits, tc.power) || !reflect.DeepEqual(got.Drivers, tc.driver) {
			t.Fatalf("%+v: %+v", tc, got)
		}
	}
	for _, raw := range []string{"null", "14", "true", "[]", "{}"} {
		got := fmcsa.New().Read(scrapes.Row{Facts: map[string]json.RawMessage{"status": json.RawMessage(`"A"`), "power_units": json.RawMessage(raw), "total_drivers": json.RawMessage(raw)}})
		if got.FleetVerdict != sources.FleetUnknown || got.PowerUnits != nil || got.Drivers != nil {
			t.Fatal(raw, got)
		}
	}
}
func number(n int64) *int64 { return &n }

// R-JL6X-GXW6
func TestCategoryRequiresForHireAuthority(t *testing.T) {
	for _, tc := range []struct{ class, tag, category string }{
		{"AUTHORIZED FOR HIRE", sources.ClassForHire, sources.CategoryTrucking},
		{"exempt for hire ", sources.ClassForHire, sources.CategoryTrucking},
		{"PRIVATE PROPERTY;AUTHORIZED FOR HIRE", sources.ClassMixed, sources.CategoryTrucking},
		{"PRIVATE PROPERTY", sources.ClassPrivate, ""},
		{"", sources.ClassUnknown, ""},
		{"LOCAL GOVERNMENT", sources.ClassUnknown, ""},
	} {
		for _, implausible := range []bool{false, true} {
			r := scrapes.Row{Facts: facts(map[string]string{"class": tc.class})}
			if implausible {
				r.Facts = facts(map[string]string{"class": tc.class, "power_units": "199999", "total_drivers": "4", "mileage": "0"})
			}
			got := fmcsa.New().Read(r)
			if got.Class != tc.tag || got.Category != tc.category {
				t.Fatal(tc, implausible, got)
			}
		}
	}
}
