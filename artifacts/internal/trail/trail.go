// Package trail records committed artifact changes in telemetry and on the bus.
package trail

import (
	"context"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/artifacts/internal/store"
)

// Artifact event names.
const (
	Created  = "artifact.created"
	Replaced = "artifact.replaced"
	Deleted  = "artifact.deleted"
	Orphaned = "artifact.orphaned"
)

// Emits returns independent declarations for the three bus events.
func Emits() []events.Emission {
	return []events.Emission{
		{Event: Created, Attrs: []string{"artifact", "path", "bytes", "content_type"}},
		{Event: Replaced, Attrs: []string{"artifact", "path", "bytes", "content_type"}},
		{Event: Deleted, Attrs: []string{"artifact", "path", "bytes", "content_type"}},
	}
}

func attributes(a store.Artifact) telemetry.Attrs {
	return telemetry.Attrs{"artifact": a.ID, "path": a.Path, "bytes": a.Bytes, "content_type": a.ContentType}
}

// Record records and emits one committed lifecycle change; other names do nothing.
func Record(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, a store.Artifact) {
	switch name {
	case Created, Replaced, Deleted:
		attrs := attributes(a)
		w.Emit(ctx, name, attrs)
		em.Emit(ctx, name, events.Attrs(attrs))
	}
}

// Orphan records a reconciled missing file in telemetry alone.
func Orphan(ctx context.Context, w *telemetry.Writer, a store.Artifact) {
	w.Emit(ctx, Orphaned, attributes(a))
}
