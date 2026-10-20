package runs_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	st  *store.Store
	d   *db.DB
	e   *runs.Engine
	tc  *telemetry.Capture
	ec  *events.Capture
	w   *telemetry.Writer
	em  *events.Emitter
	dir string
}

func setup(t *testing.T, list ...source.Scraper) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	now := func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }
	dir := t.TempDir()
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "scrapes.db"), Migrations: scrapes.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	tc := new(telemetry.Capture)
	ec := new(events.Capture)
	w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: tc, Now: now, Rand: new(random)})
	em := events.New(events.Config{Service: "scrapes", Sink: ec, Now: now, Rand: new(random), Emits: trail.Emits()})
	st := store.New(d, store.Config{Now: now, Rand: new(random), Runs: filepath.Join(dir, "runs")})
	e := runs.New(runs.Config{Store: st, Scrapers: scrapers.New(list...), Telemetry: w, Events: em, Now: now})
	f := &fixture{st, d, e, tc, ec, w, em, filepath.Join(dir, "runs")}
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
	before, err := f.st.ListRunsAfter(context.Background(), "", "", "", 100)
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
	after, err := f.st.ListRunsAfter(context.Background(), "", "", "", 100)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("refusal changed store")
	}
	f.flush(t)
	if len(f.tc.Events()) != nt || len(f.ec.Events()) != ne {
		t.Fatal("refusal event")
	}
}

// R-QVBF-7VGN R-QWJB-LN7C R-QXR7-ZEY1 R-QYZ4-D6OQ R-R1EX-4Q64
func TestWords(t *testing.T) {
	for _, w := range []string{runs.RefusalNotFound, runs.RefusalBusy, runs.RefusalNoRun, runs.RefusalStopping, runs.RefusalUnavailable, runs.RefusalMissing} {
		if (&runs.Refusal{Word: w}).Error() != w {
			t.Fatal(w)
		}
	}
	if !reflect.DeepEqual([]string{runs.RefusalNotFound, runs.RefusalBusy, runs.RefusalNoRun, runs.RefusalStopping, runs.RefusalUnavailable, runs.RefusalMissing}, []string{"not_found", "busy", "no_run", "stopping", "unavailable", "missing"}) || runs.TriggerPrefix != "scrape_" || runs.Stopping == "" || strings.Contains(runs.Stopping, "\n") {
		t.Fatal("constants")
	}
}

// R-2K2V-CMQA R-R070-QYFF
func TestFiredEvent(t *testing.T) {
	for _, name := range []string{"fmcsa", "other", "", "scrape_fmcsa", "a.b", "*", " name ", "name\n"} {
		if got, want := runs.FiredEvent(name), "cron.scrape_"+name+".fired"; got != want {
			t.Fatalf("FiredEvent(%q) = %q, want %q", name, got, want)
		}
	}
}

// R-2LAR-QEGZ R-QQFT-OSHV
func TestHandlerNames(t *testing.T) {
	for _, names := range [][]string{{}, {"fmcsa"}, {"fmcsa", "other"}, {"fmcsa", "fmcsa", "other"}} {
		var list []source.Scraper
		want := make(map[string]bool)
		for _, name := range names {
			list = append(list, fake{name, func(context.Context, func([]source.Row) error) error { return nil }})
			want[runs.FiredEvent(name)] = true
		}
		f := setup(t, list...)
		h := f.e.Handlers()
		if len(h) != len(want) {
			t.Fatalf("handlers for %v: %v", names, h)
		}
		for name, handler := range h {
			if !want[name] || handler == nil {
				t.Fatalf("unexpected handler %q", name)
			}
			d := events.Delivery{Event: events.Event{ID: "delivery-" + name, Name: name}}
			if got := handler(context.Background(), d); got != events.OK() {
				t.Fatalf("handler %q: %+v", name, got)
			}
			scraper := strings.TrimSuffix(strings.TrimPrefix(name, "cron."+runs.TriggerPrefix), ".fired")
			list, err := f.st.ListRunsAfter(context.Background(), scraper, "", "", 10)
			if err != nil || len(list) != 1 || list[0].Delivery != d.Event.ID || list[0].Trigger != runs.TriggerPrefix+scraper {
				t.Fatalf("handler %q recorded %+v: %v", name, list, err)
			}
			ended(t, f.st, list[0].ID)
			if got := handler(context.Background(), d); got != events.Skip() {
				t.Fatalf("handler %q redelivery: %+v", name, got)
			}
		}
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

//	R-1RYL-27S1 R-1T6H-FZIQ R-1WU6-LAQT   R-6IFH-K1FU
//
// R-1RYL-27S1 R-1T6H-FZIQ R-1WU6-LAQT R-6IFH-K1FU R-P9FX-ZJGZ R-34OH-GEN7 R-QO00-X90H
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
	if len(ts) != 4 || len(es) != 2 || es[1].Name != trail.Finished || es[1].Attrs["rows"] != int64(5) || es[1].Cause != cause.ID || es[1].Depth != 3 {
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

// R-2LAR-QEGZ R-25DH-9OXO R-26LD-NGOD R-27TA-18F2 R-2916-F05R R-2A92-SRWG
func TestDelivery(t *testing.T) {
	for _, viaHandler := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "handler"}[viaHandler], func(t *testing.T) {
			testDelivery(t, viaHandler)
		})
	}
}

func testDelivery(t *testing.T, viaHandler bool) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	f := setup(t, fake{"fake", func(ctx context.Context, _ func([]source.Row) error) error { entered <- ctx; <-release; return nil }})
	deliver := f.e.Deliver
	if viaHandler {
		deliver = f.e.Handlers()[runs.FiredEvent("fake")]
		if deliver == nil {
			t.Fatal("missing scraper handler")
		}
	}
	for _, name := range []string{"cron.scrape_bogus.fired", "cron.nightly.fired", "cron.scrape-fake.fired", "cron.scrape_fake.created", "cron.scrape_fake.more.fired", "other.scrape_fake.fired"} {
		if got := deliver(context.Background(), events.Delivery{Event: events.Event{ID: "evt_0101010101010101", Name: name}}); got != events.Skip() {
			t.Fatal(name, got)
		}
	}
	if deliver(context.Background(), events.Delivery{Event: events.Event{Name: "cron.scrape_fake.fired"}}) != events.Skip() {
		t.Fatal("empty delivery accepted")
	}
	ctx, cancel := context.WithCancel(identity.NewContext(events.NewContext(context.Background(), events.Cause{ID: "evt_0101010101010101", Depth: 4}), caller()))
	cancel()
	d := events.Delivery{Event: events.Event{ID: "evt_0101010101010101", Name: "cron.scrape_fake.fired", User: "owner", RequestID: "bus-request"}, Attempt: 1}
	if deliver(ctx, d) != events.OK() {
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
	if deliver(ctx, d) != events.Skip() {
		t.Fatal("redelivery running")
	}
	other := d
	other.Event.ID = "evt_0202020202020202"
	if deliver(ctx, other) != events.Skip() {
		t.Fatal("busy")
	}
	close(release)
	ended(t, f.st, rr.ID)
	if deliver(ctx, d) != events.Skip() {
		t.Fatal("redelivery ended")
	}
	f.d.SetFailing(true)
	if deliver(ctx, other) != events.Fail(store.Unreachable) {
		t.Fatal("database outcome")
	}
	f.d.SetFailing(false)
	f.e.Drain(context.Background())
	if deliver(ctx, events.Delivery{}) != events.Fail(runs.Stopping) {
		t.Fatal("stopping precedence")
	}
	f.flush(t)
	if len(f.ec.Events()) != 2 {
		t.Fatal(f.ec.Events())
	}
}

// R-S2NL-QI0M R-QSVM-GBZ9
func TestRecover(t *testing.T) {
	f := setup(t)
	for _, name := range []string{"first", "second"} {
		r, err := f.st.AddRun(context.Background(), store.Run{Scraper: name, UserID: "old", RequestID: "old-request"})
		if err != nil {
			t.Fatal(err)
		}
		loadFixture(t, f.st, r.ID, []source.Row{{SourceID: "old-row"}})
		if err := os.WriteFile(filepath.Join(f.dir, r.ID+".part"), []byte("leftover"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	done, err := f.st.AddRun(context.Background(), store.Run{Scraper: "done", UserID: "old"})
	if err != nil {
		t.Fatal(err)
	}
	c := loadFixture(t, f.st, done.ID, []source.Row{{SourceID: "kept"}})
	if _, err = f.st.FinishRun(context.Background(), done.ID, c); err != nil {
		t.Fatal(err)
	}
	failed, err := f.st.AddRun(context.Background(), store.Run{Scraper: "failed", UserID: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.st.FailRun(context.Background(), failed.ID, store.ReasonSource); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{failed.ID + ".db", "run_ffffffffffffffff.db", "foreign"} {
		if err := os.WriteFile(filepath.Join(f.dir, name), []byte("leftover"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(f.dir, "foreign-dir"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx := events.NewContext(context.Background(), events.Cause{ID: "evt_0101010101010101", Depth: 3})
	if err := f.e.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := f.st.ListRunsAfter(context.Background(), "", "", "", 10)
	if err != nil || len(list) != 4 {
		t.Fatal(err, list)
	}
	for _, r := range list {
		if r.ID == done.ID || r.ID == failed.ID {
			continue
		}
		if r.Status != store.StatusFailed || r.Reason != store.ReasonInterrupted {
			t.Fatal(r)
		}
		if _, err := f.st.GetRow(ctx, r.ID, "old-row"); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(f.dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != done.ID+".db" {
		t.Fatal(entries, err)
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

// R-2DWR-Y34J
// R-QU3I-U3PY R-PBVQ-R2YD
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

// R-PJ75-1PEJ
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
	list, err := f.st.ListRunsAfter(context.Background(), "fake", "", "", 100)
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

//	R-6IFH-K1FU
//
// R-34OH-GEN7 R-S1FP-CQ9X
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
	counts := loadFixture(t, f.st, base.ID, []source.Row{{SourceID: "one", Digest: "same"}, {SourceID: "two", Digest: "old"}, {SourceID: "gone", Digest: "gone"}})
	if _, err := f.st.FinishRun(context.Background(), base.ID, counts); err != nil {
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
		if len(ts) != 4 || len(es) != 2 {
			t.Fatal(ts, es)
		}
		if ts[i*3].Name != name || es[i].Name != name || !reflect.DeepEqual(ts[i*3].Attrs, attrs) || !reflect.DeepEqual(es[i].Attrs, events.Attrs(attrs)) || ts[i*3].RequestID != r.RequestID || es[i].RequestID != r.RequestID || ts[i*3].User != r.UserID || es[i].User != r.UserID || es[i].Cause != cause.ID || es[i].Depth != cause.Depth+1 {
			t.Fatal(ts[i], es[i])
		}
	}
}

// R-PBVQ-R2YD
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
		before, err := f.st.ListRunsAfter(context.Background(), "", "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		f.flush(t)
		nt, ne, nc := len(f.tc.Events()), len(f.ec.Events()), calls.Load()
		action()
		after, err := f.st.ListRunsAfter(context.Background(), "", "", "", 100)
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

func loadFixture(t *testing.T, s *store.Store, id string, rows []source.Row) store.Counts {
	t.Helper()
	ctx := context.Background()
	b, err := s.Build(ctx, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Add(ctx, rows); err != nil {
		t.Fatal(err)
	}
	if err = b.Seal(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := b.Load(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// R-P881-LRQA R-PANU-DB7O
func TestFilePrecedesEnding(t *testing.T) {
	f := setup(t)
	s := &fileCheckingStore{Store: f.st, t: t, dir: f.dir}
	e := retryEngine(t, f, s, fake{"fake", func(_ context.Context, b func([]source.Row) error) error { return b([]source.Row{{SourceID: "one"}}) }}, func(context.Context, time.Duration) { t.Error("unbudgeted pause") }, new(diagnosticCapture))
	r, err := e.Start(context.Background(), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	ended(t, f.st, r.ID)
	e.Drain(context.Background())
	entries, err := os.ReadDir(f.dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != r.ID+".db" {
		t.Fatal(entries, err)
	}
}

type fileCheckingStore struct {
	runs.Store
	t   *testing.T
	dir string
}

func (s *fileCheckingStore) FinishRun(ctx context.Context, id string, c store.Counts) (store.Run, error) {
	info, err := os.Stat(filepath.Join(s.dir, id+".db"))
	if err != nil || !info.Mode().IsRegular() {
		s.t.Error("ending precedes file", err)
	}
	return s.Store.FinishRun(ctx, id, c)
}

// R-PBVQ-R2YD R-PHZ8-NXNU R-PFJF-WE6G
func TestBuildFailuresDiagnosed(t *testing.T) {
	for _, op := range []string{"Build", "Add", "Seal", "Load"} {
		t.Run(op, func(t *testing.T) {
			f := setup(t)
			out := new(diagnosticCapture)
			var s runs.Store = f.st
			switch op {
			case "Build":
				s = &retryStore{Store: f.st, op: "Build", failures: 1, err: errors.New("chosen\nerror")}
			case "Load":
				if err := os.MkdirAll(filepath.Join(f.dir, "run_0101010101010101.db"), 0700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(f.dir, []byte("block"), 0600); err != nil {
					t.Fatal(err)
				}
				chunk := int64(0)
				if op == "Add" {
					chunk = 1
				}
				s = store.New(f.d, store.Config{Runs: f.dir, ChunkBytes: chunk, Now: func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }, Rand: new(random)})
			}
			calls := 0
			e := retryEngine(t, f, s, fake{"fake", func(_ context.Context, b func([]source.Row) error) error {
				calls++
				return b([]source.Row{{SourceID: "one", Raw: []byte(`{"private":"secret"}`)}})
			}}, func(context.Context, time.Duration) { t.Error("build retry") }, out)
			r, err := e.Start(context.Background(), caller(), "fake")
			if err != nil {
				t.Fatal(err)
			}
			done := ended(t, f.st, r.ID)
			e.Drain(context.Background())
			if done.Status != store.StatusFailed || done.Reason != store.ReasonStore {
				t.Fatal(done)
			}
			lines := out.snapshot()
			if len(lines) != 1 || !strings.HasPrefix(lines[0], "scrapes: "+r.ID+" "+op+": "+runs.GaveUp+": ") || strings.Contains(lines[0], "secret") {
				t.Fatal(lines)
			}
			if op == "Build" && calls != 0 {
				t.Fatal("walk called without build")
			}
			f.flush(t)
			names := []string{}
			for _, ev := range f.tc.Events() {
				names = append(names, ev.Name)
			}
			want := []string{trail.Started, trail.Finished}
			if op == "Load" {
				want = []string{trail.Started, trail.Walked, trail.Finished}
			}
			if !reflect.DeepEqual(names, want) {
				t.Fatal(names)
			}
		})
	}
}

// R-S3VI-49RB R-S53E-I1I0 R-PD3N-4UP2 R-S1FP-CQ9X
func TestSerializedLoadsAndDrain(t *testing.T) {
	for _, drain := range []bool{false, true} {
		t.Run(map[bool]string{false: "finish", true: "drain"}[drain], func(t *testing.T) {
			f := setup(t)
			out := new(diagnosticCapture)
			secondWalk := make(chan struct{})
			held := make(chan context.Context, 1)
			release := make(chan struct{})
			unexpected := make(chan struct{}, 1)
			walked := func(id string) bool {
				f.flush(t)
				for _, ev := range f.tc.Events() {
					if ev.Name == trail.Walked && ev.RequestID == id {
						return true
					}
				}
				return false
			}
			sleep := func(ctx context.Context, _ time.Duration) {
				c, _ := identity.FromContext(ctx)
				if !walked(c.RequestID) {
					return
				}
				if c.RequestID == "first-request" {
					select {
					case held <- ctx:
					default:
					}
					if drain {
						<-ctx.Done()
					} else {
						<-release
					}
				} else {
					select {
					case unexpected <- struct{}{}:
					default:
					}
				}
			}
			rows := func(_ context.Context, b func([]source.Row) error) error { return b([]source.Row{{SourceID: "one"}}) }
			e := runs.New(runs.Config{Store: f.st, Scrapers: scrapers.New(fake{"first", rows}, fake{"second", func(ctx context.Context, b func([]source.Row) error) error { <-secondWalk; return rows(ctx, b) }}), Telemetry: f.w, Events: f.em, Sleep: sleep, Stderr: out, Now: func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }, Budget: 1})
			first := caller()
			first.RequestID = "first-request"
			second := caller()
			second.RequestID = "second-request"
			a, err := e.Start(context.Background(), first, "first")
			if err != nil {
				t.Fatal(err)
			}
			receive(t, held)
			b, err := e.Start(context.Background(), second, "second")
			if err != nil {
				t.Fatal(err)
			}
			close(secondWalk)
			deadline, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			for !walked(second.RequestID) {
				select {
				case <-deadline.Done():
					t.Fatal("second walk never completed")
				default:
					runtime.Gosched()
				}
			}
			if !drain {
				select {
				case <-unexpected:
					t.Fatal("concurrent load")
				case <-time.After(30 * time.Microsecond):
				}
				close(release)
				ended(t, f.st, a.ID)
				ended(t, f.st, b.ID)
			}
			e.Drain(context.Background())
			for _, r := range []store.Run{a, b} {
				got, err := f.st.GetRun(context.Background(), r.ID)
				if err != nil {
					t.Fatal(err)
				}
				if drain {
					if got.Status != store.StatusFailed || got.Reason != store.ReasonInterrupted {
						t.Fatal(got)
					}
				} else if got.Status != store.StatusDone {
					t.Fatal(got)
				}
			}
			if len(out.snapshot()) != 0 {
				t.Fatal(out.snapshot())
			}
			f.flush(t)
			loaded := []string{}
			for _, ev := range f.tc.Events() {
				if ev.Name == trail.Loaded {
					loaded = append(loaded, ev.RequestID)
				}
			}
			if drain {
				entries, err := os.ReadDir(f.dir)
				if err != nil || len(entries) != 0 || len(loaded) != 0 {
					t.Fatal(entries, loaded, err)
				}
				for _, r := range []store.Run{a, b} {
					trailFinished, busFinished := 0, 0
					for _, ev := range f.tc.Events() {
						if ev.Name == trail.Finished && ev.Attrs["run"] == r.ID {
							trailFinished++
							if ev.Attrs["status"] != store.StatusFailed || ev.Attrs["reason"] != store.ReasonInterrupted {
								t.Fatal(ev)
							}
						}
					}
					for _, ev := range f.ec.Events() {
						if ev.Name == trail.Finished && ev.Attrs["run"] == r.ID {
							busFinished++
							if ev.Attrs["status"] != store.StatusFailed || ev.Attrs["reason"] != store.ReasonInterrupted {
								t.Fatal(ev)
							}
						}
					}
					if trailFinished != 1 || busFinished != 1 {
						t.Fatalf("run %s: finished telemetry %d, bus %d", r.ID, trailFinished, busFinished)
					}
				}
			} else if !reflect.DeepEqual(loaded, []string{first.RequestID, second.RequestID}) {
				t.Fatal(loaded)
			}
		})
	}
}

// R-PD3N-4UP2 R-S1FP-CQ9X
func TestBuildPacingAndPhaseTotals(t *testing.T) {
	for _, mode := range []string{"budget", "disabled", "pressure"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			out := new(diagnosticCapture)
			pauses := []time.Duration{}
			budget := int64(0)
			pressure := ""
			if mode == "budget" {
				budget = 1
			}
			if mode == "pressure" {
				pressure = filepath.Join(t.TempDir(), "pressure")
				if err := os.WriteFile(pressure, []byte("some avg10=99.00\nfull avg10=50.00 avg60=0.00\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			sleep := func(ctx context.Context, d time.Duration) {
				c, ok := identity.FromContext(ctx)
				if !ok || c != caller() {
					t.Error("pause caller", c)
				}
				pauses = append(pauses, d)
				if mode == "pressure" && len(pauses) == 3 {
					if err := os.WriteFile(pressure, []byte("full avg10=0.00\n"), 0600); err != nil {
						t.Error(err)
					}
				}
			}
			e := runs.New(runs.Config{Store: f.st, Scrapers: scrapers.New(fake{"fake", func(_ context.Context, b func([]source.Row) error) error {
				return b([]source.Row{{SourceID: "one", Digest: "value"}})
			}}), Telemetry: f.w, Events: f.em, Sleep: sleep, Stderr: out, Now: func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }, Budget: budget, Pressure: pressure, PressureLimit: 10})
			r, err := e.Start(context.Background(), caller(), "fake")
			if err != nil {
				t.Fatal(err)
			}
			ended(t, f.st, r.ID)
			e.Drain(context.Background())
			f.flush(t)
			ts := f.tc.Events()
			names := []string{}
			var bytes int64
			var paused time.Duration
			for _, ev := range ts {
				names = append(names, ev.Name)
				if ev.Name == trail.Walked || ev.Name == trail.Loaded {
					if ev.User != caller().UserID || ev.RequestID != caller().RequestID || ev.Attrs["scraper"] != "fake" || ev.Attrs["run"] != r.ID || ev.Attrs["duration_us"] != int64(0) {
						t.Fatal(ev)
					}
					bytes += ev.Attrs["bytes"].(int64)
					paused += time.Duration(ev.Attrs["paused_us"].(int64)) * time.Microsecond
				}
			}
			if !reflect.DeepEqual(names, []string{trail.Started, trail.Walked, trail.Loaded, trail.Finished}) || len(f.ec.Events()) != 2 {
				t.Fatal(names, f.ec.Events())
			}
			total := time.Duration(0)
			for _, d := range pauses {
				total += d
			}
			switch mode {
			case "budget":
				if bytes <= 0 || total != time.Duration(bytes)*time.Second || paused != total {
					t.Fatal(bytes, total, paused)
				}
			case "disabled":
				if len(pauses) != 0 {
					t.Fatal(pauses)
				}
			case "pressure":
				if !reflect.DeepEqual(pauses, []time.Duration{5 * time.Second, 5 * time.Second, 5 * time.Second}) || paused != total {
					t.Fatal(pauses, paused)
				}
			}
		})
	}
}

// R-P9FX-ZJGZ R-PBVQ-R2YD R-PHZ8-NXNU
func TestFailedBatchSticks(t *testing.T) {
	f := setup(t)
	if err := os.WriteFile(f.dir, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	s := store.New(f.d, store.Config{Runs: f.dir, ChunkBytes: 1, Now: func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }, Rand: new(random)})
	out := new(diagnosticCapture)
	errs := make(chan []error, 1)
	e := retryEngine(t, f, s, fake{"fake", func(_ context.Context, b func([]source.Row) error) error {
		a := b([]source.Row{{SourceID: "first"}})
		z := b([]source.Row{{SourceID: "second"}})
		errs <- []error{a, z}
		return nil
	}}, func(context.Context, time.Duration) { t.Error("build failure paused") }, out)
	r, err := e.Start(context.Background(), caller(), "fake")
	if err != nil {
		t.Fatal(err)
	}
	got := receive(t, errs)
	if got[0] == nil || got[1] == nil || !errors.Is(got[1], got[0]) {
		t.Fatal(got)
	}
	done := ended(t, f.st, r.ID)
	e.Drain(context.Background())
	if done.Reason != store.ReasonStore || len(out.snapshot()) != 1 {
		t.Fatal(done, out.snapshot())
	}
}
