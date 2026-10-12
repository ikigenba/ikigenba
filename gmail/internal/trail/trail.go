// Package trail tells Gmail's committed transfer and draft changes.
package trail

import (
	"context"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
)

// Names of the changes Gmail emits.
const (
	TransferStarted  = "transfer.started"
	TransferFinished = "transfer.finished"
	DraftCreated     = "draft.created"
	DraftSent        = "draft.sent"
	DraftDiscarded   = "draft.discarded"
)

// Emits returns independent declarations for the run's emitter.
func Emits() []events.Emission {
	return []events.Emission{
		{Event: TransferStarted, Attrs: []string{"transfer", "direction", "message", "attachment", "draft", "path"}},
		{Event: TransferFinished, Attrs: []string{"transfer", "direction", "message", "attachment", "draft", "path", "status", "reason", "bytes"}},
		{Event: DraftCreated, Attrs: []string{"draft", "message", "thread", "attachments"}},
		{Event: DraftSent, Attrs: []string{"draft", "message", "thread", "attachments"}},
		{Event: DraftDiscarded, Attrs: []string{"draft", "message", "thread", "attachments"}},
	}
}

// Caller restores the identity under which a transfer was accepted.
func Caller(t store.Transfer) identity.Caller {
	return identity.Caller{UserID: t.UserID, Email: t.UserEmail, RequestID: t.RequestID}
}

// Record tells a committed transfer change as its original caller.
func Record(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, t store.Transfer) {
	if name != TransferStarted && name != TransferFinished {
		return
	}
	attrs := telemetry.Attrs{"transfer": t.ID, "direction": t.Direction, "message": t.Message, "attachment": t.Attachment, "draft": t.Draft, "path": t.Path}
	if name == TransferFinished {
		attrs["status"], attrs["reason"], attrs["bytes"] = t.Status, t.Reason, t.Bytes
	}
	tell(identity.NewContext(ctx, Caller(t)), w, em, name, attrs)
}

// RecordDraft tells a committed draft change as the current caller.
func RecordDraft(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, d store.Draft) {
	if name != DraftCreated && name != DraftSent && name != DraftDiscarded {
		return
	}
	tell(ctx, w, em, name, telemetry.Attrs{"draft": d.ID, "message": d.Message, "thread": d.Thread, "attachments": int64(len(d.Attachments))})
}

func tell(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, attrs telemetry.Attrs) {
	if w != nil {
		w.Emit(ctx, name, attrs)
	}
	if em != nil {
		em.Emit(ctx, name, events.Attrs(attrs))
	}
}
