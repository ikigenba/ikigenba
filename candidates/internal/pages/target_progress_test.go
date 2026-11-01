package pages

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-Y40X-A5ZP R-Y58T-NXQE
func TestTargetProgressTypes(t *testing.T) {
	_ = struct {
		Kind                                      string
		NewResults, NotDue, NotApplicable, Failed int64
		Cost                                      string
	}(KindEntryRow{})
	entry := KindEntryRow{"chosen-kind", 1, 2, 3, 4, "chosen-cost"}
	if entry.Kind != "chosen-kind" || entry.NewResults != 1 || entry.NotDue != 2 || entry.NotApplicable != 3 || entry.Failed != 4 || entry.Cost != "chosen-cost" {
		t.Fatal(entry)
	}
	const ids, candidate, list string = TargetFormIDs, TargetFormCandidate, TargetFormList
	if ids != "ids" || candidate != "candidate" || list != "list" {
		t.Fatal(ids, candidate, list)
	}
}

// R-Y6GQ-1PH3
func TestTargetProgressProjection(t *testing.T) {
	const d = "cand_0123456789abcdef"
	const h = "pl_0123456789abcdef"
	for _, tc := range []struct {
		kind, target string
		ids          []string
		form         string
	}{
		{store.KindEnrichTarget, "", []string{d, "unmatched", d}, TargetFormIDs},
		{store.KindEnrichTarget, h, nil, TargetFormList},
		{store.KindEnrichTarget, d, nil, TargetFormCandidate},
		{store.KindEnrichTarget, "malformed", nil, ""},
		{store.KindEnrichTarget, "", nil, ""},
		{store.KindJudge, "", nil, ""},
		{store.KindEnrich, "", nil, ""},
		{store.KindWalk, "", nil, ""},
		{store.KindJudge, d, []string{d}, ""},
	} {
		p := store.Pass{Kind: tc.kind, Target: tc.target, TargetIDs: tc.ids}
		v := passView(p, "")
		if v.TargetForm != tc.form || v.TargetIDs == nil || !reflect.DeepEqual(v.TargetIDs, append([]string{}, tc.ids...)) || v.KindEntries == nil || len(v.KindEntries) != 0 || v.RunningKind != "" || v.Busy != 0 || v.Unmatched != 0 {
			t.Fatal(tc, v)
		}
		if tc.form == TargetFormIDs && (v.Target != "" || v.TargetPage != "") {
			t.Fatal(v)
		}
	}
	p := store.Pass{Kind: store.KindEnrichTarget, RunningKind: enrichers.KindBrief, Busy: 2, Unmatched: 1, KindEntries: []store.KindEntry{
		{Kind: enrichers.KindWebsiteSearch, NewResults: 2, NotDue: 1, Failed: 1, Cost: 23714000},
		{Kind: enrichers.KindSiteText, NotDue: 3, NotApplicable: 1},
	}}
	want := []KindEntryRow{{enrichers.KindWebsiteSearch, 2, 1, 0, 1, "$0.023714"}, {enrichers.KindSiteText, 0, 3, 1, 0, "$0.000000"}}
	v := passView(p, "")
	if v.RunningKind != enrichers.KindBrief || v.Busy != 2 || v.Unmatched != 1 || !reflect.DeepEqual(v.KindEntries, want) {
		t.Fatal(v)
	}
}

func targetPageCandidates(t *testing.T, cfg Config) []string {
	t.Helper()
	_, ids, err := cfg.Store.Attach(t.Context(), "target-run", false, []store.Observation{
		{Scraper: "fixture", SourceID: "first", Digest: "first", Reading: sources.Reading{Name: "first target", NameKey: "first target"}},
		{Scraper: "fixture", SourceID: "second", Digest: "second", Reading: sources.Reading{Name: "second target", NameKey: "second target"}},
	})
	if err != nil || len(ids) != 2 {
		t.Fatal(ids, err)
	}
	return ids
}

func capturedTargetPass(t *testing.T, cfg Config, id string) PassView {
	t.Helper()
	path := "/passes/" + id
	d := captureWalkData[PassData](t, cfg, path, "pass")
	comparePage(t, cfg, path, "pass", 200, d)
	return d.Pass
}

// R-Y7OM-FH7S
func TestTargetPassProgressPage(t *testing.T) {
	cfg, _ := pageFixture(t)
	ids := targetPageCandidates(t, cfg)
	x := "cand_ffffffffffffffff"
	given := []string{ids[0], x, ids[0], ids[1]}
	p0 := store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: "progress-owner", TargetIDs: given}
	q, _, err := cfg.Store.AddTargetPass(t.Context(), p0, given)
	if err != nil {
		t.Fatal(err)
	}
	read := func(running, status string, entries []KindEntryRow) PassView {
		t.Helper()
		v := capturedTargetPass(t, cfg, q.ID)
		if v.TargetForm != TargetFormIDs || v.Target != "" || v.TargetPage != "" || v.EnrichKind != "" || !reflect.DeepEqual(v.TargetIDs, given) || v.Status != status || v.RunningKind != running || v.Busy != 0 || v.Unmatched != 1 || !reflect.DeepEqual(v.KindEntries, entries) {
			t.Fatal(v)
		}
		return v
	}
	read("", store.StatusRunning, []KindEntryRow{})
	if _, err = cfg.Store.StartPassKind(t.Context(), q.ID, enrichers.KindWebsiteSearch); err != nil {
		t.Fatal(err)
	}
	read(enrichers.KindWebsiteSearch, store.StatusRunning, []KindEntryRow{})
	k1 := store.KindEntry{Kind: enrichers.KindWebsiteSearch, NewResults: 1, NotApplicable: 1, Cost: 23714000}
	if _, err = cfg.Store.EndPassKind(t.Context(), q.ID, k1); err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.StartPassKind(t.Context(), q.ID, enrichers.KindSiteText); err != nil {
		t.Fatal(err)
	}
	entries := []KindEntryRow{{enrichers.KindWebsiteSearch, 1, 0, 1, 0, "$0.023714"}}
	read(enrichers.KindSiteText, store.StatusRunning, entries)
	busy, _, err := cfg.Store.AddTargetPass(t.Context(), store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: "second-owner", EnrichKind: enrichers.KindBrief, Target: ids[0]}, []string{ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	v := capturedTargetPass(t, cfg, busy.ID)
	if v.TargetForm != TargetFormCandidate || v.Target != ids[0] || v.TargetPage != "/candidates/"+ids[0] || v.EnrichKind != enrichers.KindBrief || v.TargetIDs == nil || len(v.TargetIDs) != 0 || v.KindEntries == nil || len(v.KindEntries) != 0 || v.Busy != 1 || v.Unmatched != 0 {
		t.Fatal(v)
	}
	k2 := store.KindEntry{Kind: enrichers.KindSiteText, NewResults: 1, Failed: 1, Cost: 5000000}
	if _, err = cfg.Store.EndPassKind(t.Context(), q.ID, k2); err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.FinishPass(t.Context(), q.ID, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	entries = append(entries, KindEntryRow{enrichers.KindSiteText, 1, 0, 0, 1, "$0.005000"})
	if v = read("", store.StatusDone, entries); v.Finished == "" {
		t.Fatal(v)
	}
	q, _, err = cfg.Store.AddTargetPass(t.Context(), p0, given)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.StartPassKind(t.Context(), q.ID, enrichers.KindWebsiteSearch); err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.EndPassKind(t.Context(), q.ID, k1); err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.StartPassKind(t.Context(), q.ID, enrichers.KindSiteText); err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.FailPass(t.Context(), q.ID, store.ReasonInterrupted); err != nil {
		t.Fatal(err)
	}
	v = read("", store.StatusFailed, entries[:1])
	if v.Reason != store.ReasonInterrupted || v.Finished == "" {
		t.Fatal(v)
	}
}

// R-Y8WI-T8YH
func TestCandidateRunningTargetPassPage(t *testing.T) {
	for _, fail := range []bool{false, true} {
		cfg, _ := pageFixture(t)
		ids := targetPageCandidates(t, cfg)
		check := func(id, running string) {
			t.Helper()
			if got := capturedCandidate(t, cfg, id).Candidate.RunningPass; got != running {
				t.Fatal(id, got, running)
			}
		}
		check(ids[0], "")
		if _, err := cfg.Store.AddPass(t.Context(), store.Pass{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: "corpus-owner"}); err != nil {
			t.Fatal(err)
		}
		check(ids[0], "")
		p := store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: "target-owner", Target: ids[0]}
		first, _, err := cfg.Store.AddTargetPass(t.Context(), p, []string{ids[0]})
		if err != nil {
			t.Fatal(err)
		}
		check(ids[0], first.ID)
		if _, _, err = cfg.Store.AddTargetPass(t.Context(), p, []string{ids[0]}); err != nil {
			t.Fatal(err)
		}
		check(ids[0], first.ID)
		check(ids[1], "")
		if fail {
			_, err = cfg.Store.FailPass(t.Context(), first.ID, store.ReasonInterrupted)
		} else {
			_, err = cfg.Store.FinishPass(t.Context(), first.ID, store.Counts{})
		}
		if err != nil {
			t.Fatal(err)
		}
		check(ids[0], "")
		claimed, _, err := cfg.Store.AddTargetPass(t.Context(), p, []string{ids[0]})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = cfg.Store.Decide(t.Context(), store.Ruling{Kind: store.KindMerge, Candidate: ids[1], Other: ids[0], Why: "owner combined", UserID: "owner"}); err != nil {
			t.Fatal(err)
		}
		if running, lookupErr := cfg.Store.RunningTargetPass(t.Context(), ids[0]); lookupErr != nil || running != claimed.ID {
			t.Fatal(running, lookupErr, claimed.ID)
		}
		check(ids[0], "")
	}
}

// R-YBCB-KSFV
func TestTargetProgressTemplateStates(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, data any) {
		t.Helper()
		if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, name, data); err != nil {
			t.Fatal(name, data, err)
		}
	}
	for _, target := range []struct {
		form, id, path string
		ids            []string
	}{
		{TargetFormIDs, "", "", []string{"chosen-candidate"}},
		{TargetFormIDs, "", "", []string{"first-candidate", "second-candidate", "first-candidate"}},
		{TargetFormCandidate, "chosen-candidate", "/candidates/chosen-candidate", nil},
		{TargetFormList, "chosen-list", "/prospect-lists/chosen-list", nil},
	} {
		for _, kind := range []string{"", enrichers.KindBrief} {
			for _, running := range []string{"", enrichers.KindSiteText} {
				for _, busy := range []int64{0, 2} {
					for _, unmatched := range []int64{0, 3} {
						p := PassView{Kind: store.KindEnrichTarget, TargetForm: target.form, Target: target.id, TargetPage: target.path, TargetIDs: target.ids, EnrichKind: kind, RunningKind: running, Busy: busy, Unmatched: unmatched}
						render("pass", PassData{Pass: p})
					}
				}
			}
		}
		for _, status := range []string{store.StatusRunning, store.StatusDone, store.StatusFailed} {
			for _, n := range []int{0, 1, 5} {
				for mask := range 16 {
					entries := []KindEntryRow{}
					for range n {
						entries = append(entries, KindEntryRow{enrichers.KindWebsiteSearch, int64(mask & 1), int64(mask & 2), int64(mask & 4), int64(mask & 8), "$0.023714"})
					}
					render("pass", PassData{Pass: PassView{Kind: store.KindEnrichTarget, TargetForm: target.form, Target: target.id, TargetPage: target.path, TargetIDs: target.ids, Status: status, KindEntries: entries}})
				}
			}
		}
	}
	for _, kind := range []string{store.KindIngest, store.KindJudge, store.KindEnrich, store.KindParks, store.KindWalk} {
		render("pass", PassData{Pass: PassView{Kind: kind}})
	}
	for _, running := range []string{"", "pass_0123456789abcdef"} {
		render("candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive, RunningPass: running}})
	}
	render("candidate", CandidateData{Candidate: CandidateView{Status: store.StatusMerged}})
}
