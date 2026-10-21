package tools

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-ANMZ-L1NW R-Q65V-BAP3 R-RE29-31E3 R-RGI1-UKVH R-RIXU-M4CV R-7AXF-IAYJ
func TestFactsAndBriefAnswers(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC)
	minutes, power, drivers := int64(31), int64(4), int64(0)
	f := store.Facts{KnownSince: at, NewestFiling: at, Categories: []sources.Category{{Name: sources.CategoryRetail, Source: enrichers.KindBrief}}, Records: []sources.Category{{Name: sources.CategoryTrucking, Source: "fmcsa"}}, GovernmentMatch: "match", FleetVerdict: sources.FleetPass, PowerUnits: &power, Drivers: &drivers, Website: sources.WebsiteHTTPS, WebsiteURL: "https://fixture.test", WebsiteSource: store.WebsiteBySearch, Minutes: &minutes, Written: []store.Written{{Kind: enrichers.KindWebsiteSearch, At: at}}}
	want := Facts{"2026-10-05", "2026-10-05", []Category{{sources.CategoryRetail, enrichers.KindBrief}}, enrichers.KindBrief, true, []Category{{sources.CategoryTrucking, "fmcsa"}}, "match", sources.FleetPass, &power, &drivers, sources.WebsiteHTTPS, "https://fixture.test", store.WebsiteBySearch, &minutes, "", nil, "", []Written{{enrichers.KindWebsiteSearch, stamp(at)}}}
	if got := factsOf(f); !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	for _, tc := range []struct {
		value any
		want  string
	}{
		{want, `{"known_since":"2026-10-05","newest_filing":"2026-10-05","categories":[{"name":"retail","source":"brief"}],"classified_by":"brief","records_differ":true,"record_categories":[{"name":"` + sources.CategoryTrucking + `","source":"fmcsa"}],"government_match":"match","fleet_verdict":"pass","power_units":4,"drivers":0,"website":"https","website_url":"https://fixture.test","website_source":"search","minutes":31,"minutes_source":"","written":[{"kind":"website_search","at":"2026-10-05T09:00:07Z"}]}`},
		{Facts{}, `{"categories":[],"classified_by":"","records_differ":false,"record_categories":[],"fleet_verdict":"","website":"","website_source":"","minutes_source":"","written":[]}`},
		{Brief{}, `{"summary":"","categories":[],"labels":[],"at":""}`},
		{Enrichments{}, `{}`},
		{Enrichments{Facts: &Facts{}, Brief: &Brief{}}, `{"facts":{"categories":[],"classified_by":"","records_differ":false,"record_categories":[],"fleet_verdict":"","website":"","website_source":"","minutes_source":"","written":[]},"brief":{"summary":"","categories":[],"labels":[],"at":""}}`},
		{Brief{"chosen-summary", []BriefCategory{{"chosen-category", "chosen-why"}}, []string{"chosen-label"}, stamp(at)}, `{"summary":"chosen-summary","categories":[{"name":"chosen-category","why":"chosen-why"}],"labels":["chosen-label"],"at":"2026-10-05T09:00:07Z"}`},
	} {
		b, err := json.Marshal(tc.value)
		if err != nil || string(b) != tc.want {
			t.Fatal(string(b), tc.want, err)
		}
	}
	good := enrichers.BriefFound{Run: "good", Status: "exited", ExitCode: new(0), Summary: "chosen-summary", Categories: []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "chosen-why"}}, Labels: []string{"chosen-label"}}
	failed := enrichers.BriefFound{Run: "bad", Status: "failed"}
	results := []store.Enrichment{{Kind: enrichers.KindBrief, Found: encodedJSON(failed), At: at.Add(time.Hour)}, {Kind: enrichers.KindBrief, Found: encodedJSON(good), At: at}}
	expected := Brief{good.Summary, []BriefCategory{{sources.CategoryRetail, "chosen-why"}}, good.Labels, stamp(at)}
	if got := briefOf(results); got == nil || !reflect.DeepEqual(*got, expected) {
		t.Fatal(got, expected)
	}
	if briefOf(results[:1]) != nil || briefOf(nil) != nil {
		t.Fatal("unexpected brief")
	}
}

// R-ANMZ-L1NW R-AQ2S-CL5A
func TestDriveFactsEncodingAndProjection(t *testing.T) {
	minutes, miles := int64(17), 12.5
	for _, f := range []store.Facts{
		{Minutes: &minutes, MinutesSource: store.MinutesByRoute, Miles: &miles, RouteAddress: "Fixture address"},
		{Minutes: &minutes, MinutesSource: store.MinutesByZip},
		{},
	} {
		got := factsOf(f)
		if got.MinutesSource != f.MinutesSource || got.Miles != f.Miles || got.RouteAddress != f.RouteAddress {
			t.Fatal(got, f)
		}
	}
	got, err := json.Marshal(Facts{Minutes: &minutes, MinutesSource: store.MinutesByRoute, Miles: &miles, RouteAddress: "Fixture address"})
	want := `{"categories":[],"classified_by":"","records_differ":false,"record_categories":[],"fleet_verdict":"","website":"","website_source":"","minutes":17,"minutes_source":"route","miles":12.5,"route_address":"Fixture address","written":[]}`
	if err != nil || string(got) != want {
		t.Fatal(string(got), err)
	}
}
