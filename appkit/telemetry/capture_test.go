package telemetry

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

// R-VQGJ-W385 R-DFLM-04NJ
func TestCaptureSnapshot(t *testing.T) {
	var c Capture
	var sink Sink = &c
	if e := c.Events(); e == nil || len(e) != 0 {
		t.Fatal(e)
	}
	want := []Event{
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "a", "one.done", "request", "actor", Attrs{"key": "value"}},
		{time.Time{}, "b", "two.done", "other", "user", Attrs{"number": int64(2)}},
	}
	for _, event := range want {
		if err := sink.Deliver(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	e := c.Events()
	if !reflect.DeepEqual(e, want) {
		t.Fatal(e)
	}
	e[0] = Event{}
	if c.Events()[0].Name != "one.done" {
		t.Fatal("shared slice")
	}
}

// R-WNDU-7W8W
func TestCaptureConcurrent(t *testing.T) {
	var c Capture
	var g sync.WaitGroup
	for i := 0; i < 100; i++ {
		g.Go(func() {
			if err := c.Deliver(context.Background(), Event{Name: "one.done"}); err != nil {
				t.Error(err)
			}
			_ = c.Events()
		})
	}
	g.Wait()
	if len(c.Events()) != 100 {
		t.Fatal("lost events")
	}
}
