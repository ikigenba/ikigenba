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
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/trail"
)

var instant = time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)

func pair(t *testing.T, ts telemetry.Sink, es events.Sink) (*telemetry.Writer, *events.Emitter) {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return instant }
	sleep := func(context.Context, time.Duration) {}
	w := telemetry.New(telemetry.Config{Service: "webhooks", Sink: ts, Stderr: io.Discard, Now: now, Sleep: sleep, Rand: bytes.NewReader(make([]byte, 65536))})
	em := events.New(events.Config{Service: "webhooks", Sink: es, Stderr: io.Discard, Now: now, Sleep: sleep, Rand: bytes.NewReader(make([]byte, 65536)), Emits: trail.Emits()})
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

func hook(scheme string) store.Webhook {
	return store.Webhook{ID: "whk_3c8e1f5a7b9d2046", Slug: "gh_push", Scheme: scheme, OwnerID: "owner", OwnerEmail: "owner@example.com", SecretSHA256: "unused", SecretPlain: "unused"}
}

func callerContext() context.Context {
	return identity.NewContext(context.Background(), identity.Caller{UserID: "actor", Email: "actor@example.com", RequestID: "request"})
}

func signatures(func(context.Context, *telemetry.Writer, *events.Emitter, string, store.Webhook), func(context.Context, *telemetry.Writer, *events.Emitter, store.Webhook, store.Delivery)) {
}

// R-06O8-IOS4 R-07W4-WGIT R-0941-A89I
func TestDeclarationsAndNames(t *testing.T) {
	kinds := []string{trail.Created, trail.Rotated, trail.Deleted, trail.Received}
	if !reflect.DeepEqual(kinds, []string{"created", "rotated", "deleted", "received"}) {
		t.Fatal(kinds)
	}
	signatures(trail.Lifecycle, trail.Receive)
	expected := []events.Emission{
		{Event: "webhook.*.created", Attrs: []string{"hook", "scheme"}},
		{Event: "webhook.*.rotated", Attrs: []string{"hook", "scheme"}},
		{Event: "webhook.*.deleted", Attrs: []string{"hook", "scheme"}},
		{Event: "webhook.*.received", Attrs: []string{"hook", "delivery", "type", "content_type", "bytes"}},
	}
	first, second := trail.Emits(), trail.Emits()
	if !reflect.DeepEqual(first, expected) {
		t.Fatal(first)
	}
	for i := range first {
		first[i].Event = "changed"
		for j := range first[i].Attrs {
			first[i].Attrs[j] = "changed"
		}
	}
	if !reflect.DeepEqual(second, expected) || !reflect.DeepEqual(trail.Emits(), expected) {
		t.Fatal("declarations share storage")
	}
	for _, s := range []string{"gh_push", "tick", "", "x.y"} {
		for _, k := range append(kinds, "anything") {
			if got := trail.Name(s, k); got != "webhook."+s+"."+k {
				t.Fatal(got)
			}
		}
	}
}

func check(t *testing.T, ts *telemetry.Capture, es *events.Capture, name string, attrs map[string]any, identified, caused bool) {
	t.Helper()
	a, b := ts.Events(), es.Events()
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("trail %v bus %v", a, b)
	}
	request, user := "", ""
	if identified {
		request, user = "request", "actor"
	}
	if a[0].Name != name || b[0].Name != name || !reflect.DeepEqual(map[string]any(a[0].Attrs), attrs) || !reflect.DeepEqual(map[string]any(b[0].Attrs), attrs) || a[0].RequestID != request || b[0].RequestID != request || a[0].User != user || b[0].User != user {
		t.Fatalf("trail %+v bus %+v", a[0], b[0])
	}
	cause, depth := "", 0
	if caused {
		cause, depth = "evt_8c3f1a6e2d9b4075", 4
	}
	if b[0].Cause != cause || b[0].Depth != depth {
		t.Fatalf("cause %q depth %d", b[0].Cause, b[0].Depth)
	}
}

func contexts() map[string]struct {
	ctx                context.Context
	identified, caused bool
} {
	return map[string]struct {
		ctx                context.Context
		identified, caused bool
	}{
		"bare":    {context.Background(), false, false},
		"caller":  {callerContext(), true, false},
		"caused":  {events.NewContext(callerContext(), events.Cause{ID: "evt_8c3f1a6e2d9b4075", Depth: 3}), true, true},
		"orphans": {events.NewContext(context.Background(), events.Cause{ID: "evt_8c3f1a6e2d9b4075", Depth: 3}), false, true},
	}
}

// R-0ABX-O007 R-0CRQ-FJHL
func TestLifecycle(t *testing.T) {
	for _, kind := range []string{trail.Created, trail.Rotated, trail.Deleted} {
		for _, scheme := range []string{store.Bearer, store.GitHubHMAC} {
			for label, c := range contexts() {
				t.Run(kind+"/"+scheme+"/"+label, func(t *testing.T) {
					ts, es := &telemetry.Capture{}, &events.Capture{}
					w, em := pair(t, ts, es)
					h := hook(scheme)
					trail.Lifecycle(c.ctx, w, em, kind, h)
					flush(t, w, em)
					check(t, ts, es, "webhook.gh_push."+kind, map[string]any{"hook": h.ID, "scheme": scheme}, c.identified, c.caused)
				})
			}
		}
	}
	for _, kind := range []string{trail.Received, "anything", ""} {
		ts, es := &telemetry.Capture{}, &events.Capture{}
		w, em := pair(t, ts, es)
		trail.Lifecycle(callerContext(), w, em, kind, hook(store.Bearer))
		flush(t, w, em)
		if len(ts.Events()) != 0 || len(es.Events()) != 0 {
			t.Fatalf("%q: %v %v", kind, ts.Events(), es.Events())
		}
	}
}

// R-0BJU-1RQW R-0CRQ-FJHL
func TestReceive(t *testing.T) {
	d := store.Delivery{ID: "whd_0a1b2c3d4e5f6071", HookID: "whk_3c8e1f5a7b9d2046", Received: instant, ContentType: "application/json", GitHubEvent: "push", GitHubDelivery: "delivery-guid", Body: []byte(`{"ref":"main"}`)}
	for _, scheme := range []string{store.Bearer, store.GitHubHMAC} {
		for label, c := range contexts() {
			t.Run(scheme+"/"+label, func(t *testing.T) {
				ts, es := &telemetry.Capture{}, &events.Capture{}
				w, em := pair(t, ts, es)
				h := hook(scheme)
				trail.Receive(c.ctx, w, em, h, d)
				flush(t, w, em)
				kind := ""
				if scheme == store.GitHubHMAC {
					kind = "push"
				}
				check(t, ts, es, "webhook.gh_push.received", map[string]any{"hook": h.ID, "delivery": d.ID, "type": kind, "content_type": "application/json", "bytes": int64(len(d.Body))}, c.identified, c.caused)
			})
		}
	}
	ts, es := &telemetry.Capture{}, &events.Capture{}
	w, em := pair(t, ts, es)
	empty := store.Delivery{ID: "whd_0a1b2c3d4e5f6071"}
	trail.Receive(context.Background(), w, em, hook(store.Bearer), empty)
	flush(t, w, em)
	check(t, ts, es, "webhook.gh_push.received", map[string]any{"hook": "whk_3c8e1f5a7b9d2046", "delivery": empty.ID, "type": "", "content_type": "", "bytes": int64(0)}, false, false)
}
