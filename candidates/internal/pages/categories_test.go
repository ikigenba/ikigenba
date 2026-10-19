package pages

import (
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-BGGM-ZNRG R-BRFQ-FLFP R-BK4C-4YZJ R-BIWF-R78U
func TestCategoryEvidenceViews(t *testing.T) {
	records := []sources.Category{{Name: sources.CategoryRetail, Source: "ppp"}, {Name: sources.CategoryTrucking, Source: "fmcsa"}}
	brief := []sources.Category{{Name: sources.CategoryAgriculture, Source: enrichers.KindBrief}, {Name: sources.CategoryRetail, Source: enrichers.KindBrief}}
	for _, tc := range []struct {
		categories, records []sources.Category
		by                  string
		differs             bool
	}{
		{brief, records, enrichers.KindBrief, true}, {records, records, enrichers.SourceLinks, false},
		{[]sources.Category{{Name: sources.CategoryRetail, Source: enrichers.KindBrief}}, records[:1], enrichers.KindBrief, false},
		{brief, nil, enrichers.KindBrief, true}, {nil, nil, "", false},
	} {
		f := factsView(store.Facts{Categories: tc.categories, Records: tc.records})
		want := []CategoryRow{}
		if tc.differs {
			for _, c := range tc.records {
				want = append(want, CategoryRow{c.Name, c.Source})
			}
		}
		if f.ClassifiedBy != tc.by || f.RecordsDiffer != tc.differs || !reflect.DeepEqual(f.RecordCategories, want) {
			t.Fatal(f, tc)
		}
		member := memberRow(store.Ranked{Assessment: store.Assessment{Categories: tc.categories}}, 2)
		if member.ClassifiedBy != tc.by || member.Rank != 3 || member.Categories == nil {
			t.Fatal(member)
		}
	}
}

// R-BLC8-IQQ8 R-BHOJ-DFI5 R-BRFQ-FLFP R-BJJ0-IJCV
func TestCategoryEvidencePages(t *testing.T) {
	cfg, db := pageFixture(t)
	now := fixtureTime()
	cfg.Store = store.New(db, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	_, ids, err := cfg.Store.Attach(t.Context(), "category-run", false, []store.Observation{
		{Scraper: "fixture", SourceID: "one", Digest: "one", Reading: sources.Reading{Name: "first-category-business", NameKey: "one"}},
		{Scraper: "fixture", SourceID: "two", Digest: "two", Reading: sources.Reading{Name: "second-category-business", NameKey: "two"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.JudgeAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	co, err := cfg.Store.CreateCohort(t.Context(), store.Cohort{Name: "category-cohort", Size: 10, CreatedUserID: "chosen-owner"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if _, err = cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: enrichers.KindLinkFacts, Inputs: "chosen-link-input", Source: enrichers.SourceLinks, Found: encodedJSON(enrichers.LinkFound{FleetVerdict: sources.FleetUnknown, Categories: []enrichers.Category{{Name: sources.CategoryRetail, Source: "ppp"}, {Name: sources.CategoryTrucking, Source: "fmcsa"}}})}}); err != nil {
			t.Fatal(err)
		}
	}
	good := enrichers.BriefFound{Run: "brief-run", Status: "exited", ExitCode: new(0), Summary: "chosen-category-summary", Labels: []string{}, Categories: []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "first-reason"}, {Name: sources.CategoryAgriculture, Why: "second-reason"}}}
	for _, b := range []enrichers.BriefFound{good, {Run: "empty-category-run", Status: "exited", ExitCode: new(0), Summary: "chosen-empty-category-summary", Categories: []enrichers.BriefCategory{}, Labels: []string{}}} {
		now = now.Add(time.Second)
		if _, err = cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindBrief, Inputs: "chosen-brief-input", Source: enrichers.SourcePrompts, Found: encodedJSON(b)}}); err != nil {
			t.Fatal(err)
		}
		candidate := capturedCandidate(t, cfg, ids[0])
		cohort := pageCohort(t, cfg, co.ID)
		wantBy, wantCount := enrichers.KindBrief, int64(1)
		if len(b.Categories) == 0 {
			wantBy, wantCount = enrichers.SourceLinks, 0
		}
		if candidate.Facts.ClassifiedBy != wantBy || candidate.Facts.RecordsDiffer != (len(b.Categories) > 0) || candidate.Brief == nil || len(candidate.Brief.Categories) != len(b.Categories) || cohort.Classified != wantCount || cohort.Matching != 2 {
			t.Fatal(candidate, cohort)
		}
		if len(b.Categories) > 0 && !reflect.DeepEqual(candidate.Facts.RecordCategories, []CategoryRow{{sources.CategoryRetail, "ppp"}, {sources.CategoryTrucking, "fmcsa"}}) {
			t.Fatal(candidate.Facts)
		}
		if len(b.Categories) == 0 && (len(candidate.Facts.RecordCategories) != 0 || !reflect.DeepEqual(candidate.Facts.Categories, []CategoryRow{{sources.CategoryRetail, "ppp"}, {sources.CategoryTrucking, "fmcsa"}})) {
			t.Fatal(candidate.Facts)
		}
		for _, m := range cohort.Members {
			if m.ID == ids[0] && (m.ClassifiedBy != wantBy || m.Brief != now.Format(time.RFC3339)) {
				t.Fatal(m)
			}
			if m.ID == ids[1] && m.ClassifiedBy != enrichers.SourceLinks {
				t.Fatal(m)
			}
		}
	}
}
