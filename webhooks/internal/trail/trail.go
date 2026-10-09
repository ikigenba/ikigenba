// Package trail records webhook changes and deliveries in telemetry and on the bus.
package trail

import (
	"context"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
)

// Webhook event kinds.
const (
	Created  = "created"
	Rotated  = "rotated"
	Deleted  = "deleted"
	Received = "received"
)

// Emits returns independent declarations for the four webhook event kinds.
func Emits() []events.Emission {
	return []events.Emission{
		{Event: "webhook.*.created", Attrs: []string{"hook", "scheme"}},
		{Event: "webhook.*.rotated", Attrs: []string{"hook", "scheme"}},
		{Event: "webhook.*.deleted", Attrs: []string{"hook", "scheme"}},
		{Event: "webhook.*.received", Attrs: []string{"hook", "delivery", "type", "content_type", "bytes"}},
	}
}

// Name returns the event name for a webhook slug and kind.
func Name(slug, kind string) string { return "webhook." + slug + "." + kind }

// Lifecycle queues both records for a lifecycle change; other kinds do nothing.
func Lifecycle(ctx context.Context, w *telemetry.Writer, em *events.Emitter, kind string, h store.Webhook) {
	switch kind {
	case Created, Rotated, Deleted:
		w.Emit(ctx, Name(h.Slug, kind), telemetry.Attrs{"hook": h.ID, "scheme": h.Scheme})
		em.Emit(ctx, Name(h.Slug, kind), events.Attrs{"hook": h.ID, "scheme": h.Scheme})
	}
}

// Receive queues both records for an accepted delivery.
func Receive(ctx context.Context, w *telemetry.Writer, em *events.Emitter, h store.Webhook, d store.Delivery) {
	kind := ""
	if h.Scheme == store.GitHubHMAC {
		kind = d.GitHubEvent
	}
	attrs := telemetry.Attrs{"hook": h.ID, "delivery": d.ID, "type": kind, "content_type": d.ContentType, "bytes": int64(len(d.Body))}
	w.Emit(ctx, Name(h.Slug, Received), attrs)
	em.Emit(ctx, Name(h.Slug, Received), events.Attrs(attrs))
}
