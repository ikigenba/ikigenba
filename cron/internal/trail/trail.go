// Package trail records trigger changes and fires in telemetry and on the bus.
package trail

import (
	"context"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/cron/internal/store"
)

// Trigger event kinds.
const (
	Created = "created"
	Paused  = "paused"
	Resumed = "resumed"
	Deleted = "deleted"
	Fired   = "fired"
)

// Emits returns independent declarations for the five trigger event kinds.
func Emits() []events.Emission {
	return []events.Emission{
		{Event: "cron.*.created", Attrs: []string{"trigger", "when"}},
		{Event: "cron.*.paused", Attrs: []string{"trigger", "when"}},
		{Event: "cron.*.resumed", Attrs: []string{"trigger", "when"}},
		{Event: "cron.*.deleted", Attrs: []string{"trigger", "when"}},
		{Event: "cron.*.fired", Attrs: []string{"trigger", "when", "scheduled"}},
	}
}

// Name returns the event name for a trigger slug and kind.
func Name(slug, kind string) string { return "cron." + slug + "." + kind }

// Lifecycle queues both records for a lifecycle change; other kinds do nothing.
func Lifecycle(ctx context.Context, w *telemetry.Writer, em *events.Emitter, kind string, t store.Trigger) {
	switch kind {
	case Created, Paused, Resumed, Deleted:
		w.Emit(ctx, Name(t.Slug, kind), telemetry.Attrs{"trigger": t.ID, "when": t.When})
		em.Emit(ctx, Name(t.Slug, kind), events.Attrs{"trigger": t.ID, "when": t.When})
	}
}

// Fire queues both records for the given scheduled slot.
func Fire(ctx context.Context, w *telemetry.Writer, em *events.Emitter, t store.Trigger, scheduled time.Time) {
	attrs := telemetry.Attrs{"trigger": t.ID, "when": t.When, "scheduled": scheduled.UTC().Format(time.RFC3339)}
	w.Emit(ctx, Name(t.Slug, Fired), attrs)
	em.Emit(ctx, Name(t.Slug, Fired), events.Attrs(attrs))
}
