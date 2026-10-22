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

// R-THWD-6HC5 R-Q65V-BAP3 R-RIXU-M4CV
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
	good := enrichers.BriefFound{Run: "chosen-run", Status: "exited", ExitCode: new(0), Summary: "chosen-summary", Categories: []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "chosen-why"}}, Labels: []string{"chosen-label"}}
	add := func(found enrichers.BriefFound) {
		t.Helper()
		f.now = f.now.Add(time.Hour)
		_, e := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindBrief, Inputs: "chosen-inputs", Source: enrichers.SourcePrompts, Found: encodedJSON(found)}})
		if e != nil {
			t.Fatal(e)
		}
	}
	add(good)
	before := read()
	if before.Judgments != nil || before.Enrichments.Facts == nil || before.Enrichments.Brief == nil || before.Enrichments.Brief.Summary != good.Summary || before.Enrichments.Facts.ClassifiedBy != enrichers.KindBrief || len(before.Enrichments.Facts.Written) != 1 {
		t.Fatal(before)
	}
	if _, err = f.st.JudgeAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	judged := read()
	if judged.Judgments == nil {
		t.Fatal(judged)
	}
	add(enrichers.BriefFound{Run: "failed-run", Status: "failed", Categories: []enrichers.BriefCategory{}, Labels: []string{}})
	after := read()
	if !reflect.DeepEqual(after.Judgments, judged.Judgments) || !reflect.DeepEqual(after.Enrichments.Brief, before.Enrichments.Brief) || len(after.EnrichmentResults) != 2 || after.Enrichments.Facts.Written[0].At != stamp(f.now) {
		t.Fatal(after, judged)
	}
}

// R-AQ2S-CL5A R-ARAO-QCVZ R-OKTB-0E58
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
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(encodedJSON(first), &obj); err != nil {
			t.Fatal(err)
		}
		wantMembers := 6
		if found.Minutes != nil {
			wantMembers = 8
		}
		if len(obj) != wantMembers || first.Source != enrichers.SourceGoogleRoutes || first.Inputs != inputs || first.Cost != .25 || !reflect.DeepEqual(first.Found, encodedJSON(found)) {
			t.Fatal(string(encodedJSON(first)))
		}
	}
}
