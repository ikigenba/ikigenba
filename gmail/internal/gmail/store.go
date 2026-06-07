package gmail

import (
	"database/sql"
	"errors"
	"fmt"
)

// store.go is the SQL-only data layer for the producer's sync cursor (the
// sync_state singleton row, 002_gmail.sql). It mirrors dropbox's Store: every
// method takes *sql.Tx so the producer composes the {outbox event(s) + cursor
// advance} transaction atomically — no method opens its own DB handle or
// commits. That single-tx discipline is the load-bearing "emitted == recorded
// as emitted" guarantee (decisions §1): a crash never double-emits or skips.
//
// sync_state is a singleton (id INTEGER PRIMARY KEY CHECK (id = 1)); history_id
// is the Gmail History API cursor and is nullable so a fresh boot (no row) is
// distinguishable from a seeded one (decisions §"Cursor lifecycle").
type Store struct{}

// NewStore builds a Store.
func NewStore() *Store { return &Store{} }

// GetHistoryID returns the persisted Gmail historyId cursor and whether one
// exists. On first boot the row is absent OR history_id is NULL (ok == false):
// the engine seeds the cursor from getProfile().historyId and emits nothing for
// pre-existing mail (decisions §"Cursor lifecycle").
func (Store) GetHistoryID(tx *sql.Tx) (historyID string, ok bool, err error) {
	row := tx.QueryRow(`SELECT history_id FROM sync_state WHERE id = 1`)
	var h sql.NullString
	switch err := row.Scan(&h); {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("get history_id: %w", err)
	}
	if !h.Valid || h.String == "" {
		return "", false, nil
	}
	return h.String, true, nil
}

// SetHistoryID upserts the singleton cursor row (CHECK(id=1)). Called inside the
// per-poll transaction AFTER the page's events are appended, so the cursor never
// advances without the events committing too (decisions §1).
func (Store) SetHistoryID(tx *sql.Tx, historyID, updatedAt string) error {
	_, err := tx.Exec(`
		INSERT INTO sync_state (id, history_id, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET history_id = excluded.history_id, updated_at = excluded.updated_at
	`, historyID, updatedAt)
	if err != nil {
		return fmt.Errorf("set history_id: %w", err)
	}
	return nil
}
