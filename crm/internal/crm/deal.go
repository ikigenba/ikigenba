package crm

import (
	"database/sql"
	"time"
)

// dealStore is the SQL-only data layer for deals and their participants
// (deal_contacts). Fleshed out in Phase 1.
type dealStore struct{}

func (dealStore) Save(tx *sql.Tx, id string, in DealInput, now time.Time) (Summary, error) {
	return Summary{}, invalid("type", "deal.Save not yet implemented")
}

func (dealStore) Get(tx *sql.Tx, id string) (Card, error) {
	return nil, ErrNotFound
}

func (dealStore) Search(tx *sql.Tx, p SearchParams) ([]Summary, error) {
	return nil, nil
}

func (dealStore) Delete(tx *sql.Tx, id string, at time.Time) error {
	return ErrNotFound
}
