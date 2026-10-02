package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
)

type writerSinkFunc func(context.Context, Event) error

func (f writerSinkFunc) Deliver(ctx context.Context, e Event) error { return f(ctx, e) }
func writerConfig(sink Sink, stderr io.Writer) Config {
	return Config{Service: "test", Version: "development", Sink: sink, Stderr: stderr, Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.FixedZone("offset", 3600)) }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 4096))}
}
func testWriter(t *testing.T, cfg Config) *Writer {
	t.Helper()
	w := New(cfg)
	t.Cleanup(func() { w.Shutdown(context.Background(), "cleanup"); <-w.senderDone })
	return w
}
func flushWriter(t *testing.T, w *Writer) {
	t.Helper()
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func expectedWriterEvent(name string, attrs Attrs) Event {
	return Event{Time: time.Date(2026, 1, 2, 2, 4, 5, 123456000, time.UTC), Service: "test", Name: name, Attrs: attrs}
}
func eventLine(t *testing.T, e Event, kind string) string {
	t.Helper()
	attrs := map[string]any{}
	for key, value := range e.Attrs {
		attrs[key] = value
	}
	b, err := json.Marshal(struct {
		Time      string         `json:"time"`
		Service   string         `json:"service"`
		Event     string         `json:"event"`
		RequestID string         `json:"request_id"`
		User      string         `json:"user"`
		Attrs     map[string]any `json:"attrs"`
	}{e.Time.UTC().Format("2006-01-02T15:04:05.000000Z"), e.Service, e.Name, e.RequestID, e.User, attrs})
	if err != nil {
		t.Fatal(err)
	}
	return e.Service + ": " + kind + " event: " + string(b) + "\n"
}

// R-VJ55-LGRZ R-VKD1-Z8IO R-ISVO-ANR1 R-VMSU-QS02 R-VO0R-4JQR R-VP8N-IBHG R-VROG-9UYU
func TestWriterConstruction(t *testing.T) {
	if ErrRejected == nil {
		t.Fatal("nil rejection")
	}
	var capacity uint16 = QueueCapacity
	var attempts uint8 = Attempts
	backoff := RetryBackoff
	timeout := AttemptTimeout
	check := func(capacityInt, attemptsInt int, backoffDuration, timeoutDuration time.Duration) {
		if capacity != 1024 || attempts != 3 || capacityInt != 1024 || attemptsInt != 3 || backoffDuration != 50*time.Millisecond || timeoutDuration != time.Second {
			t.Fatal("constants")
		}
	}
	check(QueueCapacity, Attempts, backoff, timeout)

	var c Capture
	d := writerConfig(&c, nil)
	cfg := Config{d.Service, d.Version, d.Sink, d.Stderr, d.Now, d.Sleep, d.Rand}
	if w := testWriter(t, cfg); w == nil {
		t.Fatal("nil writer")
	}
	func() {
		defer func() {
			p := recover()
			if p == nil || !strings.Contains(fmt.Sprint(p), "service name is empty") {
				t.Errorf("panic: %v", p)
			}
		}()
		New(Config{})
	}()
}

// R-IYZ6-7IGI R-J1EY-Z1XW R-VVC5-F66X R-VWK1-SXXM
func TestWriterEnvelope(t *testing.T) {
	var c Capture
	var stderr bytes.Buffer
	cfg := writerConfig(&c, &stderr)
	calls := 0
	clock := cfg.Now
	cfg.Now = func() time.Time { calls++; return clock() }
	w := testWriter(t, cfg)
	want := time.Date(2026, 1, 2, 2, 4, 5, 123456000, time.UTC)
	if got := w.Now(); got != want || calls != 1 {
		t.Fatal(got, calls)
	}
	type label string
	attrs := Attrs{"label": label("before"), "count": int16(2), "unsigned": uint8(3), "number": float32(1.5), "ok": true}
	ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "user", RequestID: "request"})
	w.Emit(ctx, "thing.done", attrs)
	attrs["label"] = "after"
	attrs["new"] = true
	delete(attrs, "count")
	w.Emit(context.Background(), "thing.empty", nil)
	flushWriter(t, w)
	events := c.Events()
	if calls != 3 || len(events) != 2 {
		t.Fatal(calls, events)
	}
	e := events[0]
	if e.Time != want || e.Service != "test" || e.Name != "thing.done" || e.User != "user" || e.RequestID != "request" {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(e.Attrs, Attrs{"label": "before", "count": int64(2), "unsigned": uint64(3), "number": float64(1.5), "ok": true}) {
		t.Fatal(e.Attrs)
	}
	if events[1].Attrs == nil || len(events[1].Attrs) != 0 || events[1].User != "" || events[1].RequestID != "" {
		t.Fatal(events[1])
	}
	if stderr.Len() != 0 {
		t.Fatal(stderr.String())
	}
}

// R-W07Q-Y95P R-W1FN-C0WE R-DAQ0-H1OR
func TestWriterMalformed(t *testing.T) {
	var c Capture
	var stderr bytes.Buffer
	cfg := writerConfig(&c, &stderr)
	year := 2026
	cfg.Now = func() time.Time { return time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC) }
	w := testWriter(t, cfg)
	cases := []struct {
		name  string
		attrs Attrs
		year  int
	}{{"Bad.name", Attrs{"valid": "kept"}, 2026}, {"thing.done", Attrs{"Bad": "kept"}, 2026}, {"thing.done", Attrs{"secret": []string{"hidden"}, "nan": math.NaN(), "nil": nil, "pointer": new(int), "channel": make(chan int), "function": func() {}, "map": map[string]int{"hidden": 1}, "ok": true}, 2026}, {"thing.done", nil, -1}, {"thing.done", nil, 10000}}
	var expected strings.Builder
	for _, tc := range cases {
		year = tc.year
		attrs := Attrs{}
		for key, value := range tc.attrs {
			switch value.(type) {
			case string, bool:
				attrs[key] = value
			default:
				attrs[key] = nil
			}
		}
		e := Event{Time: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC), Service: cfg.Service, Name: tc.name, Attrs: attrs}
		expected.WriteString(eventLine(t, e, "malformed"))
		w.Emit(context.Background(), tc.name, tc.attrs)
		if stderr.String() != expected.String() {
			t.Fatal("malformed output was not complete before Emit returned", stderr.String(), expected.String())
		}
	}
	flushWriter(t, w)
	if len(c.Events()) != 0 || stderr.String() != expected.String() {
		t.Fatal(c.Events(), stderr.String(), expected.String())
	}
	if strings.Contains(stderr.String(), "hidden") {
		t.Fatal("secret exposed")
	}
}

// R-E16G-QBY3 R-E2ED-43OS R-W6B8-V3V6 R-W7J5-8VLV R-W2NJ-PSN3
func TestWriterRetries(t *testing.T) {
	for _, mode := range []string{"success", "retry_success", "exhausted", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			var stderr bytes.Buffer
			var names []string
			var operations []string
			var delivered []Event
			var pauses []time.Duration
			counts := map[string]int{}
			sink := writerSinkFunc(func(_ context.Context, e Event) error {
				names = append(names, e.Name)
				operations = append(operations, e.Name)
				delivered = append(delivered, e)
				counts[e.Name]++
				if e.Name == "other.done" || e.Name == "service.stopping" || mode == "success" || mode == "retry_success" && counts[e.Name] == 2 {
					return nil
				}
				if mode == "rejected" {
					return fmt.Errorf("wrapped: %w", ErrRejected)
				}
				return errors.New("offline")
			})
			cfg := writerConfig(sink, &stderr)
			cfg.Sleep = func(_ context.Context, d time.Duration) {
				pauses = append(pauses, d)
				operations = append(operations, "pause")
			}
			w := testWriter(t, cfg)
			w.Emit(context.Background(), "thing.done", nil)
			w.Emit(context.Background(), "other.done", nil)
			flushWriter(t, w)
			n := map[string]int{"success": 1, "retry_success": 2, "exhausted": 3, "rejected": 1}[mode]
			if counts["thing.done"] != n || len(names) != n+1 || names[len(names)-1] != "other.done" {
				t.Fatal(names)
			}
			want := []time.Duration(nil)
			if mode == "retry_success" {
				want = []time.Duration{RetryBackoff}
			}
			if mode == "exhausted" {
				want = []time.Duration{RetryBackoff, 2 * RetryBackoff}
			}
			if !reflect.DeepEqual(pauses, want) {
				t.Fatal(pauses)
			}
			wantOperations := []string{"thing.done"}
			for i := 1; i < n; i++ {
				wantOperations = append(wantOperations, "pause", "thing.done")
			}
			wantOperations = append(wantOperations, "other.done")
			if !reflect.DeepEqual(operations, wantOperations) {
				t.Fatal(operations)
			}
			for _, e := range delivered[:n] {
				if !reflect.DeepEqual(e, expectedWriterEvent("thing.done", Attrs{})) {
					t.Fatal("retry changed event", e)
				}
			}
			if mode == "rejected" || mode == "exhausted" {
				e := expectedWriterEvent("thing.done", nil)
				if stderr.String() != eventLine(t, e, "undelivered") {
					t.Fatal(stderr.String())
				}
			} else if stderr.Len() != 0 {
				t.Fatal(stderr.String())
			}
		})
	}
}

// R-WB6U-E6TY R-DZYK-CK7E R-WDMN-5QBC R-WEUJ-JI21 R-E4U5-VN66 R-VALU-X2L4 R-VBTR-AUBT
func TestWriterLifecycle(t *testing.T) {
	var c Capture
	var stderr bytes.Buffer
	w := testWriter(t, writerConfig(&c, &stderr))
	w.Ready()
	w.Ready()
	w.Emit(context.Background(), "thing.done", nil)
	ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "stopper", RequestID: "stop"})
	w.Shutdown(ctx, "signal")
	w.Ready()
	w.Shutdown(ctx, "again")
	e := c.Events()
	if len(e) != 3 || e[0].Name != "service.started" || e[1].Name != "thing.done" || e[2].Name != "service.stopping" {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(e[0].Attrs, Attrs{"version": "development"}) || e[0].RequestID != "" || e[0].User != "" {
		t.Fatal(e[0])
	}
	if !reflect.DeepEqual(e[2].Attrs, Attrs{"reason": "signal"}) || e[2].RequestID != "stop" || e[2].User != "stopper" {
		t.Fatal(e[2])
	}
	w.Emit(context.Background(), "late.done", nil)
	late := expectedWriterEvent("late.done", nil)
	if stderr.String() != eventLine(t, late, "undelivered") {
		t.Fatal(stderr.String())
	}
}

// R-VXRY-6POB R-W9YY-0F39 R-DZYK-CK7E R-WII8-OTA4 R-E3M9-HVFH R-WEUJ-JI21 R-E4U5-VN66
func TestWriterBlockedShutdown(t *testing.T) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	returned := make(chan struct{})
	var stderr bytes.Buffer
	sink := writerSinkFunc(func(ctx context.Context, _ Event) error { entered <- ctx; <-release; close(returned); return nil })
	w := testWriter(t, writerConfig(sink, &stderr))
	emitCtx, cancelEmit := context.WithCancel(context.Background())
	w.Emit(emitCtx, "first.done", nil)
	deliveryCtx := <-entered
	deadline, ok := deliveryCtx.Deadline()
	if !ok || time.Until(deadline) > AttemptTimeout {
		t.Fatal("deadline")
	}
	cancelEmit()
	if deliveryCtx.Err() != nil {
		t.Fatal("request canceled delivery")
	}
	for i := 0; i < QueueCapacity; i++ {
		w.Emit(context.Background(), "queued.done", Attrs{"index": i})
	}
	w.Emit(context.Background(), "overflow.done", nil)
	overflow := expectedWriterEvent("overflow.done", nil)
	if stderr.String() != eventLine(t, overflow, "undelivered") {
		t.Fatal(stderr.String())
	}
	flushCtx, cancelFlush := context.WithCancel(context.Background())
	cancelFlush()
	if err := w.Flush(flushCtx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	drainCtx, cancelDrain := context.WithCancel(context.Background())
	cancelDrain()
	w.Shutdown(drainCtx, "deadline")
	if deliveryCtx.Err() == nil {
		t.Fatal("shutdown did not cancel delivery")
	}
	lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	if len(lines) != QueueCapacity+3 || !strings.Contains(lines[1], `"event":"first.done"`) || !strings.Contains(lines[len(lines)-1], `"event":"service.stopping"`) {
		t.Fatal(len(lines))
	}
	for i := 0; i < QueueCapacity; i++ {
		if !strings.Contains(lines[i+2], fmt.Sprintf(`"index":%d}`, i)) {
			t.Fatal(i, lines[i+2])
		}
	}
	before := stderr.String()
	close(release)
	<-returned
	<-w.senderDone
	if stderr.String() != before {
		t.Fatal("late output")
	}
}

// R-WKY1-GCRI R-WJQ5-2L0T
func TestWriterConcurrent(t *testing.T) {
	var c Capture
	var stderr bytes.Buffer
	w := testWriter(t, writerConfig(&c, &stderr))
	var g sync.WaitGroup
	for i := 0; i < 64; i++ {
		g.Go(func() {
			w.Ready()
			w.Emit(context.Background(), "thing.done", Attrs{"ok": true})
			w.Emit(context.Background(), "bad", Attrs{"private": []byte("secret")})
			_ = w.Now()
			flushWriter(t, w)
		})
	}
	g.Wait()
	w.Shutdown(context.Background(), "done")
	if len(c.Events()) != 66 || strings.Count(stderr.String(), "malformed event:") != 64 {
		t.Fatal(len(c.Events()), stderr.String())
	}
	nilWriter := testWriter(t, writerConfig(writerSinkFunc(func(context.Context, Event) error { return ErrRejected }), nil))
	nilWriter.Emit(context.Background(), "bad", nil)
	nilWriter.Emit(context.Background(), "thing.done", nil)
	flushWriter(t, nilWriter)
}

// R-IWJD-FYZ4
func TestWriterRandom(t *testing.T) {
	var c Capture
	cfg := writerConfig(&c, nil)
	w := testWriter(t, cfg)
	want := strings.Repeat("07", 16)
	var g sync.WaitGroup
	for i := 0; i < 64; i++ {
		g.Go(func() {
			id, err := w.mintRequestID()
			if err != nil || id != want {
				t.Errorf("id %q err %v", id, err)
			}
		})
	}
	g.Wait()
	original := rand.Reader
	rand.Reader = bytes.NewReader(bytes.Repeat([]byte{9}, 16))
	t.Cleanup(func() { rand.Reader = original })
	cfg.Rand = nil
	randomWriter := testWriter(t, cfg)
	id, err := randomWriter.mintRequestID()
	if err != nil || id != strings.Repeat("09", 16) {
		t.Fatal(id, err)
	}

}

// R-VU49-1EG8
func TestWriterDefaultClockAndPause(t *testing.T) {
	var c Capture
	cfg := writerConfig(&c, nil)
	cfg.Now = nil
	cfg.Sleep = nil
	w := testWriter(t, cfg)
	before := time.Now().UTC().Truncate(time.Microsecond)
	w.Emit(context.Background(), "thing.done", nil)
	flushWriter(t, w)
	after := time.Now().UTC()
	stamp := c.Events()[0].Time
	if stamp.Before(before) || stamp.After(after) || stamp.Location() != time.UTC || stamp.Nanosecond()%1000 != 0 {
		t.Fatal(stamp)
	}
}

// R-VSWC-NMPJ
func TestWriterDefaultSocket(t *testing.T) {
	directory, err := os.MkdirTemp("", "writer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	socket := filepath.Join(directory, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var c Capture
	server := &http.Server{Handler: IngestHandler(&c), ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	services := filepath.Join(directory, "services")
	if err := os.WriteFile(services, []byte(fmt.Sprintf(`{"services":[{"name":"telemetry","url":"","description":"","socket":%q,"enabled":true,"mcp":false}]}`, socket)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IKIGENBA_SERVICES", services)
	cfg := writerConfig(nil, nil)
	w := testWriter(t, cfg)
	w.Emit(context.Background(), "thing.done", nil)
	flushWriter(t, w)
	if e := c.Events(); len(e) != 1 || e[0].Name != "thing.done" {
		t.Fatal(e)
	}
}

// R-DZYK-CK7E R-WEUJ-JI21 R-W2NJ-PSN3
func TestWriterFullQueueDrains(t *testing.T) {
	var capture Capture
	entered := make(chan struct{})
	release := make(chan struct{})
	formed := make(chan struct{})
	done := make(chan struct{})
	cfg := writerConfig(writerSinkFunc(func(ctx context.Context, e Event) error {
		if e.Name == "first.done" {
			close(entered)
			<-release
		}
		return capture.Deliver(ctx, e)
	}), nil)
	clock := cfg.Now
	calls := 0
	cfg.Now = func() time.Time {
		calls++
		if calls == QueueCapacity+2 {
			close(formed)
		}
		return clock()
	}
	w := testWriter(t, cfg)
	w.Emit(context.Background(), "first.done", nil)
	<-entered
	for i := 0; i < QueueCapacity; i++ {
		w.Emit(context.Background(), "queued.done", Attrs{"index": i})
	}
	go func() { w.Shutdown(context.Background(), "drain"); close(done) }()
	<-formed
	select {
	case <-done:
		t.Fatal("shutdown returned before delivery")
	default:
	}
	close(release)
	<-done
	events := capture.Events()
	if len(events) != QueueCapacity+2 || events[0].Name != "first.done" || events[len(events)-1].Name != "service.stopping" {
		t.Fatal(len(events))
	}
	for i := 0; i < QueueCapacity; i++ {
		if events[i+1].Attrs["index"] != int64(i) {
			t.Fatal(i, events[i+1])
		}
	}
}

type writerLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *writerLines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, string(p))
	return len(p), nil
}

// R-W07Q-Y95P R-W7J5-8VLV R-WJQ5-2L0T R-DAQ0-H1OR
func TestWriterSingleWrite(t *testing.T) {
	var lines writerLines
	w := testWriter(t, writerConfig(writerSinkFunc(func(context.Context, Event) error { return ErrRejected }), &lines))
	w.Emit(context.Background(), "bad", Attrs{"secret": []byte("private")})
	w.Emit(context.Background(), "thing.done", nil)
	flushWriter(t, w)
	lines.mu.Lock()
	defer lines.mu.Unlock()
	if len(lines.lines) != 2 {
		t.Fatal(lines.lines)
	}
	for _, line := range lines.lines {
		if strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") {
			t.Fatal(line)
		}
	}
	malformed := expectedWriterEvent("bad", Attrs{"secret": nil})
	valid := expectedWriterEvent("thing.done", nil)
	if lines.lines[0] != eventLine(t, malformed, "malformed") || lines.lines[1] != eventLine(t, valid, "undelivered") {
		t.Fatal(lines.lines)
	}
}

// R-E3M9-HVFH R-E4U5-VN66
func TestWriterShutdownCancelsPause(t *testing.T) {
	paused := make(chan context.Context, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var stderr bytes.Buffer
	var calls []Event
	var callErrors []error
	var pauses []context.Context
	var pauseErrors []error
	cfg := writerConfig(writerSinkFunc(func(ctx context.Context, e Event) error {
		calls = append(calls, e)
		callErrors = append(callErrors, ctx.Err())
		return errors.New("offline")
	}), &stderr)
	cfg.Sleep = func(ctx context.Context, _ time.Duration) {
		pauses = append(pauses, ctx)
		pauseErrors = append(pauseErrors, ctx.Err())
		if len(pauses) == 1 {
			paused <- ctx
			<-release
		}
	}
	w := testWriter(t, cfg)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	w.Emit(context.Background(), "thing.done", nil)
	pauseCtx := <-paused
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.Shutdown(ctx, "deadline")
	if pauseCtx.Err() == nil {
		t.Fatal("pause remained live")
	}
	before := stderr.String()
	releaseOnce.Do(func() { close(release) })
	<-w.senderDone
	if len(calls)-1+len(pauses)-1 > 1 || stderr.String() != before {
		t.Fatal("sender exceeded shutdown bound or wrote output", calls, len(pauses), stderr.String())
	}
	for i := 1; i < len(calls); i++ {
		if callErrors[i] == nil || !reflect.DeepEqual(calls[i], expectedWriterEvent("thing.done", Attrs{})) {
			t.Fatal("invalid late delivery", calls[i], callErrors[i])
		}
	}
	for _, err := range pauseErrors[1:] {
		if err == nil {
			t.Fatal("late pause had live context")
		}
	}
}

// R-VWK1-SXXM R-W7J5-8VLV
func TestWriterCopiedUndelivered(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var stderr bytes.Buffer
	sink := writerSinkFunc(func(_ context.Context, e Event) error {
		if e.Name == "first.done" {
			close(entered)
			<-release
			return nil
		}
		return ErrRejected
	})
	w := testWriter(t, writerConfig(sink, &stderr))
	w.Emit(context.Background(), "first.done", nil)
	<-entered
	attrs := Attrs{"value": "before", "count": int16(7)}
	w.Emit(context.Background(), "copied.done", attrs)
	attrs["value"] = "after"
	attrs["extra"] = true
	delete(attrs, "count")
	close(release)
	flushWriter(t, w)
	want := eventLine(t, expectedWriterEvent("copied.done", Attrs{"value": "before", "count": int64(7)}), "undelivered")
	if stderr.String() != want {
		t.Fatal(stderr.String(), want)
	}
}

type observedFlushContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *observedFlushContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

// R-WII8-OTA4
func TestWriterFlushSnapshot(t *testing.T) {
	firstEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	secondEntered := make(chan struct{})
	secondRelease := make(chan struct{})
	sink := writerSinkFunc(func(_ context.Context, e Event) error {
		switch e.Name {
		case "first.done":
			close(firstEntered)
			<-firstRelease
		case "second.done":
			close(secondEntered)
			<-secondRelease
		}
		return nil
	})
	w := testWriter(t, writerConfig(sink, nil))
	w.Emit(context.Background(), "first.done", nil)
	<-firstEntered
	ctx := &observedFlushContext{Context: context.Background(), waiting: make(chan struct{})}
	flushed := make(chan error, 1)
	go func() { flushed <- w.Flush(ctx) }()
	<-ctx.waiting
	w.Emit(context.Background(), "second.done", nil)
	close(firstRelease)
	<-secondEntered
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	close(secondRelease)
	flushWriter(t, w)
}

// R-VU49-1EG8 R-J1EY-Z1XW
func TestWriterDefaultPauseDuration(t *testing.T) {
	var firstFailure time.Time
	var secondAttempt time.Time
	calls := 0
	cfg := writerConfig(writerSinkFunc(func(_ context.Context, e Event) error {
		if e.Name != "thing.done" {
			return nil
		}
		calls++
		if calls == 1 {
			firstFailure = time.Now()
			return errors.New("transient")
		}
		secondAttempt = time.Now()
		return nil
	}), nil)
	cfg.Now = nil
	cfg.Sleep = nil
	w := testWriter(t, cfg)
	before := time.Now().UTC().Truncate(time.Microsecond)
	got := w.Now()
	after := time.Now().UTC()
	if got.Before(before) || got.After(after) || got.Location() != time.UTC || got.Nanosecond()%1000 != 0 {
		t.Fatal(got)
	}
	w.Emit(context.Background(), "thing.done", nil)
	flushWriter(t, w)
	if calls != 2 || secondAttempt.Sub(firstFailure) < RetryBackoff {
		t.Fatal(calls, secondAttempt.Sub(firstFailure))
	}
}

// R-WKY1-GCRI
func TestWriterConcurrentShutdown(t *testing.T) {
	var capture Capture
	var stderr bytes.Buffer
	w := testWriter(t, writerConfig(&capture, &stderr))
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Go(func() {
			w.Ready()
			w.Emit(context.Background(), "thing.done", nil)
			_ = w.Now()
			if err := w.Flush(context.Background()); err != nil {
				t.Error(err)
			}
			w.Shutdown(context.Background(), "done")
		})
	}
	workers.Wait()
	events := capture.Events()
	stopping, emitted := 0, 0
	for _, event := range events {
		switch event.Name {
		case "service.stopping":
			stopping++
		case "thing.done":
			emitted++
		}
	}
	lateLine := eventLine(t, expectedWriterEvent("thing.done", nil), "undelivered")
	late := strings.Count(stderr.String(), lateLine)
	if stopping != 1 || emitted+late != 32 || stderr.String() != strings.Repeat(lateLine, late) {
		t.Fatal(events, stderr.String())
	}
}

// R-DZYK-CK7E
func TestWriterMalformedShutdown(t *testing.T) {
	for _, year := range []int{-1, 10000} {
		t.Run(fmt.Sprint(year), func(t *testing.T) {
			var capture Capture
			var stderr bytes.Buffer
			cfg := writerConfig(&capture, &stderr)
			cfg.Now = func() time.Time { return time.Date(year, 1, 2, 3, 4, 5, 123456789, time.UTC) }
			w := testWriter(t, cfg)
			ctx := identity.NewContext(context.Background(), identity.Caller{RequestID: "stop", UserID: "stopper"})
			w.Shutdown(ctx, "invalid clock")
			w.Shutdown(ctx, "again")
			<-w.senderDone
			want := Event{Time: cfg.Now().Truncate(time.Microsecond), Service: cfg.Service, Name: "service.stopping", RequestID: "stop", User: "stopper", Attrs: Attrs{"reason": "invalid clock"}}
			if got := stderr.String(); got != eventLine(t, want, "malformed") {
				t.Fatal(got)
			}
			if events := capture.Events(); len(events) != 0 {
				t.Fatal(events)
			}
		})
	}
}

// R-E3M9-HVFH
func TestWriterRetryContexts(t *testing.T) {
	emitCtx, cancelEmit := context.WithCancel(context.Background())
	cancelEmit()
	var contexts []context.Context
	var pauses []context.Context
	cfg := writerConfig(writerSinkFunc(func(ctx context.Context, e Event) error {
		if e.Name == "service.stopping" {
			return nil
		}
		began := time.Now()
		deadline, ok := ctx.Deadline()
		if !ok || deadline.After(began.Add(AttemptTimeout)) {
			t.Error("attempt has no bounded deadline", deadline)
		}
		if ctx.Err() != nil {
			t.Error("canceled Emit canceled delivery", ctx.Err())
		}
		contexts = append(contexts, ctx)
		return errors.New("offline")
	}), nil)
	cfg.Sleep = func(ctx context.Context, _ time.Duration) {
		if ctx.Err() != nil {
			t.Error("canceled Emit canceled pause", ctx.Err())
		}
		pauses = append(pauses, ctx)
	}
	w := testWriter(t, cfg)
	w.Emit(emitCtx, "thing.done", nil)
	flushWriter(t, w)
	if len(contexts) != Attempts || len(pauses) != Attempts-1 {
		t.Fatal(len(contexts), len(pauses))
	}
	shutdownCtx, cancelShutdown := context.WithCancel(context.Background())
	cancelShutdown()
	w.Shutdown(shutdownCtx, "done")
	<-w.senderDone
	for _, ctx := range append(contexts, pauses...) {
		if ctx.Err() == nil {
			t.Fatal("shutdown left context live")
		}
	}
}

// R-E16G-QBY3 R-E4U5-VN66
func TestWriterBoundedCallsAfterShutdown(t *testing.T) {
	for _, result := range []error{nil, errors.New("offline"), ErrRejected} {
		t.Run(fmt.Sprint(result), func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			var stderr bytes.Buffer
			var calls []Event
			var callErrors []error
			var pauseErrors []error
			cfg := writerConfig(writerSinkFunc(func(ctx context.Context, e Event) error {
				calls = append(calls, e)
				callErrors = append(callErrors, ctx.Err())
				if len(calls) == 1 {
					close(entered)
					<-release
				}
				return result
			}), &stderr)
			cfg.Sleep = func(ctx context.Context, _ time.Duration) { pauseErrors = append(pauseErrors, ctx.Err()) }
			w := testWriter(t, cfg)
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			w.Emit(context.Background(), "first.done", nil)
			<-entered
			w.Emit(context.Background(), "second.done", nil)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			w.Shutdown(ctx, "deadline")
			want := eventLine(t, expectedWriterEvent("first.done", nil), "undelivered") + eventLine(t, expectedWriterEvent("second.done", nil), "undelivered") + eventLine(t, expectedWriterEvent("service.stopping", Attrs{"reason": "deadline"}), "undelivered")
			if got := stderr.String(); got != want {
				t.Fatal(got)
			}
			releaseOnce.Do(func() { close(release) })
			<-w.senderDone
			lateCalls := len(calls) - 1 + len(pauseErrors)
			if lateCalls > 1 || stderr.String() != want {
				t.Fatal("sender exceeded shutdown bound or wrote output", calls, pauseErrors, stderr.String())
			}
			if (result == nil || errors.Is(result, ErrRejected)) && lateCalls != 0 {
				t.Fatal("sender retried success or rejection", calls, pauseErrors)
			}
			for i := 1; i < len(calls); i++ {
				if callErrors[i] == nil || !reflect.DeepEqual(calls[i], expectedWriterEvent("first.done", Attrs{})) {
					t.Fatal("invalid late delivery", calls[i], callErrors[i])
				}
			}
			for _, err := range pauseErrors {
				if err == nil {
					t.Fatal("late pause had live context")
				}
			}
			w.Emit(context.Background(), "late.done", nil)
			w.Emit(context.Background(), "bad", nil)
			want += eventLine(t, expectedWriterEvent("late.done", nil), "undelivered") + eventLine(t, expectedWriterEvent("bad", nil), "malformed")
			if got := stderr.String(); got != want {
				t.Fatal(got)
			}
		})
	}
}
