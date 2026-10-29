package trail_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func writer(t *testing.T, sink telemetry.Sink) *telemetry.Writer {
	t.Helper()
	w := telemetry.New(telemetry.Config{Service: "accounts", Sink: sink, Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }, Rand: zeroReader{}, Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
	t.Cleanup(func() { w.Shutdown(context.Background(), "done") })
	return w
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// R-9NTT-PE98 R-9P1Q-35ZX R-9Q9M-GXQM R-9RHI-UPHB R-9SPF-8H80 R-9TXB-M8YP R-9V58-00PE R-9YSX-5BXH
func TestChangeAndResult(t *testing.T) {
	if trail.EventChangeFinished != "change.finished" || trail.ResultOK != "ok" || trail.ResultRefused != "refused" || trail.ResultFailed != "failed" {
		t.Fatal("wire constants")
	}
	capture := &telemetry.Capture{}
	w := writer(t, capture)
	caller := identity.Caller{UserID: "creator", Email: "private@example.test", RequestID: "request"}
	cases := []struct {
		err  error
		want string
	}{{nil, trail.ResultOK}, {&host.RefusedError{Status: 4, Diagnostic: "diagnostic"}, trail.ResultRefused}, {fmt.Errorf("wrapped: %w", &host.RefusedError{Status: 2, Diagnostic: "other"}), trail.ResultRefused}, {&host.FailedError{Status: 1}, trail.ResultFailed}, {errors.New("failure"), trail.ResultFailed}}
	for i, tc := range cases {
		if got := trail.Result(tc.err); got != tc.want {
			t.Fatalf("Result=%q want %q", got, tc.want)
		}
		ctx := identity.NewContext(context.Background(), caller)
		if i%2 == 0 {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			ctx = cancelled
		}
		trail.Change(ctx, w, "tool", "group", "user", tc.want)
	}
	trail.Change(context.Background(), w, "", "", "", "")
	w.Emit(context.Background(), "tool.called", telemetry.Attrs{})
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != 7 {
		t.Fatalf("events %v", events)
	}
	for i, e := range events[:5] {
		if e.Name != trail.EventChangeFinished || e.RequestID != caller.RequestID || e.User != caller.UserID || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"tool": "tool", "group": "group", "user": "user", "result": cases[i].want}) {
			t.Fatalf("event %d: %+v", i, e)
		}
	}
	if e := events[5]; e.User != "" || e.RequestID != "" || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"tool": "", "group": "", "user": "", "result": ""}) {
		t.Fatalf("guest event %+v", e)
	}
	if events[6].Name != "tool.called" {
		t.Fatal(events)
	}
}

type blockedSink struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockedSink) Deliver(_ context.Context, _ telemetry.Event) error {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return nil
}

// R-9WD4-DSG3
func TestChangeQueuesWithoutDelivery(t *testing.T) {
	sink := &blockedSink{entered: make(chan struct{}), release: make(chan struct{})}
	w := writer(t, sink)
	released := false
	defer func() {
		if !released {
			close(sink.release)
		}
	}()
	done := make(chan struct{})
	go func() { trail.Change(context.Background(), w, "tool", "", "", trail.ResultOK); close(done) }()
	select {
	case <-sink.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("delivery never started")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Change waited for delivery")
	}
	close(sink.release)
	released = true
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// R-9XL0-RK6S
func TestConcurrentChanges(t *testing.T) {
	capture := &telemetry.Capture{}
	w := writer(t, capture)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			value := fmt.Sprint(i)
			ctx := identity.NewContext(context.Background(), identity.Caller{UserID: value, RequestID: value})
			trail.Change(ctx, w, value, value, value, value)
		})
	}
	wg.Wait()
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != 100 {
		t.Fatal(len(events))
	}
	seen := map[string]bool{}
	for _, e := range events {
		v := e.User
		if seen[v] || e.RequestID != v || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"tool": v, "group": v, "user": v, "result": v}) {
			t.Fatalf("bad event %+v", e)
		}
		seen[v] = true
	}
}
