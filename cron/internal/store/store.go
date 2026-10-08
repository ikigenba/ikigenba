// Package store keeps cron's trigger records and their validation rules.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	cronparser "github.com/robfig/cron/v3"
)

// Trigger identifiers, statuses and the shared unreachable response.
const (
	IDPrefix    = "crn_"
	Active      = "active"
	Paused      = "paused"
	Unreachable = "cannot reach the database; try again later"
)

// Errors distinguish record refusals from unavailable storage.
var (
	ErrNotFound    = errors.New("trigger not found")
	ErrSlugTaken   = errors.New("trigger slug taken")
	ErrInvalid     = errors.New("invalid trigger")
	ErrUnreachable = errors.New(Unreachable)
)

// Config supplies the creation clock and id source.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Trigger is the complete persisted record of a trigger.
type Trigger struct {
	ID, Slug, When, OwnerID, OwnerEmail, Status string
	Created, LastFired                          time.Time
}

// Draft contains caller-provided fields for a new trigger.
type Draft struct{ Slug, When, OwnerID, OwnerEmail string }

// Store operates over an appkit database handle.
type Store struct {
	db  *db.DB
	cfg Config
}

// New builds a store without opening or closing its handle.
func New(d *db.DB, cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Store{db: d, cfg: cfg}
}

// ValidID reports whether s is a trigger id.
func ValidID(s string) bool {
	if len(s) != len(IDPrefix)+16 || !strings.HasPrefix(s, IDPrefix) {
		return false
	}
	for _, b := range []byte(s[len(IDPrefix):]) {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

// ValidSlug reports whether s follows the trigger slug grammar.
func ValidSlug(s string) bool {
	if len(s) < 1 || len(s) > 64 || s[0] < 'a' || s[0] > 'z' || s[len(s)-1] == '_' {
		return false
	}
	for i, b := range []byte(s) {
		if ((b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '_') || (b == '_' && i > 0 && s[i-1] == '_') {
			return false
		}
	}
	return true
}

// ValidWhen accepts the five descriptors and standard five-field expressions.
func ValidWhen(s string) bool {
	switch s {
	case "@hourly", "@daily", "@weekly", "@monthly", "@yearly":
		return true
	}
	fields := strings.Split(s, " ")
	if len(fields) != 5 {
		return false
	}
	for _, f := range fields {
		if f == "" || strings.ContainsAny(f, "\t\n\v\f\r ") {
			return false
		}
	}
	_, err := cronparser.ParseStandard(s)
	return err == nil
}

const columns = "id, slug, schedule, owner_id, owner_email, status, created, last_fired"

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Trigger, error) {
	var t Trigger
	var created string
	var fired sql.NullString
	if err := row.Scan(&t.ID, &t.Slug, &t.When, &t.OwnerID, &t.OwnerEmail, &t.Status, &created, &fired); err != nil {
		return Trigger{}, err
	}
	var err error
	t.Created, err = time.Parse(time.RFC3339, created)
	if err != nil {
		return Trigger{}, err
	}
	t.Created = t.Created.UTC()
	if fired.Valid {
		t.LastFired, err = time.Parse(time.RFC3339, fired.String)
		t.LastFired = t.LastFired.UTC()
	}
	return t, err
}
func classify(err error) error {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrSlugTaken) {
		return err
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) transaction(ctx context.Context, write bool, fn func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return classify(err)
	}
	if write {
		return classify(s.db.Write(ctx, fn))
	}
	return classify(s.db.Read(ctx, fn))
}
func byID(ctx context.Context, tx *sql.Tx, id string) (Trigger, error) {
	t, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM triggers WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Trigger{}, ErrNotFound
	}
	return t, err
}

// List returns every trigger in ascending slug order.
func (s *Store) List(ctx context.Context) ([]Trigger, error) {
	result := make([]Trigger, 0)
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM triggers ORDER BY slug COLLATE BINARY")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			t, err := scan(rows)
			if err != nil {
				return err
			}
			result = append(result, t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Get finds a trigger by its exact slug.
func (s *Store) Get(ctx context.Context, slug string) (Trigger, error) {
	var result Trigger
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		var err error
		result, err = scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM triggers WHERE slug = ?", slug))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return Trigger{}, err
	}
	return result, nil
}

// Create validates and inserts a new active trigger.
func (s *Store) Create(ctx context.Context, d Draft) (Trigger, error) {
	var result Trigger
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if !ValidSlug(d.Slug) || !ValidWhen(d.When) || d.OwnerID == "" {
			return ErrInvalid
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM triggers WHERE slug = ?", d.Slug).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrSlugTaken
		}
		var id string
		for {
			var bytes [8]byte
			if _, err := io.ReadFull(s.cfg.Rand, bytes[:]); err != nil {
				return fmt.Errorf("%w: random source: %s", ErrUnreachable, err.Error())
			}
			id = IDPrefix + hex.EncodeToString(bytes[:])
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM triggers WHERE id = ?", id).Scan(&count); err != nil {
				return err
			}
			if count == 0 {
				break
			}
		}
		result = Trigger{ID: id, Slug: d.Slug, When: d.When, OwnerID: d.OwnerID, OwnerEmail: d.OwnerEmail, Status: Active, Created: s.cfg.Now().UTC().Truncate(time.Second)}
		_, err := tx.ExecContext(ctx, "INSERT INTO triggers ("+columns+") VALUES (?, ?, ?, ?, ?, ?, ?, NULL)", result.ID, result.Slug, result.When, result.OwnerID, result.OwnerEmail, result.Status, result.Created.Format(time.RFC3339))
		return err
	})
	if err != nil {
		return Trigger{}, err
	}
	return result, nil
}
func (s *Store) change(ctx context.Context, id string, valid bool, update func(*sql.Tx, Trigger) (Trigger, error)) (Trigger, error) {
	var result Trigger
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if !valid {
			return ErrInvalid
		}
		t, err := byID(ctx, tx, id)
		if err != nil {
			return err
		}
		result, err = update(tx, t)
		return err
	})
	if err != nil {
		return Trigger{}, err
	}
	return result, nil
}

// SetWhen replaces only a trigger's schedule.
func (s *Store) SetWhen(ctx context.Context, id, when string) (Trigger, error) {
	return s.change(ctx, id, ValidWhen(when), func(tx *sql.Tx, t Trigger) (Trigger, error) {
		_, err := tx.ExecContext(ctx, "UPDATE triggers SET schedule = ? WHERE id = ?", when, id)
		t.When = when
		return t, err
	})
}

// SetStatus replaces only a trigger's active or paused status.
func (s *Store) SetStatus(ctx context.Context, id, status string) (Trigger, error) {
	return s.change(ctx, id, status == Active || status == Paused, func(tx *sql.Tx, t Trigger) (Trigger, error) {
		_, err := tx.ExecContext(ctx, "UPDATE triggers SET status = ? WHERE id = ?", status, id)
		t.Status = status
		return t, err
	})
}

// SetLastFired records a slot in UTC to the second.
func (s *Store) SetLastFired(ctx context.Context, id string, slot time.Time) (Trigger, error) {
	return s.change(ctx, id, !slot.IsZero(), func(tx *sql.Tx, t Trigger) (Trigger, error) {
		t.LastFired = slot.UTC().Truncate(time.Second)
		_, err := tx.ExecContext(ctx, "UPDATE triggers SET last_fired = ? WHERE id = ?", t.LastFired.Format(time.RFC3339), id)
		return t, err
	})
}

// Delete removes a trigger, returning its former record.
func (s *Store) Delete(ctx context.Context, id string) (Trigger, error) {
	return s.change(ctx, id, true, func(tx *sql.Tx, t Trigger) (Trigger, error) {
		_, err := tx.ExecContext(ctx, "DELETE FROM triggers WHERE id = ?", id)
		return t, err
	})
}
