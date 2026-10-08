package delivery_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	root "github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/delivery"
	"github.com/ikigenba/ikigenba/events/internal/settings"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

type timer struct {
	duration time.Duration
	fire     chan time.Time
}
type sink struct {
	telemetry.Capture
	records chan telemetry.Event
}

func (s *sink) Deliver(ctx context.Context, e telemetry.Event) error {
	_ = s.Capture.Deliver(ctx, e)
	s.records <- e
	return nil
}

type fixture struct {
	socketNumber int
	t            *testing.T
	db           *db.DB
	store        *store.Store
	writer       *telemetry.Writer
	sink         *sink
	path         string
	dir          string
	entries      []map[string]any
	timeout      chan timer
	backoff      chan timer
	cfg          delivery.Config
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	dir, err := os.MkdirTemp("", "delivery-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	var ticks atomic.Int64
	now := func() time.Time {
		return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(ticks.Add(1)) * time.Microsecond)
	}
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "log.db"), Migrations: root.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	snk := &sink{records: make(chan telemetry.Event, 4096)}
	w := telemetry.New(telemetry.Config{Service: events.ServiceName, Version: "test", Sink: snk, Now: now, Rand: bytes.NewReader(make([]byte, 100000)), Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		w.Shutdown(ctx, "test")
	})
	f := &fixture{t: t, db: d, store: store.New(d, store.Config{Now: now, DepthMax: 8}), writer: w, sink: snk, path: filepath.Join(dir, "services.json"), dir: dir, timeout: make(chan timer, 1024), backoff: make(chan timer, 1024)}
	f.cfg = delivery.Config{Store: f.store, Services: f.path, Telemetry: w, Settings: settings.Defaults(), TimeoutAfter: func(d time.Duration) <-chan time.Time {
		tm := timer{d, make(chan time.Time, 1)}
		f.timeout <- tm
		return tm.fire
	}, BackoffAfter: func(d time.Duration) <-chan time.Time {
		tm := timer{d, make(chan time.Time, 1)}
		f.backoff <- tm
		return tm.fire
	}}
	f.writeServices()
	f.declare("producer", store.Declaration{Emits: []events.Emission{{Event: "item.changed", Attrs: []string{"number"}}}})
	return f
}
func (f *fixture) declare(name string, d store.Declaration) {
	f.t.Helper()
	if err := f.store.Declare(context.Background(), name, d); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) writeServices() {
	f.t.Helper()
	data, err := json.Marshal(map[string]any{"services": f.entries})
	if err != nil {
		f.t.Fatal(err)
	}
	if err = os.WriteFile(f.path, data, 0600); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) serve(name string, h http.Handler) {
	f.t.Helper()
	f.socketNumber++
	socket := filepath.Join(f.dir, fmt.Sprintf("%s-%d.sock", name, f.socketNumber))
	if len(socket) >= 107 {
		f.t.Fatal("long socket")
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		f.t.Fatal(err)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = s.Serve(ln) }()
	f.t.Cleanup(func() { _ = s.Close() })
	f.entries = append(f.entries, map[string]any{"name": name, "url": "", "description": "", "socket": socket, "enabled": true, "mcp": false})
	f.writeServices()
	f.declare(name, store.Declaration{Accepts: []string{"item.changed"}})
}
func (f *fixture) event(i int) events.Event {
	f.t.Helper()
	e := events.Event{ID: fmt.Sprintf("evt_%016x", i), Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Service: "producer", Name: "item.changed", Attrs: events.Attrs{"number": int64(i)}, User: "user", RequestID: "request"}
	if err := f.store.Deliver(context.Background(), e); err != nil {
		f.t.Fatal(err)
	}
	page, err := f.store.Search(context.Background(), store.Filter{}, 100, "")
	if err != nil {
		f.t.Fatal(err)
	}
	for _, record := range page.Records {
		if record.ID == e.ID {
			return record
		}
	}
	f.t.Fatal("record missing")
	return events.Event{}
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("expected channel value")
		var zero T
		return zero
	}
}
func absent[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("unexpected value: %+v", v)
	default:
	}
}
func (f *fixture) record(name string) telemetry.Event {
	f.t.Helper()
	for {
		e := receive(f.t, f.sink.records)
		if e.Name == name {
			return e
		}
	}
}
func (f *fixture) subscriber(name string) store.Subscriber {
	f.t.Helper()
	subs, err := f.store.Subscribers(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	for _, s := range subs {
		if s.Service == name {
			return s
		}
	}
	f.t.Fatal("subscriber absent")
	return store.Subscriber{}
}
func (f *fixture) waitStatus(name string, status store.Status) store.Subscriber {
	f.t.Helper()
	for {
		ch := f.store.Changed()
		s := f.subscriber(name)
		if s.Status == status {
			return s
		}
		receive(f.t, ch)
	}
}
func (f *fixture) start() (*delivery.Loop, context.CancelFunc, <-chan struct{}) {
	f.t.Helper()
	l := delivery.New(f.cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	f.t.Cleanup(func() {
		cancel()
		receive(f.t, done)
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		l.Drain(ctx)
	})
	return l, cancel, done
}

// R-GQE8-ODLW R-Q6UW-O1MD R-GU1X-TOTZ R-BTKQ-IRMN
// R-Z8LS-NVI1 R-ZH53-C9OW R-G1LB-MGO7 R-H68X-NE8X R-ZM0O-VCNO
func TestOrderedDeliveryAndRecords(t *testing.T) {
	f := newFixture(t)
	calls := make(chan events.Delivery, 10)
	f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; return events.OK() }}))
	first := f.event(1)
	second := f.event(2)
	l, cancel, done := f.start()
	var methods interface {
		Run(context.Context)
		Drain(context.Context)
	} = l
	_ = methods
	for _, want := range []events.Event{first, second} {
		got := receive(t, calls)
		if got.Attempt != 1 {
			t.Fatal(got)
		}
		a, _ := got.Event.MarshalJSON()
		b, _ := want.MarshalJSON()
		if !bytes.Equal(a, b) {
			t.Fatalf("record %s != %s", a, b)
		}
		tm := receive(t, f.timeout)
		if tm.duration != f.cfg.Settings.DeliveryTimeout() {
			t.Fatal(tm.duration)
		}
		called := f.record("sibling.called")
		if called.Attrs["target"] != "consumer" || called.Attrs["method"] != "POST" || called.Attrs["path"] != events.EventsPath || called.Attrs["status"] != int64(200) || called.Attrs["duration_us"].(int64) < 0 {
			t.Fatal(called)
		}
		delivered := f.record("event.delivered")
		if !reflect.DeepEqual(delivered.Attrs, telemetry.Attrs{"event": want.ID, "service": "consumer"}) {
			t.Fatal(delivered)
		}
	}
	cancel()
	receive(t, done)
	l.Drain(context.Background())
	if err := f.writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.subscriber("consumer").Cursor != second.Seq {
		t.Fatal("cursor")
	}
	if len(f.sink.Events()) != 4 {
		t.Fatal(f.sink.Events())
	}
	absent(t, calls)
}

// R-QAIL-TCUG R-G2T8-08EW R-FWPQ-3DPF R-ZDHE-6YGT R-ZEPA-KQ7I R-ZFX6-YHY7
func TestOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		skip    bool
		message string
	}{{"skip", 200, `{"outcome":"skip"}`, true, ""}, {"missing", 404, "", true, ""}, {"error", 500, `{"outcome":"error","error":"publish failed: commit not found"}`, false, "publish failed: commit not found"}, {"empty", 500, `{"outcome":"error","error":""}`, false, "answered with status 500"}, {"bad", 200, `{}`, false, "answered with status 200"}, {"badrequest", 400, "", false, "answered with status 400"}, {"conflict", 409, "", false, "answered with status 409"}, {"busy", 429, "", false, "answered with status 429"}, {"gateway", 502, "", false, "answered with status 502"}, {"unavailable", 503, `{"outcome":"error","error":"ignored"}`, false, "answered with status 503"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.cfg.Settings.DeliveryAttempts = 1
			calls := make(chan struct{}, 10)
			f.serve("consumer", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != events.EventsPath || r.Header.Get("Content-Type") != "application/json" {
					t.Error("request contract")
				}
				calls <- struct{}{}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			e := f.event(1)
			l, cancel, done := f.start()
			receive(t, calls)
			if tc.skip {
				r := f.record("event.skipped")
				if !reflect.DeepEqual(r.Attrs, telemetry.Attrs{"event": e.ID, "service": "consumer"}) {
					t.Fatal(r)
				}
				if f.subscriber("consumer").Cursor != e.Seq {
					t.Fatal("skip cursor")
				}
			} else {
				r := f.record("subscriber.paused")
				if !reflect.DeepEqual(r.Attrs, telemetry.Attrs{"event": e.ID, "service": "consumer", "error": tc.message}) {
					t.Fatal(r)
				}
				s := f.subscriber("consumer")
				if s.Status != store.StatusPaused || s.Cursor != 0 || s.Reason == nil || *s.Reason != (store.Reason{Event: e.ID, Name: e.Name, Seq: e.Seq, Error: tc.message}) {
					t.Fatal(s)
				}
			}
			cancel()
			receive(t, done)
			l.Drain(context.Background())
			_ = f.writer.Flush(context.Background())
			if len(f.sink.Events()) != 2 {
				t.Fatal(f.sink.Events())
			}
			absent(t, f.backoff)
			absent(t, calls)
		})
	}
}

// R-QCYE-KWBU R-GCZC-GWG9 R-GE78-UO6Y R-H9WM-SPH0 R-BTKQ-IRMN R-ZM0O-VCNO
func TestRetriesAndUnchangedDeclaration(t *testing.T) {
	f := newFixture(t)
	f.cfg.Settings.DeliveryAttempts = 100
	calls := make(chan events.Delivery, 200)
	f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; return events.Fail("failed") }}))
	e := f.event(1)
	before := f.subscriber("consumer")
	_, cancel, done := f.start()
	for n := 1; n <= 100; n++ {
		got := receive(t, calls)
		if got.Attempt != n || got.Event.ID != e.ID {
			t.Fatal(got)
		}
		if n == 100 {
			break
		}
		tm := receive(t, f.backoff)
		want := 5 * time.Minute
		if n < 10 {
			want = time.Second * time.Duration(1<<(n-1))
		}
		if tm.duration != want {
			t.Fatalf("n=%d wait=%v", n, tm.duration)
		}
		if err := f.writer.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		records := f.sink.Events()
		if len(records) != n {
			t.Fatalf("after retry %d: %+v", n, records)
		}
		for _, record := range records {
			if record.Name != "sibling.called" {
				t.Fatalf("unexpected retry record: %+v", record)
			}
		}
		now := f.subscriber("consumer")
		if now.Status != before.Status || now.Cursor != before.Cursor || !now.Since.Equal(before.Since) || now.Reason != nil {
			t.Fatal(now)
		}
		if n == 3 {
			f.declare("consumer", store.Declaration{Accepts: []string{"item.changed"}})
		}
		absent(t, calls)
		tm.fire <- time.Time{}
	}
	f.record("subscriber.paused")
	cancel()
	receive(t, done)
	if err := f.writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	records := f.sink.Events()
	if len(records) != 101 {
		t.Fatalf("final retry trail: %+v", records)
	}
	for i, record := range records {
		want := "sibling.called"
		if i == 100 {
			want = "subscriber.paused"
		}
		if record.Name != want {
			t.Fatalf("record %d: %+v", i, record)
		}
	}
	absent(t, f.backoff)
	absent(t, calls)
}

// R-GCZC-GWG9
func TestRetryAfterUncapped(t *testing.T) {
	for _, seconds := range []int{30, 600} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			f := newFixture(t)
			f.serve("consumer", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", fmt.Sprint(seconds))
				w.WriteHeader(503)
			}))
			f.event(1)
			_, cancel, done := f.start()
			tm := receive(t, f.backoff)
			if tm.duration != time.Duration(seconds)*time.Second {
				t.Fatal(tm.duration)
			}
			cancel()
			receive(t, done)
		})
	}
}

// R-GRM5-25CL R-GSU1-FX3A R-FWPQ-3DPF
func TestDeadlineClosesConnection(t *testing.T) {
	for _, seconds := range []int64{1, 5} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			f := newFixture(t)
			f.cfg.Settings.DeliveryTimeoutSeconds = seconds
			f.cfg.Settings.DeliveryAttempts = 1
			started := make(chan struct{}, 1)
			closed := make(chan struct{}, 1)
			f.serve("consumer", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				started <- struct{}{}
				<-r.Context().Done()
				closed <- struct{}{}
			}))
			f.event(1)
			f.start()
			receive(t, started)
			tm := receive(t, f.timeout)
			if tm.duration != time.Duration(seconds)*time.Second {
				t.Fatal(tm.duration)
			}
			tm.fire <- time.Time{}
			receive(t, closed)
			r := f.record("subscriber.paused")
			message := "no answer from consumer within 1 second"
			if seconds != 1 {
				message = fmt.Sprintf("no answer from consumer within %d seconds", seconds)
			}
			if r.Attrs["error"] != message {
				t.Fatal(r)
			}
		})
	}
}

// R-FWPQ-3DPF R-QAIL-TCUG R-H68X-NE8X
func TestUnreachableSocket(t *testing.T) {
	f := newFixture(t)
	f.cfg.Settings.DeliveryAttempts = 1
	f.entries = []map[string]any{{"name": "consumer", "url": "", "description": "", "socket": filepath.Join(f.dir, "absent.sock"), "enabled": true, "mcp": false}}
	f.writeServices()
	f.declare("consumer", store.Declaration{Accepts: []string{"item.changed"}})
	f.event(1)
	f.start()
	called := f.record("sibling.called")
	if called.Attrs["status"] != int64(0) {
		t.Fatal(called)
	}
	paused := f.record("subscriber.paused")
	if paused.Attrs["error"] != "no answer from consumer" {
		t.Fatal(paused)
	}
}

// R-GFF5-8FXN R-ZC9H-T6Q4 R-ZPOE-0NVR R-GRM5-25CL
func TestInflightCapAndIndependence(t *testing.T) {
	for _, cap := range []int64{1, 2} {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			f := newFixture(t)
			f.cfg.Settings.InflightMax = cap
			started := make(chan string, 10)
			release := make(chan struct{}, 2)
			for _, name := range []string{"alpha", "beta"} {
				name := name
				f.serve(name, events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, _ events.Delivery) events.Outcome {
					started <- name
					<-release
					return events.OK()
				}}))
			}
			f.event(1)
			l, cancel, done := f.start()
			receive(t, started)
			receive(t, f.timeout)
			if cap == 1 {
				absent(t, started)
				absent(t, f.timeout)
				release <- struct{}{}
				f.record("event.delivered")
				receive(t, started)
				receive(t, f.timeout)
				release <- struct{}{}
			} else {
				receive(t, started)
				receive(t, f.timeout)
				release <- struct{}{}
				release <- struct{}{}
			}
			f.record("event.delivered")
			cancel()
			receive(t, done)
			l.Drain(context.Background())
			absent(t, started)
		})
	}
}

// R-ZC9H-T6Q4 R-ZPOE-0NVR
func TestBackoffAndMissingTargetDoNotHoldSlot(t *testing.T) {
	f := newFixture(t)
	f.cfg.Settings.InflightMax = 1
	failed := make(chan struct{}, 10)
	ok := make(chan struct{}, 10)
	f.serve("alpha", events.DeliveryHandler(events.Handlers{"item.changed": func(context.Context, events.Delivery) events.Outcome {
		failed <- struct{}{}
		return events.Fail("failed")
	}}))
	f.serve("beta", events.DeliveryHandler(events.Handlers{"item.changed": func(context.Context, events.Delivery) events.Outcome { ok <- struct{}{}; return events.OK() }}))
	f.declare("absent", store.Declaration{Accepts: []string{"item.changed"}})
	f.event(1)
	f.start()
	receive(t, failed)
	receive(t, ok)
	f.record("event.delivered")
	if f.subscriber("beta").Cursor != 1 {
		t.Fatal("healthy subscriber held back")
	}
	absent(t, failed)
}

// R-GWHQ-L8BD R-HB4J-6H7P R-ZN8L-94ED R-ZOGH-MW52
// R-ZICZ-Q1FL R-GYXJ-CRSR
func TestStopPreservesActiveAnswer(t *testing.T) {
	f := newFixture(t)
	calls := make(chan events.Delivery, 10)
	release := make(chan struct{})
	f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; <-release; return events.OK() }}))
	e := f.event(1)
	l, cancel, done := f.start()
	receive(t, calls)
	receive(t, f.timeout)
	cancel()
	receive(t, done)
	f.event(2)
	absent(t, calls)
	absent(t, f.timeout)
	absent(t, f.backoff)
	close(release)
	l.Drain(context.Background())
	_ = f.writer.Flush(context.Background())
	if f.subscriber("consumer").Cursor != e.Seq {
		t.Fatal("active answer lost")
	}
	records := f.sink.Events()
	if len(records) != 2 || records[0].Name != "sibling.called" || records[1].Name != "event.delivered" {
		t.Fatal(records)
	}
	absent(t, calls)
}

// R-ZKSS-HKWZ R-H05F-QJJG R-ZJKW-3T6A R-H2L8-I30U R-H3T4-VURJ
// R-H68X-NE8X R-ZOGH-MW52
func TestDrainAbandonsUnansweredAndPartialBody(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			f := newFixture(t)
			f.cfg.Settings.DeliveryAttempts = 1
			started := make(chan struct{}, 1)
			closed := make(chan struct{}, 1)
			f.serve("consumer", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if partial {
					w.WriteHeader(200)
					_, _ = io.WriteString(w, `{"outcome":`)
					w.(http.Flusher).Flush()
				}
				_, _ = io.Copy(io.Discard, r.Body)
				started <- struct{}{}
				<-r.Context().Done()
				closed <- struct{}{}
			}))
			f.event(1)
			before := f.subscriber("consumer")
			l, cancel, done := f.start()
			receive(t, started)
			receive(t, f.timeout)
			if partial {
				f.record("sibling.called")
			}
			cancel()
			receive(t, done)
			drain, abort := context.WithCancel(context.Background())
			abort()
			start := time.Now()
			l.Drain(drain)
			if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
				t.Fatal(elapsed)
			}
			receive(t, closed)
			_ = f.writer.Flush(context.Background())
			after := f.subscriber("consumer")
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("abandonment changed subscriber %+v", after)
			}
			records := f.sink.Events()
			wantStatus := int64(0)
			if partial {
				wantStatus = 200
			}
			if len(records) != 1 || records[0].Name != "sibling.called" || records[0].Attrs["status"] != wantStatus {
				t.Fatal(records)
			}
			absent(t, f.backoff)
			// A replacement loop starts the retained event with attempt one.
			f.entries[0]["socket"] = filepath.Join(f.dir, "replacement.sock")
			f.entries = nil
			calls := make(chan events.Delivery, 1)
			f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; return events.OK() }}))
			f.start()
			got := receive(t, calls)
			if got.Attempt != 1 || got.Event.Seq != 1 {
				t.Fatal(got)
			}
			f.record("event.delivered")
		})
	}
}

// R-ZICZ-Q1FL R-GYXJ-CRSR
func TestStopDuringRetryWait(t *testing.T) {
	f := newFixture(t)
	calls := make(chan events.Delivery, 10)
	f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; return events.Fail("failed") }}))
	f.event(1)
	l, cancel, done := f.start()
	receive(t, calls)
	receive(t, f.timeout)
	tm := receive(t, f.backoff)
	cancel()
	receive(t, done)
	tm.fire <- time.Time{}
	f.event(2)
	l.Drain(context.Background())
	absent(t, calls)
	absent(t, f.timeout)
	absent(t, f.backoff)
}

// R-Z7DW-A3RC R-ZB1L-FEZF
func TestMissingTargetRecoversAndSocketMoves(t *testing.T) {
	f := newFixture(t)
	f.declare("consumer", store.Declaration{Accepts: []string{"item.changed"}})
	f.event(1)
	f.start()
	tm := receive(t, f.backoff)
	if tm.duration != time.Second {
		t.Fatal(tm.duration)
	}
	absent(t, f.timeout)
	if len(f.sink.Events()) != 0 {
		t.Fatal(f.sink.Events())
	}
	calls := make(chan events.Delivery, 10)
	f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; return events.OK() }}))
	tm.fire <- time.Time{}
	receive(t, calls)
	f.record("event.delivered")
	// Replace the target socket without changing the declaration or subscriber.
	socket := filepath.Join(f.dir, "moved.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	moved := make(chan events.Delivery, 1)
	s := &http.Server{ReadHeaderTimeout: time.Second, Handler: events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { moved <- d; return events.OK() }})}
	go func() { _ = s.Serve(ln) }()
	t.Cleanup(func() { _ = s.Close() })
	f.entries[0]["socket"] = socket
	f.writeServices()
	e := f.event(2)
	got := receive(t, moved)
	if got.Event.ID != e.ID {
		t.Fatal(got)
	}
	f.record("event.delivered")
	absent(t, calls)
}

// R-Z7DW-A3RC R-ZB1L-FEZF R-ZH53-C9OW
func TestNoTargetCases(t *testing.T) {
	for _, mode := range []string{"empty", "unreadable", "disabled", "empty_socket", "self"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			name := "consumer"
			if mode == "self" {
				name = events.ServiceName
			}
			f.declare(name, store.Declaration{Accepts: []string{"item.changed"}})
			f.entries = []map[string]any{{"name": name, "url": "", "description": "", "socket": filepath.Join(f.dir, "missing.sock"), "enabled": mode != "disabled", "mcp": false}}
			if mode == "empty_socket" {
				f.entries[0]["socket"] = ""
				f.cfg.Settings.DeliveryAttempts = 1
			}
			f.writeServices()
			if mode == "empty" {
				f.cfg.Services = ""
			}
			if mode == "unreadable" {
				f.cfg.Services = filepath.Join(f.dir, "absent.json")
			}
			f.event(1)
			f.start()
			if mode == "empty_socket" {
				receive(t, f.timeout)
				called := f.record("sibling.called")
				if called.Attrs["status"] != int64(0) {
					t.Fatal(called)
				}
				paused := f.record("subscriber.paused")
				if paused.Attrs["error"] != "no answer from consumer" {
					t.Fatal(paused)
				}
				sub := f.subscriber(name)
				if sub.Status != store.StatusPaused || sub.Cursor != 0 {
					t.Fatal(sub)
				}
				if err := f.writer.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				if len(f.sink.Events()) != 2 {
					t.Fatal(f.sink.Events())
				}
				absent(t, f.backoff)
				return
			}
			tm := receive(t, f.backoff)
			if tm.duration != time.Second {
				t.Fatal(tm.duration)
			}
			absent(t, f.timeout)
			if f.subscriber(name).Cursor != 0 || len(f.sink.Events()) != 0 {
				t.Fatal("targetless attempt")
			}
		})
	}
}

// R-QK9S-VIS0 R-Z8LS-NVI1
func TestUnacceptedEventsPassWithoutAttempt(t *testing.T) {
	f := newFixture(t)
	f.declare("consumer", store.Declaration{Accepts: []string{"other.changed"}})
	e := f.event(1)
	f.start()
	limit := time.After(3 * time.Second)
	for {
		s := f.subscriber("consumer")
		if s.Cursor == e.Seq {
			if s.Lag != 0 {
				t.Fatal(s)
			}
			break
		}
		select {
		case <-limit:
			t.Fatal("cursor not advanced")
		default:
			runtime.Gosched()
		}
	}
	absent(t, f.timeout)
	absent(t, f.backoff)
	if len(f.sink.Events()) != 0 {
		t.Fatal(f.sink.Events())
	}
}

// R-ZB1L-FEZF R-G1LB-MGO7 R-ZEPA-KQ7I
func TestStoreOutcomeFailureRechecks(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			f := newFixture(t)
			f.cfg.Settings.DeliveryAttempts = 1
			calls := make(chan events.Delivery, 10)
			block := make(chan struct{})
			var count atomic.Int64
			f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome {
				calls <- d
				if count.Add(1) == 1 {
					<-block
				}
				if success {
					return events.OK()
				}
				return events.Fail("failed")
			}}))
			e := f.event(1)
			f.start()
			receive(t, calls)
			f.db.SetFailing(true)
			close(block)
			tm := receive(t, f.backoff)
			if tm.duration != time.Second {
				t.Fatal(tm.duration)
			}
			_ = f.writer.Flush(context.Background())
			if records := f.sink.Events(); len(records) != 1 || records[0].Name != "sibling.called" {
				t.Fatal(records)
			}
			f.db.SetFailing(false)
			tm.fire <- time.Time{}
			again := receive(t, calls)
			want := 1
			if !success {
				want = 2
			}
			if again.Attempt != want || again.Event.ID != e.ID {
				t.Fatal(again)
			}
			if success {
				f.record("event.delivered")
				if f.subscriber("consumer").Cursor != e.Seq {
					t.Fatal("cursor")
				}
			} else {
				f.record("subscriber.paused")
			}
		})
	}
}

// R-GLIN-5AN4 R-GMQJ-J2DT R-GP6C-ALV7 R-QCYE-KWBU R-ZH53-C9OW
func TestResumeAndSkipResetAttempts(t *testing.T) {
	f := newFixture(t)
	f.cfg.Settings.DeliveryAttempts = 2
	calls := make(chan events.Delivery, 10)
	f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; return events.Fail("failed") }}))
	first := f.event(1)
	second := f.event(2)
	f.start()
	checkPair := func(want events.Event) {
		t.Helper()
		for n := 1; n <= 2; n++ {
			d := receive(t, calls)
			if d.Attempt != n || d.Event.ID != want.ID {
				t.Fatal(d)
			}
			if n == 1 {
				receive(t, f.backoff).fire <- time.Time{}
			}
		}
		f.record("subscriber.paused")
		absent(t, calls)
	}
	checkPair(first)
	if _, err := f.store.Resume(context.Background(), "consumer"); err != nil {
		t.Fatal(err)
	}
	checkPair(first)
	if _, _, err := f.store.Skip(context.Background(), "consumer"); err != nil {
		t.Fatal(err)
	}
	checkPair(second)
}

// R-QCYE-KWBU R-Z8LS-NVI1 R-ZH53-C9OW R-G1LB-MGO7 R-ZEPA-KQ7I
func TestGoneInFlightAndReturn(t *testing.T) {
	for _, ok := range []bool{true, false} {
		t.Run(fmt.Sprint(ok), func(t *testing.T) {
			f := newFixture(t)
			f.cfg.Settings.DeliveryAttempts = 1
			calls := make(chan events.Delivery, 10)
			release := make(chan struct{})
			var count atomic.Int64
			f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome {
				calls <- d
				if count.Add(1) == 1 {
					<-release
				}
				if ok {
					return events.OK()
				}
				return events.Fail("failed")
			}}))
			f.event(1)
			f.start()
			receive(t, calls)
			if err := f.store.Forget(context.Background(), "consumer"); err != nil {
				t.Fatal(err)
			}
			close(release)
			if ok {
				f.record("event.delivered")
			} else {
				f.record("sibling.called")
			}
			before := f.waitStatus("consumer", store.StatusGone)
			if before.Cursor != 0 {
				t.Fatal(before)
			}
			f.declare("consumer", store.Declaration{Accepts: []string{"item.changed"}})
			again := receive(t, calls)
			if again.Attempt != 1 || again.Event.Seq != 1 {
				t.Fatal(again)
			}
			if ok {
				f.record("event.delivered")
			} else {
				f.record("subscriber.paused")
			}
		})
	}
}

// R-QAIL-TCUG R-FWPQ-3DPF R-H68X-NE8X
func TestConnectionLostBeforeAnswer(t *testing.T) {
	f := newFixture(t)
	f.cfg.Settings.DeliveryAttempts = 1
	f.serve("consumer", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	f.event(1)
	f.start()
	called := f.record("sibling.called")
	if called.Attrs["status"] != int64(0) {
		t.Fatal(called)
	}
	paused := f.record("subscriber.paused")
	if paused.Attrs["error"] != "no answer from consumer" {
		t.Fatal(paused)
	}
}

// R-ZB1L-FEZF R-QCYE-KWBU
func TestStoreFailureBeforeAttempt(t *testing.T) {
	f := newFixture(t)
	calls := make(chan events.Delivery, 1)
	f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; return events.OK() }}))
	f.event(1)
	f.db.SetFailing(true)
	f.start()
	tm := receive(t, f.backoff)
	if tm.duration != time.Second {
		t.Fatal(tm.duration)
	}
	absent(t, calls)
	absent(t, f.timeout)
	f.db.SetFailing(false)
	tm.fire <- time.Time{}
	d := receive(t, calls)
	if d.Attempt != 1 {
		t.Fatal(d)
	}
	f.record("event.delivered")
}

// R-G2T8-08EW R-ZB1L-FEZF
func TestSkippedStoreFailureReattempts(t *testing.T) {
	f := newFixture(t)
	calls := make(chan events.Delivery, 10)
	release := make(chan struct{})
	var count atomic.Int64
	f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome {
		calls <- d
		if count.Add(1) == 1 {
			<-release
		}
		return events.Skip()
	}}))
	e := f.event(1)
	f.start()
	receive(t, calls)
	f.db.SetFailing(true)
	close(release)
	tm := receive(t, f.backoff)
	_ = f.writer.Flush(context.Background())
	if records := f.sink.Events(); len(records) != 1 || records[0].Name != "sibling.called" {
		t.Fatal(records)
	}
	f.db.SetFailing(false)
	tm.fire <- time.Time{}
	d := receive(t, calls)
	if d.Attempt != 1 || d.Event.ID != e.ID {
		t.Fatal(d)
	}
	f.record("event.skipped")
	if f.subscriber("consumer").Cursor != e.Seq {
		t.Fatal("cursor")
	}
	page, err := f.store.Search(context.Background(), store.Filter{}, 10, "")
	if err != nil || len(page.Records) != 1 || page.Records[0].ID != e.ID {
		t.Fatal(page, err)
	}
}

// R-Z8LS-NVI1 R-ZH53-C9OW
func TestDeclarationAfterAnswerChoosesNextAcceptedEvent(t *testing.T) {
	f := newFixture(t)
	calls := make(chan events.Delivery, 10)
	release := make(chan struct{})
	var count atomic.Int64
	f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome {
		calls <- d
		if count.Add(1) == 1 {
			<-release
		}
		return events.OK()
	}}))
	first := f.event(1)
	second := f.event(2)
	l, cancel, done := f.start()
	if got := receive(t, calls); got.Event.ID != first.ID {
		t.Fatal(got)
	}
	// A literal pattern no longer accepts item.changed, even though the
	// other exact name keeps the service subscribed.
	f.declare("consumer", store.Declaration{Accepts: []string{"item.*", "other.changed"}})
	absent(t, calls)
	close(release)
	f.record("event.delivered")
	limit := time.After(3 * time.Second)
	for f.subscriber("consumer").Cursor != second.Seq {
		select {
		case <-limit:
			t.Fatal("nonaccepted event not passed")
		default:
			runtime.Gosched()
		}
	}
	cancel()
	receive(t, done)
	l.Drain(context.Background())
	absent(t, calls)
	if err := f.writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.sink.Events()) != 2 {
		t.Fatal(f.sink.Events())
	}
}

// R-Z7DW-A3RC
func TestFirstEnabledTargetWins(t *testing.T) {
	f := newFixture(t)
	calls := make(chan events.Delivery, 1)
	other := make(chan events.Delivery, 1)
	f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; return events.OK() }}))
	f.serve("second", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { other <- d; return events.OK() }}))
	if err := f.store.Forget(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	first := f.entries[0]
	second := f.entries[1]
	second["name"] = "consumer"
	disabled := map[string]any{"name": "consumer", "url": "", "description": "", "socket": filepath.Join(f.dir, "disabled.sock"), "enabled": false, "mcp": false}
	f.entries = []map[string]any{disabled, first, second}
	f.writeServices()
	e := f.event(1)
	l, cancel, done := f.start()
	if got := receive(t, calls); got.Event.ID != e.ID {
		t.Fatal(got)
	}
	f.record("event.delivered")
	cancel()
	receive(t, done)
	l.Drain(context.Background())
	absent(t, other)
}

// R-ZB1L-FEZF R-ZICZ-Q1FL
func TestMissingTargetWaitEndsOnStoreChangeOrStop(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			f := newFixture(t)
			f.declare("consumer", store.Declaration{Accepts: []string{"item.changed"}})
			e := f.event(1)
			l, cancel, done := f.start()
			tm := receive(t, f.backoff)
			if tm.duration != time.Second || f.subscriber("consumer").Cursor != 0 {
				t.Fatal("targetless wait")
			}
			if stop {
				cancel()
				receive(t, done)
			}
			calls := make(chan events.Delivery, 1)
			f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; return events.OK() }}))
			if stop {
				tm.fire <- time.Time{}
				l.Drain(context.Background())
				absent(t, calls)
				absent(t, f.timeout)
				if len(f.sink.Events()) != 0 {
					t.Fatal(f.sink.Events())
				}
				return
			}
			// A new subscriber closes Changed; the backoff stays unfired.
			f.declare("wake", store.Declaration{Accepts: []string{"other.changed"}})
			if got := receive(t, calls); got.Attempt != 1 || got.Event.ID != e.ID {
				t.Fatal(got)
			}
			f.record("event.delivered")
		})
	}
}

// R-ZC9H-T6Q4 R-ZPOE-0NVR
func TestStalledFailingAndTargetlessPeersDoNotDelayHealthy(t *testing.T) {
	f := newFixture(t)
	f.cfg.Settings.InflightMax = 2
	started := make(chan struct{}, 1)
	closed := make(chan struct{}, 1)
	f.serve("alpha", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-r.Context().Done()
		closed <- struct{}{}
	}))
	failures := make(chan struct{}, 1)
	f.serve("beta", events.DeliveryHandler(events.Handlers{"item.changed": func(context.Context, events.Delivery) events.Outcome {
		failures <- struct{}{}
		return events.Fail("failed")
	}}))
	f.declare("gamma", store.Declaration{Accepts: []string{"item.changed"}})
	healthy := make(chan events.Delivery, 2)
	f.serve("omega", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { healthy <- d; return events.OK() }}))
	first := f.event(1)
	second := f.event(2)
	l, cancel, done := f.start()
	receive(t, started)
	receive(t, failures)
	for _, e := range []events.Event{first, second} {
		if got := receive(t, healthy); got.Event.ID != e.ID {
			t.Fatal(got)
		}
		f.record("event.delivered")
	}
	if f.subscriber("omega").Cursor != second.Seq {
		t.Fatal("healthy cursor")
	}
	absent(t, failures)
	cancel()
	receive(t, done)
	ctx, abort := context.WithCancel(context.Background())
	abort()
	l.Drain(ctx)
	receive(t, closed)
}

// R-ZN8L-94ED R-ZOGH-MW52 R-ZEPA-KQ7I R-ZFX6-YHY7
func TestDrainHandlesSkipAndFailureAfterStop(t *testing.T) {
	for _, outcome := range []string{"skip", "retry", "pause"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFixture(t)
			if outcome == "pause" {
				f.cfg.Settings.DeliveryAttempts = 1
			}
			calls := make(chan struct{}, 1)
			release := make(chan struct{})
			f.serve("consumer", events.DeliveryHandler(events.Handlers{"item.changed": func(context.Context, events.Delivery) events.Outcome {
				calls <- struct{}{}
				<-release
				if outcome == "skip" {
					return events.Skip()
				}
				return events.Fail("failed")
			}}))
			e := f.event(1)
			before := f.subscriber("consumer")
			l, cancel, done := f.start()
			receive(t, calls)
			receive(t, f.timeout)
			cancel()
			receive(t, done)
			close(release)
			l.Drain(context.Background())
			if err := f.writer.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			s := f.subscriber("consumer")
			records := f.sink.Events()
			if len(records) == 0 || records[0].Name != "sibling.called" {
				t.Fatal(records)
			}
			switch outcome {
			case "skip":
				if s.Cursor != e.Seq || len(records) != 2 || records[1].Name != "event.skipped" {
					t.Fatal(s, records)
				}
			case "pause":
				if s.Status != store.StatusPaused || s.Cursor != 0 || s.Reason == nil || s.Reason.Error != "failed" || len(records) != 2 || records[1].Name != "subscriber.paused" {
					t.Fatal(s, records)
				}
			case "retry":
				if !reflect.DeepEqual(s, before) || len(records) != 1 {
					t.Fatal(s, records)
				}
			}
			absent(t, f.timeout)
			absent(t, f.backoff)
			absent(t, calls)
		})
	}
}

// R-ZB1L-FEZF
func TestStoreReadFailureRechecksWithAnotherAttemptActive(t *testing.T) {
	f := newFixture(t)
	f.cfg.Settings.InflightMax = 2
	started := make(chan struct{}, 1)
	closed := make(chan struct{}, 1)
	f.serve("alpha", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-r.Context().Done()
		closed <- struct{}{}
	}))
	calls := make(chan events.Delivery, 2)
	f.serve("beta", events.DeliveryHandler(events.Handlers{"item.changed": func(_ context.Context, d events.Delivery) events.Outcome { calls <- d; return events.Fail("failed") }}))
	f.event(1)
	l, cancel, done := f.start()
	receive(t, started)
	if got := receive(t, calls); got.Attempt != 1 {
		t.Fatal(got)
	}
	retry := receive(t, f.backoff)
	f.db.SetFailing(true)
	retry.fire <- time.Time{}
	recheck := receive(t, f.backoff)
	if recheck.duration != time.Second {
		t.Fatal(recheck.duration)
	}
	absent(t, calls)
	f.db.SetFailing(false)
	recheck.fire <- time.Time{}
	if got := receive(t, calls); got.Attempt != 2 {
		t.Fatal(got)
	}
	cancel()
	receive(t, done)
	ctx, abort := context.WithCancel(context.Background())
	abort()
	l.Drain(ctx)
	receive(t, closed)
}
