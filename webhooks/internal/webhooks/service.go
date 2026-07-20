// Package webhooks holds the domain service that sits over the Phase 1 Store
// and the same *sql.DB the appkit chassis opens and migrates. This file is the
// D1 seam: a concrete Service wired to a deterministic Clock so time-dependent
// behavior is reproducible in tests. The Outbox is exported and injected later
// by the Producer hook (Phase 3); it stays nil until then.
package webhooks

import (
	"context"
	"database/sql"
	"time"

	"eventplane/outbox"

	"webhooks/internal/db"
)

// Clock is the time seam. Production wiring uses RealClock; tests inject a
// deterministic clock so created_at and friends are reproducible.
type Clock interface{ Now() time.Time }

// RealClock returns the actual wall clock in UTC.
type RealClock struct{}

// Now returns the current time in UTC.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// Service is the webhooks domain service. It owns name/secret lifecycle over the
// concrete Store and the *sql.DB the chassis migrated. Outbox is left nil this
// phase — events arrive in Phase 3 when the Producer hook injects it.
type Service struct {
	store *db.Store
	db    *sql.DB
	clock Clock

	// Outbox is injected by the Producer hook; nil until Phase 3 (no events yet).
	Outbox *outbox.Outbox
}

// NewService builds a Service over an open, migrated *sql.DB and a Clock.
func NewService(conn *sql.DB, clock Clock) *Service {
	return &Service{
		store: db.NewStore(conn),
		db:    conn,
		clock: clock,
	}
}

// List returns exactly the webhooks owned by ownerID, ordered by name. It is a thin
// owner-scoped wrapper over Store.ListByOwner so the unexported store stays
// private to package webhooks while the MCP handler reaches owner-scoped reads
// through the Service alone.
func (s *Service) List(ctx context.Context, ownerID string) ([]db.Webhook, error) {
	return s.store.ListByOwner(ctx, ownerID)
}

// Delete removes ownerID's webhook by name, owner-scoped. deleted reports whether a
// row was actually removed; another owner's webhook is left untouched and deleted
// is false. Thin wrapper over Store.Delete for the same encapsulation reason as
// List.
func (s *Service) Delete(ctx context.Context, ownerID, name string) (deleted bool, err error) {
	return s.store.Delete(ctx, ownerID, name)
}
