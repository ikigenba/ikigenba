package tools

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-AWKK-CGKD R-AXSG-Q8B2
func TestJudgmentProjectionFullPrecision(t *testing.T) {
	op, rc, low, high := .776123456789, .456789123, int64(2400000), int64(3840000)
	power, drivers, minutes := int64(7), int64(9), int64(123)
	a := store.Assessment{GovernmentMatch: "chosen-government", Minutes: &minutes, FleetVerdict: sources.FleetPass, PowerUnits: &power, Drivers: &drivers, Excluded: true, Operating: &op, Evidence: []sources.Evidence{{Name: "signal", Source: "source", Weight: .6123456789, Description: "chosen-description"}}, RevenueLow: &low, RevenueHigh: &high, RevenueConfidence: &rc, KnownSince: time.Date(2018, 4, 9, 0, 0, 0, 0, time.UTC), NewestFiling: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Categories: []sources.Category{{Name: sources.CategoryTrucking, Source: "fmcsa"}, {Name: sources.CategoryConstruction, Source: "ppp"}, {Name: sources.CategoryTrucking, Source: "ppp"}}}
	got := assessmentOf(a)
	want := Assessment{GovernmentMatch: "chosen-government", Minutes: &minutes, FleetVerdict: sources.FleetPass, PowerUnits: &power, Drivers: &drivers, Excluded: true, OperatingConfidence: &op, OperatingEvidence: []Evidence{{"signal", "source", .6123456789, "chosen-description"}}, RevenueLow: &low, RevenueHigh: &high, RevenueConfidence: &rc, KnownSince: "2018-04-09", NewestFiling: "2020-01-01", Categories: []Category{{sources.CategoryTrucking, "fmcsa"}, {sources.CategoryConstruction, "ppp"}, {sources.CategoryTrucking, "ppp"}}, Calculated: stamp(a.Calculated)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if linkOf(store.Link{}).FleetVerdict != sources.FleetUnknown {
		t.Fatal("missing legacy fleet verdict")
	}
	entry := memberOf(store.Ranked{Assessment: a})
	wantMember := Member{Candidate: candidateOf(store.Candidate{}), OperatingConfidence: &op, RevenueLow: &low, RevenueHigh: &high, RevenueConfidence: &rc, Categories: []string{sources.CategoryConstruction, sources.CategoryTrucking}, FleetVerdict: sources.FleetPass, Minutes: &minutes, GovernmentMatch: "chosen-government", KnownSince: "2018-04-09", NewestFiling: "2020-01-01", Excluded: true}
	if !reflect.DeepEqual(entry, wantMember) {
		t.Fatalf("member got %+v want %+v", entry, wantMember)
	}

	if entry.KnownSince != want.KnownSince || entry.NewestFiling != want.NewestFiling || !entry.Excluded || entry.FleetVerdict != sources.FleetPass {
		t.Fatal(entry)
	}
	if !reflect.DeepEqual(entry.Categories, []string{sources.CategoryConstruction, sources.CategoryTrucking}) || *entry.OperatingConfidence != op || *entry.RevenueConfidence != rc {
		t.Fatal(entry)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Assessment
	if err = json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("precision %s", b)
	}
}

// R-RK27-M08T R-VO3U-KELY
func TestWebsiteProjections(t *testing.T) {
	for _, state := range []string{sources.WebsiteUnknown, sources.WebsiteHTTPS} {
		url := ""
		if state == sources.WebsiteHTTPS {
			url = "https://chosen.test"
		}
		a := store.Assessment{Website: state, WebsiteURL: url}
		if got := assessmentOf(a); got.Website != state || got.WebsiteURL != url {
			t.Fatal(got)
		}
		if got := memberOf(store.Ranked{Assessment: a}); got.Website != state || got.WebsiteURL != url {
			t.Fatal(got)
		}
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	e := store.Enrichment{Kind: sources.WebsiteCheck, Inputs: "fixture-inputs", Found: encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: "https://chosen.test", Title: "chosen", StatusCode: 200, Size: 33, Error: "chosen-error"}), Source: "chosen.test", Cost: .25, At: at}
	var wr website.Result
	_ = json.Unmarshal(e.Found, &wr)
	wantE := Enrichment{Kind: e.Kind, Source: e.Source, Inputs: e.Inputs, State: wr.State, URL: wr.URL, Title: wr.Title, StatusCode: wr.StatusCode, Size: &wr.Size, Error: wr.Error, Domain: e.Source, Cost: e.Cost, At: stamp(at)}
	if got := enrichmentOf(e); !reflect.DeepEqual(got, wantE) {
		t.Fatal(got, wantE)
	}

	b, err := json.Marshal(enrichmentOf(store.Enrichment{Kind: sources.WebsiteCheck, Inputs: "fixture-inputs", Found: encodedJSON(website.Result{State: sources.WebsiteNone})}))
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"url", "title", "status_code"} {
		if _, ok := obj[key]; ok {
			t.Fatal(string(b))
		}
	}
}

// R-VWN5-8SST
func TestWebsiteFilterPresence(t *testing.T) {
	for _, raw := range []string{`{}`, `{"website":null}`, `{"website":[]}`, `{"website":["http","https"],"website_unknown":true}`} {
		var a FilterArgs
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			t.Fatal(err)
		}
		got := filterOf(a)
		if a.Website == nil {
			if got.Website != nil {
				t.Fatal(got)
			}
		} else {
			if got.Website == nil || !reflect.DeepEqual(got.Website, *a.Website) || got.WebsiteUnknown != a.WebsiteUnknown {
				t.Fatal(got, a)
			}
			if len(*a.Website) == 0 && store.ValidCohortFilters(got) {
				t.Fatal("empty website filter accepted")
			}
		}
	}
}
