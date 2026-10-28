package tools_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-07GQ-W5OL R-VKSM-XZQL R-QOGD-1UTI
func TestBriefAnswers(t *testing.T) {
	f := newFixture(t)
	id := f.seed(1)[0]
	co := f.create("brief projection")
	before := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: co.ID, Peek: true}))
	if len(before.Members) != 1 || before.Members[0].Brief != nil {
		t.Fatal(before)
	}
	detail := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	if detail.Enrichments == nil || detail.Enrichments.Brief != nil {
		t.Fatal(detail)
	}
	good := enrichers.BriefFound{Run: "fixture-run", Status: "exited", ExitCode: new(0), Summary: "fixture-summary", Categories: []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "fixture-why"}, {Name: sources.CategoryOther, Why: "other-why"}}, Labels: []string{"fixture-label"}}
	for _, b := range []enrichers.BriefFound{good, {Run: "failed-run", Status: "exited", ExitCode: new(1), Categories: []enrichers.BriefCategory{}, Labels: []string{}}, {Run: "missing-code", Status: "failed", Categories: []enrichers.BriefCategory{}, Labels: []string{}}} {
		f.now = f.now.Add(time.Second)
		raw := encodedJSON(b)
		if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: enrichers.KindBrief, Source: enrichers.SourcePrompts, Inputs: "brief-input", Found: raw, Cost: .25}}); err != nil {
			t.Fatal(err)
		}
		after := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: co.ID, Peek: true}))
		if after.Matching != before.Matching || after.Members[0].Candidate.ID != id || after.Members[0].Brief == nil || *after.Members[0].Brief != stamp(f.now) || !reflect.DeepEqual(after.Members[0].SiteText, before.Members[0].SiteText) {
			t.Fatal(after)
		}
		detail = decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
		facts, err := f.st.GetFacts(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Enrichments == nil || detail.Enrichments.Brief == nil || detail.Enrichments.Brief.Summary != good.Summary || detail.Enrichments.Brief.Summary != facts.Brief {
			t.Fatal(detail.Judgments, facts)
		}
		e := detail.EnrichmentResults[0]
		if e.Kind != enrichers.KindBrief || e.Source != enrichers.SourcePrompts || e.Inputs != "brief-input" || string(e.Found) != string(raw) || e.Cost != .25 || e.At != stamp(f.now) {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(objectKeys(t, encodedJSON(e)), []string{"kind", "source", "inputs", "found", "cost", "at"}) {
			t.Fatal(e)
		}
	}
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: enrichers.KindSiteText, Source: enrichers.SourcePrompts, Inputs: "fixture-inputs", Found: encodedJSON(enrichers.SiteTextFound{Run: "site-run", Status: "exited", ExitCode: new(0)})}}); err != nil {
		t.Fatal(err)
	}
	detail = decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	if len(detail.EnrichmentResults) != 4 || detail.EnrichmentResults[0].Kind != enrichers.KindSiteText || detail.EnrichmentResults[1].Kind != enrichers.KindBrief {
		t.Fatal(detail.Enrichments)
	}
	_, ids, err := f.st.Attach(t.Context(), "unjudged", false, []store.Observation{{Scraper: "fixture", SourceID: "unjudged", Digest: "unjudged", Reading: sources.Reading{Name: "unjudged-business", NameKey: "unjudged"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindBrief, Source: enrichers.SourcePrompts, Inputs: "fixture-inputs", Found: encodedJSON(good)}}); err != nil {
		t.Fatal(err)
	}
	detail = decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}))
	if detail.Judgments != nil {
		t.Fatal(detail)
	}
}
