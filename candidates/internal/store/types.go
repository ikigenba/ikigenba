// Package store keeps the candidate records in the service database.
package store

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// Config supplies the store clock and random reader.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Store owns the persisted service records.
type Store struct {
	d   *db.DB
	cfg Config
	mu  sync.Mutex
}

// New constructs a store over an existing database handle.
func New(d *db.DB, cfg Config) *Store { return &Store{d: d, cfg: cfg} }

// Candidate is the display and identity of a business.
type Candidate struct {
	ID, Status, MergedInto, Name                   string
	Names                                          []string
	Street, City, State, Zip, County, Phone, Email string
	People                                         []sources.Person
	Class                                          string
	Cargo                                          []string
	Created, Updated                               time.Time
}

// Link attaches a source row to a candidate.
type Link struct {
	Candidate, Scraper, SourceID, Run, Digest string
	Reading                                   sources.Reading
	Reason                                    string
	Confidence                                float64
	FirstSeen, LastSeen                       time.Time
	Gone                                      bool
}

// Assessment is the last stored score and its evidence.
type Assessment struct {
	Candidate  string
	Fleet      bool
	Low        int64
	High       *int64
	Confidence float64
	Filed      time.Time
	Minutes    *int64
	Exclusions []sources.Exclusion
	Excluded   bool
	Base       float64
	Rules      int64
	Scored     time.Time
}

// Ruling records an owner decision.
type Ruling struct {
	Kind, Candidate, Other string
	Keys                   []string
	Why, UserID, UserEmail string
	At                     time.Time
}

// Pass records one resolution pass.
type Pass struct {
	ID, Trigger, Status, Reason                                              string
	Started, Finished                                                        time.Time
	Rows, New, Changed, Unchanged, Gone, Created, Linked, Rescored, Unranged int64
	UserID, UserEmail, RequestID, Delivery                                   string
}

// Counts contains the results of pass work.
type Counts struct{ Rows, New, Changed, Unchanged, Gone, Created, Linked, Rescored, Unranged int64 }

// Observation is one source reading to attach.
type Observation struct {
	Scraper, SourceID, Digest string
	Reading                   sources.Reading
}

// CandidateFilter selects active candidates.
type CandidateFilter struct{ Name, Zip, Phone string }

// ShortlistFilter supplies all shortlist thresholds.
type ShortlistFilter struct {
	MaxMinutes, MinRevenue int64
	Class, Cargo           string
}

// Ranked contains a candidate and its score at query time.
type Ranked struct {
	Candidate        Candidate
	Assessment       Assessment
	Effective, Score float64
}

// Record vocabulary and identifier prefixes.
const (
	CandidateIDPrefix = "cand_"
	PassIDPrefix      = "pass_"
	StatusActive      = "active"
	StatusMerged      = "merged"
	StatusRunning     = "running"
	StatusDone        = "done"
	StatusFailed      = "failed"
	ReasonScrapes     = "scrapes"
	ReasonInterrupted = "interrupted"
	ReasonStore       = "store"
	TriggerRebuild    = "rebuild"
	LinkSourceKey     = "source_key"
	LinkMatchKey      = "match_key"
	LinkDecision      = "decision"
	KindMerge         = "merge"
	KindSplit         = "split"
	KindNotSame       = "not_same"
	KindExclude       = "exclude"
	KindInclude       = "include"
	Unreachable       = "The candidate database is unavailable."
)

// Errors distinguish record refusals from database failures.
var (
	ErrNotFound    = errors.New("record not found")
	ErrBusy        = errors.New("pass running")
	ErrDelivered   = errors.New("already delivered")
	ErrEnded       = errors.New("pass ended")
	ErrInvalid     = errors.New("invalid ruling")
	ErrUnreachable = errors.New("database unreachable")
)

func validID(s, prefix string) bool {
	return len(s) == len(prefix)+16 && s[:len(prefix)] == prefix && hexDigits(s[len(prefix):])
}
func hexDigits(s string) bool {
	for _, b := range []byte(s) {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

// ValidCandidateID recognizes candidate identifiers.
func ValidCandidateID(s string) bool { return validID(s, CandidateIDPrefix) }

// ValidPassID recognizes pass identifiers.
func ValidPassID(s string) bool { return validID(s, PassIDPrefix) }
func validZip(s string) bool {
	if len(s) != 5 {
		return false
	}
	for _, b := range []byte(s) {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}
func unreachable(err error) error {
	if err == nil {
		return nil
	}
	for _, e := range []error{ErrNotFound, ErrBusy, ErrDelivered, ErrEnded, ErrInvalid} {
		if errors.Is(err, e) {
			return err
		}
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) now() time.Time { return s.cfg.Now().UTC().Truncate(time.Second) }
func (s *Store) transaction(ctx context.Context, write bool, fn func(*records) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return unreachable(err)
	}
	return s.transact(ctx, write, fn)
}
