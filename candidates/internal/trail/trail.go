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
	Started           = "pass.started"
	Finished          = "pass.finished"
	Accepted          = "scrape.finished"
	AcceptedJudge     = "cron.judge_candidates.fired"
	AcceptedAdded     = "campaign.member.added"
	AcceptedRejected  = "campaign.member.rejected"
	AcceptedDelivered = "campaign.piece.delivered"
)

// Emits returns independent declarations of the pass lifecycle events.
func Emits() []events.Emission {
	return []events.Emission{{Event: Started, Attrs: []string{"pass", "kind", "origin", "trigger", "target", "enrich_kind"}}, {Event: Finished, Attrs: []string{"pass", "kind", "origin", "trigger", "target", "enrich_kind", "status", "reason", "parcel_run", "rows", "new", "changed", "unchanged", "gone", "created", "linked", "judged", "unranged", "derived", "due", "fetched", "https", "http", "none", "runs", "runs_failed", "pages", "cost", "parcels_read", "by_label", "by_value", "by_candidate", "parks_created", "parks_updated", "parks_gone", "parks_grown", "parks_walked", "circles", "requests", "found", "new_candidates"}}}
}

// Record records and emits a lifecycle event as the saved pass starter.
func Record(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, p store.Pass) {
	if name != Started && name != Finished {
		return
	}
	ctx = identity.NewContext(ctx, identity.Caller{UserID: p.UserID, RequestID: p.RequestID})
	attrs := telemetry.Attrs{"pass": p.ID, "kind": p.Kind, "origin": p.Origin, "trigger": p.Trigger, "target": p.Target, "enrich_kind": p.EnrichKind}
	if name == Finished {
		attrs["parcel_run"] = p.ParcelRun
		attrs["parcels_read"], attrs["by_label"], attrs["by_value"], attrs["by_candidate"] = p.ParcelsRead, p.ByLabel, p.ByValue, p.ByCandidate
		attrs["parks_created"], attrs["parks_updated"], attrs["parks_gone"], attrs["parks_grown"] = p.ParksCreated, p.ParksUpdated, p.ParksGone, p.ParksGrown
		attrs["parks_walked"], attrs["circles"], attrs["requests"], attrs["found"], attrs["new_candidates"] = p.ParksWalked, p.Circles, p.Requests, p.Found, p.NewCandidates
		attrs["judged"] = p.Judged
		attrs["runs"], attrs["runs_failed"], attrs["pages"], attrs["cost"] = p.Runs, p.RunsFailed, p.Pages, p.Cost
		attrs["derived"] = p.Derived
		attrs["due"], attrs["fetched"], attrs["https"], attrs["http"], attrs["none"] = p.Due, p.Fetched, p.HTTPS, p.HTTP, p.None
		attrs["status"], attrs["reason"] = p.Status, p.Reason
		attrs["rows"], attrs["new"], attrs["changed"], attrs["unchanged"] = p.Rows, p.New, p.Changed, p.Unchanged
		attrs["gone"], attrs["created"], attrs["linked"], attrs["unranged"] = p.Gone, p.Created, p.Linked, p.Unranged
	}
	if w != nil {
		w.Emit(ctx, name, attrs)
	}
	if em != nil {
		em.Emit(ctx, name, events.Attrs(attrs))
	}
}
