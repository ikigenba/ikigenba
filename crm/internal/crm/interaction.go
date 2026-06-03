package crm

import (
	"database/sql"
	"time"
)

// interactionStore is the SQL-only data layer for the append-only interactions
// timeline. It satisfies the entity interface (Get/Search/Delete) and adds
// Insert, the append used by crm_log. Interactions are created via Log, never
// crm_save (PLAN.md §3). Fleshed out in Phase 1.
type interactionStore struct{}

// Insert appends one interaction. The dispatcher resolves the subject_id to
// exactly one of contactID/orgID/dealID (by probe-by-id) before calling.
func (interactionStore) Insert(tx *sql.Tx, kind, body string, occurredAt time.Time, contactID, orgID, dealID *string, now time.Time) (Summary, error) {
	return Summary{}, invalid("type", "interaction.Insert not yet implemented")
}

func (interactionStore) Get(tx *sql.Tx, id string) (Card, error) {
	return nil, ErrNotFound
}

func (interactionStore) Search(tx *sql.Tx, p SearchParams) ([]Summary, error) {
	return nil, nil
}

func (interactionStore) Delete(tx *sql.Tx, id string, at time.Time) error {
	return ErrNotFound
}
