// Package trail records and emits the run lifecycle.
package trail

import (
	"context"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

// Started names a newly recorded run.
const Started = "scrape.started"

// Finished names a recorded run ending.
const Finished = "scrape.finished"

// Emits returns independent declarations of the lifecycle events.
func Emits() []events.Emission {
	return []events.Emission{{Event: Started, Attrs: []string{"scraper", "run"}}, {Event: Finished, Attrs: []string{"scraper", "run", "status", "reason", "rows", "new", "changed", "unchanged", "gone"}}}
}

// Record sends a lifecycle event as the stored starter, preserving its cause.
func Record(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, r store.Run) {
	attrs := telemetry.Attrs{"scraper": r.Scraper, "run": r.ID}
	switch name {
	case Started:
	case Finished:
		attrs["status"], attrs["reason"] = r.Status, r.Reason
		attrs["rows"], attrs["new"], attrs["changed"], attrs["unchanged"], attrs["gone"] = r.Rows, r.New, r.Changed, r.Unchanged, r.Gone
	default:
		return
	}
	ctx = identity.NewContext(ctx, identity.Caller{UserID: r.UserID, RequestID: r.RequestID})
	if w != nil {
		w.Emit(ctx, name, attrs)
	}
	if em != nil {
		em.Emit(ctx, name, events.Attrs(attrs))
	}
}

// Collected names a completed collect phase.
const Collected = "scrape.collected"

// Loaded names a completed load phase.
const Loaded = "scrape.loaded"

// Phase records successful build phases in telemetry as the stored starter.
func Phase(ctx context.Context, w *telemetry.Writer, name string, r store.Run, bytes int64, elapsed, paused time.Duration) {
	if w == nil || (name != Collected && name != Loaded) {
		return
	}
	ctx = identity.NewContext(ctx, identity.Caller{UserID: r.UserID, RequestID: r.RequestID})
	w.Emit(ctx, name, telemetry.Attrs{"scraper": r.Scraper, "run": r.ID, "bytes": bytes, "duration_us": int64(elapsed / time.Microsecond), "paused_us": int64(paused / time.Microsecond)})
}
