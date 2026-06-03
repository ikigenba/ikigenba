package crm

import (
	"database/sql"
	"time"
)

// taskStore is the SQL-only data layer for the tasks table. Fleshed out in
// Phase 1.
type taskStore struct{}

func (taskStore) Save(tx *sql.Tx, id string, in TaskInput, now time.Time) (Summary, error) {
	return Summary{}, invalid("type", "task.Save not yet implemented")
}

func (taskStore) Get(tx *sql.Tx, id string) (Card, error) {
	return nil, ErrNotFound
}

func (taskStore) Search(tx *sql.Tx, p SearchParams) ([]Summary, error) {
	return nil, nil
}

func (taskStore) Delete(tx *sql.Tx, id string, at time.Time) error {
	return ErrNotFound
}
