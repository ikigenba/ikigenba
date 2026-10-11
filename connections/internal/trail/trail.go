// Package trail records committed connection changes.
package trail

import (
	"context"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/connections/internal/store"
)

// Connection event names.
const (
	Created      = "connection.created"
	Deleted      = "connection.deleted"
	Broken       = "connection.broken"
	RevokeFailed = "connection.revoke_failed"
)

// Emits returns independent declarations of the three bus events.
func Emits() []events.Emission {
	return []events.Emission{{Event: Created, Attrs: []string{"provider", "account", "scopes"}}, {Event: Deleted, Attrs: []string{"provider", "account", "scopes"}}, {Event: Broken, Attrs: []string{"provider", "account", "scopes"}}}
}

// Record records and emits a supported connection lifecycle change.
func Record(ctx context.Context, w *telemetry.Writer, em *events.Emitter, name string, c store.Connection) {
	switch name {
	case Created, Deleted, Broken:
		attrs := telemetry.Attrs{"provider": c.Provider, "account": c.AccountEmail, "scopes": strings.Join(c.Scopes, " ")}
		w.Emit(ctx, name, attrs)
		em.Emit(ctx, name, events.Attrs(attrs))
	}
}

// RevokeFailure records a provider revoke failure in telemetry alone.
func RevokeFailure(ctx context.Context, w *telemetry.Writer, c store.Connection, status int) {
	w.Emit(ctx, RevokeFailed, telemetry.Attrs{"provider": c.Provider, "account": c.AccountEmail, "status": status})
}
