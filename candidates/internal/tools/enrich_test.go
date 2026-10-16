package tools_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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

// R-V895-LDYX R-VBWU-QP70 R-VVF8-V124 R-VSZG-3HKQ R-VJ89-1BN6
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
			if string(info.InputSchema) != `{"type":"object","properties":{"kind":{"type":"string"},"older_than_days":{"type":"integer"}},"required":["kind"],"additionalProperties":false}` {
				t.Fatal(string(info.InputSchema))
			}
		}
	}
	if !found {
		t.Fatal("missing enrich")
	}
	for _, args := range []map[string]any{{"kind": sources.WebsiteCheck, "older_than_days": 1.5}, {"kind": sources.WebsiteCheck, "older_than_days": 1e30}, {"kind": sources.WebsiteCheck, "cohort": "chosen"}} {
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
	minus := -1
	for _, fail := range []bool{true, false} {
		f.d.SetFailing(fail)
		if !fail {
			f.eng.Drain(context.Background())
		}
		for _, args := range []tools.EnrichArgs{{Kind: ""}, {Kind: strings.ToUpper(sources.WebsiteCheck)}, {Kind: sources.WebsiteCheck, OlderThanDays: &minus}} {
			refusal(t, f.call("enrich", args), passes.RefusalInvalid, tools.RefuseInvalid)
		}
	}
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 0 {
		t.Fatal(ps, err)
	}
}

// R-VU7C-H9BF R-VXV1-MKJI
func TestEnrichDispatchAndExplicitZeroAge(t *testing.T) {
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
	ids := f.seed(1)
	f.now = f.now.Add(-24 * time.Hour)
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: sources.WebsiteCheck, State: sources.WebsiteHTTPS, Source: "business.test"}}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(24 * time.Hour)
	history, err := f.st.Enrichments(t.Context(), ids[0])
	if err != nil || len(history) != 1 || !history[0].At.Equal(f.now.Add(-24*time.Hour)) {
		t.Fatal(history, err)
	}
	r := f.call("enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	record := decodeList[passes.Record](t, r)
	if record.Kind != store.KindEnrich || record.Origin != store.OriginEnrich || record.Trigger != store.TriggerEnrich || record.Status != store.StatusRunning || record.User != caller.UserID {
		t.Fatal(record)
	}
	if p := f.waitEnrich(record.ID); p.Status != store.StatusDone || p.Due != 0 {
		t.Fatal(p)
	}
	zero := 0
	r = f.call("enrich", tools.EnrichArgs{Kind: sources.WebsiteCheck, OlderThanDays: &zero})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	record = decodeList[passes.Record](t, r)
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

// R-VQJN-BY3C R-VMVY-6MV9
func TestCandidateEnrichmentHistory(t *testing.T) {
	f := newFixture(t)
	id := f.seed(1)[0]
	first := store.Enrichment{Candidate: id, Kind: sources.WebsiteCheck, State: sources.WebsiteHTTPS, URL: "https://business.test", Title: "chosen", StatusCode: 200, Size: 8, Source: "business.test"}
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{first}); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, State: sources.WebsiteNone, Source: "business.test"}}); err != nil {
		t.Fatal(err)
	}
	r := f.call("candidate", tools.CandidateArgs{Candidate: id})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	got := decodeList[tools.Detail](t, r)
	es, err := f.st.Enrichments(t.Context(), id)
	if err != nil || len(got.Enrichments) != len(es) || len(es) != 2 {
		t.Fatal(got, err)
	}
	for i, e := range es {
		want := tools.Enrichment{Kind: e.Kind, State: e.State, URL: e.URL, Title: e.Title, StatusCode: e.StatusCode, Size: e.Size, Error: e.Error, Domain: e.Source, Cost: e.Cost, At: stamp(e.At)}
		a, _ := json.Marshal(got.Enrichments[i])
		b, _ := json.Marshal(want)
		if string(a) != string(b) {
			t.Fatal(string(a), string(b))
		}
	}
	if got.Enrichments[0].State != sources.WebsiteNone || got.Enrichments[1].State != sources.WebsiteHTTPS {
		t.Fatal(got.Enrichments)
	}
}

// R-8YDY-8SNX R-VU7C-H9BF
func TestToolsAllowIndependentPassLanes(t *testing.T) {
	t.Run("rebuild", func(t *testing.T) {
		f := fixtureWithScrapes(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
		if _, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: caller.UserID}); err != nil {
			t.Fatal(err)
		}
		r := f.call("rebuild", tools.NoArgs{})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		if p := decodeList[passes.Record](t, r); p.Kind != store.KindRebuild || p.Status != store.StatusRunning {
			t.Fatal(p)
		}
	})
	t.Run("enrich", func(t *testing.T) {
		f := fixtureWithWebsite(t, nil, &website.Checker{Client: &http.Client{Transport: websiteTransport(func(*http.Request) (*http.Response, error) { t.Error("empty pass fetched"); return nil, io.EOF })}, After: immediateWebsite})
		if _, err := f.st.AddPass(t.Context(), store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: caller.UserID}); err != nil {
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
