package runs_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	scrapes "github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/fmcsa"
	"github.com/ikigenba/ikigenba/scrapes/internal/runs"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/trail"
)

type random struct {
	mu sync.Mutex
	n  byte
}

func (r *random) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	for i := range p {
		p[i] = r.n
	}
	return len(p), nil
}

var _ io.Reader = (*random)(nil)

type fake struct {
	name   string
	scrape func(context.Context, func([]source.Row) error) error
}

func (f fake) Name() string                                                 { return f.name }
func (f fake) Scrape(ctx context.Context, b func([]source.Row) error) error { return f.scrape(ctx, b) }

type fixture struct {
	st *store.Store
	d  *db.DB
	e  *runs.Engine
	tc *telemetry.Capture
	ec *events.Capture
	w  *telemetry.Writer
	em *events.Emitter
}

func setup(t *testing.T, list ...source.Scraper) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	now := func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state", "scrapes.db"), Migrations: scrapes.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	tc := new(telemetry.Capture)
	ec := new(events.Capture)
	w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: tc, Now: now, Rand: new(random)})
	em := events.New(events.Config{Service: "scrapes", Sink: ec, Now: now, Rand: new(random), Emits: trail.Emits()})
	st := store.New(d, store.Config{Now: now, Rand: new(random)})
	e := runs.New(runs.Config{Store: st, Scrapers: scrapers.New(list...), Telemetry: w, Events: em})
	f := &fixture{st, d, e, tc, ec, w, em}
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		e.Drain(ctx)
		em.Shutdown(ctx)
		w.Shutdown(ctx, "test")
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}
func (f *fixture) flush(t *testing.T) {
	t.Helper()
	ctx, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	if err := f.em.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
func ended(t *testing.T, st *store.Store, id string) store.Run {
	t.Helper()
	ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
	defer c()
	for {
		r, err := st.GetRun(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != store.StatusRunning {
			return r
		}
		select {
		case <-ctx.Done():
			t.Fatal("run did not end")
		default:
			runtime.Gosched()
		}
	}
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("deadline")
		var zero T
		return zero
	}
}
func caller() identity.Caller {
	return identity.Caller{UserID: "tester", Email: "tester@example.invalid", RequestID: "req-test"}
}
func refused(t *testing.T, f *fixture, name, word string) {
	t.Helper()
	before, err := f.st.ListRuns(context.Background(), "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	f.flush(t)
	nt, ne := len(f.tc.Events()), len(f.ec.Events())
	r, err := f.e.Start(context.Background(), caller(), name)
	var rf *runs.Refusal
	if r != (store.Run{}) || !errors.As(err, &rf) || rf.Word != word {
		t.Fatalf("refusal %#v %v", r, err)
	}
	after, err := f.st.ListRuns(context.Background(), "", 0, 100)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("refusal changed store")
	}
	f.flush(t)
	if len(f.tc.Events()) != nt || len(f.ec.Events()) != ne {
		t.Fatal("refusal event")
	}
}

// R-1N2Z-J4T9
func TestWords(t *testing.T) {
	for _, w := range []string{runs.RefusalNotFound, runs.RefusalBusy, runs.RefusalNoRun, runs.RefusalStopping, runs.RefusalUnavailable} {
		if (&runs.Refusal{Word: w}).Error() != w {
			t.Fatal(w)
		}
	}
	if !reflect.DeepEqual([]string{runs.RefusalNotFound, runs.RefusalBusy, runs.RefusalNoRun, runs.RefusalStopping, runs.RefusalUnavailable}, []string{"not_found", "busy", "no_run", "stopping", "unavailable"}) || runs.TriggerPrefix != "scrape_" || runs.Accepts != "cron.*.fired" || runs.Stopping == "" || strings.Contains(runs.Stopping, "\n") {
		t.Fatal("constants")
	}
}

// R-1OAV-WWJY R-1PIS-AOAN R-1QQO-OG1C
func TestRecord(t *testing.T) {
	start := time.Date(2025, 2, 3, 4, 5, 6, 0, time.FixedZone("offset", 3600))
	r := store.Run{ID: "run-test", Scraper: "fake", Status: store.StatusRunning, Started: start, Rows: 5, New: 1, Changed: 2, Unchanged: 2, Gone: 4, UserID: "user", UserEmail: "email"}
	v := runs.RecordOf(r)
	want := runs.Record{"run-test", "fake", "running", "", "2025-02-03T03:05:06Z", "", 5, 1, 2, 2, 4, "user", "email", ""}
	if v != want {
		t.Fatal(v)
	}
	b, err := json.Marshal(v)
	if err != nil || string(b) != `{"id":"run-test","scraper":"fake","status":"running","started":"2025-02-03T03:05:06Z","rows":5,"new":1,"changed":2,"unchanged":2,"gone":4,"user":"user","email":"email"}` {
		t.Fatalf("%s %v", b, err)
	}
	r.Trigger = "scrape_fake"
	r.Status = store.StatusFailed
	r.Reason = store.ReasonSource
	r.Finished = start.Add(time.Hour)
	v = runs.RecordOf(r)
	if v.User != "" || v.Email != "" || v.Trigger != r.Trigger || v.Finished != "2025-02-03T04:05:06Z" || v.Reason != r.Reason {
		t.Fatal(v)
	}
	b, err = json.Marshal(v)
	if err != nil || string(b) != `{"id":"run-test","scraper":"fake","status":"failed","reason":"source","started":"2025-02-03T03:05:06Z","finished":"2025-02-03T04:05:06Z","rows":5,"new":1,"changed":2,"unchanged":2,"gone":4,"trigger":"scrape_fake"}` {
		t.Fatalf("%s %v", b, err)
	}
}

// R-1LV3-5D2K R-1RYL-27S1 R-1T6H-FZIQ R-1WU6-LAQT R-6KVA-BKX8 R-6M36-PCNX R-6IFH-K1FU
func TestStartAndBatches(t *testing.T) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	sc := fake{"fake", func(ctx context.Context, b func([]source.Row) error) error {
		entered <- ctx
		<-release
		for _, rows := range [][]source.Row{{{SourceID: "a", Digest: "a"}, {SourceID: "b", Digest: "b"}}, {{SourceID: "c", Digest: "c"}, {SourceID: "d", Digest: "d"}, {SourceID: "e", Digest: "e"}}} {
			if err := b(rows); err != nil {
				return err
			}
		}
		return nil
	}}
	f := setup(t, sc)
	refused(t, f, "unknown", runs.RefusalNotFound)
	ctx, cancel := context.WithCancel(events.NewContext(context.Background(), events.Cause{ID: "evt_0101010101010101", Depth: 2}))
	r, err := f.e.Start(ctx, caller(), "fake")
	if err != nil || r.Status != store.StatusRunning || r.UserID != caller().UserID || r.UserEmail != caller().Email || r.RequestID != caller().RequestID || r.Trigger != "" || r.Delivery != "" {
		t.Fatalf("%#v %v", r, err)
	}
	work := receive(t, entered)
	cancel()
	if work.Err() != nil {
		t.Fatal("work cancelled")
	}
	wc, _ := identity.FromContext(work)
	cause, _ := events.FromContext(work)
	if wc != caller() || cause.ID != "evt_0101010101010101" || cause.Depth != 2 {
		t.Fatal(wc, cause)
	}
	refused(t, f, "fake", runs.RefusalBusy)
	f.flush(t)
	if len(f.tc.Events()) != 1 || len(f.ec.Events()) != 1 || f.ec.Events()[0].Name != trail.Started {
		t.Fatal("start not emitted before return")
	}
	close(release)
	done := ended(t, f.st, r.ID)
	if done.Status != store.StatusDone || done.Rows != 5 || done.New != 5 || done.Changed != 0 || done.Unchanged != 0 || done.Gone != 0 || done.Finished.IsZero() {
		t.Fatal(done)
	}
	f.e.Drain(context.Background())
	f.flush(t)
	ts, es := f.tc.Events(), f.ec.Events()
	if len(ts) != 2 || len(es) != 2 || es[1].Name != trail.Finished || es[1].Attrs["rows"] != int64(5) || es[1].Cause != cause.ID || es[1].Depth != 3 {
		t.Fatal(ts, es)
	}
	refused(t, f, "fake", runs.RefusalStopping)
	refused(t, f, "unknown", runs.RefusalNotFound)
}

// R-1VMA-7J04
func TestDatabaseStartFailure(t *testing.T) {
	called := false
	f := setup(t, fake{"fake", func(context.Context, func([]source.Row) error) error { called = true; return nil }})
	f.d.SetFailing(true)
	r, err := f.e.Start(context.Background(), caller(), "fake")
	var refusal *runs.Refusal
	if r != (store.Run{}) || !errors.Is(err, store.ErrUnreachable) || errors.As(err, &refusal) || called {
		t.Fatal(r, err, called)
	}
	f.d.SetFailing(false)
	f.flush(t)
	if len(f.ec.Events()) != 0 || len(f.tc.Events()) != 0 {
		t.Fatal("events for failure")
	}
}

// R-6NB3-34EM R-21PS-4DPL
func TestFailures(t *testing.T) {
	for _, mode := range []string{"source", "store", "ending"} {
		t.Run(mode, func(t *testing.T) {
			var f *fixture
			released := make(chan struct{})
			batchResult := make(chan error, 1)
			f = setup(t, fake{"fake", func(_ context.Context, b func([]source.Row) error) error {
				if mode == "store" {
					f.d.SetFailing(true)
				}
				err := b([]source.Row{{SourceID: "one"}})
				f.d.SetFailing(false)
				batchResult <- err
				<-released
				if mode == "ending" {
					f.d.SetFailing(true)
					return nil
				}
				return errors.New("walk failed")
			}})
			r, err := f.e.Start(context.Background(), caller(), "fake")
			if err != nil {
				t.Fatal(err)
			}
			batcherr := receive(t, batchResult)
			if (mode == "store") != (batcherr != nil) {
				t.Fatal(batcherr)
			}
			close(released)
			if mode == "ending" {
				// Waiting for Drain also proves an unrecorded ending releases the work.
				f.e.Drain(context.Background())
				f.d.SetFailing(false)
				got, err := f.st.GetRun(context.Background(), r.ID)
				if err != nil || got.Status != store.StatusRunning {
					t.Fatal(got, err)
				}
				f.flush(t)
				if len(f.ec.Events()) != 1 || len(f.tc.Events()) != 1 {
					t.Fatal(f.ec.Events())
				}
				return
			}
			done := ended(t, f.st, r.ID)
			if done.Status != store.StatusFailed || done.Reason != mode || done.Rows != 0 {
				t.Fatal(done)
			}
			if _, err := f.st.GetRow(context.Background(), r.ID, "one"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal(err)
			}
		})
	}
}

// R-245K-VX6Z R-25DH-9OXO R-26LD-NGOD R-27TA-18F2 R-2916-F05R R-2A92-SRWG
func TestDelivery(t *testing.T) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	f := setup(t, fake{"fake", func(ctx context.Context, _ func([]source.Row) error) error { entered <- ctx; <-release; return nil }})
	h := f.e.Handlers()
	if len(h) != 1 || h[runs.Accepts] == nil {
		t.Fatal(h)
	}
	for _, name := range []string{"cron.scrape_bogus.fired", "cron.nightly.fired", "cron.scrape-fake.fired", "cron.scrape_fake.created", "cron.scrape_fake.more.fired", "other.scrape_fake.fired"} {
		if got := h[runs.Accepts](context.Background(), events.Delivery{Event: events.Event{ID: "evt_0101010101010101", Name: name}}); got != events.Skip() {
			t.Fatal(name, got)
		}
	}
	if f.e.Deliver(context.Background(), events.Delivery{Event: events.Event{Name: "cron.scrape_fake.fired"}}) != events.Skip() {
		t.Fatal("empty delivery accepted")
	}
	ctx, cancel := context.WithCancel(identity.NewContext(events.NewContext(context.Background(), events.Cause{ID: "evt_0101010101010101", Depth: 4}), caller()))
	cancel()
	d := events.Delivery{Event: events.Event{ID: "evt_0101010101010101", Name: "cron.scrape_fake.fired", User: "owner", RequestID: "bus-request"}, Attempt: 1}
	if f.e.Deliver(ctx, d) != events.OK() {
		t.Fatal("delivery refused")
	}
	work := receive(t, entered)
	if work.Err() != nil {
		t.Fatal("work cancelled")
	}
	c, _ := identity.FromContext(work)
	cause, _ := events.FromContext(work)
	if c.UserID != caller().UserID || c.RequestID != caller().RequestID || c.Email != "" || cause.Depth != 4 {
		t.Fatal(c, cause)
	}
	rr, err := f.st.RunningRun(context.Background(), "fake")
	if err != nil || rr.Trigger != "scrape_fake" || rr.Delivery != d.Event.ID || rr.UserEmail != "" || rr.RequestID != caller().RequestID {
		t.Fatal(rr, err)
	}
	if f.e.Deliver(ctx, d) != events.Skip() {
		t.Fatal("redelivery running")
	}
	other := d
	other.Event.ID = "evt_0202020202020202"
	if f.e.Deliver(ctx, other) != events.Skip() {
		t.Fatal("busy")
	}
	close(release)
	ended(t, f.st, rr.ID)
	if f.e.Deliver(ctx, d) != events.Skip() {
		t.Fatal("redelivery ended")
	}
	f.d.SetFailing(true)
	if f.e.Deliver(ctx, other) != events.Fail(store.Unreachable) {
		t.Fatal("database outcome")
	}
	f.d.SetFailing(false)
	f.e.Drain(context.Background())
	if f.e.Deliver(ctx, events.Delivery{}) != events.Fail(runs.Stopping) {
		t.Fatal("stopping precedence")
	}
	f.flush(t)
	if len(f.ec.Events()) != 2 {
		t.Fatal(f.ec.Events())
	}
}

// R-6OIZ-GW5B
func TestRecover(t *testing.T) {
	f := setup(t)
	for _, name := range []string{"first", "second"} {
		r, err := f.st.AddRun(context.Background(), store.Run{Scraper: name, UserID: "old", RequestID: "old-request"})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.st.PutRows(context.Background(), r.ID, []source.Row{{SourceID: "old-row"}}); err != nil {
			t.Fatal(err)
		}
	}
	ctx := events.NewContext(context.Background(), events.Cause{ID: "evt_0101010101010101", Depth: 3})
	if err := f.e.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := f.st.ListRuns(context.Background(), "", 0, 10)
	if err != nil || len(list) != 2 {
		t.Fatal(err, list)
	}
	for _, r := range list {
		if r.Status != store.StatusFailed || r.Reason != store.ReasonInterrupted {
			t.Fatal(r)
		}
		if _, err := f.st.GetRow(ctx, r.ID, "old-row"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
	}
	f.flush(t)
	if len(f.ec.Events()) != 2 || len(f.tc.Events()) != 2 {
		t.Fatal("recover events")
	}
	for _, ev := range f.ec.Events() {
		if ev.Name != trail.Finished || ev.User != "old" || ev.RequestID != "old-request" || ev.Cause != "" || ev.Depth != 0 {
			t.Fatal(ev)
		}
	}
	f.d.SetFailing(true)
	if !errors.Is(f.e.Recover(context.Background()), store.ErrUnreachable) {
		t.Fatal("recover failure")
	}
	f.d.SetFailing(false)
}

// R-2DWR-Y34J R-2F4O-BUV8
func TestDrainCancellation(t *testing.T) {
	entered := make(chan struct{})
	f := setup(t, fake{"fake", func(ctx context.Context, b func([]source.Row) error) error {
		if err := b([]source.Row{{SourceID: "one"}}); err != nil {
			return err
		}
		close(entered)
		<-ctx.Done()
		return nil
	}})
	r, err := f.e.Start(context.Background(), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	receive(t, entered)
	drained := make(chan struct{})
	go func() { f.e.Drain(context.Background()); close(drained) }()
	receive(t, drained)
	got, err := f.st.GetRun(context.Background(), r.ID)
	if err != nil || got.Status != store.StatusFailed || got.Reason != store.ReasonInterrupted {
		t.Fatal(got, err)
	}
	if _, err := f.st.GetRow(context.Background(), r.ID, "one"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	refused(t, f, "fake", runs.RefusalStopping)
}

type observedContext struct {
	context.Context
	seen chan struct{}
	once sync.Once
}

func (c *observedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.seen) })
	return c.Context.Done()
}

// R-2F4O-BUV8
func TestDrainDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	f := setup(t, fake{"fake", func(context.Context, func([]source.Row) error) error { close(entered); <-release; return nil }})
	r, err := f.e.Start(context.Background(), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	receive(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	observed := &observedContext{Context: ctx, seen: make(chan struct{})}
	go func() { f.e.Drain(observed); close(returned) }()
	receive(t, observed.seen)
	select {
	case <-returned:
		t.Fatal("drain returned while work still active")
	default:
	}
	cancel()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("drain missed deadline")
	}
	stored, err := f.st.GetRun(context.Background(), r.ID)
	if err != nil || stored.Status != store.StatusFailed || stored.Reason != store.ReasonInterrupted {
		t.Fatal(stored, err)
	}
	close(release)
	ended(t, f.st, r.ID)
}

// R-2GCK-PMLX
func TestConcurrentMethods(t *testing.T) {
	gate := make(chan struct{})
	f := setup(t, fake{"fake", func(ctx context.Context, _ func([]source.Row) error) error {
		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}, fake{"other", func(ctx context.Context, _ func([]source.Row) error) error { <-ctx.Done(); return ctx.Err() }})
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Go(func() { _, _ = f.e.Start(context.Background(), caller(), "fake") })
		wg.Go(func() {
			_ = f.e.Deliver(context.Background(), events.Delivery{Event: events.Event{ID: "evt_0101010101010101", Name: "cron.scrape_fake.fired"}})
		})
	}
	wg.Wait()
	list, err := f.st.ListRuns(context.Background(), "fake", 0, 100)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	wg.Go(func() { _ = f.e.Recover(context.Background()) })
	wg.Go(func() { _, _ = f.e.Start(context.Background(), caller(), "other") })
	wg.Go(func() { f.e.Drain(context.Background()) })
	wg.Go(func() { _ = f.e.Deliver(context.Background(), events.Delivery{}) })
	close(gate)
	wg.Wait()
	f.e.Drain(context.Background())
}

// R-6KVA-BKX8 R-6M36-PCNX R-6IFH-K1FU
func TestBaselineAndLifecycle(t *testing.T) {
	count := 0
	observed := make(chan struct{})
	release := make(chan struct{})
	f := setup(t, fake{"fake", func(_ context.Context, b func([]source.Row) error) error {
		count++
		if err := b([]source.Row{{SourceID: "one", Digest: "same"}, {SourceID: "two", Digest: "new"}}); err != nil {
			return err
		}
		if err := b([]source.Row{{SourceID: "three", Digest: "added"}}); err != nil {
			return err
		}
		close(observed)
		<-release
		return nil
	}})
	base, err := f.st.AddRun(context.Background(), store.Run{Scraper: "fake", UserID: "baseline"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.PutRows(context.Background(), base.ID, []source.Row{{SourceID: "one", Digest: "same"}, {SourceID: "two", Digest: "old"}, {SourceID: "gone", Digest: "gone"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.FinishRun(context.Background(), base.ID); err != nil {
		t.Fatal(err)
	}
	cause := events.Cause{ID: "evt_0101010101010101", Depth: 2}
	r, err := f.e.Start(events.NewContext(context.Background(), cause), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	receive(t, observed)
	close(release)
	done := ended(t, f.st, r.ID)
	f.e.Drain(context.Background())
	f.flush(t)
	if count != 1 || done.Status != store.StatusDone || done.Rows != 3 || done.New != 1 || done.Changed != 1 || done.Unchanged != 1 || done.Gone != 1 || done.Finished != time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) {
		t.Fatal(done, count)
	}
	for i, run := range []store.Run{r, done} {
		name := trail.Started
		attrs := telemetry.Attrs{"scraper": run.Scraper, "run": run.ID}
		if i == 1 {
			name = trail.Finished
			attrs["status"] = run.Status
			attrs["reason"] = run.Reason
			attrs["rows"] = run.Rows
			attrs["new"] = run.New
			attrs["changed"] = run.Changed
			attrs["unchanged"] = run.Unchanged
			attrs["gone"] = run.Gone
		}
		ts, es := f.tc.Events(), f.ec.Events()
		if len(ts) != 2 || len(es) != 2 {
			t.Fatal(ts, es)
		}
		if ts[i].Name != name || es[i].Name != name || !reflect.DeepEqual(ts[i].Attrs, attrs) || !reflect.DeepEqual(es[i].Attrs, events.Attrs(attrs)) || ts[i].RequestID != r.RequestID || es[i].RequestID != r.RequestID || ts[i].User != r.UserID || es[i].User != r.UserID || es[i].Cause != cause.ID || es[i].Depth != cause.Depth+1 {
			t.Fatal(ts[i], es[i])
		}
	}
}

// R-6KVA-BKX8
func TestBatchStoredBeforeReturn(t *testing.T) {
	observed := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	f := setup(t, fake{"fake", func(_ context.Context, b func([]source.Row) error) error {
		calls++
		for _, rows := range [][]source.Row{{{SourceID: "one"}, {SourceID: "two"}}, {{SourceID: "three"}, {SourceID: "four"}, {SourceID: "five"}}} {
			if err := b(rows); err != nil {
				return err
			}
		}
		close(observed)
		<-release
		return nil
	}})
	r, err := f.e.Start(context.Background(), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	receive(t, observed)
	done, err := f.st.FinishRun(context.Background(), r.ID)
	if err != nil || done.Rows != 5 {
		t.Fatal(done, err)
	}
	for _, id := range []string{"one", "two", "three", "four", "five"} {
		if _, err := f.st.GetRow(context.Background(), r.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	close(release)
	f.e.Drain(context.Background())
	if calls != 1 {
		t.Fatal(calls)
	}
}

// R-6NB3-34EM
func TestInterruptedPrecedence(t *testing.T) {
	for _, batchFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "batch"}[batchFailure], func(t *testing.T) {
			var f *fixture
			entered := make(chan struct{})
			f = setup(t, fake{"fake", func(ctx context.Context, b func([]source.Row) error) error {
				if batchFailure {
					f.d.SetFailing(true)
				}
				err := b([]source.Row{{SourceID: "one"}})
				f.d.SetFailing(false)
				if batchFailure && err == nil {
					panic("accepted failing batch")
				}
				close(entered)
				<-ctx.Done()
				return errors.New("source failure")
			}})
			r, err := f.e.Start(context.Background(), caller(), "fake")
			if err != nil {
				t.Fatal(err)
			}
			receive(t, entered)
			f.e.Drain(context.Background())
			done, err := f.st.GetRun(context.Background(), r.ID)
			if err != nil || done.Status != store.StatusFailed || done.Reason != store.ReasonInterrupted {
				t.Fatal(done, err)
			}
		})
	}
}

// R-6NB3-34EM
func TestFMCSASourceFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	sc := fmcsa.New(fmcsa.Config{HTTP: srv.Client(), BaseURL: srv.URL, Now: func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }, Sleep: func(context.Context, time.Duration) {}, States: []string{"MN"}, Status: "A"})
	f := setup(t, sc)
	r, err := f.e.Start(context.Background(), caller(), fmcsa.Name)
	if err != nil {
		t.Fatal(err)
	}
	done := ended(t, f.st, r.ID)
	if done.Status != store.StatusFailed || done.Reason != store.ReasonSource {
		t.Fatal(done)
	}
}

// R-1RYL-27S1 R-1T6H-FZIQ R-1VMA-7J04 R-26LD-NGOD R-27TA-18F2 R-2916-F05R
func TestRefusedCallsHaveNoEffects(t *testing.T) {
	var calls atomic.Int64
	started := make(chan struct{}, 2)
	f := setup(t, fake{"fake", func(ctx context.Context, _ func([]source.Row) error) error {
		calls.Add(1)
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}})
	check := func(action func()) {
		t.Helper()
		before, err := f.st.ListRuns(context.Background(), "", 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		f.flush(t)
		nt, ne, nc := len(f.tc.Events()), len(f.ec.Events()), calls.Load()
		action()
		after, err := f.st.ListRuns(context.Background(), "", 0, 100)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("refusal changed records", err)
		}
		f.flush(t)
		if len(f.tc.Events()) != nt || len(f.ec.Events()) != ne || calls.Load() != nc {
			t.Fatal("refusal triggered work or events")
		}
	}
	check(func() { refused(t, f, "unknown", runs.RefusalNotFound) })
	for _, d := range []events.Delivery{{}, {Event: events.Event{ID: "evt_0303030303030303", Name: "cron.scrape_bogus.fired"}}, {Event: events.Event{Name: "cron.scrape_fake.fired"}}} {
		check(func() {
			if f.e.Deliver(context.Background(), d) != events.Skip() {
				t.Fatal("not skipped")
			}
		})
	}
	check(func() {
		f.d.SetFailing(true)
		r, err := f.e.Start(context.Background(), caller(), "fake")
		var rf *runs.Refusal
		if r != (store.Run{}) || !errors.Is(err, store.ErrUnreachable) || errors.As(err, &rf) {
			t.Fatal(r, err)
		}
		if f.e.Deliver(context.Background(), events.Delivery{Event: events.Event{ID: "evt_0101010101010101", Name: "cron.scrape_fake.fired"}}) != events.Fail(store.Unreachable) {
			t.Fatal("not failed")
		}
		f.d.SetFailing(false)
	})
	d := events.Delivery{Event: events.Event{ID: "evt_0101010101010101", Name: "cron.scrape_fake.fired"}}
	if f.e.Deliver(context.Background(), d) != events.OK() {
		t.Fatal("start")
	}
	receive(t, started)
	check(func() { refused(t, f, "fake", runs.RefusalBusy) })
	check(func() {
		if f.e.Deliver(context.Background(), d) != events.Skip() {
			t.Fatal("redelivery")
		}
		other := d
		other.Event.ID = "evt_0202020202020202"
		if f.e.Deliver(context.Background(), other) != events.Skip() {
			t.Fatal("busy delivery")
		}
	})
	f.e.Drain(context.Background())
	check(func() {
		refused(t, f, "fake", runs.RefusalStopping)
		refused(t, f, "unknown", runs.RefusalNotFound)
		if f.e.Deliver(context.Background(), events.Delivery{}) != events.Fail(runs.Stopping) {
			t.Fatal("stopping")
		}
	})
}

// R-6KVA-BKX8
func TestFirstBatchStoredBeforeSecond(t *testing.T) {
	first := make(chan struct{})
	next := make(chan struct{})
	second := make(chan error, 1)
	f := setup(t, fake{"fake", func(_ context.Context, b func([]source.Row) error) error {
		if err := b([]source.Row{{SourceID: "first-one"}, {SourceID: "first-two"}}); err != nil {
			return err
		}
		close(first)
		<-next
		err := b([]source.Row{{SourceID: "second"}})
		second <- err
		return err
	}})
	r, err := f.e.Start(context.Background(), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	receive(t, first)
	snapshot, err := f.st.FinishRun(context.Background(), r.ID)
	if err != nil || snapshot.Rows != 2 {
		t.Fatal(snapshot, err)
	}
	for _, id := range []string{"first-one", "first-two"} {
		if _, err := f.st.GetRow(context.Background(), r.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	close(next)
	if receive(t, second) == nil {
		t.Fatal("batch of ended run accepted")
	}
	f.e.Drain(context.Background())
}
