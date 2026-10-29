package pages

import (
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-GOAB-PI3C R-UKGN-3BU2
// R-F6NA-M1VD
func TestBriefViewSelection(t *testing.T) {
	good := enrichers.BriefFound{Run: "fixture-run", Status: "exited", ExitCode: new(0), Summary: "fixture-summary", Categories: []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "fixture-why"}}, Labels: []string{"fixture-label", "other-label"}}
	result := func(b enrichers.BriefFound) store.Enrichment {
		return store.Enrichment{Kind: enrichers.KindBrief, Source: enrichers.SourcePrompts, Inputs: "fixture-inputs", Found: encodedJSON(b), At: fixtureTime()}
	}
	success := result(good)
	failed := result(enrichers.BriefFound{Status: "failed"})
	failed.At = fixtureTime().Add(2 * time.Minute)
	empty := result(enrichers.BriefFound{Status: "exited", ExitCode: new(0)})
	empty.At = fixtureTime().Add(time.Minute)
	for _, es := range [][]store.Enrichment{nil, {{Kind: sources.WebsiteCheck}, {Kind: enrichers.KindLinkFacts}, {Kind: enrichers.KindSiteText}}, {failed, empty}, {{Kind: enrichers.KindBrief, Found: []byte("malformed")}}} {
		if got := briefView(es); got != nil {
			t.Fatal(got)
		}
	}
	old := good
	old.Summary = "older-summary"
	older := result(old)
	older.At = fixtureTime().Add(-time.Hour)
	got := briefView([]store.Enrichment{failed, empty, success, older})
	want := &BriefView{good.Summary, []BriefCategoryRow{{sources.CategoryRetail, "fixture-why"}}, good.Labels, "2026-10-05T09:00:07Z", "2026-10-05 09:00:07 UTC"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	good.Categories = nil
	good.Labels = nil
	got = briefView([]store.Enrichment{result(good)})
	if got.Categories == nil || got.Labels == nil || len(got.Categories) != 0 || len(got.Labels) != 0 {
		t.Fatal(got)
	}
	for _, e := range []store.Enrichment{success, failed, empty} {
		got := enrichmentRow(e, enrichers.Subject{})
		if got.Failed != (string(e.Found) != string(success.Found)) {
			t.Fatal(got)
		}
	}
}

// R-9A14-AF9F R-9IK6-T8WB R-9SBD-VETV R-UKGN-3BU2
func TestBriefCandidateAndProspectList(t *testing.T) {
	cfg, db := pageFixture(t)
	now := fixtureTime()
	cfg.Store = store.New(db, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	ctx := t.Context()
	_, ids, err := cfg.Store.Attach(ctx, "fixture-run", false, []store.Observation{{Scraper: "fixture", SourceID: "one", Digest: "one", Reading: sources.Reading{Name: "brief-business", NameKey: "one"}}, {Scraper: "fixture", SourceID: "two", Digest: "two", Reading: sources.Reading{Name: "other-business", NameKey: "two"}}})
	if err != nil {
		t.Fatal(err)
	}
	co, err := cfg.Store.CreateProspectList(ctx, store.ProspectList{Name: "fixture-prospect_list", Size: 10, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if got := capturedCandidate(t, cfg, ids[0]); got.Enrichments.Brief != nil {
		t.Fatal(got)
	}
	for i, b := range []enrichers.BriefFound{{Run: "fixture-run", Status: "exited", ExitCode: new(0), Summary: "fixture-summary", Categories: []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "fixture-why"}}, Labels: []string{"fixture-label"}}, {Run: "failed-run", Status: "failed", Categories: []enrichers.BriefCategory{}, Labels: []string{}}} {
		now = fixtureTime().Add(time.Duration(i) * 2 * time.Minute)
		if _, err = cfg.Store.AddEnrichments(ctx, []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindBrief, Source: enrichers.SourcePrompts, Inputs: "fixture-inputs", Found: encodedJSON(b)}}); err != nil {
			t.Fatal(err)
		}
	}
	got := capturedCandidate(t, cfg, ids[0])
	if got.Enrichments.Brief == nil || got.Enrichments.Brief.Summary != "fixture-summary" || got.Enrichments.Brief.At != "2026-10-05T09:00:07Z" || got.Enrichments.Brief.AtText != "2026-10-05 09:00:07 UTC" || len(got.EnrichmentResults) != 2 || !got.EnrichmentResults[0].Failed || got.EnrichmentResults[1].Failed || got.Judgments != nil {
		t.Fatal(got)
	}
	if err = judgePageFixture(ctx, cfg.Store); err != nil {
		t.Fatal(err)
	}
	written := ""
	for _, w := range got.Enrichments.Facts.Written {
		if w.Kind == enrichers.KindBrief {
			written = w.At
		}
	}
	if written != "2026-10-05T09:02:07Z" {
		t.Fatal(got.Enrichments.Facts)
	}
	prospectList := pageProspectList(t, cfg, co.ID)
	if len(prospectList.Members) != 2 {
		t.Fatal(prospectList)
	}
	for _, m := range prospectList.Members {
		if m.ID == ids[0] {
			if m.Brief != written || m.BriefText != "2026-10-05 09:02" {
				t.Fatal(m)
			}
		} else if m.Brief != "" || m.BriefText != "" {
			t.Fatal(m)
		}
	}
	for _, at := range []time.Time{{}, fixtureTime()} {
		m := memberRow(store.Ranked{Brief: at}, 0)
		a, b := stamp(at, MinuteLayout)
		if m.Brief != a || m.BriefText != b {
			t.Fatal(m)
		}
	}
	if _, _, err = cfg.Store.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[1], Other: ids[0], UserID: "owner", Why: "fixture-merge"}); err != nil {
		t.Fatal(err)
	}
	if got := capturedCandidate(t, cfg, ids[0]); got.Enrichments != nil || got.Judgments != nil || len(got.EnrichmentResults) != 0 {
		t.Fatal(got)
	}
}
