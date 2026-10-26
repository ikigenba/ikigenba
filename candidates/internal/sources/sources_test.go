package sources_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-JNFR-RHJA
func TestRecordShapes(t *testing.T) {
	// Conversions use the records' complete underlying shapes, including order.
	type personShape struct{ Name, Role string }
	type evidenceShape struct {
		Name, Source string
		Weight       float64
		Description  string
	}
	type categoryShape struct{ Name, Source string }
	type estimateShape struct {
		Source     string
		Low        int64
		High       *int64
		Confidence float64
		Facts      map[string]json.RawMessage
		Filed      time.Time
		Formula    int64
	}
	type headcountShape struct {
		Scraper, Kind string
		Count         int64
		AsOf          time.Time
		Hours         *int64
	}
	if headcountShape(sources.Headcount{}) != (headcountShape{}) {
		t.Fatal("headcount shape")
	}
	type readingShape struct {
		NameKey                                        string
		NameKeys                                       []string
		EINKey, PhoneKey, DomainKey, Name              string
		Names                                          []string
		Street, City, State, Zip, County, Phone, Email string
		People                                         []sources.Person
		Class                                          string
		Cargo                                          []string
		Filed                                          time.Time
		Fleet                                          bool
		FleetVerdict                                   string
		PowerUnits, Drivers                            *int64
		Headcount                                      *sources.Headcount
		Estimate                                       *sources.Estimate
		Evidence                                       []sources.Evidence
		KnownSince                                     time.Time
		Category, GovernmentMatch, Establishment       string
		Year, Employees, Hours                         *int64
	}
	if personShape(sources.Person{}) != (personShape{}) || evidenceShape(sources.Evidence{}) != (evidenceShape{}) || categoryShape(sources.Category{}) != (categoryShape{}) {
		t.Fatal("zero record")
	}
	if !reflect.DeepEqual(estimateShape(sources.Estimate{}), estimateShape{}) || !reflect.DeepEqual(readingShape(sources.Reading{}), readingShape{}) {
		t.Fatal("zero record")
	}
}

type fake struct{ name string }

func (f *fake) Name() string                   { return f.name }
func (*fake) Read(scrapes.Row) sources.Reading { return sources.Reading{} }

// R-295U-IVI9 R-0PRE-CG2Z
func TestRegistry(t *testing.T) {
	a, b, c := &fake{"z"}, &fake{"a"}, &fake{"z"}
	r := sources.New(a, b, c)
	for range 2 {
		ns := r.Names()
		if !reflect.DeepEqual(ns, []string{"z", "a"}) {
			t.Fatal(ns)
		}
		ns[0] = "changed"
	}
	if got, ok := r.Get("z"); !ok || got != a {
		t.Fatal(got, ok)
	}
	if got, ok := r.Get("none"); ok || got != nil {
		t.Fatal(got, ok)
	}
	if ns := sources.New().Names(); ns == nil || len(ns) != 0 {
		t.Fatal(ns)
	}
}

// R-W9RP-BU4B
func TestNameKey(t *testing.T) {
	for in, want := range map[string]string{"Acme Trucking, Inc.": "ACME TRUCKING", "J.B. Hunt Transport Co Inc": "JB HUNT TRANSPORT", "  smith  &  sons l.l.c. ": "SMITH SONS", "COMPANY": "COMPANY", "INC LLC": "INC", "CO-OP SERVICES": "COOP SERVICES", "Peña Hauling LLC": "PEÑA HAULING", "": "", "...": "", "A LTD CORP COMPANY CO INC LLC": "A", "hauling\t\n42": "HAULING 42"} {
		if got := sources.NameKey(in); got != want {
			t.Errorf("%q: %q != %q", in, got, want)
		}
	}
}

// R-G2Z6-83Y4
func TestPhoneKey(t *testing.T) {
	for in, want := range map[string]string{"(320) 555-0134": "3205550134", "+1 320.555.0134": "3205550134", "555-0134": "5550134", "": "", "none": "", "１２3٤5": "35", "123456789012345": "6789012345"} {
		if got := sources.PhoneKey(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

// R-G472-LVOT
func TestDomainKey(t *testing.T) {
	for in, want := range map[string]string{"Dispatch@AcmeTrucking.COM ": "acmetrucking.com", "a@b@acme.net": "acme.net", "nobody": "", "a@ ": "", "@X.COM": "x.com"} {
		if got := sources.DomainKey(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	for _, d := range []string{"aol.com", "arvig.net", "att.net", "bellsouth.net", "centurylink.net", "centurytel.net", "charter.net", "comcast.net", "earthlink.net", "embarqmail.com", "frontier.com", "frontiernet.net", "gmail.com", "gmx.com", "googlemail.com", "hotmail.com", "hughes.net", "icloud.com", "live.com", "mac.com", "mail.com", "mchsi.com", "me.com", "midco.net", "msn.com", "outlook.com", "protonmail.com", "q.com", "qwest.net", "rocketmail.com", "sbcglobal.net", "tds.net", "usfamily.net", "verizon.net", "windstream.net", "yahoo.com", "ymail.com"} {
		if got := sources.DomainKey("x@" + d); got != "" {
			t.Error(d, got)
		}
	}
}

// R-G5EY-ZNFI
func TestCountySourceKey(t *testing.T) {
	for _, v := range [][3]string{{" MN ", " 115 ", "MN-115"}, {"", "115", ""}, {"MN", " \t", ""}, {" mn ", " 001 ", "mn-001"}} {
		if got := sources.County(v[0], v[1]); got != v[2] {
			t.Fatal(got)
		}
	}
	if got := sources.SourceKey("a:b", "c:d"); got != "a:b:c:d" {
		t.Fatal(got)
	}
	if sources.SourceKey("", "") != ":" {
		t.Fatal("empty key")
	}
}

// R-SIM7-I6M5
func TestConstants(t *testing.T) {
	const rules float64 = sources.Rules
	const parity = sources.Rules % 2
	_ = rules
	_ = parity
	const (
		classPrivate                string = sources.ClassPrivate
		classForHire                string = sources.ClassForHire
		classMixed                  string = sources.ClassMixed
		classUnknown                string = sources.ClassUnknown
		categoryConstruction        string = sources.CategoryConstruction
		categoryTrucking            string = sources.CategoryTrucking
		categoryAgriculture         string = sources.CategoryAgriculture
		categoryManufacturing       string = sources.CategoryManufacturing
		categoryWholesale           string = sources.CategoryWholesale
		categoryMining              string = sources.CategoryMining
		categoryWaste               string = sources.CategoryWaste
		categoryUtilities           string = sources.CategoryUtilities
		categoryRetail              string = sources.CategoryRetail
		categoryOtherTransportation string = sources.CategoryOtherTransportation
		categoryInformation         string = sources.CategoryInformation
		categoryFinance             string = sources.CategoryFinance
		categoryRealEstate          string = sources.CategoryRealEstate
		categoryProfessional        string = sources.CategoryProfessional
		categoryAdministrative      string = sources.CategoryAdministrative
		categoryEducation           string = sources.CategoryEducation
		categoryHealthCare          string = sources.CategoryHealthCare
		categoryArts                string = sources.CategoryArts
		categoryFood                string = sources.CategoryFood
		categoryRepair              string = sources.CategoryRepair
		categoryReligious           string = sources.CategoryReligious
		categoryGovernment          string = sources.CategoryGovernment
		categoryOther               string = sources.CategoryOther
		evidenceFMCSAActive         string = sources.EvidenceFMCSAActive
		evidenceFMCSAActivity       string = sources.EvidenceFMCSAActivity
		evidencePPPLoan             string = sources.EvidencePPPLoan
		evidenceOwnDomain           string = sources.EvidenceOwnDomain
		evidenceWebsiteLive         string = sources.EvidenceWebsiteLive
	)
	got := []string{classPrivate, classForHire, classMixed, classUnknown, categoryConstruction, categoryTrucking, categoryAgriculture, categoryManufacturing, categoryWholesale, categoryMining, categoryWaste, categoryUtilities, categoryRetail, categoryOtherTransportation, categoryInformation, categoryFinance, categoryRealEstate, categoryProfessional, categoryAdministrative, categoryEducation, categoryHealthCare, categoryArts, categoryFood, categoryRepair, categoryReligious, categoryGovernment, categoryOther, evidenceFMCSAActive, evidenceFMCSAActivity, evidencePPPLoan, sources.EvidenceForm5500Filing, sources.EvidenceOSHAITASummary, evidenceOwnDomain, evidenceWebsiteLive}
	want := []string{"private", "for_hire", "mixed", "unknown", "construction", "trucking and logistics", "agriculture", "manufacturing", "wholesale and distribution", "mining and aggregates", "waste and environmental", "utilities", "retail", "other transportation", "information", "finance and insurance", "real estate", "professional services", "administrative services", "education", "health care", "arts and recreation", "food and lodging", "repair and personal services", "religious and civic", "government", "other", "fmcsa_active", "fmcsa_activity", "ppp_loan", "form5500_filing", "osha_ita_summary", "own_domain", "website_live"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

// R-1PBY-SGWN R-ZV7O-0UX3 R-27MG-J112
func TestDescriptions(t *testing.T) {
	const a string = sources.DescribeFMCSAActive
	const b string = sources.DescribeFMCSAActivity
	const c string = sources.DescribePPPLoan
	const d string = sources.DescribeOwnDomain
	seen := map[string]bool{}
	for _, s := range []string{a, b, c, d, sources.DescribeForm5500Filing, sources.DescribeOSHAITASummary, sources.DescribeWebsiteLive} {
		if s == "" || strings.Contains(s, "\n") || seen[s] {
			t.Fatal(s)
		}
		seen[s] = true
	}
}

// R-ZWFK-EMNS
func TestGovernmentMatch(t *testing.T) {
	patterns := []string{"COUNTY", "CITY OF", "TOWN OF", "VILLAGE OF", "TOWNSHIP", "SCHOOL DISTRICT", "STATE OF", "DEPT", "DEPARTMENT", "UNIVERSITY", "PUBLIC WORKS"}
	for i, p := range patterns {
		if got := sources.GovernmentMatch("PREFIX " + strings.Join(patterns[i:], " ") + " SUFFIX"); got != p {
			t.Fatal(got, p)
		}
	}
	for _, k := range []string{"COUNTYLINE TRUCKING INC", "DEPTFORD LLC", "TOWNSHIPS UNLIMITED", "", "county"} {
		if got := sources.GovernmentMatch(k); got != "" {
			t.Fatal(k, got)
		}
	}
}

// R-YVJG-N9LW
func TestFleetVerdictConstants(t *testing.T) {
	const pass string = sources.FleetPass
	const fail string = sources.FleetFail
	const unknown string = sources.FleetUnknown
	if pass != "pass" || fail != "fail" || unknown != "unknown" {
		t.Fatal(pass, fail, unknown)
	}
}

// R-Y3HR-UJJX
func TestWebsiteConstants(t *testing.T) {
	const check string = sources.WebsiteCheck
	const secure string = sources.WebsiteHTTPS
	const plain string = sources.WebsiteHTTP
	const none string = sources.WebsiteNone
	const unknown string = sources.WebsiteUnknown
	got := []string{check, secure, plain, none, unknown}
	if !reflect.DeepEqual(got, []string{"website_check", "https", "http", "none", "unknown"}) {
		t.Fatal(got)
	}
}

// R-3ZNV-YYLR R-40VS-CQCG
func TestWebsiteEvidenceDescription(t *testing.T) {
	const description string = sources.DescribeWebsiteLive
	if description == "" || strings.Contains(description, "\n") {
		t.Fatal(description)
	}
	for _, other := range []string{sources.DescribeFMCSAActive, sources.DescribeFMCSAActivity, sources.DescribePPPLoan, sources.DescribeOwnDomain} {
		if description == other {
			t.Fatal(description)
		}
	}
}

// R-KVPQ-8YVJ R-SL20-9Q3J
func TestCategories(t *testing.T) {
	want := []string{sources.CategoryConstruction, sources.CategoryTrucking, sources.CategoryAgriculture, sources.CategoryManufacturing, sources.CategoryWholesale, sources.CategoryMining, sources.CategoryWaste, sources.CategoryUtilities, sources.CategoryRetail, sources.CategoryOtherTransportation, sources.CategoryInformation, sources.CategoryFinance, sources.CategoryRealEstate, sources.CategoryProfessional, sources.CategoryAdministrative, sources.CategoryEducation, sources.CategoryHealthCare, sources.CategoryArts, sources.CategoryFood, sources.CategoryRepair, sources.CategoryReligious, sources.CategoryGovernment, sources.CategoryOther}
	got := sources.Categories()
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	for i := range got {
		got[i] = "changed"
	}
	if !reflect.DeepEqual(sources.Categories(), want) {
		t.Fatal("shared category storage")
	}
}

// R-4EN3-4QGZ
func TestHeadcountKinds(t *testing.T) {
	const kinds = sources.HeadcountEmployees + sources.HeadcountParticipants + sources.HeadcountJobs + sources.HeadcountDrivers + sources.HeadcountSnippet
	if kinds == "" {
		t.Fatal(kinds)
	}
	if got := []string{sources.HeadcountEmployees, sources.HeadcountParticipants, sources.HeadcountJobs, sources.HeadcountDrivers, sources.HeadcountSnippet}; !reflect.DeepEqual(got, []string{"employees", "participants", "jobs", "drivers", "snippet"}) {
		t.Fatal(got)
	}
}

// R-IJ0W-7ON6 R-295U-IVI9
func TestNameKeys(t *testing.T) {
	for _, names := range [][]string{nil, {""}, {"Brown Hawk LLC", "Northern Lights Steel Fab", "BROWN HAWK, L.L.C.", "...", "Northern Lights Steel Fabrication"}} {
		before := append([]string{}, names...)
		got := sources.NameKeys(names)
		want := []string{}
		if len(names) > 1 {
			want = []string{"BROWN HAWK", "NORTHERN LIGHTS STEEL FAB", "NORTHERN LIGHTS STEEL FABRICATION"}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got)
		}
		if len(got) > 0 {
			got[0] = "changed"
		}
		if !reflect.DeepEqual(before, append([]string{}, names...)) {
			t.Fatal(names)
		}
	}
}

// R-IK8S-LGDV R-295U-IVI9
func TestEINKey(t *testing.T) {
	for _, s := range []string{"411234567", "000000001", "000000000", "41123456", "4112345678", "41-1234567", " 411234567", "411234567 ", "41123456A", "４１１２３４５６７", ""} {
		want := ""
		if s == "411234567" || s == "000000001" {
			want = s
		}
		if got := sources.EINKey(s); got != want {
			t.Fatal(s, got)
		}
	}
}
