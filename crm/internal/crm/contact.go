package crm

import (
	"database/sql"
	"time"
)

// contactStore is the SQL-only data layer for contacts and their owned children
// (emails, phones, tags). Fleshed out in Phase 1.
type contactStore struct{}

func (contactStore) Save(tx *sql.Tx, id string, in ContactInput, now time.Time) (Summary, error) {
	return Summary{}, invalid("type", "contact.Save not yet implemented")
}

func (contactStore) Get(tx *sql.Tx, id string) (Card, error) {
	return nil, ErrNotFound
}

func (contactStore) Search(tx *sql.Tx, p SearchParams) ([]Summary, error) {
	return nil, nil
}

func (contactStore) Delete(tx *sql.Tx, id string, at time.Time) error {
	return ErrNotFound
}
