package tools

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
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
