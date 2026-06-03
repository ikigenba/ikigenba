package crm

import (
	"database/sql"
	"time"
)

// organizationStore is the SQL-only data layer for the organizations table.
// Every method takes a *sql.Tx so the dispatcher composes the transaction
// (PLAN.md §8). Fleshed out in Phase 1.
type organizationStore struct{}

func (organizationStore) Save(tx *sql.Tx, id string, in OrganizationInput, now time.Time) (Summary, error) {
	return Summary{}, invalid("type", "organization.Save not yet implemented")
}

func (organizationStore) Get(tx *sql.Tx, id string) (Card, error) {
	return nil, ErrNotFound
}

func (organizationStore) Search(tx *sql.Tx, p SearchParams) ([]Summary, error) {
	return nil, nil
}

func (organizationStore) Delete(tx *sql.Tx, id string, at time.Time) error {
	return ErrNotFound
}
