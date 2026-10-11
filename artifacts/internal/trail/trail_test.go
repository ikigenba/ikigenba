package trail_test

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
	"github.com/ikigenba/ikigenba/artifacts/internal/trail"
)

func captures(t *testing.T) (*telemetry.Writer, *events.Emitter, *telemetry.Capture, *events.Capture) {
	t.Helper()
	ts, es := &telemetry.Capture{}, &events.Capture{}
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	sleep := func(context.Context, time.Duration) {}
	w := telemetry.New(telemetry.Config{Service: "artifacts", Sink: ts, Now: now, Sleep: sleep, Rand: bytes.NewReader(make([]byte, 65536)), Stderr: io.Discard})
	em := events.New(events.Config{Service: "artifacts", Sink: es, Now: now, Sleep: sleep, Rand: bytes.NewReader(make([]byte, 65536)), Stderr: io.Discard, Emits: trail.Emits()})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		em.Shutdown(ctx)
		w.Shutdown(ctx, "test")
	})
	return w, em, ts, es
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

func signatures(func() []events.Emission, func(context.Context, *telemetry.Writer, *events.Emitter, string, store.Artifact), func(context.Context, *telemetry.Writer, store.Artifact)) {
}

// R-4PEU-O9QI R-C0BC-DA8N
func TestSurfaceAndDeclarations(t *testing.T) {
	signatures(trail.Emits, trail.Record, trail.Orphan)
	if got := []string{trail.Created, trail.Replaced, trail.Deleted, trail.Orphaned}; !reflect.DeepEqual(got, []string{"artifact.created", "artifact.replaced", "artifact.deleted", "artifact.orphaned"}) {
		t.Fatal(got)
	}
	want := []events.Emission{
		{Event: "artifact.created", Attrs: []string{"artifact", "path", "bytes", "content_type"}},
		{Event: "artifact.replaced", Attrs: []string{"artifact", "path", "bytes", "content_type"}},
		{Event: "artifact.deleted", Attrs: []string{"artifact", "path", "bytes", "content_type"}},
	}
	first, second := trail.Emits(), trail.Emits()
	if !reflect.DeepEqual(first, want) {
		t.Fatal(first)
	}
	for i := range first {
		first[i].Event = "changed"
		for j := range first[i].Attrs {
			first[i].Attrs[j] = "changed"
		}
	}
	if !reflect.DeepEqual(second, want) || !reflect.DeepEqual(trail.Emits(), want) {
		t.Fatal("declarations share storage")
	}
}

// R-C2R5-4TQ1 R-C3Z1-ILGQ
func TestRecord(t *testing.T) {
	a := store.Artifact{ID: "art_0123456789abcdef", Path: "reports/data.bin", Bytes: 1234, ContentType: "application/custom", SHA256: "excluded", OwnerID: "excluded-owner", OwnerEmail: "excluded-email"}
	for _, name := range []string{trail.Created, trail.Replaced, trail.Deleted} {
		for _, identified := range []bool{false, true} {
			for _, caused := range []bool{false, true} {
				t.Run(name+"/"+boolName(identified)+"/"+boolName(caused), func(t *testing.T) {
					w, em, ts, es := captures(t)
					ctx := context.Background()
					request, user := "", ""
					if identified {
						request, user = "request", "caller"
						ctx = identity.NewContext(ctx, identity.Caller{UserID: user, Email: "caller@example.test", RequestID: request})
					}
					cause, depth := "", 0
					if caused {
						cause, depth = "evt_0123456789abcdef", 5
						ctx = events.NewContext(ctx, events.Cause{ID: cause, Depth: depth - 1})
					}
					trail.Record(ctx, w, em, name, a)
					flush(t, w, em)
					recorded, bus := ts.Events(), es.Events()
					if len(recorded) != 1 || len(bus) != 1 {
						t.Fatalf("trail=%v bus=%v", recorded, bus)
					}
					attrs := map[string]any{"artifact": a.ID, "path": a.Path, "bytes": a.Bytes, "content_type": a.ContentType}
					if recorded[0].Name != name || bus[0].Name != name || !reflect.DeepEqual(map[string]any(recorded[0].Attrs), attrs) || !reflect.DeepEqual(map[string]any(bus[0].Attrs), attrs) || recorded[0].RequestID != request || bus[0].RequestID != request || recorded[0].User != user || bus[0].User != user || bus[0].Cause != cause || bus[0].Depth != depth {
						t.Fatalf("trail=%+v bus=%+v", recorded[0], bus[0])
					}
				})
			}
		}
	}
	for _, name := range []string{"", "unknown", trail.Orphaned, "artifact.created.extra"} {
		w, em, ts, es := captures(t)
		trail.Record(context.Background(), w, em, name, a)
		flush(t, w, em)
		if len(ts.Events()) != 0 || len(es.Events()) != 0 {
			t.Fatalf("invalid name %q emitted", name)
		}
	}
}

func boolName(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// R-4QMR-21H7
func TestOrphan(t *testing.T) {
	a := store.Artifact{ID: "art_abcdef0123456789", Path: "lost.bin", Bytes: 17, ContentType: "application/custom", SHA256: "excluded", OwnerEmail: "excluded"}
	for _, identified := range []bool{false, true} {
		w, em, ts, es := captures(t)
		ctx := context.Background()
		request, user := "", ""
		if identified {
			request, user = "request", "owner"
			ctx = identity.NewContext(ctx, identity.Caller{UserID: user, RequestID: request})
		}
		trail.Orphan(ctx, w, a)
		trail.Record(ctx, w, em, trail.Orphaned, a)
		flush(t, w, em)
		got := ts.Events()
		attrs := map[string]any{"artifact": a.ID, "path": a.Path, "bytes": a.Bytes, "content_type": a.ContentType}
		if len(got) != 1 || len(es.Events()) != 0 {
			t.Fatalf("trail=%v bus=%v", got, es.Events())
		}
		if got[0].Name != trail.Orphaned || got[0].User != user || got[0].RequestID != request || !reflect.DeepEqual(map[string]any(got[0].Attrs), attrs) {
			t.Fatal(got[0])
		}
	}
}
