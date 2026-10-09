// Package store owns the prompts catalog and its record transitions.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Config supplies the catalog clock and random source.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Store accesses the catalog through an appkit database handle.
type Store struct {
	d   *db.DB
	cfg Config
}

// New constructs a store over an existing database handle.
func New(d *db.DB, cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Store{d, cfg}
}

// Prompt holds a prompt together with its subscriptions and latest run.
type Prompt struct {
	ID, Name, Owner, OwnerEmail, Model, Prompt, System string
	Tools                                              []string
	Schema                                             json.RawMessage
	Created                                            time.Time
	Subscriptions                                      []Subscription
	Last                                               *Run
}

// Subscription records an exact event pattern and its creation time.
type Subscription struct {
	Event   string
	Created time.Time
}

// Usage holds the counts and nano-USD cost of one run.
type Usage struct{ Calls, ToolCalls, InputTokens, CachedTokens, OutputTokens, ReasoningTokens, CostNanos int64 }

// Run holds one recorded prompt execution.
type Run struct {
	ID, Prompt, Model, User, RequestID, Trigger, Event, Status string
	ExitCode                                                   int
	Started, Finished                                          time.Time
	StdoutBytes, StderrBytes                                   int64
	StdoutTruncated, StderrTruncated                           bool
	Reason                                                     string
	Usage                                                      Usage
}

// Truncated reports whether either output stream was cut.
func (r Run) Truncated() bool { return r.StdoutTruncated || r.StderrTruncated }

// Draft supplies the fields of a new prompt.
type Draft struct {
	Owner, OwnerEmail, Name, Model, Prompt, System string
	Tools                                          []string
	Schema                                         json.RawMessage
}

// Change replaces the non-nil fields of a prompt.
type Change struct {
	Model, Prompt, System *string
	Tools                 *[]string
	Schema                *json.RawMessage
}

// Ending supplies the terminal fields of a run.
type Ending struct {
	Status                           string
	ExitCode                         int
	Finished                         time.Time
	StdoutBytes, StderrBytes         int64
	StdoutTruncated, StderrTruncated bool
	Reason                           string
	Usage                            Usage
}

// Catalog identifiers, status words, triggers and failure reasons.
const (
	PromptPrefix         = "prm_"
	RunPrefix            = "prr_"
	StatusQueued         = "queued"
	StatusRunning        = "running"
	StatusExited         = "exited"
	StatusKilled         = "killed"
	StatusTimedOut       = "timed_out"
	StatusFailed         = "failed"
	ReasonStartFailed    = "start_failed"
	ReasonQueueAbandoned = "queue_abandoned"
	TriggerManual        = "manual"
	TriggerEvent         = "event"
	Unreachable          = "The prompt catalog is unavailable."
)

// Content errors distinguish missing or conflicting catalog records.
var (
	ErrNotFound      = errors.New("not found")
	ErrNameTaken     = errors.New("name taken")
	ErrEnded         = errors.New("run ended")
	ErrNotSubscribed = errors.New("not subscribed")
	ErrDelivered     = errors.New("event delivered")
	errInvalid       = errors.New("invalid catalog argument")
	eventPattern     = regexp.MustCompile(`^([a-z][a-z0-9]*(_[a-z0-9]+)*|\*)(\.([a-z][a-z0-9]*(_[a-z0-9]+)*|\*))+$`)
)

// ValidEvent reports whether s is a valid event subscription pattern.
func ValidEvent(s string) bool { return eventPattern.MatchString(s) }

// ValidName reports whether s is a valid prompt name.
func ValidName(s string) bool {
	if len(s) < 1 || len(s) > 64 || s[0] == '-' || s == "about" || s == "mcp" || s == "events" || s == "declarations" {
		return false
	}
	for i := range len(s) {
		b := s[i]
		if (b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '-' {
			return false
		}
	}
	return true
}
func validID(s, prefix string) bool {
	if len(s) != 20 || s[:4] != prefix {
		return false
	}
	for i := 4; i < len(s); i++ {
		b := s[i]
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

// ValidPromptID reports whether s has the prompt ID form.
func ValidPromptID(s string) bool { return validID(s, PromptPrefix) }

// ValidRunID reports whether s has the run ID form.
func ValidRunID(s string) bool { return validID(s, RunPrefix) }
func newID(r io.Reader, prefix string) (string, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

// NewPromptID draws a prompt ID from exactly eight bytes.
func NewPromptID(r io.Reader) (string, error) { return newID(r, PromptPrefix) }

// NewRunID draws a run ID from exactly eight bytes.
func NewRunID(r io.Reader) (string, error) { return newID(r, RunPrefix) }
func stamp(t time.Time) time.Time          { return t.UTC().Truncate(time.Second) }
