package trail_test

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
	"github.com/ikigenba/ikigenba/gdrive/internal/trail"
)

// R-ZZXT-2KFI R-015P-GC67 R-02DL-U3WW R-03LI-7VNL R-04TE-LNEA R-061A-ZF4Z
func TestTrail(t *testing.T) {
	decl := trail.Emits()
	want := []events.Emission{{Event: "transfer.started", Attrs: []string{"transfer", "direction", "file", "path"}}, {Event: "transfer.finished", Attrs: []string{"transfer", "direction", "file", "path", "status", "reason", "bytes"}}}
	if !reflect.DeepEqual(decl, want) {
		t.Fatal(decl)
	}
	decl[0].Attrs[0] = "mutated"
	if !reflect.DeepEqual(trail.Emits(), want) {
		t.Fatal("shared declarations")
	}
	now := func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	sink := &telemetry.Capture{}
	bus := &events.Capture{}
	w := telemetry.New(telemetry.Config{Service: "test", Sink: sink, Stderr: io.Discard, Now: now, Rand: strings.NewReader(strings.Repeat("q", 1024)), Sleep: func(context.Context, time.Duration) {}})
	em := events.New(events.Config{Service: "test", Sink: bus, Stderr: io.Discard, Now: now, Rand: strings.NewReader(strings.Repeat("w", 1024)), Sleep: func(context.Context, time.Duration) {}, Emits: trail.Emits()})
	defer w.Shutdown(context.Background(), "test")
	defer em.Shutdown(context.Background())
	tr := store.Transfer{ID: "xfr_sample", Direction: "push", File: "", Path: "thing", Status: "done", Bytes: 19, UserID: "owner", UserEmail: "owner@test", RequestID: "request"}
	if trail.Caller(tr) != (identity.Caller{UserID: "owner", Email: "owner@test", RequestID: "request"}) {
		t.Fatal("caller")
	}
	ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "other"})
	ctx = events.NewContext(ctx, events.Cause{ID: "evt_0123456789abcdef", Depth: 3})
	trail.Record(ctx, w, em, trail.Started, tr)
	trail.Record(ctx, w, em, trail.Finished, tr)
	trail.Record(ctx, w, em, "unknown", tr)
	trail.Record(context.Background(), w, em, trail.Started, tr)
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := em.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	ts, es := sink.Events(), bus.Events()
	if len(ts) != 3 || len(es) != 3 {
		t.Fatal(len(ts), len(es))
	}
	for i := range ts {
		attrs := telemetry.Attrs{"transfer": tr.ID, "direction": tr.Direction, "file": tr.File, "path": tr.Path}
		name := trail.Started
		if i == 1 {
			name = trail.Finished
			attrs["status"] = tr.Status
			attrs["reason"] = tr.Reason
			attrs["bytes"] = tr.Bytes
		}
		if ts[i].Name != name || es[i].Name != name || ts[i].RequestID != tr.RequestID || es[i].RequestID != tr.RequestID || ts[i].User != tr.UserID || es[i].User != tr.UserID || !reflect.DeepEqual(ts[i].Attrs, attrs) || !reflect.DeepEqual(es[i].Attrs, events.Attrs(attrs)) {
			t.Fatal(ts[i], es[i])
		}
		if i < 2 && (es[i].Cause != "evt_0123456789abcdef" || es[i].Depth != 4) {
			t.Fatal(es[i])
		}
		if i == 2 && (es[i].Cause != "" || es[i].Depth != 0) {
			t.Fatal(es[i])
		}
	}
}
