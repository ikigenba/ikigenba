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

// R-KQRJ-I4QT R-RT1O-11SD R-QR8S-65SU R-0ECF-HYB5
func TestCategoryEvidenceAnswers(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(3)
	if _, _, err := f.st.Derive(t.Context(), enrichers.KindLinkFacts, "", false, 100); err != nil {
		t.Fatal(err)
	}
	if err := walkJudgments(t.Context(), f.st, f.now); err != nil {
		t.Fatal(err)
	}
	co := f.create("category-evidence-prospect_list")
	add := func(id, kind string, found any) {
		t.Helper()
		f.now = f.now.Add(time.Second)
		if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: kind, Inputs: "chosen-category-input", Found: encodedJSON(found)}}); err != nil {
			t.Fatal(err)
		}
	}
	read := func() tools.ProspectListRead {
		return decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: co.ID, Peek: true}))
	}
	detail := func(id string) *tools.Facts {
		t.Helper()
		v := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
		if v.Enrichments == nil || v.Enrichments.Facts == nil {
			t.Fatal(v)
		}
		return v.Enrichments.Facts
	}
	for _, id := range ids {
		a := detail(id)
		if a.ClassifiedBy != "" || a.RecordsDiffer || a.RecordCategories == nil || len(a.RecordCategories) != 0 {
			t.Fatal(a)
		}
	}
	add(ids[0], enrichers.KindLinkFacts, enrichers.LinkFound{FleetVerdict: sources.FleetUnknown, Categories: []enrichers.Category{{Name: sources.CategoryRetail, Source: "ppp"}, {Name: sources.CategoryTrucking, Source: "fmcsa"}}})
	add(ids[1], enrichers.KindLinkFacts, enrichers.LinkFound{FleetVerdict: sources.FleetUnknown, Categories: []enrichers.Category{{Name: sources.CategoryRetail, Source: "ppp"}, {Name: sources.CategoryTrucking, Source: "fmcsa"}}})
	a := detail(ids[0])
	if a.ClassifiedBy != enrichers.SourceLinks || a.RecordsDiffer || len(a.RecordCategories) != 0 || !reflect.DeepEqual(a.Categories, []tools.Category{{Name: sources.CategoryRetail, Source: "ppp"}, {Name: sources.CategoryTrucking, Source: "fmcsa"}}) {
		t.Fatal(a)
	}
	good := enrichers.BriefFound{Run: "category-brief", Status: "exited", ExitCode: new(0), Summary: "chosen-category-summary", Labels: []string{}, Categories: []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "chosen-retail-reason"}, {Name: sources.CategoryAgriculture, Why: "chosen-agriculture-reason"}}}
	add(ids[1], enrichers.KindBrief, good)
	a = detail(ids[1])
	if a.ClassifiedBy != enrichers.KindBrief || !a.RecordsDiffer || !reflect.DeepEqual(a.RecordCategories, []tools.Category{{Name: sources.CategoryRetail, Source: "ppp"}, {Name: sources.CategoryTrucking, Source: "fmcsa"}}) || !reflect.DeepEqual(a.Categories, []tools.Category{{Name: sources.CategoryAgriculture, Source: enrichers.KindBrief}, {Name: sources.CategoryRetail, Source: enrichers.KindBrief}}) {
		t.Fatal(a)
	}
	r := read()
	if r.Matching != 3 || r.Classified != 1 {
		t.Fatal(r)
	}
	for _, m := range r.Members {
		switch m.Candidate.ID {
		case ids[0]:
			if m.ClassifiedBy != enrichers.SourceLinks || !reflect.DeepEqual(m.Categories, []string{sources.CategoryRetail, sources.CategoryTrucking}) {
				t.Fatal(m)
			}
		case ids[1]:
			if m.ClassifiedBy != enrichers.KindBrief || !reflect.DeepEqual(m.Categories, []string{sources.CategoryAgriculture, sources.CategoryRetail}) {
				t.Fatal(m)
			}
		case ids[2]:
			if m.ClassifiedBy != "" || m.Categories == nil || len(m.Categories) != 0 {
				t.Fatal(m)
			}
		}
	}
	add(ids[1], enrichers.KindBrief, enrichers.BriefFound{Run: "chosen-failed-run", Status: "failed", Categories: []enrichers.BriefCategory{}, Labels: []string{}})
	if a = detail(ids[1]); !a.RecordsDiffer || a.ClassifiedBy != enrichers.KindBrief {
		t.Fatal(a)
	}
	good.Categories = []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "retail"}, {Name: sources.CategoryTrucking, Why: "trucking"}}
	add(ids[1], enrichers.KindBrief, good)
	if a = detail(ids[1]); a.RecordsDiffer || a.RecordCategories == nil || len(a.RecordCategories) != 0 || a.ClassifiedBy != enrichers.KindBrief {
		t.Fatal(a)
	}
	add(ids[2], enrichers.KindBrief, good)
	if a = detail(ids[2]); !a.RecordsDiffer || a.RecordCategories == nil || len(a.RecordCategories) != 0 {
		t.Fatal(a)
	}
	empty := f.create("empty-category-prospect_list")
	for _, id := range ids {
		if _, err := f.st.Reject(t.Context(), store.Reject{Kind: store.KindReject, ProspectList: empty.ID, Candidate: id, Why: "chosen-reason", UserID: "chosen-owner"}); err != nil {
			t.Fatal(err)
		}
	}
	er := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: empty.ID, Peek: true}))
	if er.Classified != 0 || len(er.Members) != 0 {
		t.Fatal(er)
	}
}
