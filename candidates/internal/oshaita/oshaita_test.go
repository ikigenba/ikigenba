package oshaita_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func row(f map[string]any) scrapes.Row {
	r := scrapes.Row{Name: "HJORT EXCAVATING, INC.", Names: []string{"HJORT EXCAVATING, INC.", "North Site"}, Facts: map[string]json.RawMessage{}}
	for k, v := range f {
		b, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		r.Facts[k] = b
	}
	return r
}
func ptr(n int64) *int64                       { return &n }
func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
func read(f map[string]any) sources.Reading    { return oshaita.New().Read(row(f)) }

// R-SOPP-F1BM R-1QJV-68NC R-1XV9-GV3I R-20B2-8EKW R-23YR-DPSZ
func TestReading(t *testing.T) {
	var src sources.Source = oshaita.New()
	if src.Name() != oshaita.Name || oshaita.Name != "osha_ita" {
		t.Fatal(src.Name())
	}
	r := row(map[string]any{"ein": "10739420", "phone": "6515550100", "email": "ops@example.test", "class": "AUTHORIZED FOR HIRE", "status": "A", "power_units": "14", "total_drivers": "4", "cargo": []string{"genfreight"}})
	r.Address.Street = "1 Main"
	r.Address.City = "Town"
	r.Address.State = "MN"
	r.Address.Zip = "55072"
	r.Address.County = "ANOKA"
	r.People = append(r.People, scrapes.Person{Name: "Alex", Role: "owner"})
	want := sources.Reading{NameKey: sources.NameKey(r.Name), NameKeys: sources.NameKeys(r.Names), EINKey: "010739420", Name: r.Name, Names: append([]string{}, r.Names...), Street: r.Address.Street, City: r.Address.City, State: r.Address.State, Zip: r.Address.Zip, People: []sources.Person{}, Cargo: []string{}, FleetVerdict: sources.FleetUnknown, Evidence: []sources.Evidence{{Name: sources.EvidenceOSHAITASummary, Source: oshaita.Name, Weight: .2, Description: sources.DescribeOSHAITASummary}}}
	if got := src.Read(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
	r.Name = "INDEPENDENT SCHOOL DISTRICT 728"
	r.Names = nil
	r.Facts = nil
	got := src.Read(r)
	if got.GovernmentMatch != "SCHOOL DISTRICT" || got.Names == nil || got.NameKeys == nil || len(got.Names) != 0 || len(got.NameKeys) != 0 || !reflect.DeepEqual(got.Evidence, want.Evidence) {
		t.Fatal(got)
	}
	r.Names = []string{"DESIGN READY CONTROLS INC", "Design Ready Controls - BP8"}
	if got := src.Read(r); !reflect.DeepEqual(got.NameKeys, []string{"DESIGN READY CONTROLS", "DESIGN READY CONTROLS BP8"}) {
		t.Fatal(got.NameKeys)
	}
}

// R-1RRR-K0E1 R-1Z35-UMU7
func TestFigures(t *testing.T) {
	cases := []struct {
		v    any
		want *int64
	}{{"23", ptr(23)}, {"23.00", ptr(23)}, {"23.0", ptr(23)}, {"023", ptr(23)}, {"0", ptr(0)}, {"0.00", ptr(0)}, {"36893488147419", ptr(36893488147419)}, {"36893488147419.00", ptr(36893488147419)}, {"", nil}, {" 23", nil}, {"23 ", nil}, {"23.", nil}, {"23.5", nil}, {"23.50", nil}, {".00", nil}, {"-3", nil}, {"+3", nil}, {"1,200", nil}, {"1e2", nil}, {"n/a", nil}, {"36893488147420", nil}, {"36893488147420.00", nil}, {23, nil}, {nil, nil}, {"99999999999999999999999999999", nil}}
	for _, c := range cases {
		t.Run(string(mustJSON(c.v)), func(t *testing.T) {
			got := read(map[string]any{"annual_average_employees": c.v, "total_hours_worked": c.v})
			if !reflect.DeepEqual(got.Employees, c.want) || !reflect.DeepEqual(got.Hours, c.want) {
				t.Fatal(got.Employees, got.Hours, c.want)
			}
		})
	}
	got := read(map[string]any{"establishment_name": "HJORT EXCAVATING, INC.", "year_filing_for": "2025", "annual_average_employees": "43694", "total_hours_worked": "25"})
	if got.Establishment != "HJORT EXCAVATING, INC." || *got.Year != 2025 || *got.Employees != 43694 || *got.Hours != 25 || got.Headcount != nil {
		t.Fatal(got)
	}
	if got := read(nil); got.Year != nil || got.Hours != nil || got.Employees != nil || got.Establishment != "" {
		t.Fatal(got)
	}
}
func mustJSON(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return b
}

// R-1SZN-XS4Q
func TestFilingYear(t *testing.T) {
	for _, c := range []struct {
		v    any
		want *int64
	}{{"2025", ptr(2025)}, {"2019.00", ptr(2019)}, {"9999", ptr(9999)}, {"0001", ptr(1)}, {"0", nil}, {"10000", nil}, {"", nil}, {"FY2025", nil}, {" 2025", nil}, {2025, nil}, {nil, nil}} {
		got := read(map[string]any{"year_filing_for": c.v})
		if !reflect.DeepEqual(got.Year, c.want) {
			t.Fatal(c.v, got.Year)
		}
	}
	if read(nil).Year != nil {
		t.Fatal("absent year")
	}
}

// R-1U7K-BJVF
func TestSubmissionDate(t *testing.T) {
	for _, c := range []struct {
		v    any
		want time.Time
	}{{"1/2/2018 8:16:46", day(2018, 1, 2)}, {"12/31/2020 23:59:59", day(2020, 12, 31)}, {"01JAN22:11:14:00", day(2022, 1, 1)}, {"01jan22:11:14:00", day(2022, 1, 1)}, {"15MAR21:08:05:09", day(2021, 3, 15)}, {"1/1/2023", day(2023, 1, 1)}, {"01/01/2023", day(2023, 1, 1)}, {"", time.Time{}}, {" 1/1/2023", time.Time{}}, {"1/1/2023 ", time.Time{}}, {"2023-01-01", time.Time{}}, {"2/30/2023", time.Time{}}, {"13/1/2023", time.Time{}}, {"1/1/23", time.Time{}}, {"1/2/2018 8:16", time.Time{}}, {"01JAN22", time.Time{}}, {20230101, time.Time{}}, {nil, time.Time{}}} {
		if got := read(map[string]any{"date_submitted": c.v}); !got.Filed.Equal(c.want) {
			t.Fatal(c.v, got.Filed, c.want)
		}
	}
	if !read(nil).Filed.IsZero() {
		t.Fatal("absent submission")
	}
}

// R-1VFG-PBM4
func TestEIN(t *testing.T) {
	for _, v := range []any{"010739420", "10739420", "411234567", "00000000", "000000000", "1073942", "4112345678", "41-1234567", " 10739420", "10739420 ", "411234567.00", "", 10739420, nil} {
		want := ""
		if v == "010739420" || v == "10739420" {
			want = "010739420"
		}
		if v == "411234567" {
			want = "411234567"
		}
		if got := read(map[string]any{"ein": v}).EINKey; got != want {
			t.Fatal(v, got)
		}
	}
	if read(nil).EINKey != "" {
		t.Fatal("absent EIN")
	}
}

// R-1WND-33CT R-21IY-M6BL
func TestHeadcountPlausibility(t *testing.T) {
	for _, c := range []struct {
		e  string
		h  any
		ok bool
	}{{"23", "", true}, {"23", "   ", true}, {"23", nil, true}, {"23", "47840", true}, {"23", "47840.00", true}, {"23", "11500", true}, {"23", "92000", true}, {"23", "11499", false}, {"23", "92001", false}, {"23", "0", false}, {"23", "0.00", false}, {"23", "n/a", false}, {"23", "47,840", false}, {"23", " 47840", false}, {"23", "47840.5", false}, {"23", "36893488147420", false}, {"152574", "92", false}, {"3", "39261", false}, {"357", "715864", true}, {"43694", "25", false}, {"0", "", false}, {"", "", false}, {"23.5", "", false}, {"1450", "2610000", true}, {"36893488147419", "36893488147419", false}} {
		got := read(map[string]any{"year_filing_for": "2025", "annual_average_employees": c.e, "total_hours_worked": c.h})
		if (got.Headcount != nil) != c.ok {
			t.Fatalf("%v %v: %#v", c.e, c.h, got)
		}
		if c.ok {
			want := sources.Headcount{Scraper: oshaita.Name, Kind: sources.HeadcountEmployees, Count: *got.Employees, AsOf: day(2025, 12, 31), Hours: got.Hours}
			if !reflect.DeepEqual(got.Headcount, &want) {
				t.Fatal(got.Headcount, want)
			}
		}
	}
	for _, y := range []any{nil, "0", "2019.00"} {
		got := read(map[string]any{"annual_average_employees": "23.00", "year_filing_for": y})
		if got.Headcount == nil || got.Headcount.Count != 23 || got.Headcount.Hours != nil {
			t.Fatal(got)
		}
		want := time.Time{}
		if y == "2019.00" {
			want = day(2019, 12, 31)
		}
		if !got.Headcount.AsOf.Equal(want) {
			t.Fatal(got.Headcount)
		}
	}
	if read(nil).Headcount != nil {
		t.Fatal("absent employees")
	}
}

// R-22QU-ZY2A
func TestCategoryAndKnownSince(t *testing.T) {
	for _, c := range []struct {
		code any
		want string
	}{{"238910", sources.CategoryConstruction}, {"238910.00", sources.CategoryConstruction}, {" 238910 ", sources.CategoryConstruction}, {"238910.0", sources.CategoryConstruction}, {"484121", sources.CategoryTrucking}, {"611110", sources.CategoryEducation}, {"424510", sources.CategoryWholesale}, {"", ""}, {"N/A", ""}, {"23.5", ""}, {".00", ""}, {238910, ""}, {nil, ""}} {
		got := read(map[string]any{"naics_code": c.code, "year_filing_for": "2019.00"})
		if got.Category != c.want || !got.KnownSince.Equal(day(2019, 1, 1)) {
			t.Fatal(c.code, got)
		}
	}
	for _, v := range []any{nil, "", "0"} {
		if !read(map[string]any{"year_filing_for": v}).KnownSince.IsZero() {
			t.Fatal(v)
		}
	}
}

// R-23YR-DPSZ R-256N-RHJO
func TestIndependentReadings(t *testing.T) {
	r := row(map[string]any{"establishment_name": "Site A", "annual_average_employees": "23", "total_hours_worked": "47840", "year_filing_for": "2025"})
	before := string(mustJSON(r))
	a, b := oshaita.New().Read(r), oshaita.New().Read(r)
	if !reflect.DeepEqual(a, b) || string(mustJSON(r)) != before {
		t.Fatal(a, b)
	}
	a.Names[0] = "changed"
	a.NameKeys[0] = "changed"
	a.People = append(a.People, sources.Person{Name: "changed"})
	a.Cargo = append(a.Cargo, "changed")
	a.Evidence[0].Weight = 1
	*a.Headcount.Hours = 1
	*a.Headcount = sources.Headcount{}
	*a.Year = 1
	*a.Employees = 1
	*a.Hours = 1
	if !reflect.DeepEqual(oshaita.New().Read(r), b) || string(mustJSON(r)) != before {
		t.Fatal("shared storage")
	}
	for _, f := range []map[string]any{nil, {"annual_average_employees": "43694", "total_hours_worked": "25"}, {"annual_average_employees": "0"}, {"date_submitted": "", "year_filing_for": ""}} {
		if got := read(f).Evidence; !reflect.DeepEqual(got, b.Evidence) {
			t.Fatal(got)
		}
	}
}
