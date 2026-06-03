package crm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"eventplane/outbox"
)

// Service is the dispatcher seam (PLAN.md §4, §8): it owns the single *sql.Tx and
// runs decode → entity write → dedup probe → outbox append → commit atomically
// over the one SQLite file. Entity stores are pure SQL against the passed tx and
// never own a transaction.
type Service struct {
	DB  *sql.DB
	Now func() time.Time
	// Outbox, when set, makes the service an event-plane producer: first-wave
	// contact events are appended atomically with the domain write and the feed
	// is rung after commit (PLAN.md §6). Nil disables event emission.
	Outbox *outbox.Outbox

	orgs         organizationStore
	contacts     contactStore
	deals        dealStore
	tasks        taskStore
	interactions interactionStore
}

func NewService(db *sql.DB) *Service {
	return &Service{DB: db, Now: time.Now}
}

// entity is the uniform read/delete contract every entity store satisfies. Save
// is intentionally absent: each entity's Save takes a typed <Type>Input, so the
// polymorphic create/update path is a typed switch in Save below — the one honest
// type-switch (PLAN.md §4). Get/Search/Delete are uniform and are dispatched
// generically here.
type entity interface {
	Get(tx *sql.Tx, id string) (Card, error)
	Search(tx *sql.Tx, p SearchParams) ([]Summary, error)
	Delete(tx *sql.Tx, id string, at time.Time) error
}

// typedEntity pairs an entity store with its type name. The probe order for
// crm_get is fixed (PLAN.md §4: up to five indexed point lookups, one per table).
func (s *Service) entities() []struct {
	name string
	e    entity
} {
	return []struct {
		name string
		e    entity
	}{
		{"organization", s.orgs},
		{"contact", s.contacts},
		{"deal", s.deals},
		{"task", s.tasks},
		{"interaction", s.interactions},
	}
}

// byType returns the entity store for a type name, or ok=false for an unknown
// type. interaction is reachable for Get/Search/Delete but not Save (it is
// created via Log).
func (s *Service) byType(typ string) (entity, bool) {
	for _, te := range s.entities() {
		if te.name == typ {
			return te.e, true
		}
	}
	return nil, false
}

// ── Get: probe-by-id type resolution → per-type card (PLAN.md §4) ────────────

func (s *Service) Get(ctx context.Context, id string) (Card, error) {
	if id == "" {
		return nil, invalid("id", "id is required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	for _, te := range s.entities() {
		card, err := te.e.Get(tx, id)
		if err == nil {
			return card, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	return nil, ErrNotFound
}

// ── Delete: shallow soft-delete, routed by type (PLAN.md §8) ─────────────────
//
// Delete is shallow: each entity soft-deletes only its own row + owned children.
// It does not cascade to or block on other entities; dangling FKs are tolerated
// because every read path filters deleted_at IS NULL.
func (s *Service) Delete(ctx context.Context, typ, id string) error {
	if id == "" {
		return invalid("id", "id is required")
	}
	e, ok := s.byType(typ)
	if !ok {
		return invalid("type", "unknown type "+typ)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if err := e.Delete(tx, id, s.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

// ── Search, Save, Log: fleshed out in Phase 2 (PLAN.md §9) ───────────────────

// Search is implemented in search.go (cross-entity filtered/recency search).

// Save is the polymorphic upsert. The typed decode/normalize switch, the exact
// dedup probe, and the first-wave event emission are wired in Phase 2.
func (s *Service) Save(ctx context.Context, typ, id string, fields []byte, force bool) (Summary, error) {
	return Summary{}, invalid("type", "crm_save is not yet implemented")
}

// Log appends an interaction to the timeline. Subject resolution (subject_id →
// contact/org/deal FK by probe-by-id) and the append are wired in Phase 2.
func (s *Service) Log(ctx context.Context, in LogInput) (Summary, error) {
	return Summary{}, invalid("type", "crm_log is not yet implemented")
}
