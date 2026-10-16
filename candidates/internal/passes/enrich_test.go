package passes_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

type enrichTransport func(*http.Request) (*http.Response, error)

func (fn enrichTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
func pageResponse(r *http.Request) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<title>fixture title</title>")), Request: r}
}
func fastAfter(d time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	if d == website.Interval {
		c <- time.Time{}
	}
	return c
}
func (f *fixture) checker(fn enrichTransport) {
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em, Website: &website.Checker{Client: &http.Client{Transport: fn}, After: fastAfter}})
}
func seedEmails(t *testing.T, f *fixture, n int, email func(int) string) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for offset := 0; offset < n; offset += passes.BatchSize {
		batch := make([]store.Observation, 0, passes.BatchSize)
		for i := offset; i < n && i < offset+passes.BatchSize; i++ {
			batch = append(batch, store.Observation{Scraper: "fmcsa", SourceID: fmt.Sprint(i), Digest: fmt.Sprint(i), Reading: sources.Reading{Name: fmt.Sprintf("Business %d", i), Email: email(i)}})
		}
		_, cs, err := f.st.Attach(context.Background(), "run_seed", false, batch)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, cs...)
	}
	return ids
}
func (f *fixture) enrich(o *int64) store.Pass {
	f.t.Helper()
	p, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner", Email: "owner@private.test", RequestID: "enrich-request"}, o)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.ended(p.ID)
}
func waitFollow(t *testing.T, f *fixture, id string) store.Pass {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		for _, p := range f.list() {
			if p.Trigger == id {
				return f.ended(p.ID)
			}
		}
		select {
		case <-deadline.C:
			t.Fatal("missing follow-on")
		default:
			runtime.Gosched()
		}
	}
}

// R-YMBG-CJ2H R-IQTS-AOZJ R-IS1O-OGQ8 R-HI03-PTKO R-HJ80-3LBD R-SCNW-OD58 R-S541-PPF0 R-RWKR-1B85 R-RXSN-F2YU R-RZ0J-SUPJ
func TestEnrichCountsDedupAndFollowOn(t *testing.T) {
	f := newFixture(t, nil, false)
	var mu sync.Mutex
	requests := map[string]int{}
	f.checker(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests[r.URL.String()]++
		mu.Unlock()
		if strings.TrimPrefix(r.URL.Host, "www.") == "gone.test" {
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
		}
		return pageResponse(r), nil
	})
	ids := seedEmails(t, f, 5, func(i int) string {
		return []string{"one@acme.test", "two@acme.test", "three@gone.test", "user@gmail.com", ""}[i]
	})
	p := f.enrich(nil)
	if p.Status != store.StatusDone || p.Due != 3 || p.Fetched != 2 || p.HTTPS != 2 || p.HTTP != 0 || p.None != 1 || p.Rows+p.New+p.Changed+p.Unchanged+p.Gone+p.Created+p.Linked+p.Judged+p.Unranged != 0 {
		t.Fatal(p)
	}
	for _, id := range ids {
		c, err := f.st.GetCandidate(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		es, err := f.st.Enrichments(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		dom := website.Domain(c.Email)
		if dom == "" {
			if len(es) != 0 {
				t.Fatal(es)
			}
			continue
		}
		if len(es) != 1 || es[0].Kind != sources.WebsiteCheck || es[0].Source != dom || es[0].Cost != 0 || es[0].At != p.Started {
			t.Fatal(es)
		}
		if dom == "acme.test" && (es[0].State != sources.WebsiteHTTPS || es[0].URL != "https://acme.test" || es[0].Title != "fixture title" || es[0].StatusCode != 200 || es[0].Size <= 0 || es[0].Error != "") {
			t.Fatal(es)
		}
	}
	j := waitFollow(t, f, p.ID)
	if j.Kind != store.KindJudge || j.Origin != store.OriginPass || j.UserID != "" || j.UserEmail != "" || j.RequestID != p.RequestID || j.Judged != 5 {
		t.Fatal(j)
	}
	f.flush()
	es := f.ec.Events()
	finished := -1
	for i, event := range es {
		if event.Attrs["pass"] == p.ID && event.Name == trail.Finished {
			finished = i
		}
		if event.Attrs["pass"] == j.ID && (finished < 0 || i <= finished || event.User != "" || event.RequestID != p.RequestID || event.Cause != "" || event.Depth != 0) {
			t.Fatal(es)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if requests["https://acme.test"] != 1 || len(requests) != 5 {
		t.Fatal(requests)
	}
}

// R-IS1O-OGQ8 R-IT9L-28GX R-IVPD-TRYB
func TestEnrichAcrossBatches(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			f := newFixture(t, nil, false)
			var mu sync.Mutex
			calls := 0
			f.checker(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				if !same {
					return nil, fmt.Errorf("fixture offline")
				}
				return pageResponse(r), nil
			})
			n := 2*passes.BatchSize + 1
			ids := seedEmails(t, f, n, func(i int) string {
				if same {
					return "one@shared.test"
				}
				return fmt.Sprintf("one@domain-%d.test", i)
			})
			zero := int64(0)
			p := f.enrich(&zero)
			if p.Status != store.StatusDone || p.Due != int64(n) {
				t.Fatal(p)
			}
			wantCalls := 4 * n
			if same {
				wantCalls = 1
			}
			mu.Lock()
			gotCalls := calls
			mu.Unlock()
			if gotCalls != wantCalls {
				t.Fatal(gotCalls, wantCalls)
			}
			for _, id := range ids {
				es, err := f.st.Enrichments(context.Background(), id)
				if err != nil || len(es) != 1 {
					t.Fatal(id, es, err)
				}
			}
			waitFollow(t, f, p.ID)
			second := f.enrich(nil)
			if second.Status != store.StatusDone || second.Due+second.Fetched+second.HTTPS+second.HTTP+second.None != 0 {
				t.Fatal(second)
			}
			mu.Lock()
			defer mu.Unlock()
			if calls != gotCalls {
				t.Fatal(calls)
			}
		})
	}
}

// R-RFI5-OIUF R-RHXY-G2BT R-RKDR-7LT7
func TestEnrichRefusals(t *testing.T) {
	for _, mode := range []string{"unavailable", "invalid", "busy", "store", "stopping"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, nil, false)
			f.checker(func(r *http.Request) (*http.Response, error) {
				t.Error("refused pass fetched")
				return pageResponse(r), nil
			})
			negative := int64(-1)
			var age *int64
			switch mode {
			case "unavailable":
				f.eng = passes.New(passes.Config{Store: f.st})
				age = &negative
			case "invalid":
				age = &negative
			case "busy":
				if _, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: "owner"}); err != nil {
					t.Fatal(err)
				}
			case "store":
				f.d.SetFailing(true)
			case "stopping":
				f.eng.Drain(context.Background())
				age = &negative
			}
			before := []store.Pass(nil)
			if mode != "store" {
				before = f.list()
			}
			p, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, age)
			if mode == "store" {
				var refusal *passes.Refusal
				if p.ID != "" || !errors.Is(err, store.ErrUnreachable) || errors.As(err, &refusal) {
					t.Fatal(p, err)
				}
				f.d.SetFailing(false)
			} else {
				assertRefusal(t, p, err, mode)
				if len(f.list()) != len(before) {
					t.Fatal(f.list())
				}
			}
			f.flush()
			if len(f.tc.Events())+len(f.ec.Events()) != 0 {
				t.Fatal("refusal emitted")
			}
		})
	}
}

// R-RLLN-LDJW R-RMTJ-Z5AL R-IY56-LBFP R-Q3XB-GX8X R-S08G-6MG8
func TestEnrichPersistsCompletedChecksAndDrain(t *testing.T) {
	f := newFixture(t, nil, false)
	held := make(chan struct{})
	aDone := make(chan struct{})
	var once sync.Once
	f.checker(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "b.test" {
			once.Do(func() { close(held) })
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		close(aDone)
		return pageResponse(r), nil
	})
	ids := seedEmails(t, f, 2, func(i int) string {
		if i == 0 {
			return "x@a.test"
		}
		return "x@b.test"
	})
	ctx, cancel := context.WithCancel(context.Background())
	p, err := f.eng.Enrich(ctx, identity.Caller{UserID: "owner"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	waitSignal(t, held)
	waitSignal(t, aDone)
	var first string
	for _, id := range ids {
		c, _ := f.st.GetCandidate(context.Background(), id)
		if c.Email == "x@a.test" {
			first = id
		}
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		es, err := f.st.Enrichments(context.Background(), first)
		if err != nil {
			t.Fatal(err)
		}
		if len(es) == 1 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("completed result not written")
		default:
			runtime.Gosched()
		}
	}
	running, err := f.st.GetPass(context.Background(), p.ID)
	if err != nil || running.Status != store.StatusRunning {
		t.Fatal(running, err)
	}
	f.eng.Drain(context.Background())
	end := f.ended(p.ID)
	if end.Status != store.StatusFailed || end.Reason != store.ReasonInterrupted || end.Due+end.Fetched+end.HTTPS+end.HTTP+end.None != 0 {
		t.Fatal(end)
	}
	es, err := f.st.Enrichments(context.Background(), first)
	if err != nil || len(es) != 1 {
		t.Fatal(es, err)
	}
	p, err = f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, nil)
	assertRefusal(t, p, err, passes.RefusalStopping)
}

// R-RVCU-NJHG R-RXSN-F2YU R-RZ0J-SUPJ
func TestIngestStartsEnrichThenJudgeWithLineage(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/record") {
			record(w, "run_a", 0, "done")
		}
	}, true)
	f.checker(func(r *http.Request) (*http.Response, error) { return pageResponse(r), nil })
	seedEmails(t, f, 1, func(int) string { return "owner@acme.test" })
	if out := f.delivery("event-chain", "run_a"); out != events.OK() {
		t.Fatal(out)
	}
	var ing store.Pass
	for _, p := range f.list() {
		if p.Kind == store.KindIngest {
			ing = f.ended(p.ID)
		}
	}
	en := waitFollow(t, f, ing.ID)
	ju := waitFollow(t, f, en.ID)
	if en.Kind != store.KindEnrich || en.Origin != store.OriginPass || ju.Kind != store.KindJudge {
		t.Fatal(en, ju)
	}
	f.flush()
	for _, event := range f.ec.Events() {
		if event.Attrs["pass"] == en.ID || event.Attrs["pass"] == ju.ID {
			if event.User != "" || event.RequestID != "delivery_request" || event.Cause != "evt_0101010101010101" || event.Depth != 3 {
				t.Fatal(event)
			}
		}
	}
	for _, p := range f.list() {
		if p.Trigger == ju.ID {
			t.Fatal("judge spawned pass", p)
		}
	}
}

// R-IWXA-7JP0
func TestEnrichRefreshAges(t *testing.T) {
	f := newFixture(t, nil, false)
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return now }, Rand: &random{}})
	ids := seedEmails(t, f, 4, func(i int) string { return fmt.Sprintf("x@age-%d.test", i) })
	for i, id := range ids {
		age := []int{89, 91, 29, 31}[i]
		state := sources.WebsiteHTTPS
		if i >= 2 {
			state = sources.WebsiteNone
		}
		at := now
		now = now.Add(-time.Duration(age) * 24 * time.Hour)
		_, err := f.st.AddEnrichments(context.Background(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: fmt.Sprintf("age-%d.test", i), State: state}})
		now = at
		if err != nil {
			t.Fatal(err)
		}
	}
	f.checker(func(r *http.Request) (*http.Response, error) { return pageResponse(r), nil })
	p := f.enrich(nil)
	if p.Due != 2 || p.Fetched != 2 || p.HTTPS != 2 {
		t.Fatal(p)
	}
	waitFollow(t, f, p.ID)
	zero := int64(0)
	p = f.enrich(&zero)
	if p.Due != 4 || p.Fetched != 4 || p.HTTPS != 4 {
		t.Fatal(p)
	}
}

// R-Q6D4-8GQB
func TestEnrichMemoryBound(t *testing.T) {
	for _, n := range []int{passes.BatchSize, 20 * passes.BatchSize} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			f := newFixture(t, nil, false)
			_ = seedEmails(t, f, n, func(i int) string { return fmt.Sprintf("x@heap-%d.test", i) })
			var mu sync.Mutex
			count := 0
			var peak uint64
			f.checker(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				count++
				sample := count == 1 || count%passes.BatchSize == 0
				mu.Unlock()
				if sample {
					runtime.GC()
					var stats runtime.MemStats
					runtime.ReadMemStats(&stats)
					mu.Lock()
					if stats.HeapInuse > peak {
						peak = stats.HeapInuse
					}
					mu.Unlock()
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<title>" + strings.Repeat("x", 960) + "</title>")), Request: r}, nil
			})
			runtime.GC()
			p, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			p = f.endedWithin(p.ID, 120*time.Second)
			if p.Status != store.StatusDone || p.Due != int64(n) {
				t.Fatal(p)
			}
			mu.Lock()
			defer mu.Unlock()
			if peak >= 6291456 {
				t.Fatalf("heap in use %d exceeds bound", peak)
			}
		})
	}
}

// R-S2O8-Y5XM R-RGQ2-2AL4 R-RHXY-G2BT R-RLLN-LDJW
func TestConcurrentEnrichStartsAndIndependentLanes(t *testing.T) {
	f := newFixture(t, nil, true)
	held := make(chan struct{})
	var once sync.Once
	f.checker(func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(held) })
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	seedEmails(t, f, 1, func(int) string { return "x@held.test" })
	rb, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	started, busy := 0, 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, nil)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				if p.Kind != store.KindEnrich || p.Status != store.StatusRunning || p.Origin != store.OriginEnrich || p.Trigger != store.TriggerEnrich {
					t.Errorf("wrong pass %v", p)
				}
				started++
			} else {
				assertRefusal(t, p, err, passes.RefusalBusy)
				busy++
			}
		}()
	}
	wg.Wait()
	waitSignal(t, held)
	if started != 1 || busy != 11 {
		t.Fatal(started, busy)
	}
	r, err := f.st.GetPass(context.Background(), rb.ID)
	if err != nil || r.Status != store.StatusRunning {
		t.Fatal(r, err)
	}
	if _, err = f.st.FailPass(context.Background(), rb.ID, store.ReasonInterrupted); err != nil {
		t.Fatal(err)
	}
	// The enrich lane does not block rebuild admission; absent scrapes can
	// fail the rebuild's background work, but its start must be admitted.
	p, err := f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner"})
	if err != nil || p.Kind != store.KindRebuild {
		t.Fatal(p, err)
	}
	f.eng.Drain(context.Background())
}

// R-HJ80-3LBD R-SCNW-OD58 R-RWKR-1B85
func TestEnrichSkipsMergedCandidateAndBlockedFollowOn(t *testing.T) {
	f := newFixture(t, nil, false)
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.checker(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "b.test" {
			once.Do(func() { close(held) })
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return pageResponse(r), nil
	})
	ids := seedEmails(t, f, 3, func(i int) string { return []string{"x@a.test", "x@b.test", "x@gmail.com"}[i] })
	var merging, into string
	for _, id := range ids {
		c, _ := f.st.GetCandidate(context.Background(), id)
		if c.Email == "x@b.test" {
			merging = id
		}
		if c.Email == "x@gmail.com" {
			into = id
		}
	}
	blocker, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, held)
	_, _, err = f.st.Decide(context.Background(), store.Ruling{Kind: store.KindMerge, Candidate: into, Other: merging, UserID: "owner", Why: "fixture merge"})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	p = f.ended(p.ID)
	if p.Due != 2 || p.HTTPS+p.HTTP+p.None != 1 || p.Fetched != 2 {
		t.Fatal(p)
	}
	// Complete a subsequent admitted pass under the same blocked judge lane.
	zero := int64(0)
	second := f.enrich(&zero)
	if second.Status != store.StatusDone {
		t.Fatal(second)
	}
	for _, v := range f.list() {
		if v.Trigger == p.ID {
			t.Fatal("blocked follow-on recorded", v)
		}
	}
	f.eng.Drain(context.Background())
	if _, err = f.st.FailPass(context.Background(), blocker.ID, store.ReasonInterrupted); err != nil {
		t.Fatal(err)
	}
}

// R-Q557-UOZM
func TestEnrichDistinctSuccessfulBatchesWithinBudget(t *testing.T) {
	f := newFixture(t, nil, false)
	f.checker(func(r *http.Request) (*http.Response, error) { return pageResponse(r), nil })
	n := 2*passes.BatchSize + 1
	seedEmails(t, f, n, func(i int) string { return fmt.Sprintf("x@success-%d.test", i) })
	began := time.Now()
	p, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p = f.endedWithin(p.ID, 120*time.Second)
	elapsed := time.Since(began)
	if p.Status != store.StatusDone || p.Due != int64(n) || p.Fetched != int64(n) || p.HTTPS != int64(n) || elapsed > 120*time.Second {
		t.Fatal(p, elapsed)
	}
}

// R-RVCU-NJHG
func TestIngestSkipsFollowOnWhileEnrichRuns(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/record") {
			record(w, "run_a", 0, "done")
		}
	}, true)
	held := make(chan struct{})
	var once sync.Once
	f.checker(func(r *http.Request) (*http.Response, error) {
		once.Do(func() { close(held) })
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	seedEmails(t, f, 1, func(int) string { return "x@held.test" })
	en, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, held)
	if out := f.delivery("event-blocked", "run_a"); out != events.OK() {
		t.Fatal(out)
	}
	var ing store.Pass
	for _, p := range f.list() {
		if p.Kind == store.KindIngest {
			ing = f.ended(p.ID)
		}
	}
	// Complete a subsequent delivery while the enrich lane is still held.
	if out := f.eng.Deliver(context.Background(), events.Delivery{Event: events.Event{ID: "judge-after-ingest", Name: trail.AcceptedJudge}}); out != events.OK() {
		t.Fatal(out)
	}
	for _, p := range f.list() {
		if p.Delivery == "judge-after-ingest" {
			f.ended(p.ID)
		}
	}
	if ing.Status != store.StatusDone {
		t.Fatal(ing)
	}
	for _, p := range f.list() {
		if p.Trigger == ing.ID {
			t.Fatal("blocked enrich follow-on", p)
		}
	}
	current, err := f.st.GetPass(context.Background(), en.ID)
	if err != nil || current.Status != store.StatusRunning {
		t.Fatal(current, err)
	}
	f.eng.Drain(context.Background())
}
