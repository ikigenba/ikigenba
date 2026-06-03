package crm

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// ── time helpers ─────────────────────────────────────────────────────────────

func fmtTime(t time.Time) string { return t.UTC().Format(timeFormat) }

// parseTime parses a stored timestamp; a malformed value yields the zero time
// rather than an error (storage is always written by fmtTime, so this is
// defensive).
func parseTime(s string) time.Time {
	t, _ := time.Parse(timeFormat, s)
	return t
}

// ── null helpers ─────────────────────────────────────────────────────────────

// nullStr returns nil for a nil pointer and the dereferenced value otherwise,
// for binding into a nullable column.
func nullStr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// nullStrOrNil binds a string, treating "" as SQL NULL — used for nullable
// columns where a provided empty string means "clear".
func nullStrOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ptr returns a pointer from a NullString (nil when not valid).
func strPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

// timePtr maps a NullString timestamp to *time.Time.
func timePtr(ns sql.NullString) *time.Time {
	if !ns.Valid {
		return nil
	}
	t := parseTime(ns.String)
	return &t
}

// mapUniqueErr translates a SQLite UNIQUE/CHECK constraint violation into the
// right sentinel: a duplicate live row is a conflict (an invariant race — the
// dedup probe handles the friendly "duplicate" path before insert), a CHECK
// failure is a validation error. Other errors are wrapped with context.
func mapUniqueErr(err error, what string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "UNIQUE constraint failed"):
		return fmt.Errorf("%w: duplicate %s", ErrConflict, what)
	case strings.Contains(msg, "CHECK constraint failed"):
		return fmt.Errorf("%w: %s violates an allowed-value constraint", ErrValidation, what)
	case strings.Contains(msg, "constraint failed"):
		return fmt.Errorf("%w: %s", ErrConflict, what)
	default:
		return fmt.Errorf("%s: %w", what, err)
	}
}

// liveExists reports whether a live (non-soft-deleted) row with id exists in the
// named table. Used by the dispatcher to validate cross-entity FK targets and to
// resolve subject ids. The table name is a trusted internal constant, never user
// input.
func liveExists(tx *sql.Tx, table, id string) (bool, error) {
	if id == "" {
		return false, nil
	}
	var got string
	err := tx.QueryRow(`SELECT id FROM `+table+` WHERE id = ? AND deleted_at IS NULL`, id).Scan(&got)
	switch err {
	case nil:
		return true, nil
	case sql.ErrNoRows:
		return false, nil
	default:
		return false, fmt.Errorf("probe %s: %w", table, err)
	}
}
