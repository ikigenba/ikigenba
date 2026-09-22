// Package widget owns widgets, their validation, and their in-memory store.
package widget

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
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

// Widget is an accepted name, count, and status.
type Widget struct {
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

// Store holds widgets in creation order and is safe for concurrent use.
type Store struct {
	mu      sync.RWMutex
	widgets []Widget
}

// NewStore creates an independent store with the three starting widgets.
func NewStore() *Store {
	return &Store{widgets: []Widget{
		{Name: "alpha", Count: 3, Status: StatusActive},
		{Name: "beta", Count: 0, Status: StatusPaused},
		{Name: "gamma", Count: 12, Status: StatusRetired},
	}}
}

// All returns an independent snapshot of the widgets in creation order.
func (s *Store) All() []Widget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.widgets)
}

// Create validates all trimmed fields and appends an accepted widget atomically.
func (s *Store) Create(sub Submission) (Widget, FieldErrors) {
	name := strings.TrimSpace(sub.Name)
	countText := strings.TrimSpace(sub.Count)
	status := Status(strings.TrimSpace(sub.Status))
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs FieldErrors
	switch {
	case name == "":
		errs.Name = NameRequiredMessage
	case utf8.RuneCountInString(name) > MaxNameRunes:
		errs.Name = NameTooLongMessage
	default:
		for _, existing := range s.widgets {
			if existing.Name == name {
				errs.Name = NameTakenMessage
				break
			}
		}
	}
	count, err := strconv.Atoi(countText)
	switch {
	case err != nil:
		errs.Count = CountNotWholeMessage
	case count < 0:
		errs.Count = CountNegativeMessage
	}
	switch status {
	case StatusActive, StatusPaused, StatusRetired:
	default:
		errs.Status = StatusNotAllowedMessage
	}
	if errs.Any() {
		return Widget{}, errs
	}
	w := Widget{Name: name, Count: count, Status: status}
	s.widgets = append(s.widgets, w)
	return w, errs
}
