package tools_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-HJLO-5P2Y R-VAPN-3BP5 R-VD5F-UV6J
func TestHeadcountCandidateAndListAnswers(t *testing.T) {
	f := newFixture(t)
	ids := f.seed(4)
	list := f.create("chosen-headcount-list")
	cases := []struct {
		counts    []enrichers.Headcount
		low, high *int64
	}{
		{[]enrichers.Headcount{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 100}, {Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: 40}}, new(int64(40)), new(int64(100))},
		{[]enrichers.Headcount{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 30}, {Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: 28}}, new(int64(30)), new(int64(30))},
		{[]enrichers.Headcount{}, nil, nil},
		{[]enrichers.Headcount{{Scraper: ppp.Name, Kind: sources.HeadcountJobs, Count: 26, Date: "2020-04-28"}}, new(int64(26)), new(int64(26))},
	}
	brief := enrichers.BriefFound{Run: "chosen-brief", Status: "exited", ExitCode: new(0), Summary: "chosen-summary", Categories: []enrichers.BriefCategory{}, Labels: []string{"chosen-label-one", "chosen-label-two"}}
	for i, tc := range cases {
		found := enrichers.LinkFound{Categories: []enrichers.Category{}, FleetVerdict: sources.FleetUnknown, Headcounts: tc.counts}
		if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[i], Kind: enrichers.KindLinkFacts, Source: enrichers.SourceLinks, Inputs: "chosen-inputs", Found: encodedJSON(found)}}); err != nil {
			t.Fatal(err)
		}
		if i == 3 {
			f.now = f.now.Add(time.Second)
			if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[i], Kind: enrichers.KindBrief, Source: enrichers.SourcePrompts, Inputs: "chosen-brief-inputs", Found: encodedJSON(brief)}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := walkJudgments(t.Context(), f.st, f.now); err != nil {
		t.Fatal(err)
	}
	members := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: list.ID, Peek: true})).Members
	if len(members) != len(ids) {
		t.Fatal(members)
	}
	byID := map[string]tools.Member{}
	for _, m := range members {
		byID[m.Candidate.ID] = m
	}
	for i, tc := range cases {
		d := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: ids[i]}))
		j, err := f.st.GetJudgments(t.Context(), ids[i])
		if err != nil {
			t.Fatal(err)
		}
		if d.Judgments == nil || !reflect.DeepEqual(d.Judgments.HeadcountLow, tc.low) || !reflect.DeepEqual(d.Judgments.HeadcountHigh, tc.high) || d.Judgments.HeadcountBasis != j.HeadcountBasis || (d.Judgments.HeadcountBasis != "") != (tc.low != nil) {
			t.Fatal(d.Judgments, j)
		}
		if !reflect.DeepEqual(byID[ids[i]].HeadcountLow, tc.low) || !reflect.DeepEqual(byID[ids[i]].RevenueLow, j.RevenueLow) {
			t.Fatal(byID[ids[i]], j)
		}
		var object map[string]json.RawMessage
		if err = json.Unmarshal(encodedJSON(d.Judgments), &object); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"headcount_low", "headcount_high", "headcount_basis"} {
			if _, ok := object[key]; ok != (tc.low != nil) {
				t.Fatal(string(encodedJSON(d.Judgments)))
			}
		}
		facts := d.Enrichments.Facts
		if len(facts.Headcounts) != len(tc.counts) {
			t.Fatal(facts)
		}
		for k, h := range tc.counts {
			if facts.Headcounts[k] != (tools.Headcount{Scraper: h.Scraper, Kind: h.Kind, Count: h.Count, Date: h.Date}) {
				t.Fatal(facts.Headcounts)
			}
		}
		wantLabels := []string{}
		if i == 3 {
			wantLabels = brief.Labels
		}
		if !reflect.DeepEqual(facts.BriefLabels, wantLabels) {
			t.Fatal(facts)
		}
		if facts.Headcounts == nil || facts.BriefLabels == nil {
			t.Fatal(facts)
		}
	}
}

// R-VFL8-MENX
func TestCandidateAnswersHeadcountEstimate(t *testing.T) {
	f := newFixture(t)
	_, ids, err := f.st.Attach(t.Context(), "chosen-run", false, []store.Observation{{Scraper: fmcsa.Name, SourceID: "chosen-row", Digest: "chosen-digest", Reading: sources.Reading{Name: "chosen-business", NameKey: "chosen-business", Estimate: &sources.Estimate{Source: fmcsa.Name, Low: 1500000, High: new(int64(2500000)), Confidence: .7}}}})
	if err != nil {
		t.Fatal(err)
	}
	found := enrichers.LinkFound{Categories: []enrichers.Category{{Name: sources.CategoryTrucking, Source: fmcsa.Name}}, FleetVerdict: sources.FleetUnknown, Headcounts: []enrichers.Headcount{{Scraper: fmcsa.Name, Kind: sources.HeadcountDrivers, Count: 12}}}
	if _, err = f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindLinkFacts, Source: enrichers.SourceLinks, Inputs: "chosen-inputs", Found: encodedJSON(found)}}); err != nil {
		t.Fatal(err)
	}
	if err = walkJudgments(t.Context(), f.st, f.now); err != nil {
		t.Fatal(err)
	}
	d := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: ids[0]}))
	es, err := f.st.Estimates(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Estimates) != 2 || len(es) != 2 {
		t.Fatal(d.Estimates, es)
	}
	for i, e := range es {
		want := tools.Estimate{Source: e.Source, Low: e.Low, High: e.High, Confidence: e.Confidence, Facts: e.Facts, Formula: e.Formula}
		if !e.Filed.IsZero() {
			want.Filed = e.Filed.UTC().Format("2006-01-02")
		}
		if string(encodedJSON(d.Estimates[i])) != string(encodedJSON(want)) {
			t.Fatal(d.Estimates[i], want)
		}
	}
	a, b := d.Estimates[0], d.Estimates[1]
	if a.Source != fmcsa.Name || a.Low != 1500000 || a.High == nil || *a.High != 2500000 || a.Confidence != .7 || b.Source != judging.HeadcountSource || b.Low != 1440000 || b.High == nil || *b.High != 2400000 || b.Confidence != .3 || b.Filed != "" || b.Formula != sources.Rules {
		t.Fatal(d.Estimates)
	}
}
