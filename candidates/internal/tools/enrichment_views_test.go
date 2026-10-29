package tools_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-FIUA-FRAB R-Q65V-BAP3 R-FBIW-54U5
func TestEnrichmentsAreVisibleBeforeAndApartFromJudgments(t *testing.T) {
	f := newFixture(t)
	_, ids, err := f.st.Attach(t.Context(), "new-run", false, []store.Observation{{Scraper: "fmcsa", SourceID: "new-row", Digest: "new-digest", Reading: sources.Reading{Name: "unjudged-business", Names: []string{"unjudged-business"}, NameKey: "unjudged"}}})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	read := func() tools.Detail {
		t.Helper()
		r := f.call("candidate", tools.CandidateArgs{Candidate: ids[0]})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		return decodeList[tools.Detail](t, r)
	}
	initial := read()
	if initial.Enrichments == nil || initial.Enrichments.Facts != nil || initial.Enrichments.Brief != nil || initial.Judgments != nil {
		t.Fatal(initial)
	}
	good := enrichers.BriefFound{Run: "chosen-run", Status: "exited", ExitCode: new(0), Summary: "chosen-summary", Categories: []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "chosen-why"}, {Name: sources.CategoryAgriculture, Why: "other-why"}}, Labels: []string{"chosen-label", "other-label"}}
	add := func(found enrichers.BriefFound) {
		t.Helper()
		f.now = f.now.Add(time.Hour)
		_, e := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindBrief, Inputs: "chosen-inputs", Source: enrichers.SourcePrompts, Found: encodedJSON(found)}})
		if e != nil {
			t.Fatal(e)
		}
	}
	add(enrichers.BriefFound{Run: "first-failed-run", Status: "failed", Categories: []enrichers.BriefCategory{}, Labels: []string{}})
	if failed := read(); failed.Enrichments.Brief != nil || failed.Judgments != nil {
		t.Fatal(failed)
	}
	add(good)
	before := read()
	wantBrief := &tools.Brief{Summary: good.Summary, Categories: []tools.BriefCategory{{Name: good.Categories[0].Name, Why: good.Categories[0].Why}, {Name: good.Categories[1].Name, Why: good.Categories[1].Why}}, Labels: good.Labels, At: stamp(f.now)}
	if before.Judgments != nil || before.Enrichments.Facts == nil || !reflect.DeepEqual(before.Enrichments.Brief, wantBrief) || before.Enrichments.Facts.ClassifiedBy != enrichers.KindBrief || len(before.Enrichments.Facts.Written) != 1 {
		t.Fatal(before)
	}
	if err = walkJudgments(t.Context(), f.st, f.now); err != nil {
		t.Fatal(err)
	}
	judged := read()
	if judged.Judgments == nil {
		t.Fatal(judged)
	}
	add(enrichers.BriefFound{Run: "failed-run", Status: "failed", Categories: []enrichers.BriefCategory{}, Labels: []string{}})
	after := read()
	if !reflect.DeepEqual(after.Judgments, judged.Judgments) || !reflect.DeepEqual(after.Enrichments.Brief, before.Enrichments.Brief) || len(after.EnrichmentResults) != 3 || after.Enrichments.Facts.Written[0].At != stamp(f.now) {
		t.Fatal(after, judged)
	}
	good.Summary = "newest-good-summary"
	add(good)
	wantBrief.Summary, wantBrief.At = good.Summary, stamp(f.now)
	if newest := read(); !reflect.DeepEqual(newest.Enrichments.Brief, wantBrief) || !reflect.DeepEqual(newest.Judgments, judged.Judgments) {
		t.Fatal(newest)
	}
}

// R-AQ2S-CL5A R-ARAO-QCVZ R-FK26-TJ10 R-HQ3R-AUHG
func TestDriveRouteAnswersAndLiveMemberProjection(t *testing.T) {
	f := fixtureWithRoutes(t, nil, nil, nil, nil, "Chosen origin")
	id := f.seed(1)[0]
	list := f.create("Drive fixture")
	member := func() tools.Member {
		t.Helper()
		r := f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: list.ID, Peek: true})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		v := decodeList[tools.ProspectListRead](t, r)
		if len(v.Members) != 1 {
			t.Fatal(v)
		}
		return v.Members[0]
	}
	if m := member(); m.Minutes != nil || m.MinutesSource != "" {
		t.Fatal(m)
	}
	add := func(kind, source, inputs string, found any) {
		t.Helper()
		f.now = f.now.Add(time.Second)
		if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: kind, Source: source, Inputs: inputs, Found: encodedJSON(found), Cost: .25}}); err != nil {
			t.Fatal(err)
		}
	}
	z, m, d := int64(43), int64(17), 12.5
	add(enrichers.KindDriveMinutes, enrichers.SourceZipDrive, "zip-fixture", enrichers.MinutesFound{Minutes: &z})
	if got := member(); got.Minutes == nil || *got.Minutes != z || got.MinutesSource != store.MinutesByZip {
		t.Fatal(got)
	}
	subject, err := f.st.Subject(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	inputs := (enrichers.DriveRoute{}).Inputs(subject)
	if inputs == "" {
		t.Fatal("empty route inputs")
	}
	for _, found := range []enrichers.RouteFound{
		{Why: "No fixture route"},
		{Minutes: &m, Miles: &d, Address: "Resolved fixture", Precision: routes.PrecisionStreet},
		{Error: "Fixture request failed"},
	} {
		add(enrichers.KindDriveRoute, enrichers.SourceGoogleRoutes, inputs, found)
		r := f.call("candidate", tools.CandidateArgs{Candidate: id})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		var raw struct {
			Enrichments struct {
				Facts map[string]json.RawMessage `json:"facts"`
			} `json:"enrichments"`
			EnrichmentResults []json.RawMessage `json:"enrichment_results"`
		}
		if err := json.Unmarshal(decode(t, r).Structured, &raw); err != nil {
			t.Fatal(err)
		}
		if found.Minutes != nil {
			if string(raw.Enrichments.Facts["minutes_source"]) != string(encodedJSON(store.MinutesByRoute)) || string(raw.Enrichments.Facts["miles"]) != string(encodedJSON(d)) || string(raw.Enrichments.Facts["route_address"]) != string(encodedJSON(found.Address)) {
				t.Fatal(raw.Enrichments.Facts)
			}
		} else if !found.Failed() {
			if string(raw.Enrichments.Facts["minutes_source"]) != string(encodedJSON(store.MinutesByZip)) || raw.Enrichments.Facts["miles"] != nil || raw.Enrichments.Facts["route_address"] != nil {
				t.Fatal(raw.Enrichments.Facts)
			}
		}
		detail := decodeList[tools.Detail](t, r)
		got := detail.Enrichments.Facts
		if got == nil {
			t.Fatal(detail)
		}
		if found.Minutes != nil {
			if got.Minutes == nil || *got.Minutes != m || got.MinutesSource != store.MinutesByRoute || got.Miles == nil || *got.Miles != d || got.RouteAddress != found.Address {
				t.Fatal(got)
			}
			if v := member(); v.Minutes == nil || *v.Minutes != m || v.MinutesSource != store.MinutesByRoute {
				t.Fatal(v)
			}
		} else if !found.Failed() {
			if got.Minutes == nil || *got.Minutes != z || got.MinutesSource != store.MinutesByZip || got.Miles != nil || got.RouteAddress != "" {
				t.Fatal(got)
			}
		}
		written := false
		for _, w := range got.Written {
			if w.Kind == enrichers.KindDriveRoute {
				written = true
			}
		}
		if !written {
			t.Fatal(got)
		}
		results := detail.EnrichmentResults
		if len(results) < 2 || results[0].Kind != enrichers.KindDriveRoute || results[len(results)-1].Kind != enrichers.KindDriveMinutes {
			t.Fatal(results)
		}
		first := results[0]
		wantMembers := []string{"kind", "source", "inputs", "found", "cost", "at"}
		if found.Minutes != nil {
			wantMembers = []string{"kind", "source", "inputs", "found", "place_url", "directions_url", "cost", "at"}
		}
		if len(raw.EnrichmentResults) != len(results) || !reflect.DeepEqual(objectKeys(t, raw.EnrichmentResults[0]), wantMembers) || first.Source != enrichers.SourceGoogleRoutes || first.Inputs != inputs || first.Cost != .25 || first.At != stamp(f.now) || !reflect.DeepEqual(first.Found, encodedJSON(found)) {
			t.Fatal(string(encodedJSON(first)))
		}
	}
}

// R-FIUA-FRAB
func TestBriefArrivesAfterJudgments(t *testing.T) {
	f := newFixture(t)
	id := f.seed(1)[0]
	if _, _, err := f.st.Derive(t.Context(), enrichers.KindLinkFacts, "", false, 100); err != nil {
		t.Fatal(err)
	}
	read := func() tools.Detail {
		return decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	}
	initial := read()
	if initial.Judgments == nil || initial.Enrichments.Brief != nil || initial.Enrichments.Facts.RecordsDiffer || initial.Enrichments.Facts.RecordCategories == nil || len(initial.Enrichments.Facts.RecordCategories) != 0 {
		t.Fatal(initial)
	}
	good := enrichers.BriefFound{Run: "later-brief", Status: enrichers.RunExited, ExitCode: new(0), Summary: "later-summary", Categories: []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "later-why"}}, Labels: []string{"later-label"}}
	f.now = f.now.Add(time.Second)
	at := f.now
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: enrichers.KindBrief, Source: enrichers.SourcePrompts, Inputs: "later-input", Found: encodedJSON(good)}}); err != nil {
		t.Fatal(err)
	}
	want := &tools.Brief{Summary: good.Summary, Categories: []tools.BriefCategory{{Name: sources.CategoryRetail, Why: "later-why"}}, Labels: good.Labels, At: stamp(at)}
	if got := read(); !reflect.DeepEqual(got.Judgments, initial.Judgments) || !reflect.DeepEqual(got.Enrichments.Brief, want) {
		t.Fatal(got)
	}
	f.now = f.now.Add(time.Second)
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: enrichers.KindBrief, Source: enrichers.SourcePrompts, Inputs: "later-input", Found: encodedJSON(enrichers.BriefFound{Run: "failed-later", Status: "failed", Categories: []enrichers.BriefCategory{}, Labels: []string{}})}}); err != nil {
		t.Fatal(err)
	}
	if got := read(); !reflect.DeepEqual(got.Judgments, initial.Judgments) || !reflect.DeepEqual(got.Enrichments.Brief, want) {
		t.Fatal(got)
	}
}
