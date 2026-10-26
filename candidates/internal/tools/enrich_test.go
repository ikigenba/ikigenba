package tools_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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

type websiteTransport func(*http.Request) (*http.Response, error)

func (f websiteTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func immediateWebsite(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	if d == website.Interval {
		ch <- time.Time{}
	}
	return ch
}
func (f *fixture) waitEnrich(id string) store.Pass {
	f.t.Helper()
	deadline, cancel := context.WithTimeout(f.t.Context(), 3*time.Second)
	defer cancel()
	for {
		p, err := f.st.GetPass(deadline, id)
		if err != nil {
			f.t.Fatal(err)
		}
		if p.Status != store.StatusRunning {
			return p
		}
		if deadline.Err() != nil {
			f.t.Fatal("pass did not end")
		}
	}
}

// R-ZCZ9-BW58 R-2ZGI-9CHJ R-0DK8-T0E2 R-050Y-4M77 R-QQW5-TEAW
func TestEnrichSchemaAndValidation(t *testing.T) {
	f := newFixture(t)
	infos, err := f.client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, info := range infos {
		if info.Name == "enrich" {
			found = true
			if string(info.InputSchema) != `{"type":"object","properties":{"kind":{"type":"string"},"prospect_list":{"type":"string"},"candidate":{"type":"string"},"force":{"type":"boolean"}},"additionalProperties":false}` {
				t.Fatal(string(info.InputSchema))
			}
		}
	}
	if !found {
		t.Fatal("missing enrich")
	}
	for _, args := range []map[string]any{{"kind": sources.WebsiteCheck, "older_than_days": 1.5}, {"kind": sources.WebsiteCheck, "older_than_days": 1e30}, {"older_than_days": 0}} {
		r := f.call("enrich", args)
		if !r.IsError() {
			t.Fatal(r)
		}
		text := decode(t, r).Content[0].Text
		for _, word := range []string{passes.RefusalBusy, passes.RefusalInvalid, passes.RefusalNotFound, passes.RefusalStopping, passes.RefusalUnavailable} {
			if strings.HasPrefix(text, word+":") {
				t.Fatal(text)
			}
		}
	}
	for _, fail := range []bool{true, false} {
		f.d.SetFailing(fail)
		if !fail {
			f.eng.Drain(context.Background())
		}
		for _, args := range []tools.EnrichArgs{{Kind: strings.ToUpper(sources.WebsiteCheck)}, {Kind: enrichers.SourceLinks}, {Kind: enrichers.KindSiteText}, {Force: true}, {Kind: enrichers.KindLinkFacts, ProspectList: "pl_0000000000000000", Candidate: "cand_0000000000000000"}, {ProspectList: "pl_0000000000000000"}} {
			refusal(t, f.call("enrich", args), passes.RefusalInvalid, tools.RefuseInvalid)
		}
	}
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 0 {
		t.Fatal(ps, err)
	}
}

// R-UZO7-ZHHN R-QQW5-TEAW
func TestEnrichDispatchAndRefusals(t *testing.T) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	k := &website.Checker{Client: &http.Client{Transport: websiteTransport(func(r *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("fixture"))}, nil
	})}, After: immediateWebsite}
	f := fixtureWithWebsite(t, nil, k)
	f.seed(1)
	r := f.call("enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	record := decodeList[passes.Record](t, r)
	if record.Kind != store.KindEnrich || record.Origin != store.OriginEnrich || record.Trigger != store.TriggerEnrich || record.Status != store.StatusRunning || record.User != caller.UserID {
		t.Fatal(record)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("check not started")
	}
	refusal(t, f.call("enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck}), passes.RefusalBusy, tools.RefuseBusy)
	close(release)
	if p := f.waitEnrich(record.ID); p.Status != store.StatusDone || p.Due != 1 {
		t.Fatal(p)
	}
	f.d.SetFailing(true)
	refusal(t, f.call("enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck}), passes.RefusalUnavailable, tools.RefuseUnavailable)
	f.d.SetFailing(false)
	f.eng.Drain(context.Background())
	refusal(t, f.call("enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck}), passes.RefusalStopping, tools.RefuseStopping)
	absent := newFixture(t)
	refusal(t, absent.call("enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck}), passes.RefusalUnavailable, tools.RefuseUnavailable)
}

// R-KT7C-9O87 R-Q65V-BAP3
func TestCandidateEnrichmentHistory(t *testing.T) {
	f := newFixture(t)
	id := f.seed(1)[0]
	first := store.Enrichment{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "fixture-inputs", Found: encodedJSON(website.Result{State: sources.WebsiteHTTPS, URL: "https://business.test", Title: "chosen", StatusCode: 200, Size: 8}), Source: "business.test"}
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{first}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "fixture-inputs", Found: encodedJSON(website.Result{State: sources.WebsiteNone}), Source: "business.test"}}); err != nil {
		t.Fatal(err)
	}
	r := f.call("candidate", tools.CandidateArgs{Candidate: id})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	got := decodeList[tools.Detail](t, r)
	es, err := f.st.Enrichments(t.Context(), id)
	if err != nil || len(got.EnrichmentResults) != len(es) || len(es) != 2 {
		t.Fatal(got, err)
	}
	for i, e := range es {
		var w website.Result
		_ = json.Unmarshal(e.Found, &w)
		want := tools.EnrichmentResult{Kind: e.Kind, Source: e.Source, Inputs: e.Inputs, State: w.State, URL: w.URL, Title: w.Title, StatusCode: w.StatusCode, Size: &w.Size, Error: w.Error, Domain: e.Source, Cost: e.Cost, At: stamp(e.At)}
		a, _ := json.Marshal(got.EnrichmentResults[i])
		b, _ := json.Marshal(want)
		if string(a) != string(b) {
			t.Fatal(string(a), string(b))
		}
	}
	if got.EnrichmentResults[0].State != sources.WebsiteNone || got.EnrichmentResults[1].State != sources.WebsiteHTTPS {
		t.Fatal(got.EnrichmentResults)
	}
}

// R-SV30-JZ1Y R-UZO7-ZHHN
func TestToolsAllowIndependentPassLanes(t *testing.T) {
	t.Run("ingest", func(t *testing.T) {
		f := fixtureWithScrapes(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
		if _, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: caller.UserID}); err != nil {
			t.Fatal(err)
		}
		r := f.call("ingest", tools.NoArgs{})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		if p := decodeList[passes.Record](t, r); p.Kind != store.KindIngest || p.Status != store.StatusRunning {
			t.Fatal(p)
		}
	})
	t.Run("enrich", func(t *testing.T) {
		f := fixtureWithWebsite(t, nil, &website.Checker{Client: &http.Client{Transport: websiteTransport(func(*http.Request) (*http.Response, error) { t.Error("empty pass fetched"); return nil, io.EOF })}, After: immediateWebsite})
		if _, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: caller.UserID}); err != nil {
			t.Fatal(err)
		}
		r := f.call("enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		p := decodeList[passes.Record](t, r)
		if p.Kind != store.KindEnrich || p.Status != store.StatusRunning {
			t.Fatal(p)
		}
		if done := f.waitEnrich(p.ID); done.Status != store.StatusDone {
			t.Fatal(done)
		}
	})
}

// R-UZO7-ZHHN
func TestEnrichDerivedAndAllWithoutChecker(t *testing.T) {
	for _, kind := range []string{enrichers.KindDriveMinutes, enrichers.KindLinkFacts, ""} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			f.seed(1)
			r := f.call("enrich", tools.EnrichArgs{Kind: kind})
			if r.IsError() {
				t.Fatal(decode(t, r))
			}
			p := decodeList[passes.Record](t, r)
			if p.Kind != store.KindEnrich || p.Status != store.StatusRunning || p.User != caller.UserID {
				t.Fatal(p)
			}
			done := f.waitEnrich(p.ID)
			if done.Status != store.StatusDone || done.Due != 0 {
				t.Fatal(done)
			}
			if kind == enrichers.KindDriveMinutes && done.Derived != 1 {
				t.Fatal(done)
			}
		})
	}
	f := newFixture(t)
	r := f.call("enrich", map[string]any{})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	p := decodeList[passes.Record](t, r)
	if p.Kind != store.KindEnrich || p.Status != store.StatusRunning {
		t.Fatal(p)
	}
	if f.waitEnrich(p.ID).Status != store.StatusDone {
		t.Fatal(p)
	}
}

// R-KT7C-9O87
func TestCandidateHistoryIncludesDerivedKinds(t *testing.T) {
	f := newFixture(t)
	id := f.seed(1)[0]
	for _, state := range []string{sources.WebsiteHTTPS, sources.WebsiteNone} {
		if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: "business.test", Inputs: "fixture", Found: encodedJSON(website.Result{State: state})}}); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(time.Second)
	}
	if _, _, err := f.st.Derive(t.Context(), enrichers.KindLinkFacts, "", false, 100); err != nil {
		t.Fatal(err)
	}
	r := f.call("candidate", tools.CandidateArgs{Candidate: id})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	got := decodeList[tools.Detail](t, r)
	if len(got.EnrichmentResults) != 3 || got.EnrichmentResults[0].Kind != enrichers.KindLinkFacts || len(got.EnrichmentResults[0].Found) == 0 || got.EnrichmentResults[0].Size != nil || got.EnrichmentResults[1].State != sources.WebsiteNone || got.EnrichmentResults[2].State != sources.WebsiteHTTPS {
		t.Fatal(got.EnrichmentResults)
	}
}
