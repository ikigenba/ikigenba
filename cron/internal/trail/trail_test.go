package trail_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/cron/internal/store"
	"github.com/ikigenba/ikigenba/cron/internal/trail"
)

var instant = time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)

func pair(t *testing.T, ts telemetry.Sink, es events.Sink, stderr io.Writer) (*telemetry.Writer, *events.Emitter) {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return instant }
	sleep := func(context.Context, time.Duration) {}
	w := telemetry.New(telemetry.Config{Service: "cron", Sink: ts, Stderr: stderr, Now: now, Sleep: sleep, Rand: bytes.NewReader(make([]byte, 65536))})
	em := events.New(events.Config{Service: "cron", Sink: es, Stderr: stderr, Now: now, Sleep: sleep, Rand: bytes.NewReader(make([]byte, 65536)), Emits: trail.Emits()})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		em.Shutdown(ctx)
		w.Shutdown(ctx, "test")
	})
	return w, em
}
func flush(t *testing.T, w *telemetry.Writer, em *events.Emitter) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := em.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
func trigger() store.Trigger {
	return store.Trigger{ID: "crn_3f9a1c7e5b2d8046", Slug: "hourly", When: "@hourly", OwnerID: "owner", OwnerEmail: "owner@example.com"}
}
func callerContext() context.Context {
	return identity.NewContext(context.Background(), identity.Caller{UserID: "actor", Email: "actor@example.com", RequestID: "request"})
}

// R-CGPH-3E3V R-CJ59-UXL9 R-CKD6-8PBY R-CO0V-E0K1 R-CP8R-RSAQ R-CROK-JBS4
func TestDeclarationsAndNames(t *testing.T) {
	kinds := []string{trail.Created, trail.Paused, trail.Resumed, trail.Deleted, trail.Fired}
	if !reflect.DeepEqual(kinds, []string{"created", "paused", "resumed", "deleted", "fired"}) {
		t.Fatal(kinds)
	}
	emits := trail.Emits
	name := trail.Name
	expected := make([]events.Emission, 5)
	for i, k := range kinds {
		expected[i] = events.Emission{Event: "cron.*." + k, Attrs: []string{"trigger", "when"}}
	}
	expected[4].Attrs = append(expected[4].Attrs, "scheduled")
	first, second := emits(), emits()
	if !reflect.DeepEqual(first, expected) {
		t.Fatal(first)
	}
	for i := range first {
		first[i].Event = "changed"
		for j := range first[i].Attrs {
			first[i].Attrs[j] = "changed"
		}
	}
	if !reflect.DeepEqual(second, expected) || !reflect.DeepEqual(emits(), expected) {
		t.Fatal("declarations share storage")
	}
	for _, s := range []string{"crm_sync", "hourly", "", "arbitrary.slug"} {
		for _, k := range append(kinds, "anything") {
			if got := name(s, k); got != "cron."+s+"."+k {
				t.Fatal(got)
			}
		}
	}
	w, em := pair(t, &telemetry.Capture{}, &events.Capture{}, io.Discard)
	flush(t, w, em)
}

// R-CLL2-MH2N R-CMSZ-08TC R-UK4H-S06M R-KV7N-268C R-L1M0-EKGR R-KWFJ-FXZ1
func TestTriggerEvents(t *testing.T) {
	lifecycle := trail.Lifecycle
	fire := trail.Fire
	for _, kind := range []string{trail.Created, trail.Paused, trail.Resumed, trail.Deleted, trail.Fired} {
		for _, identified := range []bool{false, true} {
			for _, caused := range []bool{false, true} {
				for _, cancelled := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/identity%v/cause%v/cancel%v", kind, identified, caused, cancelled), func(t *testing.T) {
						ts, es := &telemetry.Capture{}, &events.Capture{}
						w, em := pair(t, ts, es, io.Discard)
						ctx := context.Background()
						if identified {
							ctx = callerContext()
						}
						if caused {
							ctx = events.NewContext(ctx, events.Cause{ID: "evt_8c3f1a6e2d9b4075", Depth: 1000000})
						}
						if cancelled {
							c, cancel := context.WithCancel(ctx)
							cancel()
							ctx = c
						}
						tr := trigger()
						expected := map[string]any{"trigger": tr.ID, "when": tr.When}
						if kind == trail.Fired {
							slot := instant.In(time.FixedZone("offset", 7200))
							fire(ctx, w, em, tr, slot)
							expected["scheduled"] = instant.Format(time.RFC3339)
						} else {
							lifecycle(ctx, w, em, kind, tr)
						}
						flush(t, w, em)
						trailEvents, bus := ts.Events(), es.Events()
						if len(trailEvents) != 1 || len(bus) != 1 {
							t.Fatalf("trail %v bus %v", trailEvents, bus)
						}
						a, b := trailEvents[0], bus[0]
						request, user := "", ""
						if identified {
							request, user = "request", "actor"
						}
						if a.Name != trail.Name(tr.Slug, kind) || b.Name != a.Name || !reflect.DeepEqual(map[string]any(a.Attrs), expected) || !reflect.DeepEqual(map[string]any(b.Attrs), expected) || a.RequestID != request || b.RequestID != request || a.User != user || b.User != user {
							t.Fatalf("trail %+v bus %+v", a, b)
						}
						cause, depth := "", 0
						if caused {
							cause, depth = "evt_8c3f1a6e2d9b4075", 1000001
						}
						if b.Cause != cause || b.Depth != depth {
							t.Fatal(b)
						}
					})
				}
			}
		}
	}
}

// R-CVC9-ON07
func TestInvalidLifecycleDoesNothing(t *testing.T) {
	ts, es := &telemetry.Capture{}, &events.Capture{}
	var stderr bytes.Buffer
	w, em := pair(t, ts, es, &stderr)
	for _, kind := range []string{trail.Fired, "", "updated", "CREATED"} {
		trail.Lifecycle(callerContext(), w, em, kind, trigger())
	}
	flush(t, w, em)
	if len(ts.Events()) != 0 || len(es.Events()) != 0 || stderr.Len() != 0 {
		t.Fatalf("trail %v bus %v stderr %s", ts.Events(), es.Events(), stderr.String())
	}
}

// R-CYZY-TY8A
func TestOrderingAfterReturn(t *testing.T) {
	ts, es := &telemetry.Capture{}, &events.Capture{}
	w, em := pair(t, ts, es, io.Discard)
	trail.Lifecycle(callerContext(), w, em, trail.Paused, trigger())
	w.Emit(callerContext(), "tool.called", telemetry.Attrs{"tool": "pause"})
	trail.Fire(callerContext(), w, em, trigger(), instant)
	w.Emit(callerContext(), "tool.called", telemetry.Attrs{"tool": "later"})
	em.Emit(callerContext(), trail.Name("hourly", trail.Resumed), events.Attrs{"trigger": trigger().ID, "when": "@hourly"})
	flush(t, w, em)
	var names []string
	for _, e := range ts.Events() {
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, []string{"cron.hourly.paused", "tool.called", "cron.hourly.fired", "tool.called"}) {
		t.Fatal(names)
	}
	names = nil
	for _, e := range es.Events() {
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, []string{"cron.hourly.paused", "cron.hourly.fired", "cron.hourly.resumed"}) {
		t.Fatal(names)
	}
}

type telemetrySinkFunc func(context.Context, telemetry.Event) error

func (f telemetrySinkFunc) Deliver(ctx context.Context, e telemetry.Event) error { return f(ctx, e) }

type eventSinkFunc func(context.Context, events.Event) error

func (f eventSinkFunc) Deliver(ctx context.Context, e events.Event) error { return f(ctx, e) }
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("operation did not complete")
	}
}

// R-KXNF-TPPQ
func TestBothHalvesQueueWithoutWaiting(t *testing.T) {
	for _, fire := range []bool{false, true} {
		for _, blockTrail := range []bool{false, true} {
			for _, blockBus := range []bool{false, true} {
				t.Run(fmt.Sprintf("fire%v/trail%v/bus%v", fire, blockTrail, blockBus), func(t *testing.T) {
					ts, es := &telemetry.Capture{}, &events.Capture{}
					release := make(chan struct{})
					enteredTrail, enteredBus := make(chan struct{}, 1), make(chan struct{}, 1)
					w, em := pair(t, telemetrySinkFunc(func(ctx context.Context, e telemetry.Event) error {
						if blockTrail {
							enteredTrail <- struct{}{}
							<-release
						}
						return ts.Deliver(ctx, e)
					}), eventSinkFunc(func(ctx context.Context, e events.Event) error {
						if blockBus {
							enteredBus <- struct{}{}
							<-release
						}
						return es.Deliver(ctx, e)
					}), io.Discard)
					done := make(chan struct{})
					go func() {
						if fire {
							trail.Fire(callerContext(), w, em, trigger(), instant)
						} else {
							trail.Lifecycle(callerContext(), w, em, trail.Created, trigger())
						}
						close(done)
					}()
					await(t, done)
					if blockTrail {
						await(t, enteredTrail)
					}
					if blockBus {
						await(t, enteredBus)
					}
					close(release)
					flush(t, w, em)
					if len(ts.Events()) != 1 || len(es.Events()) != 1 {
						t.Fatalf("trail %v bus %v", ts.Events(), es.Events())
					}
				})
			}
		}
	}
}

// R-KXNF-TPPQ
func TestIndependentFailureAndShutdown(t *testing.T) {
	for _, fire := range []bool{false, true} {
		for _, mode := range []string{"bus-rejected", "bus-error", "trail-error", "bus-shutdown"} {
			t.Run(fmt.Sprintf("fire%v/%s", fire, mode), func(t *testing.T) {
				ts, es := &telemetry.Capture{}, &events.Capture{}
				failed := false
				busSink := eventSinkFunc(func(ctx context.Context, e events.Event) error {
					if mode == "bus-rejected" {
						return fmt.Errorf("refused: %w", events.ErrRejected)
					}
					if mode == "bus-error" && !failed {
						failed = true
						return errors.New("failed")
					}
					return es.Deliver(ctx, e)
				})
				w, em := pair(t, telemetrySinkFunc(func(ctx context.Context, e telemetry.Event) error {
					_ = ts.Deliver(ctx, e)
					if mode == "trail-error" {
						return telemetry.ErrRejected
					}
					return nil
				}), busSink, io.Discard)
				if mode == "bus-shutdown" {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					em.Shutdown(ctx)
					cancel()
				}
				ctx := events.NewContext(callerContext(), events.Cause{ID: "evt_8c3f1a6e2d9b4075", Depth: 3})
				kind := trail.Created
				expected := map[string]any{"trigger": trigger().ID, "when": trigger().When}
				if fire {
					kind = trail.Fired
					expected["scheduled"] = instant.Format(time.RFC3339)
					trail.Fire(ctx, w, em, trigger(), instant)
				} else {
					trail.Lifecycle(ctx, w, em, trail.Created, trigger())
				}
				flush(t, w, em)
				if len(ts.Events()) != 1 {
					t.Fatal(ts.Events())
				}
				te := ts.Events()[0]
				if te.Name != trail.Name(trigger().Slug, kind) || te.User != "actor" || te.RequestID != "request" || !reflect.DeepEqual(map[string]any(te.Attrs), expected) {
					t.Fatalf("bad surviving trail event %+v", te)
				}
				switch {
				case mode == "bus-shutdown" || mode == "bus-rejected":
					if len(es.Events()) != 0 {
						t.Fatal(es.Events())
					}
				case len(es.Events()) != 1:
					t.Fatal(es.Events())
				default:
					be := es.Events()[0]
					if be.Name != te.Name || be.User != "actor" || be.RequestID != "request" || !reflect.DeepEqual(map[string]any(be.Attrs), expected) || be.Cause != "evt_8c3f1a6e2d9b4075" || be.Depth != 4 {
						t.Fatalf("bad surviving bus event %+v", be)
					}
				}
			})
		}
	}
}

// R-KYVC-7HGF
func TestConcurrentEventsKeepOwnArguments(t *testing.T) {
	ts, es := &telemetry.Capture{}, &events.Capture{}
	w, em := pair(t, ts, es, io.Discard)
	var wg sync.WaitGroup
	const count = 64
	for i := range count {
		wg.Go(func() {
			tr := trigger()
			tr.Slug = fmt.Sprintf("trigger%d", i)
			tr.ID = fmt.Sprintf("crn_%016x", i)
			tr.When = fmt.Sprintf("%d %d * * *", i%60, i/60)
			ctx := identity.NewContext(context.Background(), identity.Caller{UserID: fmt.Sprintf("user%d", i), RequestID: fmt.Sprintf("request%d", i)})
			ctx = events.NewContext(ctx, events.Cause{ID: fmt.Sprintf("evt_%016x", i), Depth: i})
			if i%2 == 0 {
				trail.Fire(ctx, w, em, tr, instant.Add(time.Duration(i)*time.Minute))
			} else {
				trail.Lifecycle(ctx, w, em, trail.Created, tr)
			}
		})
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	await(t, done)
	flush(t, w, em)
	a, b := ts.Events(), es.Events()
	if len(a) != count || len(b) != count {
		t.Fatalf("trail %d bus %d", len(a), len(b))
	}
	byName := make(map[string]telemetry.Event)
	for _, e := range a {
		if _, found := byName[e.Name]; found {
			t.Fatal("duplicate", e.Name)
		}
		byName[e.Name] = e
	}
	for i := range count {
		kind := trail.Created
		if i%2 == 0 {
			kind = trail.Fired
		}
		name := trail.Name(fmt.Sprintf("trigger%d", i), kind)
		e, ok := byName[name]
		expected := map[string]any{"trigger": fmt.Sprintf("crn_%016x", i), "when": fmt.Sprintf("%d %d * * *", i%60, i/60)}
		if kind == trail.Fired {
			expected["scheduled"] = instant.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		}
		if !ok || e.User != fmt.Sprintf("user%d", i) || e.RequestID != fmt.Sprintf("request%d", i) || !reflect.DeepEqual(map[string]any(e.Attrs), expected) {
			t.Fatalf("bad trail %+v", e)
		}
		if kind == trail.Fired && e.Attrs["scheduled"] != instant.Add(time.Duration(i)*time.Minute).Format(time.RFC3339) {
			t.Fatal(e)
		}
	}
	seen := make(map[string]bool)
	for _, e := range b {
		a, ok := byName[e.Name]
		if !ok || seen[e.Name] || a.User != e.User || a.RequestID != e.RequestID || !reflect.DeepEqual(map[string]any(a.Attrs), map[string]any(e.Attrs)) {
			t.Fatalf("bad bus %+v", e)
		}
		seen[e.Name] = true
		var i int
		if _, err := fmt.Sscanf(e.User, "user%d", &i); err != nil {
			t.Fatal(err)
		}
		if e.Cause != fmt.Sprintf("evt_%016x", i) || e.Depth != i+1 {
			t.Fatalf("bad bus cause %+v", e)
		}
	}
}
