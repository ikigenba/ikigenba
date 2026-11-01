// Package store keeps sessions, turns and their derived summaries.
package store

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/sessions/internal/figures"
)

// MaxPushTurns and the other limits bound accepted requests and refresh pages.
const (
	MaxPushTurns      = 1000
	MaxPushSessions   = 500
	MaxLatestSessions = 500
	MaxPeriods        = 60
	MinAwayMinutes    = 1
	MaxAwayMinutes    = 1440
	MaxTurnCount      = 1000000000000
	BatchSize         = 500
)

// ErrUnreachable identifies a database or context failure.
var ErrUnreachable = errors.New("sessions database unavailable")

// Unreachable and the refusal constants hold the messages returned to callers.
const (
	Unreachable     string = "The sessions database cannot be reached."
	TooManySessions string = "Too many sessions in this request."
	TooManyTurns    string = "Too many turns in this request."
	NoSessions      string = "At least one session is required."
	UnknownTool     string = "The tool is unknown."
	MissingID       string = "A session id is required."
	BadParent       string = "The parent session reference is invalid."
	OwnParent       string = "A session cannot be its own parent."
	MissingHost     string = "A host is required."
	BadDirectory    string = "The directory must be an absolute path without a trailing slash."
	BadMoment       string = "The turn moment must be an RFC 3339 instant at or after the Unix epoch."
	UnknownWhose    string = "The turn's party is unknown."
	BadCount        string = "A token count is outside the accepted range."
	OtherOwner      string = "The session belongs to another user."
	UnknownPeriod   string = "The period is unknown."
	UnpairedPeriod  string = "The period and its date must be supplied together."
	BadDate         string = "The date is invalid."
	UnknownKind     string = "The kind is unknown."
	UnknownGroup    string = "The grouping is unknown."
	ToBeforeFrom    string = "The ending date precedes the starting date."
	TooManyPeriods  string = "The range contains too many periods."
	UntaggedRule    string = "A rule cannot assign the untagged kind."
	RuleHeld        string = "A rule with that key is already held."
	RuleAbsent      string = "No rule with that key is held."
	MissingModel    string = "A model is required."
	BadRate         string = "A price rate is invalid."
	PriceAbsent     string = "No price with that key is held."
	AwayOutOfRange  string = "The away limit is outside the accepted range."
)

// RefusedError identifies a request that cannot be accepted.
type RefusedError struct{ Reason string }

// Error returns the refusal reason.
func (e *RefusedError) Error() string { return e.Reason }

// Config supplies the clock used to place the current landing week.
type Config struct{ Now func() time.Time }

// Store provides synchronized access to the records on its database handle.
type Store struct {
	d   *db.DB
	cfg Config
	mu  sync.Mutex
}

// New builds a store over an already opened database.
func New(d *db.DB, cfg Config) *Store { return &Store{d: d, cfg: cfg} }

// Command is the first command name and its argument.
type Command struct{ Name, Argument string }

// PushTurn carries the wire representation of one turn.
type PushTurn struct {
	Moment, Whose, Model             string
	Input, Cached, Output, Reasoning int64
}

// SessionRef identifies a session by tool and opaque id.
type SessionRef struct{ Tool, ID string }

// PushSession carries metadata and turns for one session.
type PushSession struct {
	Tool, ID        string
	Parent          *SessionRef
	Host, Directory string
	Headless        bool
	Command         Command
	Turns           []PushTurn
}

// LatestTurn reports the latest held moment for a session reference.
type LatestTurn struct {
	Tool, ID string
	Latest   time.Time
}

// Pushed reports the latest moments and changes made by a push.
type Pushed struct {
	Sessions                []LatestTurn
	New, Changed, Refreshed int
}

// Filter selects root summaries by their first moment and metadata.
type Filter struct{ Period, Of, Person, Kind, Tool, Host, Command string }

// Position is the exclusive keyset position in descending list order.
type Position struct {
	First    time.Time
	Tool, ID string
}

// SessionRow describes a root and the current summary of its tree.
type SessionRow struct {
	Tool                        figures.Tool
	ID, Host, Person, Directory string
	Headless                    bool
	Command                     Command
	First, Last                 time.Time
	Summary                     figures.Summary
}

// TotalsQuery selects a range of periods and a grouping of their roots.
type TotalsQuery struct {
	Period, From, To, Group string
	Filter                  Filter
}

// GroupTotals holds the sum of the roots in one group.
type GroupTotals struct {
	Group    string
	Figures  figures.Figures
	Sessions int
}

// PeriodTotals holds the groups of one UTC period.
type PeriodTotals struct {
	Start  time.Time
	Groups []GroupTotals
}

// KindTotals holds one kind in a landing week.
type KindTotals struct {
	Kind    figures.Kind
	Figures figures.Figures
}

// Week holds all kinds and the total of one landing week.
type Week struct {
	Start time.Time
	Kinds []KindTotals
	Total figures.Figures
}

// Landing is the business summary for the current and eight previous weeks.
type Landing struct {
	Held      bool
	AwayLimit time.Duration
	Weeks     []Week
}

// PriceInput carries the wire representation of one model price.
type PriceInput struct{ Model, Effective, Input, Cached, Output, Reasoning string }

func refused(reason string) error { return &RefusedError{Reason: reason} }
func inaccessible(err error) error {
	if err == nil {
		return nil
	}
	var r *RefusedError
	if errors.As(err, &r) {
		return err
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return inaccessible(err)
	}
	return inaccessible(s.d.Read(ctx, func(*sql.Tx) error { return nil }))
}
func instant(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
