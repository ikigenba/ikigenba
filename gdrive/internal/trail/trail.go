// Package trail records and emits transfer lifecycle events.
package trail

import (
	"context"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
)

// Started names the beginning of a transfer.
const Started = "transfer.started"

// Finished names a transfer ending.
const Finished = "transfer.finished"

// Emits declares the two transfer events and their attribute keys.
func Emits() []events.Emission {
	return []events.Emission{{Event: Started, Attrs: []string{"transfer", "direction", "file", "path"}}, {Event: Finished, Attrs: []string{"transfer", "direction", "file", "path", "status", "reason", "bytes"}}}
}

// Caller reconstructs the original caller of a transfer.
func Caller(t store.Transfer) identity.Caller {
	return identity.Caller{UserID: t.UserID, Email: t.UserEmail, RequestID: t.RequestID}
}

// Record tells a transfer event to telemetry and the event bus.
func Record(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, t store.Transfer) {
	if name != Started && name != Finished {
		return
	}
	ctx = identity.NewContext(ctx, Caller(t))
	attrs := telemetry.Attrs{"transfer": t.ID, "direction": t.Direction, "file": t.File, "path": t.Path}
	if name == Finished {
		attrs["status"] = t.Status
		attrs["reason"] = t.Reason
		attrs["bytes"] = t.Bytes
	}
	if w != nil {
		w.Emit(ctx, name, attrs)
	}
	if em != nil {
		em.Emit(ctx, name, events.Attrs(attrs))
	}
}
