package events_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type emitterSink func(context.Context, events.Event) error

func (f emitterSink) Deliver(ctx context.Context, e events.Event) error { return f(ctx, e) }

var emitterTime = time.Date(2025, 2, 3, 4, 5, 6, 123456789, time.FixedZone("local", 3600))

func emitterConfig(s events.Sink) events.Config {
	return events.Config{Service: "unit", Sink: s, Now: func() time.Time { return emitterTime }, Rand: bytes.NewReader(bytes.Repeat([]byte{0, 1, 2, 3, 4, 5, 6, 7}, 4096)), Sleep: func(context.Context, time.Duration) {}, Emits: []events.Emission{{Event: "thing.changed", Attrs: []string{"value"}}}}
}
func emitterNew(t *testing.T, c events.Config) *events.Emitter {
	t.Helper()
	e := events.New(c)
	t.Cleanup(func() { ctx, cancel := context.WithCancel(context.Background()); cancel(); e.Shutdown(ctx) })
	return e
}
func emitterEmit(e *events.Emitter, v any) {
	e.Emit(context.Background(), "thing.changed", events.Attrs{"value": v})
}
func emitterFlush(t *testing.T, e *events.Emitter) {
	t.Helper()
	if err := e.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// R-F1J5-24P5 R-F2R1-FWFU R-F3YX-TO6J R-F56U-7FX8 R-F6EQ-L7NX R-JXTD-W7ES R-F8UJ-CR5B
func TestEmitterAPI(t *testing.T) {
	var sink events.Sink = &events.Capture{}
	if events.ErrRejected == nil {
		t.Fatal("nil rejection")
	}
	const unsigned uint64 = events.DefaultQueueCapacity
	const floating float64 = events.DefaultQueueCapacity
	got := []time.Duration{events.DefaultRetryWindow, events.RetryBackoff, events.MaxRetryBackoff, events.AttemptTimeout}
	if unsigned != 1024 || floating != 1024 || !reflect.DeepEqual(got, []time.Duration{5 * time.Minute, 100 * time.Millisecond, 30 * time.Second, 5 * time.Second}) {
		t.Fatal(got)
	}
	emission := events.Emission{"thing.changed", []string{"value"}}
	cfg := events.Config{"unit", sink, io.Discard, func() time.Time { return emitterTime }, func(context.Context, time.Duration) {}, bytes.NewReader(make([]byte, 8)), nil, 1, time.Second, []events.Emission{emission}}
	newEmitter := emitterFactory(events.New)
	e := newEmitter(cfg)
	t.Cleanup(func() { e.Shutdown(context.Background()) })
	emit := e.Emit
	ready := e.Ready
	flush := e.Flush
	shutdown := e.Shutdown
	emits := e.Emits
	emitterMethodShapes(emit, ready, flush, shutdown, emits)
	ready()
	emit(context.Background(), emission.Event, events.Attrs{"value": true})
	if err := flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(emits(), []events.Emission{emission}) {
		t.Fatal(emits())
	}
	shutdown(context.Background())
}

// R-FCI8-I2DE R-FDQ4-VU43
func TestEmitterDeclarationPanics(t *testing.T) {
	cases := []events.Config{{}, {Service: "unit", Emits: []events.Emission{{Event: "bad"}}}, {Service: "unit", Emits: []events.Emission{{Event: "thing.changed"}, {Event: "thing.changed"}}}, {Service: "unit", Emits: []events.Emission{{Event: "thing.changed", Attrs: []string{"Bad"}}}}, {Service: "unit", Emits: []events.Emission{{Event: "thing.changed", Attrs: []string{"key", "key"}}}}}
	for i, cfg := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			defer func() {
				p := recover()
				if p == nil {
					t.Fatal("no panic")
				}
				if i == 0 && !strings.Contains(fmt.Sprint(p), "service name is empty") {
					t.Fatal(p)
				}
			}()
			events.New(cfg)
		})
	}
	if emitterNew(t, emitterConfig(&events.Capture{})) == nil {
		t.Fatal("nil emitter")
	}
}

// R-JZ1A-9Z5H
func TestEmitterDeclarationCopies(t *testing.T) {
	c := &events.Capture{}
	cfg := emitterConfig(c)
	cfg.Emits = append(cfg.Emits, events.Emission{Event: "thing.empty"})
	e := emitterNew(t, cfg)
	cfg.Emits[0].Event = "other.changed"
	cfg.Emits[0].Attrs[0] = "other"
	first := e.Emits()
	first[0].Event = "different.changed"
	first[0].Attrs[0] = "different"
	got := e.Emits()
	if !reflect.DeepEqual(got, []events.Emission{{Event: "thing.changed", Attrs: []string{"value"}}, {Event: "thing.empty", Attrs: []string{}}}) || got[1].Attrs == nil {
		t.Fatal(got)
	}
	emitterEmit(e, true)
	e.Emit(context.Background(), "thing.empty", nil)
	emitterFlush(t, e)
	if len(c.Events()) != 2 {
		t.Fatal(c.Events())
	}
	cfg.Emits = nil
	got = emitterNew(t, cfg).Emits()
	if got == nil || len(got) != 0 {
		t.Fatal(got)
	}
}

// R-K096-NQW6 R-K2OZ-FADK R-K54S-6TUY R-FR51-3B9Q
func TestEmitterStampAndCopy(t *testing.T) {
	c := &events.Capture{}
	e := emitterNew(t, emitterConfig(c))
	ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "usr", RequestID: "req"})
	ctx = events.NewContext(ctx, events.Cause{ID: "evt_aaaaaaaaaaaaaaaa", Depth: 2})
	type namedInt int
	attrs := events.Attrs{"value": namedInt(7)}
	e.Emit(ctx, "thing.changed", attrs)
	attrs["value"] = 9
	attrs["extra"] = true
	delete(attrs, "value")
	emitterEmit(e, "next")
	emitterFlush(t, e)
	got := c.Events()
	want := events.Event{ID: "evt_0001020304050607", Time: emitterTime.UTC().Truncate(time.Microsecond), Service: "unit", Name: "thing.changed", RequestID: "req", User: "usr", Attrs: events.Attrs{"value": int64(7)}, Cause: "evt_aaaaaaaaaaaaaaaa", Depth: 3}
	if len(got) != 2 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if got[1].RequestID != "" || got[1].User != "" || got[1].Cause != "" || got[1].Depth != 0 || got[1].Attrs == nil {
		t.Fatal(got[1])
	}
}

type emitterReader struct {
	active  atomic.Bool
	overlap atomic.Bool
	mu      sync.Mutex
	calls   int
	lengths []int
	fail    bool
}

func (r *emitterReader) Read(p []byte) (int, error) {
	if r.active.Swap(true) {
		r.overlap.Store(true)
	}
	defer r.active.Store(false)
	runtime.Gosched()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.lengths = append(r.lengths, len(p))
	for i := range p {
		p[i] = 0xaa
	}
	if r.fail {
		return 3, io.ErrUnexpectedEOF
	}
	return len(p), nil
}

// R-FHDU-15C6 R-K3WV-T249 R-K2OZ-FADK
func TestEmitterRandom(t *testing.T) {
	for _, fail := range []bool{false, true} {
		r := &emitterReader{fail: fail}
		c := &events.Capture{}
		cfg := emitterConfig(c)
		cfg.Rand = r
		e := emitterNew(t, cfg)
		emitterEmit(e, 1)
		emitterEmit(e, 2)
		emitterFlush(t, e)
		got := c.Events()
		if r.calls != 2 || !reflect.DeepEqual(r.lengths, []int{8, 8}) || len(got) != 2 {
			t.Fatal(r.calls, r.lengths, got)
		}
		if fail {
			if got[0].ID == got[1].ID || got[0].ID == "evt_aaaaaaaaaaaaaaaa" || got[1].ID == "evt_aaaaaaaaaaaaaaaa" {
				t.Fatal(got)
			}
			for _, event := range got {
				if _, err := event.MarshalJSON(); err != nil {
					t.Fatal(err)
				}
			}
		} else if got[0].ID != "evt_aaaaaaaaaaaaaaaa" {
			t.Fatal(got)
		}
	}
	c := &events.Capture{}
	cfg := emitterConfig(c)
	cfg.Rand = nil
	e := emitterNew(t, cfg)
	emitterEmit(e, true)
	emitterEmit(e, true)
	emitterFlush(t, e)
	got := c.Events()
	if len(got) != 2 || got[0].ID == got[1].ID {
		t.Fatal(got)
	}
}

// R-K6CO-KLLN R-FUSQ-8MHT R-KKZH-5UHZ
func TestEmitterMalformed(t *testing.T) {
	c := &events.Capture{}
	var stderr bytes.Buffer
	tc := &telemetry.Capture{}
	tw := telemetry.New(telemetry.Config{Service: "unit", Sink: tc, Now: func() time.Time { return emitterTime }})
	t.Cleanup(func() { tw.Shutdown(context.Background(), "test") })
	cfg := emitterConfig(c)
	cfg.Stderr = &stderr
	cfg.Telemetry = tw
	e := emitterNew(t, cfg)
	cases := []struct {
		name  string
		attrs events.Attrs
		ctx   context.Context
	}{{"invalid", events.Attrs{"value": true}, nil}, {"undeclared.event", events.Attrs{"value": true}, nil}, {"thing.changed", nil, nil}, {"thing.changed", events.Attrs{"value": true, "extra": "shown"}, nil}, {"thing.changed", events.Attrs{"other": true}, nil}, {"thing.changed", events.Attrs{"value": map[string]string{"secret": "hidden"}}, nil}, {"thing.changed", events.Attrs{"value": nil}, nil}, {"thing.changed", events.Attrs{"value": true}, events.NewContext(context.Background(), events.Cause{ID: "bad", Depth: 2})}}
	for _, c := range cases {
		e.Emit(c.ctx, c.name, c.attrs)
	}
	emitterFlush(t, e)
	if err := tw.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.Events()) != 0 || len(tc.Events()) != 0 {
		t.Fatal("malformed delivered or lost")
	}
	lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	if len(lines) != len(cases) {
		t.Fatal(stderr.String())
	}
	for i, line := range lines {
		expectedAttrs := make(map[string]any)
		for key, value := range cases[i].attrs {
			expectedAttrs[key] = value
		}
		if i == 5 || i == 6 {
			expectedAttrs["value"] = nil
		}
		cause := ""
		depth := 0
		if i == 7 {
			cause = "bad"
			depth = 3
		}
		expectedBody, err := json.Marshal(struct {
			ID        string         `json:"id"`
			Time      string         `json:"time"`
			Service   string         `json:"service"`
			Event     string         `json:"event"`
			RequestID string         `json:"request_id"`
			User      string         `json:"user"`
			Attrs     map[string]any `json:"attrs"`
			Cause     string         `json:"cause"`
			Depth     int            `json:"depth"`
		}{"evt_0001020304050607", emitterTime.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"), "unit", cases[i].name, "", "", expectedAttrs, cause, depth})
		if err != nil {
			t.Fatal(err)
		}
		if line != "unit: malformed event: "+string(expectedBody) {
			t.Fatalf("got %s want %s", line, expectedBody)
		}
		if !strings.HasPrefix(line, "unit: malformed event: ") {
			t.Fatal(line)
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "unit: malformed event: ")), &obj); err != nil {
			t.Fatal(err)
		}
		if obj["event"] != cases[i].name {
			t.Fatal(obj)
		}
		attrs := obj["attrs"].(map[string]any)
		if i == 3 && attrs["extra"] != "shown" {
			t.Fatal(attrs)
		}
		if (i == 5 || i == 6) && attrs["value"] != nil {
			t.Fatal(attrs)
		}
	}
	if strings.Contains(stderr.String(), "hidden") {
		t.Fatal(stderr.String())
	}
}

// R-FG5X-NDLH
func TestEmitterDefaultClock(t *testing.T) {
	c := &events.Capture{}
	cfg := emitterConfig(c)
	cfg.Now = nil
	e := emitterNew(t, cfg)
	a := time.Now().UTC().Truncate(time.Microsecond)
	emitterEmit(e, true)
	b := time.Now().UTC().Truncate(time.Microsecond)
	emitterFlush(t, e)
	got := c.Events()[0].Time
	if got.Before(a) || got.After(b) || got.Location() != time.UTC || got.Nanosecond()%1000 != 0 {
		t.Fatal(got, a, b)
	}
}

// R-FSCX-H30F R-FW0M-ME8I R-K7KK-YDCC R-G6ZQ-2BWR R-G9FI-TVE5
func TestEmitterBlockedOrderAndFlush(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	c := &events.Capture{}
	var stderr bytes.Buffer
	cfg := emitterConfig(emitterSink(func(ctx context.Context, e events.Event) error {
		once.Do(func() { close(entered); <-release })
		return c.Deliver(ctx, e)
	}))
	cfg.Stderr = &stderr
	e := emitterNew(t, cfg)
	e.Ready()
	emitterEmit(e, 1)
	<-entered
	emitterEmit(e, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.Flush(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	emitterEmit(e, 3)
	close(release)
	emitterFlush(t, e)
	e.Ready()
	got := c.Events()
	if len(got) != 3 {
		t.Fatal(got)
	}
	for i, event := range got {
		if event.Attrs["value"] != int64(i+1) {
			t.Fatal(got)
		}
	}
	if stderr.Len() != 0 {
		t.Fatal(stderr.String())
	}
}

// R-K8SH-C531 R-KA0D-PWTQ R-FILQ-EX2V R-KDO2-V81T R-KEVZ-8ZSI
func TestEmitterRetryCurveAndLoss(t *testing.T) {
	for _, window := range []time.Duration{0, 350 * time.Millisecond} {
		t.Run(window.String(), func(t *testing.T) {
			var clockMu sync.Mutex
			now := emitterTime
			var pauses []time.Duration
			var attempts []events.Event
			var stderr bytes.Buffer
			tc := &telemetry.Capture{}
			tw := telemetry.New(telemetry.Config{Service: "unit", Sink: tc, Now: func() time.Time { return emitterTime }})
			t.Cleanup(func() { tw.Shutdown(context.Background(), "test") })
			cfg := emitterConfig(emitterSink(func(_ context.Context, event events.Event) error {
				attempts = append(attempts, event)
				return errors.New("offline")
			}))
			cfg.Now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
			cfg.Sleep = func(_ context.Context, d time.Duration) {
				pauses = append(pauses, d)
				clockMu.Lock()
				now = now.Add(d)
				clockMu.Unlock()
			}
			cfg.Stderr = &stderr
			cfg.Telemetry = tw
			cfg.RetryWindow = window
			e := emitterNew(t, cfg)
			ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "usr", RequestID: "req"})
			ctx = events.NewContext(ctx, events.Cause{ID: "evt_bbbbbbbbbbbbbbbb", Depth: 1})
			e.Emit(ctx, "thing.changed", events.Attrs{"value": true})
			emitterFlush(t, e)
			effective := window
			if effective == 0 {
				effective = 5 * time.Minute
			}
			total := time.Duration(0)
			want := []time.Duration{}
			backoff := 100 * time.Millisecond
			for total < effective {
				want = append(want, backoff)
				total += backoff
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
			if !reflect.DeepEqual(pauses, want) || len(attempts) != len(want)+1 {
				t.Fatal(pauses, want, len(attempts))
			}
			for _, event := range attempts {
				if !reflect.DeepEqual(event, attempts[0]) {
					t.Fatal("changed retry")
				}
			}
			body, err := attempts[0].MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if stderr.String() != "unit: lost event: "+string(body)+"\n" {
				t.Fatal(stderr.String())
			}
			if err := tw.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			trail := tc.Events()
			if len(trail) != 1 || trail[0].Name != "event.lost" || trail[0].RequestID != "req" || trail[0].User != "usr" || !reflect.DeepEqual(trail[0].Attrs, telemetry.Attrs{"event": attempts[0].ID, "cause": "evt_bbbbbbbbbbbbbbbb"}) {
				t.Fatal(trail)
			}
		})
	}
}

// R-KB8A-3OKF R-K7KK-YDCC
func TestEmitterRejectAndRetrySuccess(t *testing.T) {
	for _, reject := range []bool{true, false} {
		calls := 0
		sleeps := 0
		var stderr bytes.Buffer
		cfg := emitterConfig(emitterSink(func(context.Context, events.Event) error {
			calls++
			if reject {
				return fmt.Errorf("broker: %w", events.ErrRejected)
			}
			if calls == 1 {
				return errors.New("temporary")
			}
			return nil
		}))
		cfg.Stderr = &stderr
		cfg.Sleep = func(context.Context, time.Duration) { sleeps++ }
		e := emitterNew(t, cfg)
		emitterEmit(e, true)
		emitterFlush(t, e)
		if reject {
			if calls != 1 || sleeps != 0 || !strings.HasPrefix(stderr.String(), "unit: lost event: ") {
				t.Fatal(calls, sleeps, stderr.String())
			}
		} else if calls != 2 || sleeps != 1 || stderr.Len() != 0 {
			t.Fatal(calls, sleeps, stderr.String())
		}
	}
}

// R-3YDQ-WT3M R-KG3V-MRJ7 R-KHBS-0J9W R-KIJO-EB0L R-GEB4-CYCX
func TestEmitterShutdownBlockedDelivery(t *testing.T) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	returned := make(chan struct{})
	var stderr bytes.Buffer
	a := time.Now()
	cfg := emitterConfig(emitterSink(func(ctx context.Context, _ events.Event) error {
		entered <- ctx
		<-release
		close(returned)
		return nil
	}))
	cfg.Stderr = &stderr
	e := emitterNew(t, cfg)
	request, cancelRequest := context.WithCancel(context.Background())
	e.Emit(request, "thing.changed", events.Attrs{"value": 1})
	cancelRequest()
	delivery := <-entered
	b := time.Now()
	deadline, ok := delivery.Deadline()
	if !ok || deadline.Before(a.Add(events.AttemptTimeout)) || deadline.After(b.Add(events.AttemptTimeout)) || delivery.Err() != nil {
		t.Fatal(deadline, delivery.Err())
	}
	emitterEmit(e, 2)
	emitterEmit(e, 3)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.Shutdown(ctx)
	if delivery.Err() == nil {
		t.Fatal("live delivery")
	}
	lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatal(stderr.String())
	}
	for i, line := range lines {
		var event events.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "unit: lost event: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event.Attrs["value"] != int64(i+1) {
			t.Fatal(line)
		}
	}
	saved := stderr.String()
	close(release)
	<-returned
	e.Shutdown(context.Background())
	emitterFlush(t, e)
	if stderr.String() != saved {
		t.Fatal("late output")
	}
	emitterEmit(e, 4)
	if strings.Count(stderr.String(), "lost event:") != 4 {
		t.Fatal(stderr.String())
	}
}

type emitterDrainContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *emitterDrainContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

// R-GEB4-CYCX R-KG3V-MRJ7
func TestEmitterRepeatShutdownContext(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	cfg := emitterConfig(emitterSink(func(context.Context, events.Event) error { close(entered); <-release; return nil }))
	e := emitterNew(t, cfg)
	emitterEmit(e, true)
	<-entered
	drainBase, cancelDrain := context.WithCancel(context.Background())
	defer cancelDrain()
	drain := &emitterDrainContext{Context: drainBase, observed: make(chan struct{})}
	firstDone := make(chan struct{})
	go func() { e.Shutdown(drain); close(firstDone) }()
	<-drain.observed
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.Shutdown(ctx)
	select {
	case <-firstDone:
		t.Fatal("premature shutdown")
	default:
	}
	close(release)
	<-firstDone
	e.Shutdown(context.Background())
}

// R-KCG6-HGB4 R-FILQ-EX2V
func TestEmitterQueueCapacity(t *testing.T) {
	for _, capacity := range []int{1, 0, -1} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			c := &events.Capture{}
			var stderr bytes.Buffer
			cfg := emitterConfig(emitterSink(func(ctx context.Context, event events.Event) error {
				once.Do(func() { close(entered); <-release })
				return c.Deliver(ctx, event)
			}))
			cfg.QueueCapacity = capacity
			cfg.Stderr = &stderr
			e := emitterNew(t, cfg)
			emitterEmit(e, -1)
			<-entered
			effective := capacity
			if effective <= 0 {
				effective = 1024
			}
			for i := 0; i < effective; i++ {
				emitterEmit(e, i)
			}
			emitterEmit(e, "overflow")
			if strings.Count(stderr.String(), "lost event:") != 1 || !strings.Contains(stderr.String(), `"value":"overflow"`) {
				t.Fatal(stderr.String())
			}
			close(release)
			emitterFlush(t, e)
			if len(c.Events()) != effective+1 {
				t.Fatal(len(c.Events()))
			}
		})
	}
}

// R-KCG6-HGB4
func TestEmitterCapacityBeforeFirstDelivery(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	reads := 0
	c := &events.Capture{}
	var stderr bytes.Buffer
	cfg := emitterConfig(c)
	cfg.QueueCapacity = 1
	cfg.Stderr = &stderr
	cfg.Now = func() time.Time {
		mu.Lock()
		reads++
		n := reads
		mu.Unlock()
		if n == 2 {
			close(entered)
			<-release
		}
		return emitterTime
	}
	e := emitterNew(t, cfg)
	emitterEmit(e, 1)
	<-entered
	emitterEmit(e, 2)
	if !strings.Contains(stderr.String(), `"value":2`) {
		close(release)
		t.Fatal("event before first delivery did not consume capacity")
	}
	close(release)
	emitterFlush(t, e)
	if len(c.Events()) != 1 {
		t.Fatal(c.Events())
	}
}

type emitterLineWriter struct {
	active  atomic.Bool
	overlap atomic.Bool
	mu      sync.Mutex
	lines   []string
}

func (w *emitterLineWriter) Write(p []byte) (int, error) {
	if w.active.Swap(true) {
		w.overlap.Store(true)
	}
	defer w.active.Store(false)
	runtime.Gosched()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = append(w.lines, string(p))
	return len(p), nil
}

// R-GFJ0-QQ3M R-GHYT-I9L0 R-FHDU-15C6
func TestEmitterConcurrentMethods(t *testing.T) {
	writer := &emitterLineWriter{}
	reader := &emitterReader{}
	cfg := emitterConfig(&events.Capture{})
	cfg.Stderr = writer
	cfg.Rand = reader
	e := emitterNew(t, cfg)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.Ready()
			_ = e.Emits()
			e.Emit(emitterNilContext, "bad", events.Attrs{"value": true})
			emitterEmit(e, true)
			_ = e.Flush(context.Background())
		}()
	}
	wg.Wait()
	e.Shutdown(context.Background())
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.lines) != 40 {
		t.Fatal(len(writer.lines))
	}
	for _, line := range writer.lines {
		if strings.Count(line, "\n") != 1 || !strings.HasPrefix(line, "unit: malformed event: ") {
			t.Fatal(line)
		}
	}
	if reader.overlap.Load() || writer.overlap.Load() {
		t.Fatal("overlapping source or diagnostic calls")
	}
	if reader.calls != 80 {
		t.Fatal(reader.calls)
	}
	cfg.Stderr = nil
	other := emitterNew(t, cfg)
	other.Emit(emitterNilContext, "bad", nil)
	other.Shutdown(context.Background())
	emitterEmit(other, true)
}

// R-FEY1-9LUS
func TestEmitterDefaultSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "ev-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	socket := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan events.Event, 1)
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/emit" {
			t.Errorf("path %s", r.URL.Path)
		}
		var event events.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		received <- event
		w.WriteHeader(http.StatusNoContent)
	})}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	path := filepath.Join(dir, "services.json")
	data, _ := json.Marshal(map[string]any{"services": []map[string]any{{"name": "events", "url": "https://events.test", "enabled": true, "description": "bus", "mcp": false, "socket": socket}}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IKIGENBA_SERVICES", path)
	e := emitterNew(t, emitterConfig(nil))
	emitterEmit(e, true)
	emitterFlush(t, e)
	got := <-received
	if got.Name != "thing.changed" || got.Attrs["value"] != true {
		t.Fatal(got)
	}
}

// R-FG5X-NDLH
func TestEmitterDefaultPauseDuration(t *testing.T) {
	var failed, retried time.Time
	calls := 0
	cfg := emitterConfig(emitterSink(func(context.Context, events.Event) error {
		calls++
		if calls == 1 {
			failed = time.Now()
			return errors.New("temporary")
		}
		retried = time.Now()
		return nil
	}))
	cfg.Sleep = nil
	e := emitterNew(t, cfg)
	emitterEmit(e, true)
	emitterFlush(t, e)
	if calls != 2 || retried.Sub(failed) < events.RetryBackoff {
		t.Fatal(calls, retried.Sub(failed))
	}
}

// R-KA0D-PWTQ R-FW0M-ME8I
func TestEmitterRetryClockReadsAndOrder(t *testing.T) {
	var mu sync.Mutex
	var trace []string
	now := emitterTime
	firstEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	attempts := 0
	sleeps := 0
	record := func(s string) { mu.Lock(); trace = append(trace, s); mu.Unlock() }
	cfg := emitterConfig(emitterSink(func(_ context.Context, event events.Event) error {
		v := event.Attrs["value"].(int64)
		record(fmt.Sprintf("deliver%d", v))
		if v == 1 {
			close(firstEntered)
			<-firstRelease
			record("complete1")
			return nil
		}
		attempts++
		mu.Lock()
		now = now.Add(300 * time.Millisecond)
		mu.Unlock()
		return errors.New("slow failure")
	}))
	cfg.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); trace = append(trace, "clock"); return now }
	cfg.RetryWindow = 300 * time.Millisecond
	cfg.Sleep = func(context.Context, time.Duration) { sleeps++ }
	e := emitterNew(t, cfg)
	emitterEmit(e, 1)
	<-firstEntered
	emitterEmit(e, 2)
	close(firstRelease)
	emitterFlush(t, e)
	want := []string{"clock", "clock", "deliver1", "clock", "complete1", "clock", "deliver2", "clock"}
	if !reflect.DeepEqual(trace, want) || attempts != 1 || sleeps != 0 {
		t.Fatal(trace, attempts, sleeps)
	}
}

// R-3YDQ-WT3M R-KG3V-MRJ7 R-KHBS-0J9W
func TestEmitterShutdownBlockedPause(t *testing.T) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	returned := make(chan struct{})
	calls := atomic.Int32{}
	writer := &emitterLineWriter{}
	cfg := emitterConfig(emitterSink(func(context.Context, events.Event) error { calls.Add(1); return errors.New("offline") }))
	cfg.Sleep = func(ctx context.Context, _ time.Duration) { entered <- ctx; <-release; close(returned) }
	cfg.Stderr = writer
	e := emitterNew(t, cfg)
	request, cancelRequest := context.WithCancel(context.Background())
	e.Emit(request, "thing.changed", events.Attrs{"value": 1})
	cancelRequest()
	pause := <-entered
	if pause.Err() != nil {
		t.Fatal("request canceled pause")
	}
	emitterEmit(e, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.Shutdown(ctx)
	if pause.Err() == nil {
		t.Fatal("pause context live")
	}
	writer.mu.Lock()
	before := append([]string(nil), writer.lines...)
	writer.mu.Unlock()
	if len(before) != 2 {
		t.Fatal(before)
	}
	close(release)
	<-returned
	e.Shutdown(context.Background())
	emitterFlush(t, e)
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if !reflect.DeepEqual(writer.lines, before) || calls.Load() != 1 {
		t.Fatal(writer.lines, calls.Load())
	}
}

// R-KEVZ-8ZSI R-KCG6-HGB4 R-KIJO-EB0L
func TestEmitterSynchronousLossTelemetry(t *testing.T) {
	tc := &telemetry.Capture{}
	tw := telemetry.New(telemetry.Config{Service: "unit", Sink: tc, Now: func() time.Time { return emitterTime }})
	t.Cleanup(func() { tw.Shutdown(context.Background(), "test") })
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	cfg := emitterConfig(emitterSink(func(context.Context, events.Event) error { once.Do(func() { close(entered); <-release }); return nil }))
	cfg.QueueCapacity = 1
	cfg.Telemetry = tw
	e := emitterNew(t, cfg)
	emitterEmit(e, 0)
	<-entered
	emitterEmit(e, 1)
	ctx := identity.NewContext(context.Background(), identity.Caller{RequestID: "request", UserID: "user"})
	e.Emit(ctx, "thing.changed", events.Attrs{"value": 2})
	if err := tw.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := tc.Events()
	if len(got) != 1 || got[0].Name != "event.lost" || got[0].RequestID != "request" || got[0].User != "user" || !reflect.DeepEqual(got[0].Attrs, telemetry.Attrs{"event": "evt_0001020304050607", "cause": ""}) {
		t.Fatal(got)
	}
	close(release)
	e.Shutdown(context.Background())
	ctx = events.NewContext(ctx, events.Cause{ID: "evt_cccccccccccccccc", Depth: 4})
	e.Emit(ctx, "thing.changed", events.Attrs{"value": 3})
	if err := tw.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got = tc.Events()
	if len(got) != 2 || got[1].RequestID != "request" || got[1].User != "user" || !reflect.DeepEqual(got[1].Attrs, telemetry.Attrs{"event": "evt_0001020304050607", "cause": "evt_cccccccccccccccc"}) {
		t.Fatal(got)
	}
}

// R-GHYT-I9L0
func TestEmitterConcurrentShutdownAndMethods(t *testing.T) {
	e := emitterNew(t, emitterConfig(&events.Capture{}))
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.Ready()
			emitterEmit(e, true)
			_ = e.Emits()
			_ = e.Flush(context.Background())
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			e.Shutdown(ctx)
		}()
	}
	wg.Wait()
	e.Shutdown(context.Background())
	emitterFlush(t, e)
}

var emitterNilContext context.Context

func emitterFactory(f func(events.Config) *events.Emitter) func(events.Config) *events.Emitter {
	return f
}
func emitterMethodShapes(func(context.Context, string, events.Attrs), func(), func(context.Context) error, func(context.Context), func() []events.Emission) {
}
