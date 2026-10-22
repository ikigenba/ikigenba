package tools_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func putBasisResult(t *testing.T, f *fixture, id, kind string, found any) {
	t.Helper()
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: kind, Inputs: "basis-input", Found: encodedJSON(found)}}); err != nil {
		t.Fatalf("%s: %v", kind, err)
	}
}
func failedBrief() enrichers.BriefFound {
	return enrichers.BriefFound{Run: "fixture-run", Status: "failed", Categories: []enrichers.BriefCategory{}, Labels: []string{}}
}
func goodBrief() enrichers.BriefFound {
	code := 0
	return enrichers.BriefFound{Run: "fixture-run", Categories: []enrichers.BriefCategory{}, Labels: []string{}, Status: enrichers.RunExited, ExitCode: &code, Summary: "fixture summary"}
}
func makeEnriched(t *testing.T, f *fixture, id string) {
	t.Helper()
	putBasisResult(t, f, id, enrichers.KindBrief, goodBrief())
	minutes := int64(17)
	putBasisResult(t, f, id, enrichers.KindDriveRoute, enrichers.RouteFound{Minutes: &minutes})
}
func peekBasis(t *testing.T, f *fixture, id string) tools.ProspectListRead {
	t.Helper()
	r := f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: id, Peek: true})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	return decodeList[tools.ProspectListRead](t, r)
}

// R-T39K-L8FT R-T4HG-Z06I R-T5PD-CRX7
func TestBasisAnswersFollowNewestResultsWithoutJudging(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(3)
	pl := f.create("basis fixture")
	initial := peekBasis(t, f, pl.ID)
	for _, kind := range []string{sources.WebsiteCheck, enrichers.KindLinkFacts, enrichers.KindDriveMinutes} {
		var found any
		switch kind {
		case sources.WebsiteCheck:
			found = website.Result{State: sources.WebsiteNone}
		case enrichers.KindLinkFacts:
			found = enrichers.LinkFound{FleetVerdict: sources.FleetUnknown, Categories: []enrichers.Category{}}
		default:
			found = enrichers.MinutesFound{}
		}
		putBasisResult(t, f, ids[0], kind, found)
	}
	for _, m := range peekBasis(t, f, pl.ID).Members {
		if m.Basis != store.BasisRecords {
			t.Fatal(m)
		}
	}
	for _, step := range []struct {
		kind  string
		found any
		basis string
	}{
		{enrichers.KindBrief, failedBrief(), store.BasisPartial},
		{enrichers.KindBrief, goodBrief(), store.BasisPartial},
		{enrichers.KindDriveRoute, enrichers.RouteFound{Minutes: ptrInt(18)}, store.BasisEnriched},
	} {
		putBasisResult(t, f, ids[0], step.kind, step.found)
		got := peekBasis(t, f, pl.ID)
		if got.Matching != initial.Matching {
			t.Fatal(got)
		}
		for _, m := range got.Members {
			if m.Candidate.ID == ids[0] && m.Basis != step.basis {
				t.Fatal(m)
			}
		}
	}
	putBasisResult(t, f, ids[1], enrichers.KindBrief, failedBrief())
	putBasisResult(t, f, ids[1], enrichers.KindDriveRoute, enrichers.RouteFound{})
	got := peekBasis(t, f, pl.ID)
	if got.ByBasis != (tools.BasisCounts{Records: 1, Partial: 1, Enriched: 1}) || got.Changes.Since != nil || len(got.Changes.Joined) != 0 || len(got.Changes.Left) != 0 || len(got.Changes.DefinitionChanged) != 0 {
		t.Fatal(got)
	}
	for _, m := range got.Members {
		d := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: m.Candidate.ID}))
		if d.Enrichments == nil || d.Enrichments.Basis != m.Basis {
			t.Fatal(d, m)
		}
		if m.Candidate.ID == ids[2] && (!reflect.DeepEqual(d.Enrichments.Kinds.Present, []string{}) || !reflect.DeepEqual(d.Enrichments.Kinds.Missing, []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText, enrichers.KindBrief, enrichers.KindDriveRoute})) {
			t.Fatal(d.Enrichments)
		}
		if m.Candidate.ID == ids[1] && (!reflect.DeepEqual(d.Enrichments.Kinds.Present, []string{enrichers.KindBrief, enrichers.KindDriveRoute}) || !reflect.DeepEqual(d.Enrichments.Kinds.Missing, []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText})) {
			t.Fatal(d.Enrichments)
		}
	}
	size := 2
	f.call("edit_prospect_list", tools.EditProspectListArgs{ProspectList: pl.ID, Size: &size})
	got = peekBasis(t, f, pl.ID)
	if got.ByBasis.Records+got.ByBasis.Partial+got.ByBasis.Enriched != 2 {
		t.Fatal(got)
	}
	// Array and null forms are part of the wire answer.
	raw, _ := json.Marshal(initial.Changes)
	if string(raw) != `{"since":null,"definition_changed":[],"joined":[],"left":[]}` {
		t.Fatal(string(raw))
	}
}

// R-T6X9-QJNW
func TestProspectListOpenAndPeekClock(t *testing.T) {
	f := newFixture(t)
	f.seed(5)
	for _, peek := range []bool{false, true} {
		refusal(t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: "pl_0000000000000000", Peek: peek}), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	pl := f.create("open fixture")
	size := 2
	f.call("edit_prospect_list", tools.EditProspectListArgs{ProspectList: pl.ID, Size: &size})
	before := f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: pl.ID, Peek: true})
	got := decodeList[tools.ProspectListRead](t, before)
	if got.Matching != 5 || len(got.Members) != 2 || got.Changes.Since != nil {
		t.Fatal(got)
	}
	success(t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: "coh_" + strings.TrimPrefix(pl.ID, "pl_"), Peek: true}), got)
	refusal(t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: "coh_0123456789ABCDEF", Peek: true}), passes.RefusalNotFound, tools.RefuseNotFound)
	t1 := stamp(f.now)
	success(t, f.call("prospect_list", map[string]any{"prospect_list": pl.ID}), got)
	f.now = f.now.Add(time.Minute)
	got = peekBasis(t, f, pl.ID)
	if got.Changes.Since == nil || *got.Changes.Since != t1 || len(got.Changes.Joined)+len(got.Changes.Left)+len(got.Changes.DefinitionChanged) != 0 {
		t.Fatal(got)
	}
	success(t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: pl.ID}), got)
	t2 := stamp(f.now)
	f.now = f.now.Add(time.Minute)
	if next := peekBasis(t, f, pl.ID); next.Changes.Since == nil || *next.Changes.Since != t2 {
		t.Fatal(next)
	}
	f.call("delete_prospect_list", tools.ProspectListArgs{ProspectList: pl.ID})
	for _, peek := range []bool{false, true} {
		refusal(t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: pl.ID, Peek: peek}), passes.RefusalNotFound, tools.RefuseNotFound)
	}
	f.d.SetFailing(true)
	refusal(t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: "bad"}), passes.RefusalNotFound, tools.RefuseNotFound)
	refusal(t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: pl.ID}), passes.RefusalUnavailable, tools.RefuseUnavailable)
}

// R-SDSA-2HAN R-T9D2-I35A
func TestBasisFiltersNormalizeValidateAndReplace(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(3)
	makeEnriched(t, f, ids[0])
	putBasisResult(t, f, ids[1], enrichers.KindBrief, failedBrief())
	for _, filters := range []any{map[string]any{}, map[string]any{"basis": nil}, map[string]any{"basis": []string{}}} {
		r := f.call("create_prospect_list", map[string]any{"name": "unrestricted", "filters": filters})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		pl := decodeList[tools.ProspectList](t, r)
		b, err := json.Marshal(pl.Filters)
		if err != nil || string(b) != "{}" {
			t.Fatal(string(b), err)
		}
		if len(pl.Filters.Basis) != 0 || len(peekBasis(t, f, pl.ID).Members) != 3 {
			t.Fatal(pl)
		}
	}
	values := []string{store.BasisEnriched, store.BasisRecords, store.BasisEnriched}
	r := f.call("create_prospect_list", tools.CreateProspectListArgs{Name: "restricted", Filters: &tools.FilterArgs{Categories: []string{}, Basis: &values}})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	pl := decodeList[tools.ProspectList](t, r)
	want := []string{store.BasisRecords, store.BasisEnriched}
	if !reflect.DeepEqual(pl.Filters.Basis, want) {
		t.Fatal(pl)
	}
	got := peekBasis(t, f, pl.ID)
	all := peekBasis(t, f, f.create("all").ID)
	var expected []tools.Member
	for _, m := range all.Members {
		if m.Basis != store.BasisPartial {
			expected = append(expected, m)
		}
	}
	if !reflect.DeepEqual(got.Members, expected) || !reflect.DeepEqual(got.ProspectList.Filters.Basis, want) {
		t.Fatal(got, expected)
	}
	for _, args := range []map[string]any{{"prospect_list": pl.ID}, {"prospect_list": pl.ID, "filters": nil}} {
		edited := decodeList[tools.ProspectList](t, f.call("edit_prospect_list", args))
		if !reflect.DeepEqual(edited.Filters.Basis, want) {
			t.Fatal(edited)
		}
	}
	edited := decodeList[tools.ProspectList](t, f.call("edit_prospect_list", map[string]any{"prospect_list": pl.ID, "filters": map[string]any{}}))
	if len(edited.Filters.Basis) != 0 {
		t.Fatal(edited)
	}
	f.d.SetFailing(true)
	bad := []string{"unknown-basis"}
	refusal(t, f.call("create_prospect_list", tools.CreateProspectListArgs{Name: "invalid", Filters: &tools.FilterArgs{Categories: []string{}, Basis: &bad}}), passes.RefusalInvalid, tools.RefuseInvalid)
	refusal(t, f.call("edit_prospect_list", tools.EditProspectListArgs{ProspectList: pl.ID, Filters: &tools.FilterArgs{Categories: []string{}, Basis: &bad}}), passes.RefusalInvalid, tools.RefuseInvalid)
}

// R-XJLL-UGMG
func TestProspectListChangesDescribeMutations(t *testing.T) {
	for _, scenario := range []string{"reject", "exclude", "size", "basis", "merge", "new", "moved-up", "passes"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			ids := f.seed(3)
			size := 4
			var filters *tools.FilterArgs
			if scenario == "moved-up" {
				size = 2
			}
			if scenario == "passes" {
				makeEnriched(t, f, ids[0])
				values := []string{store.BasisEnriched}
				filters = &tools.FilterArgs{Categories: []string{}, Basis: &values}
			}
			r := f.call("create_prospect_list", tools.CreateProspectListArgs{Name: "changes", Size: &size, Filters: filters})
			pl := decodeList[tools.ProspectList](t, r)
			initial := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: pl.ID}))
			f.now = f.now.Add(time.Minute)
			target := initial.Members[0]
			var wantJoined []tools.Joined
			var wantLeft []tools.Left
			var wantDefinition []string
			switch scenario {
			case "reject", "moved-up":
				f.call("reject", tools.RejectArgs{ProspectList: pl.ID, Candidate: target.Candidate.ID, Why: "fixture reason"})
				wantLeft = []tools.Left{{Candidate: target.Candidate.ID, Name: target.Candidate.Name, BasisThen: target.Basis, BasisNow: target.Basis, Why: store.WhyRejected}}
				if scenario == "moved-up" {
					// The unrestricted read exposes the candidate beyond this list's size cut.
					full := peekBasis(t, f, f.create("full").ID)
					for _, m := range full.Members {
						if m.Candidate.ID != initial.Members[0].Candidate.ID && m.Candidate.ID != initial.Members[1].Candidate.ID {
							wantJoined = []tools.Joined{{Candidate: m.Candidate.ID, Name: m.Candidate.Name, Basis: m.Basis, Why: store.WhyMovedUp}}
						}
					}
				}
			case "exclude":
				f.call("exclude", tools.RulingArgs{Candidate: target.Candidate.ID, Why: "fixture exclusion"})
				wantLeft = []tools.Left{{Candidate: target.Candidate.ID, Name: target.Candidate.Name, BasisThen: target.Basis, BasisNow: target.Basis, Why: store.WhyExcluded}}
			case "size":
				n := 2
				f.call("edit_prospect_list", tools.EditProspectListArgs{ProspectList: pl.ID, Size: &n})
				m := initial.Members[2]
				wantDefinition = []string{store.DefinitionSize}
				wantLeft = []tools.Left{{Candidate: m.Candidate.ID, Name: m.Candidate.Name, BasisThen: m.Basis, BasisNow: m.Basis, Why: store.WhyCut}}
			case "basis":
				values := []string{store.BasisEnriched}
				f.call("edit_prospect_list", tools.EditProspectListArgs{ProspectList: pl.ID, Filters: &tools.FilterArgs{Categories: []string{}, Basis: &values}})
				wantDefinition = []string{store.DefinitionFilters}
				for _, m := range initial.Members {
					wantLeft = append(wantLeft, tools.Left{Candidate: m.Candidate.ID, Name: m.Candidate.Name, BasisThen: m.Basis, BasisNow: m.Basis, Why: store.WhyFilter, Filter: "basis"})
				}
			case "merge":
				f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: initial.Members[1].Candidate.ID, Other: target.Candidate.ID, Why: "fixture merge"})
				wantLeft = []tools.Left{{Candidate: target.Candidate.ID, Name: target.Candidate.Name, BasisThen: target.Basis, Why: store.WhyGone}}
			case "new":
				_, created, err := f.st.Attach(t.Context(), "new-run", true, []store.Observation{{Scraper: "fmcsa", SourceID: "new-row", Digest: "new-digest", Reading: sources.Reading{Name: "New candidate", NameKey: "new-candidate"}}})
				if err != nil || len(created) != 1 {
					t.Fatal(created, err)
				}
				if _, err = f.st.Judge(t.Context(), created); err != nil {
					t.Fatal(err)
				}
				wantJoined = []tools.Joined{{Candidate: created[0], Name: "New candidate", Basis: store.BasisRecords, Why: store.WhyNew}}
			case "passes":
				makeEnriched(t, f, ids[1])
				c, err := f.st.GetCandidate(t.Context(), ids[1])
				if err != nil {
					t.Fatal(err)
				}
				wantJoined = []tools.Joined{{Candidate: c.ID, Name: c.Name, Basis: store.BasisEnriched, Why: store.WhyPasses}}
			}
			got := peekBasis(t, f, pl.ID)
			if len(got.Changes.Joined) != len(wantJoined) || len(got.Changes.Left) != len(wantLeft) || len(got.Changes.DefinitionChanged) != len(wantDefinition) {
				t.Fatal(got.Changes, wantJoined, wantLeft, wantDefinition)
			}
			for i, v := range wantJoined {
				if got.Changes.Joined[i] != v {
					t.Fatal(got.Changes, wantJoined)
				}
			}
			for i, v := range wantLeft {
				if got.Changes.Left[i] != v {
					t.Fatal(got.Changes, wantLeft)
				}
			}
			for i, v := range wantDefinition {
				if got.Changes.DefinitionChanged[i] != v {
					t.Fatal(got.Changes, wantDefinition)
				}
			}
			success(t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: pl.ID}), got)
		})
	}
}
