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

// R-7AXF-IAYJ R-Q65V-BAP3 R-RIXU-M4CV
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
