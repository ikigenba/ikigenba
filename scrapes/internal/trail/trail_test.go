package trail_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/trail"
)

// R-2MG2-MHBE R-2NNZ-0923
func TestDeclarations(t *testing.T) {
	if trail.Started != "scrape.started" || trail.Finished != "scrape.finished" {
		t.Fatal("names")
	}
	want := []events.Emission{{Event: trail.Started, Attrs: []string{"scraper", "run"}}, {Event: trail.Finished, Attrs: []string{"scraper", "run", "status", "reason", "rows", "new", "changed", "unchanged", "gone"}}}
	first := trail.Emits()
	second := trail.Emits()
	if !reflect.DeepEqual(first, want) {
		t.Fatal(first)
	}
	first[0].Attrs[0] = "changed"
	first[1].Event = "other"
	if !reflect.DeepEqual(second, want) || !reflect.DeepEqual(trail.Emits(), want) {
		t.Fatal("shared storage")
	}
}

// R-2OVV-E0SS R-2Q3R-RSJH R-2RBO-5KA6
func TestLifecycle(t *testing.T) {
	for _, cause := range []bool{false, true} {
		for _, caller := range []bool{false, true} {
			for _, reason := range []string{"", store.ReasonSource} {
				tc := new(telemetry.Capture)
				ec := new(events.Capture)
				now := func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) }
				w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: tc, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 100))})
				em := events.New(events.Config{Service: "scrapes", Sink: ec, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{2}, 100)), Emits: trail.Emits()})
				ctx := context.Background()
				if caller {
					ctx = identity.NewContext(ctx, identity.Caller{UserID: "other", RequestID: "other-request"})
				}
				if cause {
					ctx = events.NewContext(ctx, events.Cause{ID: "evt_0101010101010101", Depth: 3})
				}
				r := store.Run{ID: "run_0202020202020202", Scraper: "test", Status: store.StatusDone, RequestID: "starter-request", UserID: "starter", Rows: 5, New: 2, Changed: 1, Unchanged: 2, Gone: 3}
				r.Reason = reason
				if reason != "" {
					r.Status = store.StatusFailed
				}
				trail.Record(ctx, w, em, trail.Started, r)
				trail.Record(ctx, w, em, trail.Finished, r)
				trail.Record(ctx, w, em, "other", r)
				deadline, cancel := context.WithTimeout(context.Background(), time.Second)
				if err := w.Flush(deadline); err != nil {
					t.Fatal(err)
				}
				if err := em.Flush(deadline); err != nil {
					t.Fatal(err)
				}
				ts, es := tc.Events(), ec.Events()
				if len(ts) != 2 || len(es) != 2 {
					t.Fatalf("counts %d %d", len(ts), len(es))
				}
				for i, name := range []string{trail.Started, trail.Finished} {
					want := telemetry.Attrs{"scraper": r.Scraper, "run": r.ID}
					if i == 1 {
						want["status"] = r.Status
						want["reason"] = r.Reason
						want["rows"] = r.Rows
						want["new"] = r.New
						want["changed"] = r.Changed
						want["unchanged"] = r.Unchanged
						want["gone"] = r.Gone
					}
					if ts[i].Name != name || es[i].Name != name || ts[i].RequestID != r.RequestID || es[i].RequestID != r.RequestID || ts[i].User != r.UserID || es[i].User != r.UserID || !reflect.DeepEqual(ts[i].Attrs, want) || !reflect.DeepEqual(es[i].Attrs, events.Attrs(want)) {
						t.Fatalf("wrong lifecycle: %#v %#v", ts[i], es[i])
					}
					if cause {
						if es[i].Cause != "evt_0101010101010101" || es[i].Depth != 4 {
							t.Fatal(es[i])
						}
					} else if es[i].Cause != "" || es[i].Depth != 0 {
						t.Fatal(es[i])
					}
				}
				em.Shutdown(deadline)
				w.Shutdown(deadline, "test")
				cancel()
			}
		}
	}
}
