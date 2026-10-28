package passes_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

type evidenceSource struct{}

func (evidenceSource) Name() string { return "fmcsa" }
func (evidenceSource) Read(r scrapes.Row) sources.Reading {
	return sources.Reading{Name: r.Name, Email: "owner@held.test", NameKey: sources.NameKey(r.Name), Filed: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Evidence: []sources.Evidence{{Name: sources.EvidenceFMCSAActive, Source: "fmcsa", Weight: 1, Description: "fixture"}}}
}
func judgeDelivery(id string) events.Delivery {
	return events.Delivery{Event: events.Event{Name: trail.AcceptedJudge, ID: id, Attrs: events.Attrs{"anything": true}}}
}

// R-ZRW3-1A2O R-2UPL-VAID R-3QEZ-TBSF R-TV9E-3V5P R-TQDS-KS6X
func TestJudgeSeparatesIngestFromJudgments(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/runs/run_a/record" {
			record(w, "run_a", 2, "done")
			return
		}
		row(w, 1)
		row(w, 2)
	}, true)
	f.eng.Drain(context.Background())
	var clock atomic.Int64
	clock.Store(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC).Unix())
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return time.Unix(clock.Load(), 0) }, Rand: &random{}})
	src := evidenceSource{}
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(src), Scrapes: f.sc, Telemetry: f.w, Events: f.em})
	_, ids, err := f.st.Attach(context.Background(), "seed", false, []store.Observation{{Scraper: "fmcsa", SourceID: "1", Digest: "seed", Reading: src.Read(scrapes.Row{Name: "Business 1"})}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.GetJudgments(context.Background(), ids[0]); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	held := make(chan struct{})
	var once sync.Once
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(src), Scrapes: f.sc, Telemetry: f.w, Events: f.em, Website: &website.Checker{Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(held) })
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}, After: fastAfter}})
	if _, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, ""); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, held)
	judge := func(id string) store.Pass {
		t.Helper()
		before := len(f.requests())
		if out := f.eng.Deliver(context.Background(), judgeDelivery(id)); out != events.OK() {
			t.Fatal(out)
		}
		var p store.Pass
		for _, v := range f.list() {
			if v.Delivery == id {
				p = v
			}
		}
		p = f.ended(p.ID)
		if p.Kind != store.KindJudge || p.Origin != store.OriginCron || p.Trigger != store.TriggerJudge || p.Status != store.StatusDone || p.Rows != 0 || p.New != 0 || p.Changed != 0 || p.Unchanged != 0 || p.Gone != 0 || p.Created != 0 || p.Linked != 0 || p.Derived+p.Due+p.Fetched+p.HTTPS+p.HTTP+p.None+p.Runs+p.RunsFailed+p.Pages+p.Cost != 0 || len(f.requests()) != before {
			t.Fatal(p, f.requests())
		}
		if f.eng.Deliver(context.Background(), judgeDelivery(id)) != events.Skip() {
			t.Fatal("judge redelivery")
		}
		return p
	}
	first := judge("first-fire")
	a, err := f.st.GetJudgments(context.Background(), ids[0])
	if err != nil || len(a.Evidence) != 1 || a.Evidence[0].Name != sources.EvidenceFMCSAActive || first.Judged != 1 || a.Calculated.Before(first.Started) || a.Calculated.After(first.Finished) {
		t.Fatal(a, first, err)
	}
	clock.Store(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).Unix())
	if out := f.delivery("ingest", "run_a"); out != events.OK() {
		t.Fatal(out)
	}
	var ip store.Pass
	for _, p := range f.list() {
		if p.Delivery == "ingest" {
			ip = p
		}
	}
	ip = f.ended(ip.ID)
	if ip.Kind != store.KindIngest || ip.Origin != store.OriginRun || ip.Judged != 0 || ip.Unranged != 0 {
		t.Fatal(ip)
	}
	after, err := f.st.GetJudgments(context.Background(), ids[0])
	if err != nil || !reflect.DeepEqual(a, after) {
		t.Fatal(after, a, err)
	}
	cs, err := f.st.ListCandidates(context.Background(), store.CandidateFilter{}, "", "", 10)
	if err != nil || len(cs) != 2 {
		t.Fatal(cs, err)
	}
	for _, c := range cs {
		if c.ID != ids[0] {
			if _, err := f.st.GetJudgments(context.Background(), c.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatal(err)
			}
		}
	}
	second := judge("second-fire")
	if second.Judged != 2 {
		t.Fatal(second)
	}
	for _, c := range cs {
		a, err := f.st.GetJudgments(context.Background(), c.ID)
		if err != nil || len(a.Evidence) != 0 || !a.Calculated.Equal(second.Started) {
			t.Fatal(a, err)
		}
	}
}

// R-2UPL-VAID
func TestJudgeNeedsNoScrapesAndHonorsRefusals(t *testing.T) {
	f := newFixture(t, nil, false)
	if out := f.eng.Deliver(context.Background(), judgeDelivery("nil-client")); out != events.OK() {
		t.Fatal(out)
	}
	p := f.ended(f.list()[0].ID)
	if p.Kind != store.KindJudge || p.Judged != 0 || p.Status != store.StatusDone {
		t.Fatal(p)
	}
	if out := f.eng.Handlers()[trail.AcceptedJudge](context.Background(), judgeDelivery("nil-client")); out != events.Skip() {
		t.Fatal(out)
	}
	f.d.SetFailing(true)
	if out := f.eng.Deliver(context.Background(), judgeDelivery("failing")); out != events.Fail(store.Unreachable) {
		t.Fatal(out)
	}
	f.d.SetFailing(false)
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("repeat-%d", i)
		if out := f.eng.Deliver(context.Background(), judgeDelivery(id)); out != events.OK() {
			t.Fatal(out)
		}
		for _, p := range f.list() {
			if p.Delivery == id {
				f.ended(p.ID)
			}
		}
	}
	if len(f.list()) != 3 {
		t.Fatal(f.list())
	}
	_, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindJudge, Origin: store.OriginCron, Trigger: store.TriggerJudge, Delivery: "held-judge"})
	if err != nil {
		t.Fatal(err)
	}
	if out := f.eng.Deliver(context.Background(), judgeDelivery("busy")); out != events.Fail(passes.Busy) {
		t.Fatal(out)
	}
	f.flush()
	if len(f.ec.Events()) != 6 {
		t.Fatal(f.ec.Events())
	}
}

// R-ZH16-3I6T R-373Q-6C3X
func TestDrainDeadlineAfterEndingHasAlreadyBlocked(t *testing.T) {
	f := newFixture(t, nil, false)
	f.eng.Drain(context.Background())
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var calls atomic.Int64
	f.st = store.New(f.d, store.Config{Now: func() time.Time {
		if calls.Add(1) == 2 {
			close(entered)
			<-release
		}
		return time.Unix(100000, 0)
	}, Rand: &random{}})
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(evidenceSource{}), Telemetry: f.w, Events: f.em})
	if out := f.eng.Deliver(context.Background(), judgeDelivery("held-ending")); out != events.OK() {
		t.Fatal(out)
	}
	waitSignal(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { f.eng.Drain(ctx); close(done) }()
	select {
	case <-done:
		t.Fatal("drain returned while ending held")
	default:
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("drain failed to respect deadline while ending blocked")
	}
	once.Do(func() { close(release) })
	f.eng.Drain(context.Background())
	ps := f.list()
	if len(ps) != 1 || ps[0].Status != store.StatusFailed || ps[0].Reason != store.ReasonInterrupted || failedCounts(ps[0]) != (store.Counts{}) {
		t.Fatal(ps)
	}
	f.flush()
	es := f.ec.Events()
	if len(es) != 2 || es[1].Name != trail.Finished || es[1].Attrs["status"] != store.StatusFailed || es[1].Attrs["reason"] != store.ReasonInterrupted {
		t.Fatal(es)
	}
}
