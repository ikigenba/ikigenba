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

// Check judges all fields without modifying the store.
func (s *Store) Check(d Draft) FieldErrors {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.check(d)
}

// Create judges and appends an accepted widget atomically.
func (s *Store) Create(d Draft) (Widget, FieldErrors) {
	s.mu.Lock()
	defer s.mu.Unlock()
	errs := s.check(d)
	if errs.Any() {
		return Widget{}, errs
	}
	w := Widget{Name: strings.TrimSpace(d.Name), Count: d.Count, Status: d.Status}
	s.widgets = append(s.widgets, w)
	return w, errs
}

// check requires the caller to hold the store's lock.
func (s *Store) check(d Draft) FieldErrors {
	name := strings.TrimSpace(d.Name)
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
	if d.Count < 0 {
		errs.Count = CountNegativeMessage
	}
	if !allowedStatus(d.Status) {
		errs.Status = StatusNotAllowedMessage
	}
	return errs
}

func allowedStatus(status Status) bool {
	switch status {
	case StatusActive, StatusPaused, StatusRetired:
		return true
	default:
		return false
	}
}
