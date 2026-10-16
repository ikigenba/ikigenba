// Package trail records and emits the candidate pass lifecycle.
package trail

import (
	"context"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// Lifecycle event names and the accepted scraper completion event.
const (
	Started       = "pass.started"
	Finished      = "pass.finished"
	Accepted      = "scrape.finished"
	AcceptedJudge = "cron.judge_candidates.fired"
)

// Emits returns independent declarations of the pass lifecycle events.
func Emits() []events.Emission {
	return []events.Emission{{Event: Started, Attrs: []string{"pass", "kind", "origin", "trigger"}}, {Event: Finished, Attrs: []string{"pass", "kind", "origin", "trigger", "status", "reason", "rows", "new", "changed", "unchanged", "gone", "created", "linked", "judged", "rescored", "unranged", "due", "fetched", "https", "http", "none"}}}
}

// Record records and emits a lifecycle event as the saved pass starter.
func Record(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, p store.Pass) {
	if name != Started && name != Finished {
		return
	}
	ctx = identity.NewContext(ctx, identity.Caller{UserID: p.UserID, RequestID: p.RequestID})
	attrs := telemetry.Attrs{"pass": p.ID, "kind": p.Kind, "origin": p.Origin, "trigger": p.Trigger}
	if name == Finished {
		attrs["judged"] = p.Judged
		attrs["due"], attrs["fetched"], attrs["https"], attrs["http"], attrs["none"] = p.Due, p.Fetched, p.HTTPS, p.HTTP, p.None
		attrs["status"], attrs["reason"] = p.Status, p.Reason
		attrs["rows"], attrs["new"], attrs["changed"], attrs["unchanged"] = p.Rows, p.New, p.Changed, p.Unchanged
		attrs["gone"], attrs["created"], attrs["linked"], attrs["rescored"], attrs["unranged"] = p.Gone, p.Created, p.Linked, p.Judged, p.Unranged
	}
	if w != nil {
		w.Emit(ctx, name, attrs)
	}
	if em != nil {
		em.Emit(ctx, name, events.Attrs(attrs))
	}
}
