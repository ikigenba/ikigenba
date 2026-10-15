package tools

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-0GSM-WO8U R-0I0J-AFZJ
func TestJudgmentProjectionFullPrecision(t *testing.T) {
	op, rc, low, high := .776123456789, .456789123, int64(2400000), int64(3840000)
	a := store.Assessment{Operating: &op, Evidence: []sources.Evidence{{Name: "signal", Source: "source", Weight: .6123456789, Description: "chosen-description"}}, RevenueLow: &low, RevenueHigh: &high, RevenueConfidence: &rc, KnownSince: time.Date(2018, 4, 9, 0, 0, 0, 0, time.UTC), NewestFiling: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Categories: []sources.Category{{Name: sources.CategoryTrucking, Source: "fmcsa"}, {Name: sources.CategoryConstruction, Source: "ppp"}, {Name: sources.CategoryTrucking, Source: "ppp"}}}
	got := assessmentOf(a)
	want := Assessment{OperatingConfidence: &op, OperatingEvidence: []Evidence{{"signal", "source", .6123456789, "chosen-description"}}, RevenueLow: &low, RevenueHigh: &high, RevenueConfidence: &rc, KnownSince: "2018-04-09", NewestFiling: "2020-01-01", Categories: []Category{{sources.CategoryTrucking, "fmcsa"}, {sources.CategoryConstruction, "ppp"}, {sources.CategoryTrucking, "ppp"}}, Calculated: stamp(a.Calculated)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	entry := shortlistOf(store.Ranked{Assessment: a})
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
