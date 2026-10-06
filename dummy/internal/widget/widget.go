// Package widget owns widgets, their validation, and their database store.
package widget

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Status is a widget's lifecycle status.
type Status string

// The allowed widget statuses, in form order.
const (
	StatusActive  Status = "active"
	StatusPaused  Status = "paused"
	StatusRetired Status = "retired"
)

// MaxNameRunes is the maximum length of a trimmed widget name.
const MaxNameRunes = 40

// Validation messages accompany their respective rejected fields.
const (
	NameRequiredMessage     = "a name is required"
	NameTooLongMessage      = "the name is too long; the limit is 40 characters"
	NameTakenMessage        = "that name is already taken"
	CountNotWholeMessage    = "the count must be a whole number"
	CountNegativeMessage    = "the count cannot be negative"
	StatusNotAllowedMessage = "the status must be one of active, paused, or retired"
)

// Statuses returns a fresh slice of allowed statuses in form order.
func Statuses() []Status {
	return []Status{StatusActive, StatusPaused, StatusRetired}
}

// Enum returns the allowed statuses independently of its receiver.
func (s Status) Enum() []string {
	statuses := Statuses()
	values := make([]string, len(statuses))
	for i, status := range statuses {
		values[i] = string(status)
	}
	return values
}

// Widget is an accepted name, count, and status.
type Widget struct {
	ID     string
	Name   string
	Count  int
	Status Status
}

// Submission holds the caller's unmodified field values.
type Submission struct {
	Name   string
	Count  string
	Status string
}

// Draft holds typed field values for checking or creation.
type Draft struct {
	Name   string
	Count  int
	Status Status
}

// FieldErrors holds one rejection message per field, or an empty string.
type FieldErrors struct {
	Name   string
	Count  string
	Status string
}

// Any reports whether at least one field was rejected.
func (e FieldErrors) Any() bool {
	return e.Name != "" || e.Count != "" || e.Status != ""
}

// Unreachable is the shared response when the widgets cannot be reached.
const Unreachable = "cannot reach the widgets; try again later"

// Store reads and writes widgets through a caller-owned database handle.
type Store struct {
	mu     sync.Mutex
	handle *db.DB
	src    io.Reader
}

// NewStore creates a store over the handle without reading or seeding it.
func NewStore(d *db.DB, src io.Reader) *Store {
	if src == nil {
		src = rand.Reader
	}
	return &Store{handle: d, src: src}
}

// nextID runs inside the creation transaction with exclusive source access.
func (s *Store) nextID(ctx context.Context, tx *sql.Tx) (string, error) {
	for {
		var data [8]byte
		if _, err := io.ReadFull(s.src, data[:]); err != nil {
			data = [8]byte{}
			if _, err := io.ReadFull(rand.Reader, data[:]); err != nil {
				panic(err)
			}
		}
		id := "wgt_" + hex.EncodeToString(data[:])
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM widgets WHERE id = ?)", id).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return id, nil
		}
	}
}

// All returns an independent snapshot in creation order.
func (s *Store) All(ctx context.Context) ([]Widget, error) {
	var widgets []Widget
	err := s.handle.Read(ctx, func(tx *sql.Tx) (err error) {
		rows, err := tx.QueryContext(ctx, "SELECT id, name, count, status FROM widgets ORDER BY seq")
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, rows.Close()) }()
		for rows.Next() {
			var w Widget
			if err := rows.Scan(&w.ID, &w.Name, &w.Count, &w.Status); err != nil {
				return err
			}
			widgets = append(widgets, w)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return widgets, nil
}

// ParseSubmission converts the form's fields without judging typed values.
func ParseSubmission(sub Submission) (Draft, FieldErrors) {
	d := Draft{Name: sub.Name}
	var errs FieldErrors
	count, err := strconv.Atoi(strings.TrimSpace(sub.Count))
	if err != nil {
		errs.Count = CountNotWholeMessage
	} else {
		d.Count = count
	}
	status := Status(strings.TrimSpace(sub.Status))
	if allowedStatus(status) {
		d.Status = status
	} else {
		errs.Status = StatusNotAllowedMessage
	}
	return d, errs
}

// Check judges all fields without modifying the database.
func (s *Store) Check(ctx context.Context, d Draft) (FieldErrors, error) {
	var fields FieldErrors
	err := s.handle.Read(ctx, func(tx *sql.Tx) error {
		var err error
		fields, err = check(ctx, tx, d)
		return err
	})
	if err != nil {
		return FieldErrors{}, err
	}
	return fields, nil
}

// Create checks and inserts in one write transaction.
func (s *Store) Create(ctx context.Context, d Draft) (Widget, FieldErrors, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var w Widget
	var fields FieldErrors
	err := s.handle.Write(ctx, func(tx *sql.Tx) error {
		var err error
		fields, err = check(ctx, tx, d)
		if err != nil || fields.Any() {
			return err
		}
		id, err := s.nextID(ctx, tx)
		if err != nil {
			return err
		}
		w = Widget{ID: id, Name: strings.TrimSpace(d.Name), Count: d.Count, Status: d.Status}
		_, err = tx.ExecContext(ctx, "INSERT INTO widgets (id, name, count, status) VALUES (?, ?, ?, ?)", w.ID, w.Name, w.Count, w.Status)
		return err
	})
	if err != nil {
		return Widget{}, FieldErrors{}, err
	}
	return w, fields, nil
}

func check(ctx context.Context, tx *sql.Tx, d Draft) (FieldErrors, error) {
	name := strings.TrimSpace(d.Name)
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM widgets WHERE name = ?)", name).Scan(&exists); err != nil {
		return FieldErrors{}, err
	}
	var fields FieldErrors
	switch {
	case name == "":
		fields.Name = NameRequiredMessage
	case utf8.RuneCountInString(name) > MaxNameRunes:
		fields.Name = NameTooLongMessage
	case exists:
		fields.Name = NameTakenMessage
	}
	if d.Count < 0 {
		fields.Count = CountNegativeMessage
	}
	if !allowedStatus(d.Status) {
		fields.Status = StatusNotAllowedMessage
	}
	return fields, nil
}

func allowedStatus(status Status) bool {
	switch status {
	case StatusActive, StatusPaused, StatusRetired:
		return true
	default:
		return false
	}
}
