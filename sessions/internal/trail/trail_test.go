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

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/trail"
)

var trailNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func trailWriter(t *testing.T, sink telemetry.Sink) *telemetry.Writer {
	t.Helper()
	t.Setenv(services.Variable, "")
	w := telemetry.New(telemetry.Config{Service: "sessions", Sink: sink, Stderr: io.Discard, Now: func() time.Time { return trailNow }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(make([]byte, 4096))})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		w.Shutdown(ctx, "test")
	})
	return w
}

func trailFlush(t *testing.T, w *telemetry.Writer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}

type recorderCase struct {
	name  string
	call  func(context.Context, *telemetry.Writer)
	attrs telemetry.Attrs
}

func recorderCases(n int, text string) []recorderCase {
	var (
		push         func(context.Context, *telemetry.Writer, int, int, int, string)
		addRule      func(context.Context, *telemetry.Writer, string, string, string, string, int, string)
		removeRule   func(context.Context, *telemetry.Writer, string, string, string, string, int, string)
		setPrice     func(context.Context, *telemetry.Writer, string, string, int, string)
		removePrice  func(context.Context, *telemetry.Writer, string, string, int, string)
		setAwayLimit func(context.Context, *telemetry.Writer, int, int, string)
		refresh      func(context.Context, *telemetry.Writer, int, string)
	)
	push, addRule, removeRule, setPrice, removePrice, setAwayLimit, refresh = trail.PushTurns, trail.AddRule, trail.RemoveRule, trail.SetPrice, trail.RemovePrice, trail.SetAwayLimit, trail.Refresh

	value := func(field string) string {
		if text == "" {
			return ""
		}
		return text + "-" + field
	}
	host, directory, command, kind := value("host"), value("directory"), value("command"), value("kind")
	model, effective, result := value("model"), value("effective"), value("result")
	next, changed := n+1, n+2
	if n == 0 {
		next = 0
		changed = 0
	}
	return []recorderCase{
		{trail.EventChangeFinished, func(ctx context.Context, w *telemetry.Writer) { push(ctx, w, n, next, changed, result) }, telemetry.Attrs{"tool": "push_turns", "sessions": int64(n), "turns_new": int64(next), "turns_changed": int64(changed), "result": result}},
		{trail.EventChangeFinished, func(ctx context.Context, w *telemetry.Writer) {
			addRule(ctx, w, host, directory, command, kind, n, result)
		}, telemetry.Attrs{"tool": "add_rule", "host": host, "directory": directory, "command": command, "kind": kind, "roots": int64(n), "result": result}},
		{trail.EventChangeFinished, func(ctx context.Context, w *telemetry.Writer) {
			removeRule(ctx, w, host, directory, command, kind, n, result)
		}, telemetry.Attrs{"tool": "remove_rule", "host": host, "directory": directory, "command": command, "kind": kind, "roots": int64(n), "result": result}},
		{trail.EventChangeFinished, func(ctx context.Context, w *telemetry.Writer) { setPrice(ctx, w, model, effective, n, result) }, telemetry.Attrs{"tool": "set_price", "model": model, "effective": effective, "roots": int64(n), "result": result}},
		{trail.EventChangeFinished, func(ctx context.Context, w *telemetry.Writer) { removePrice(ctx, w, model, effective, n, result) }, telemetry.Attrs{"tool": "remove_price", "model": model, "effective": effective, "roots": int64(n), "result": result}},
		{trail.EventChangeFinished, func(ctx context.Context, w *telemetry.Writer) { setAwayLimit(ctx, w, n, next, result) }, telemetry.Attrs{"tool": "set_away_limit", "minutes": int64(n), "roots": int64(next), "result": result}},
		{trail.EventRefreshFinished, func(ctx context.Context, w *telemetry.Writer) { refresh(ctx, w, n, result) }, telemetry.Attrs{"roots": int64(n), "result": result}},
	}
}

// R-PSPN-5XQ1 R-PV5F-XH7F R-PWDC-B8Y4
func TestTrailConstants(t *testing.T) {
	const (
		changeFinished  = trail.EventChangeFinished
		refreshFinished = trail.EventRefreshFinished
		resultOK        = trail.ResultOK
		resultRefused   = trail.ResultRefused
		resultFailed    = trail.ResultFailed
	)
	if changeFinished != "change.finished" || refreshFinished != "refresh.finished" || resultOK != "ok" || resultRefused != "refused" || resultFailed != "failed" {
		t.Fatal("trail constants differ")
	}
}

// R-PXL8-P0OT R-PYT5-2SFI R-QMSP-CE47 R-Q18X-UBWW R-Q2GU-83NL R-Q3OQ-LVEA R-Q4WM-ZN4Z
// R-Q7CF-R6MD R-Q8KC-4YD2 R-Q9S8-IQ3R R-QO0L-Q5UW R-HZ8A-HUOT R-I0G6-VMFI R-QENU-1T2J R-QFVQ-FKT8
// R-I1O3-9E67 R-QIBJ-74AM
func TestRecordersExactEvents(t *testing.T) {
	for _, n := range []int{7, 0, -3} {
		for _, text := range []string{"named value", ""} {
			for i, rc := range recorderCases(n, text) {
				for _, identified := range []bool{false, true} {
					for _, cancelled := range []bool{false, true} {
						t.Run(fmt.Sprintf("%d/%q/%d/%t/%t", n, text, i, identified, cancelled), func(t *testing.T) {
							capture := &telemetry.Capture{}
							w := trailWriter(t, capture)
							ctx, cancel := context.WithCancel(context.Background())
							defer cancel()
							if identified {
								ctx = identity.NewContext(ctx, identity.Caller{UserID: "caller", Email: "private@invalid.test", RequestID: "call-id"})
							}
							if cancelled {
								cancel()
							}
							rc.call(ctx, w)
							w.Emit(context.Background(), "tool.called", nil)
							trailFlush(t, w)
							got := capture.Events()
							if len(got) != 2 {
								t.Fatalf("events=%+v", got)
							}
							want := telemetry.Event{Time: trailNow, Service: "sessions", Name: rc.name, Attrs: rc.attrs}
							if identified {
								want.User = "caller"
								want.RequestID = "call-id"
							}
							if !reflect.DeepEqual(got[0], want) || got[1].Name != "tool.called" {
								t.Fatalf("got=%+v want first=%+v", got, want)
							}
						})
					}
				}
			}
		}
	}
}

// R-Q64J-DEVO R-QLZ8-CFIP
func TestResultClassification(t *testing.T) {
	var classify func(error) string
	refusal := &store.RefusedError{Reason: "test refusal"}
	classify = trail.Result
	for _, tc := range []struct {
		err  error
		want string
	}{{nil, trail.ResultOK}, {refusal, trail.ResultRefused}, {fmt.Errorf("wrapped: %w", refusal), trail.ResultRefused}, {store.ErrUnreachable, trail.ResultFailed}, {fmt.Errorf("wrapped: %w", store.ErrUnreachable), trail.ResultFailed}, {errors.New("other"), trail.ResultFailed}} {
		if got := classify(tc.err); got != tc.want {
			t.Errorf("Result(%v)=%q want %q", tc.err, got, tc.want)
		}
	}
}

type blockingTrailSink struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingTrailSink) Deliver(context.Context, telemetry.Event) error {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return nil
}

// R-QJJF-KW1B
func TestRecordersDoNotWaitForSink(t *testing.T) {
	for i, rc := range recorderCases(3, "value") {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			sink := &blockingTrailSink{entered: make(chan struct{}), release: make(chan struct{})}
			w := trailWriter(t, sink)
			defer close(sink.release)
			returned := make(chan struct{})
			go func() { rc.call(context.Background(), w); close(returned) }()
			select {
			case <-returned:
			case <-time.After(3 * time.Second):
				t.Fatal("recorder waited for delivery")
			}
			select {
			case <-sink.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("delivery never began")
			}
		})
	}
}

// R-I2VZ-N5WW
func TestRecordersConcurrent(t *testing.T) {
	capture := &telemetry.Capture{}
	w := trailWriter(t, capture)
	expected := map[string]telemetry.Event{}
	var wg sync.WaitGroup
	for n := range 24 {
		for i, rc := range recorderCases(n, fmt.Sprint(n)) {
			key := fmt.Sprintf("request-%d-%d", n, i)
			expected[key] = telemetry.Event{Time: trailNow, Service: "sessions", Name: rc.name, RequestID: key, User: fmt.Sprint(n), Attrs: rc.attrs}
			wg.Go(func() {
				rc.call(identity.NewContext(context.Background(), identity.Caller{UserID: fmt.Sprint(n), Email: "private@invalid.test", RequestID: key}), w)
			})
		}
	}
	wg.Wait()
	trailFlush(t, w)
	events := capture.Events()
	if len(events) != len(expected) {
		t.Fatalf("got %d events want %d", len(events), len(expected))
	}
	for _, e := range events {
		want, ok := expected[e.RequestID]
		if !ok || !reflect.DeepEqual(e, want) {
			t.Errorf("unexpected event %+v; want %+v", e, want)
		}
		delete(expected, e.RequestID)
	}
	if len(expected) != 0 {
		t.Fatalf("missing %v", expected)
	}
}
