package ppp_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func row(values map[string]string) scrapes.Row {
	r := scrapes.Row{Facts: map[string]json.RawMessage{}}
	for k, v := range values {
		b, _ := json.Marshal(v)
		r.Facts[k] = b
	}
	return r
}

// R-PC28-L68D R-01B5-XPMK
func TestSource(t *testing.T) {
	var source sources.Source = ppp.New()
	if source.Name() != ppp.Name || ppp.Name != "ppp" {
		t.Fatal(source.Name())
	}
	if got := source.Read(scrapes.Row{}); got.Estimate != nil {
		t.Fatal(got)
	}
}

// R-02J2-BHD9 R-08MK-8C2Q R-09UG-M3TF
func TestAmount(t *testing.T) {
	for _, tc := range []struct {
		s         string
		low, high int64
	}{{"200000", 2400000, 3840000}, {"152162.5", 1825950, 2921520}, {"0200000.00", 2400000, 3840000}, {"1.04", 12, 20}, {"0.01", 0, 0}, {"1000000000", 12000000000, 19200000000}} {
		rd := ppp.New().Read(row(map[string]string{"amount": tc.s, "date_approved": "04/09/2020"}))
		e := rd.Estimate
		if e == nil || e.Source != ppp.Name || e.Low != tc.low || e.High == nil || *e.High != tc.high || e.Confidence != .4 || e.Formula != sources.Rules || e.Filed != rd.Filed {
			t.Fatal(tc, e)
		}
	}
	for _, s := range []string{"0", "0.00", "", " 200000", "200000 ", "200000.", ".5", "-5", "+5", "1,000", "1e5", "1000000000.01", "1.2.3", "NaN", "２", "99999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999"} {
		if got := ppp.New().Read(row(map[string]string{"amount": s})); got.Estimate != nil {
			t.Fatal(s, got.Estimate)
		}
	}
	for _, raw := range []string{"200000", "null", "true", "[]", "{}", "bad"} {
		if got := ppp.New().Read(scrapes.Row{Facts: map[string]json.RawMessage{"amount": json.RawMessage(raw)}}); got.Estimate != nil {
			t.Fatal(raw, got)
		}
	}
	if ppp.New().Read(scrapes.Row{}).Estimate != nil {
		t.Fatal("absent")
	}
}

// R-03QY-P93Y R-04YV-30UN R-0EQ2-56S7
func TestDatesAndEstablished(t *testing.T) {
	approval := time.Date(2020, 4, 9, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		s    string
		want time.Time
	}{{"04/09/2020", approval}, {"02/29/2020", time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC)}, {"4/9/2020", time.Time{}}, {"02/30/2020", time.Time{}}, {"2020-04-09", time.Time{}}, {"04/09/20", time.Time{}}, {" 04/09/2020", time.Time{}}, {"", time.Time{}}} {
		for _, established := range []bool{false, true} {
			age := "New Business or 2 years or less"
			if established {
				age = "Existing or more than 2 years old"
			}
			got := ppp.New().Read(row(map[string]string{"date_approved": tc.s, "business_age": age}))
			want := tc.want
			if !want.IsZero() && established {
				want = want.AddDate(-2, 0, 0)
			}
			if got.Filed != tc.want || got.KnownSince != want {
				t.Fatal(tc, got)
			}
		}
	}
	for _, age := range []string{"existing or more than 2 years old", "Existing or more than 2 years old ", "New Business or 2 years or less", ""} {
		if got := ppp.New().Read(row(map[string]string{"date_approved": "04/09/2020", "business_age": age})); got.KnownSince != approval {
			t.Fatal(age, got)
		}
	}
	for _, raw := range []string{"4092020", "null", "true"} {
		got := ppp.New().Read(scrapes.Row{Facts: map[string]json.RawMessage{"date_approved": json.RawMessage(raw)}})
		if !got.Filed.IsZero() || !got.KnownSince.IsZero() {
			t.Fatal(got)
		}
	}
}

// R-7YF4-SGPW R-0EQ2-56S7
func TestCategoryGovernment(t *testing.T) {
	for category, codes := range map[string][]string{
		sources.CategoryConstruction:        {" 236220 ", "23"},
		sources.CategoryTrucking:            {"484121", "488510", "492110", "493110"},
		sources.CategoryAgriculture:         {"11", "111150", "112111", "113310", "114111", "115310", "119000"},
		sources.CategoryManufacturing:       {"311811", "325998", "332710"},
		sources.CategoryWholesale:           {"423810"},
		sources.CategoryMining:              {"21", "211120", "212321", "213112", "219000"},
		sources.CategoryWaste:               {"562111"},
		sources.CategoryUtilities:           {"221122"},
		sources.CategoryRetail:              {"445110", "452311"},
		sources.CategoryOtherTransportation: {"481111", "482111", "483111", "485111", "486110", "487110", "491110"},
		sources.CategoryInformation:         {"517311"},
		sources.CategoryFinance:             {"522110"},
		sources.CategoryRealEstate:          {"531110"},
		sources.CategoryProfessional:        {"541330", "551114"},
		sources.CategoryAdministrative:      {"561730"},
		sources.CategoryEducation:           {"611110"},
		sources.CategoryHealthCare:          {"621210"},
		sources.CategoryArts:                {"713940"},
		sources.CategoryFood:                {"722511"},
		sources.CategoryRepair:              {"811111", "812990"},
		sources.CategoryReligious:           {"813110"},
		sources.CategoryGovernment:          {"921110"},
		sources.CategoryOther:               {"814110", "999990", "000000", "489000", "563000", "48", "56", "81", "4", "2"},
		"":                                  {"", "   ", "48A1", "+236220", "２３６２２０", "23-6220", "N/A"},
	} {
		for _, code := range codes {
			r := row(map[string]string{"naics": code})
			r.Name = "CITY OF EDINA"
			got := ppp.New().Read(r)
			if got.Category != category || got.GovernmentMatch != "CITY OF" {
				t.Fatal(code, got)
			}
		}
	}
	for _, raw := range []string{"236220", "null", "true", "[]", "{}"} {
		if got := ppp.New().Read(scrapes.Row{Facts: map[string]json.RawMessage{"naics": json.RawMessage(raw)}}); got.Category != "" {
			t.Fatal(raw, got)
		}
	}
	if got := ppp.New().Read(scrapes.Row{Name: "Acme Builders Inc"}); got.Category != "" || got.GovernmentMatch != "" {
		t.Fatal(got)
	}
}

// R-07EN-UKC1 R-0DI5-RF1I
func TestIdentityAndEvidence(t *testing.T) {
	r := row(map[string]string{"phone": "6125550100", "email": "ops@acmebuilders.com", "amount": "0", "date_approved": ""})
	r.Name = "ACME BUILDERS INC"
	r.Names = []string{"Alias", "Other"}
	r.Address = scrapes.Address{Street: "1 Road", City: "Town", State: "MN", Zip: "55072", County: "HENNEPIN"}
	r.People = []scrapes.Person{{Name: "Pat", Role: "owner"}}
	want := sources.Reading{FleetVerdict: sources.FleetUnknown, NameKey: "ACME BUILDERS", Name: r.Name, Names: []string{"Alias", "Other"}, Street: "1 Road", City: "Town", State: "MN", Zip: "55072", County: "MN-HENNEPIN", People: []sources.Person{}, Cargo: []string{}, Evidence: []sources.Evidence{{Name: sources.EvidencePPPLoan, Source: ppp.Name, Weight: .2, Description: sources.DescribePPPLoan}}}
	if got := ppp.New().Read(r); !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	got := ppp.New().Read(scrapes.Row{})
	if got.Names == nil || got.People == nil || got.Cargo == nil || !reflect.DeepEqual(got.Evidence, want.Evidence) {
		t.Fatal(got)
	}
}

// R-0B2C-ZVK4 R-0FXY-IYIW
func TestFactsAndOwnership(t *testing.T) {
	r := row(map[string]string{"amount": "200000", "naics": " 236220 ", "date_approved": "04/09/2020", "business_age": "Existing or more than 2 years old", "jobs_reported": "12", "lender": "First Bank", "business_type": "Corporation"})
	r.Names = []string{"alias"}
	before, _ := json.Marshal(r)
	a, b := ppp.New().Read(r), ppp.New().Read(r)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("non-deterministic")
	}
	want := map[string]json.RawMessage{}
	for _, k := range []string{"amount", "naics", "date_approved", "business_age", "jobs_reported"} {
		want[k] = r.Facts[k]
	}
	if !reflect.DeepEqual(a.Estimate.Facts, want) {
		t.Fatal(a.Estimate.Facts)
	}
	a.Names[0] = "changed"
	a.Evidence[0].Description = "changed"
	a.People = append(a.People, sources.Person{Name: "new"})
	a.Cargo = append(a.Cargo, "new")
	*a.Estimate.High = 1
	a.Estimate.Low = 1
	for k := range a.Estimate.Facts {
		a.Estimate.Facts[k][1] = 'X'
	}
	after, _ := json.Marshal(r)
	if string(before) != string(after) || !reflect.DeepEqual(b, ppp.New().Read(r)) {
		t.Fatal("shares state")
	}
	r.Names[0] = "row changed"
	r.Facts["amount"][1] = '9'
	if b.Names[0] != "alias" || string(b.Estimate.Facts["amount"]) != `"200000"` {
		t.Fatal("shares row")
	}
	sparse := row(map[string]string{"amount": "200000", "naics": " 236220 ", "jobs_reported": "12", "lender": "First Bank"})
	want = map[string]json.RawMessage{"amount": sparse.Facts["amount"], "naics": sparse.Facts["naics"], "jobs_reported": sparse.Facts["jobs_reported"]}
	if got := ppp.New().Read(sparse).Estimate.Facts; !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

// R-47RG-75NH
func TestFleetUnknown(t *testing.T) {
	for _, r := range []scrapes.Row{{}, row(map[string]string{"power_units": "14", "total_drivers": "4", "status": "A"})} {
		got := ppp.New().Read(r)
		if got.FleetVerdict != sources.FleetUnknown || got.PowerUnits != nil || got.Drivers != nil {
			t.Fatal(got)
		}
	}
}
