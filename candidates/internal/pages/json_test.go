package pages

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func encodedJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// R-BGGM-ZNRG R-0A1O-OXF5
func TestFactsProjection(t *testing.T) {
	at := fixtureTime()
	power, minutes := int64(12), int64(84)
	f := store.Facts{KnownSince: time.Date(2018, 4, 9, 0, 0, 0, 0, time.UTC), NewestFiling: at, Categories: []sources.Category{{Name: sources.CategoryTrucking, Source: "fmcsa"}, {Name: sources.CategoryTrucking, Source: "ppp"}}, GovernmentMatch: "fixture-government", FleetVerdict: sources.FleetPass, PowerUnits: &power, Minutes: &minutes, Written: []store.Written{{Kind: enrichers.KindDriveMinutes, At: at}, {Kind: sources.WebsiteCheck, At: at}}}
	want := FactsView{"2018-04-09", "2018-04-09", "2026-10-05", "2026-10-05", []CategoryRow{{sources.CategoryTrucking, "fmcsa"}, {sources.CategoryTrucking, "ppp"}}, enrichers.SourceLinks, false, []CategoryRow{}, "fixture-government", sources.FleetPass, "12", "", "84", []WrittenRow{{enrichers.KindDriveMinutes, "2026-10-05T09:00:07Z", "2026-10-05 09:00:07 UTC"}, {sources.WebsiteCheck, "2026-10-05T09:00:07Z", "2026-10-05 09:00:07 UTC"}}}
	if got := factsView(f); !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	if got := factsView(store.Facts{}); !reflect.DeepEqual(got, FactsView{Categories: []CategoryRow{}, RecordCategories: []CategoryRow{}, Written: []WrittenRow{}}) {
		t.Fatal(got)
	}
}

// R-DUEN-N7F8 R-E2XY-BLM3 R-E1Q1-XTVE
func TestDerivedProjection(t *testing.T) {
	for _, tc := range []struct {
		kind, source, raw string
		want              []FactRow
	}{
		{enrichers.KindDriveMinutes, enrichers.SourceZipDrive, `{ "minutes": 84 }`, []FactRow{{"minutes", "84"}}},
		{enrichers.KindDriveMinutes, enrichers.SourceZipDrive, `{}`, []FactRow{}},
		{enrichers.KindLinkFacts, enrichers.SourceLinks, `{"fleet_verdict":"pass", "categories": []}`, []FactRow{{"categories", "[]"}, {"fleet_verdict", `"pass"`}}},
	} {
		e := store.Enrichment{Kind: tc.kind, Source: tc.source, Found: json.RawMessage(tc.raw)}
		got := enrichmentRow(e)
		want := EnrichmentRow{Kind: tc.kind, Source: tc.source, Found: tc.want}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
		var c CandidateView
		setWebsite(&c, []store.Enrichment{e})
		if c.Website != sources.WebsiteUnknown || c.WebsiteURL != "" || c.WebsiteTitle != "" || c.WebsiteChecked != "" || c.WebsiteCheckedText != "" {
			t.Fatal(c)
		}
	}
}

// R-E6LN-GWU6
func TestCandidateFactsWithoutJudgment(t *testing.T) {
	cfg, _ := pageFixture(t)
	_, ids, err := cfg.Store.Attach(t.Context(), "run", false, []store.Observation{{Scraper: "fixture", SourceID: "row", Digest: "digest", Reading: sources.Reading{Name: "facts-business", NameKey: "facts-business"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = cfg.Store.Derive(t.Context(), enrichers.KindLinkFacts, "", false, 100); err != nil {
		t.Fatal(err)
	}
	got := capturedCandidate(t, cfg, ids[0])
	facts, err := cfg.Store.GetFacts(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	want := factsView(facts)
	if got.Assessment != nil || got.Facts == nil || !reflect.DeepEqual(*got.Facts, want) || len(got.Facts.Written) != 1 || got.Facts.Written[0].Kind != enrichers.KindLinkFacts {
		t.Fatal(got)
	}
	results, err := cfg.Store.Enrichments(t.Context(), ids[0])
	if err != nil || len(results) != 1 || results[0].Kind != enrichers.KindLinkFacts {
		t.Fatal(results, err)
	}
	if expected := []EnrichmentRow{enrichmentRow(results[0])}; !reflect.DeepEqual(got.Enrichments, expected) || len(got.Enrichments[0].Found) == 0 {
		t.Fatal(got.Enrichments, expected)
	}
	if _, err := cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: sources.WebsiteCheck, Inputs: "fixture-inputs", Source: "fixture.test", Found: encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: "https://fixture.test/"})}}); err != nil {
		t.Fatal(err)
	}
	results, err = cfg.Store.Enrichments(t.Context(), ids[0])
	if err != nil || len(results) != 2 {
		t.Fatal(results, err)
	}
	got = capturedCandidate(t, cfg, ids[0])
	expected := []EnrichmentRow{enrichmentRow(results[0]), enrichmentRow(results[1])}
	if !reflect.DeepEqual(got.Enrichments, expected) || got.Enrichments[0].Kind != sources.WebsiteCheck || got.Enrichments[1].Kind != enrichers.KindLinkFacts || len(got.Enrichments[1].Found) == 0 {
		t.Fatal(got.Enrichments, expected)
	}
}

// R-3TJU-KG5A R-DT6R-9FOJ R-BGGM-ZNRG R-02QA-EAYZ
func TestIndependentFactsAndAssessmentTemplateStates(t *testing.T) {
	t.Chdir(t.TempDir())
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, assessment := range []*AssessmentView{nil, {OperatingEvidence: []EvidenceRow{}, Excluded: true}} {
		for _, facts := range []*FactsView{nil, {Categories: []CategoryRow{}, Written: []WrittenRow{}}} {
			for _, text := range []string{"", "fixture-value"} {
				for _, fleet := range []string{sources.FleetPass, sources.FleetFail, sources.FleetUnknown} {
					for _, n := range []int{0, 1, 3} {
						var fv *FactsView
						if facts != nil {
							v := *facts
							v.KnownSince, v.KnownSinceText, v.NewestFiling, v.NewestFilingText = text, text, text, text
							v.GovernmentMatch, v.FleetVerdict, v.PowerUnits, v.Drivers, v.Minutes = text, fleet, text, text, text
							if text != "" {
								v.Categories = []CategoryRow{{"fixture-category", "fixture-source"}}
							}
							for i := 0; i < n; i++ {
								v.Written = append(v.Written, WrittenRow{[]string{enrichers.KindDriveMinutes, enrichers.KindLinkFacts, sources.WebsiteCheck}[i], text, text})
							}
							fv = &v
						}
						d := CandidateData{Facts: fv, Assessment: assessment}
						for _, kind := range []string{enrichers.KindDriveMinutes, enrichers.KindLinkFacts} {
							for _, found := range [][]FactRow{{}, {{"fixture-key", "fixture-value"}}, {{"first", "1"}, {"second", "2"}}} {
								d.Enrichments = []EnrichmentRow{{Kind: kind, Source: "fixture-source", Found: found}}
								if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, "candidate", d); err != nil {
									t.Fatal(err)
								}
							}
						}
					}
				}
			}
		}
	}
}
