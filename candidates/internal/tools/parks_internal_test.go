package tools

import (
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-EHOU-JBBF
func TestParkAnswerOptionalFields(t *testing.T) {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.FixedZone("fixture", 3600))
	minutes, value := int64(91), int64(-2700)
	p := store.Park{ID: "chosen-id", Name: "chosen-name", County: "chosen-county", Town: "chosen-town", Zip: "chosen-zip", State: parks.StateGone, Lat: 45.6, Lon: -93.1, Radius: 555.5, Parcels: 7, BuildingValue: value, Minutes: &minutes, GrownFrom: 4, Walked: at, WalkRun: "chosen-walk", Found: 13, Run: "chosen-run", Created: at.Add(-time.Hour), Updated: at.Add(time.Hour)}
	want := Park{"chosen-id", "chosen-name", "chosen-county", "chosen-town", "chosen-zip", parks.StateGone, 45.6, -93.1, 555.5, 7, value, &minutes, 4, "2026-03-04T04:06:07Z", "chosen-walk", 13, "chosen-run", parks.MapURL(45.6, -93.1), "2026-03-04T03:06:07Z", "2026-03-04T05:06:07Z"}
	if got := parkOf(p); !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	p.Minutes, p.Walked = nil, time.Time{}
	want.Minutes, want.Walked = nil, ""
	if got := parkOf(p); !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	q := store.ParkParcel{PIN: "chosen-pin", Street: "chosen-street", City: "chosen-city", Zip: "chosen-zip", Lat: 46.2, Lon: -92.8, Rule: parks.RuleLabel}
	wantParcel := ParkParcel{"chosen-pin", []string{}, "chosen-street", "chosen-city", "chosen-zip", nil, parks.RuleLabel, 46.2, -92.8}
	if got := parkParcelOf(q); !reflect.DeepEqual(got, wantParcel) {
		t.Fatal(got, wantParcel)
	}
	q.Owners, q.BuildingValue = []string{"second-owner", "first-owner"}, &value
	wantParcel.Owners, wantParcel.BuildingValue = []string{"second-owner", "first-owner"}, &value
	if got := parkParcelOf(q); !reflect.DeepEqual(got, wantParcel) {
		t.Fatal(got, wantParcel)
	}
	r := parks.Rules{Distance: 321, MinParcels: 5, ValueThreshold: 987, Labels: map[string][]string{"County B": {"second", "first"}, "County A": {"only"}}}
	wantRules := ParkRules{321, 5, 987, []CountyLabels{{"County B", []string{"second", "first"}}, {"County A", []string{"only"}}, {"County Empty", []string{}}}}
	if got := parkRulesOf(r, []string{"County B", "County A", "County Empty"}); !reflect.DeepEqual(got, wantRules) {
		t.Fatal(got, wantRules)
	}
}
