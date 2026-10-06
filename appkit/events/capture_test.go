package events_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/events"
)

// R-FBAC-4AMP R-GJ6P-W1BP
func TestCaptureZeroAndSnapshots(t *testing.T) {
	var c events.Capture
	var sink events.Sink = &c
	deliver := c.Deliver
	snapshot := c.Events
	captureMethodShapes(deliver, snapshot)
	if got := snapshot(); got == nil || len(got) != 0 {
		t.Fatal(got)
	}
	a := events.Event{ID: "first"}
	b := events.Event{ID: "second"}
	if err := deliver(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := sink.Deliver(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	got := snapshot()
	if !reflect.DeepEqual(got, []events.Event{a, b}) {
		t.Fatal(got)
	}
	got[0] = b
	if !reflect.DeepEqual(snapshot(), []events.Event{a, b}) {
		t.Fatal(snapshot())
	}
}

// R-GKEM-9T2E
func TestCaptureConcurrent(t *testing.T) {
	var c events.Capture
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := c.Deliver(context.Background(), events.Event{Depth: i}); err != nil {
				t.Error(err)
			}
			_ = c.Events()
		}(i)
	}
	wg.Wait()
	got := c.Events()
	if len(got) != 100 {
		t.Fatal(len(got))
	}
	seen := make(map[int]bool)
	for _, e := range got {
		if seen[e.Depth] {
			t.Fatal("duplicate", e.Depth)
		}
		seen[e.Depth] = true
	}
}

func captureMethodShapes(func(context.Context, events.Event) error, func() []events.Event) {}
