package passes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func searchTargets(t *testing.T, f *fixture, n int) []string {
	t.Helper()
	observations := make([]store.Observation, n)
	for i := range n {
		observations[i] = store.Observation{Scraper: "fmcsa", SourceID: fmt.Sprint(i), Digest: fmt.Sprint(i), Reading: sources.Reading{Name: fmt.Sprintf("Search Company %d", i), NameKey: fmt.Sprintf("search-company-%d", i)}}
	}
	_, ids, err := f.st.Attach(t.Context(), "search-seed", false, observations)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}
func searchAfter(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	if d == passes.PollInterval || d == passes.QueueWait {
		ch <- time.Time{}
	}
	return ch
}

// R-33P6-601K R-2NUH-6ZEJ R-38KR-P30C R-301H-0OTH
func TestWebsiteSearchTargetResultsAndConfirmation(t *testing.T) {
	f := newFixture(t, nil, false)
	ids := searchTargets(t, f, 6)
	ctx := context.Background()
	known, _ := json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: "https://known.test"})
	if _, err := f.st.AddEnrichments(ctx, []store.Enrichment{{Candidate: ids[5], Kind: sources.WebsiteCheck, Inputs: "known.test", Source: "fixture", Found: known}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.JudgeAll(ctx); err != nil {
		t.Fatal(err)
	}
	co, err := f.st.CreateCohort(ctx, store.Cohort{Name: "search targets", Size: 6, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]json.RawMessage{}
	for _, id := range ids {
		s, err := f.st.Subject(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		inputs[id] = enrichers.WebsiteSearch{}.Input(s)
	}
	var mu sync.Mutex
	fetched := []string{}
	checker := &website.Checker{After: searchAfter, Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		fetched = append(fetched, r.URL.String())
		mu.Unlock()
		if r.URL.Host == "broken.test" {
			return nil, fmt.Errorf("fixture failure")
		}
		return pageResponse(r), nil
	})}}
	pc, fake := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
		result, refused, code := promptAnswer(name, args)
		if name == "run" {
			var input struct {
				Candidate string `json:"candidate"`
			}
			_ = json.Unmarshal(args["input"], &input)
			var got, want any
			_ = json.Unmarshal(args["input"], &got)
			_ = json.Unmarshal(inputs[input.Candidate], &want)
			if !reflect.DeepEqual(got, want) {
				t.Error(got, want)
			}
			return map[string]any{"id": input.Candidate, "status": enrichers.RunQueued}, false, 0
		}
		if name == "result" {
			var id string
			_ = json.Unmarshal(args["run"], &id)
			r := result.(map[string]any)
			r["id"] = id
			r["stdout"] = `{"url":"https://confirmed.test/","why":"matches","rejected":[]}`
			switch id {
			case ids[1]:
				r["stdout"] = `{"url":"","why":"no match","rejected":[]}`
			case ids[2]:
				r["stdout"] = `{"url":"https://broken.test/","why":"matches","rejected":[]}`
			case ids[3]:
				r["stdout"] = "malformed"
			case ids[4]:
				r["exit_code"] = 1
			case ids[5]:
				t.Error("known website searched")
			}
		}
		return result, refused, code
	})
	f.eng.Drain(ctx)
	f.eng = passes.New(passes.Config{Store: f.st, Prompts: pc, Website: checker, Telemetry: f.w, Events: f.em, After: searchAfter})
	caller := identity.Caller{UserID: "owner", Email: "owner@example.test", RequestID: "search-request"}
	p, err := f.eng.EnrichCohort(ctx, caller, enrichers.KindWebsiteSearch, co.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.Due != 5 || p.Runs != 5 || p.RunsFailed != 2 || p.Pages != 0 || p.Cost != 5000 || p.Derived+p.Fetched+p.HTTPS+p.HTTP+p.None+p.Rows+p.New+p.Changed+p.Unchanged+p.Gone+p.Created+p.Linked+p.Judged+p.Unranged != 0 {
		t.Fatal(p)
	}
	mu.Lock()
	gotFetch := append([]string{}, fetched...)
	mu.Unlock()
	if len(gotFetch) != 2 {
		t.Fatal(gotFetch)
	}
	fetchCounts := map[string]int{}
	for _, u := range gotFetch {
		fetchCounts[u]++
	}
	if fetchCounts["https://confirmed.test/"] != 1 || fetchCounts["https://broken.test/"] != 1 {
		t.Fatal(gotFetch)
	}
	for i, id := range ids {
		es, err := f.st.Enrichments(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, e := range es {
			if e.Kind != enrichers.KindWebsiteSearch {
				continue
			}
			count++
			var v enrichers.WebsiteSearchFound
			if err := json.Unmarshal(e.Found, &v); err != nil {
				t.Fatal(err)
			}
			if v.Run.ID != id || v.Run.CostNanos != 1000 || v.Failed() != (i == 3 || i == 4) || e.Cost != 0.000001 || e.Source != enrichers.SourcePrompts {
				t.Fatal(e, v)
			}
			if i == 0 && (v.Confirmation == nil || v.Confirmation.URL != "https://confirmed.test/" || v.Confirmation.State != sources.WebsiteHTTPS) {
				t.Fatal(v)
			}
		}
		want := 1
		if i == 5 {
			want = 0
		}
		if count != want {
			t.Fatal(id, count, want)
		}
		pages, err := f.st.CountPages(ctx, id)
		if err != nil || pages.Copies != 0 {
			t.Fatal(pages, err)
		}
	}
	facts, err := f.st.GetFacts(ctx, ids[0])
	if err != nil || facts.WebsiteURL != "https://confirmed.test/" || facts.WebsiteSource != store.WebsiteBySearch {
		t.Fatal(facts, err)
	}
	for _, c := range fake.snapshot() {
		if c.user != caller.UserID || c.email != caller.Email || c.request != caller.RequestID {
			t.Fatal(c)
		}
	}
	for _, other := range f.list() {
		if other.Trigger == p.ID {
			t.Fatal("unexpected search follow-on", other)
		}
	}
}

// R-3B0K-GMHQ
func TestWebsiteSearchDrainCancelsConfirmation(t *testing.T) {
	f := newFixture(t, nil, false)
	id := searchTargets(t, f, 1)[0]
	arrived, cancelled := make(chan struct{}), make(chan struct{})
	checker := &website.Checker{After: searchAfter, Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		close(arrived)
		<-r.Context().Done()
		close(cancelled)
		return nil, r.Context().Err()
	})}}
	pc, _ := fakePrompts(t, f, func(name string, args map[string]json.RawMessage) (any, bool, int) {
		r, b, c := promptAnswer(name, args)
		if name == "result" {
			r.(map[string]any)["stdout"] = `{"url":"https://held.test/","why":"match","rejected":[]}`
		}
		return r, b, c
	})
	f.eng.Drain(t.Context())
	f.eng = passes.New(passes.Config{Store: f.st, Prompts: pc, Website: checker, Telemetry: f.w, Events: f.em, After: searchAfter})
	p, err := f.eng.EnrichCohort(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindWebsiteSearch, id, false)
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, arrived)
	f.eng.Drain(context.Background())
	waitSignal(t, cancelled)
	p = f.ended(p.ID)
	if p.Status != store.StatusFailed || p.Reason != store.ReasonInterrupted {
		t.Fatal(p)
	}
	es, err := f.st.Enrichments(t.Context(), id)
	if err != nil || len(es) != 0 {
		t.Fatal(es, err)
	}
	due, err := f.st.DueAmong(t.Context(), enrichers.KindWebsiteSearch, []string{id}, false)
	if err != nil || len(due) != 1 {
		t.Fatal(due, err)
	}
}

// R-39SO-2UR1
func TestWebsiteSearchNeverRunsOverCorpus(t *testing.T) {
	f := newFixture(t, nil, false)
	id := searchTargets(t, f, 1)[0]
	pc, fake := fakePrompts(t, f, promptAnswer)
	checker := &website.Checker{After: searchAfter, Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		t.Error("unexpected fetch")
		return pageResponse(r), nil
	})}}
	f.eng.Drain(t.Context())
	f.eng = passes.New(passes.Config{Store: f.st, Prompts: pc, Website: checker, Telemetry: f.w, Events: f.em, After: searchAfter})
	p, err := f.eng.Enrich(t.Context(), identity.Caller{UserID: "owner"}, enrichers.KindWebsiteSearch)
	assertRefusal(t, p, err, passes.RefusalInvalid)
	f.flush()
	if len(f.list()) != 0 || len(fake.snapshot()) != 0 || len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
		t.Fatal("refusal side effects")
	}
	p, err = f.eng.Enrich(t.Context(), identity.Caller{UserID: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone {
		t.Fatal(p)
	}
	waitFollow(t, f, p.ID)
	if len(fake.snapshot()) != 0 {
		t.Fatal(fake.snapshot())
	}
	es, err := f.st.Enrichments(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if e.Kind == enrichers.KindWebsiteSearch {
			t.Fatal(e)
		}
	}
}
