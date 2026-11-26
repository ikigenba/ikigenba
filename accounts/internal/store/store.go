// Package store owns accounts' hosts, operations, accounts, users and reports.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

// Config supplies the clock and operation identifier source.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Host records a machine and the latest summary of its reconciliation.
type Host struct {
	Name                                                                     string
	Seq                                                                      int64
	State, Reason, Type                                                      string
	RootGB, DataGB                                                           int
	InstanceID, RootVolume, DataVolume, AllocationID, AssociationID, EIP, AZ string
	Version, Applied, Attempted                                              int64
	ReportError                                                              bool
	Alarm                                                                    string
	RunningSince, Reported                                                   time.Time
	Operation                                                                string
	Accounts                                                                 int
	Created                                                                  time.Time
	CreatedBy                                                                string
	Allowlist                                                                []string
	Change, RecordReason                                                     string
	OnDelete                                                                 string
}

// KeptVolume records a deleted host's data volume and its own deletion state.
type KeptVolume struct {
	Volume, Host  string
	SizeGB        int
	Deleted       time.Time
	State, Reason string
	Attempts      int
	NextTry       time.Time
	Caller        identity.Caller
}

// Delete choices and kept-volume states.
const (
	OnDeleteKeep   = "keep"
	OnDeleteDelete = "delete"
	VolumeKept     = "kept"
	VolumeDeleting = "deleting"
	VolumeDeleted  = "deleted"
)

// HostSpec supplies the initial type and sizes.
type HostSpec struct {
	Type           string
	RootGB, DataGB int
}

// Target supplies changed values for a resize.
type Target struct {
	Type           string
	RootGB, DataGB int
}

// Operation is a persistent engine checkpoint.
type Operation struct {
	ID, Host, Account, Kind, Step, Status   string
	Generation                              int
	Target                                  Target
	ResumeState, Error                      string
	Attempts                                int
	Started, StepStarted, NextTry, Finished time.Time
	Supersedes                              string
	Caller                                  identity.Caller
}

// Update carries fields learned by an engine step.
type Update struct {
	Step                                                                           string
	Generation                                                                     int
	InstanceID, RootVolume, DataVolume, AllocationID, AssociationID, EIP, AZ, Type string
	RootGB, DataGB                                                                 int
}

// Account is a business placed on one host.
type Account struct {
	Slug, Host, State, Reason    string
	GID, Version, RemovedVersion int64
	Change, Operation            string
	Users, Invites               int
	Created                      time.Time
	CreatedBy                    string
}

// Key is an authorized public key and its original line.
type Key struct{ Type, Body, Line string }

// User is an account member.
type User struct {
	Name, Account       string
	Admin               bool
	State               string
	UID, Version        int64
	Pending             bool
	Keys                []Key
	LastSignIn, Created time.Time
	CreatedBy           string
}

// Report is a host's latest accepted report.
type Report struct {
	Host     string
	Received time.Time
	Body     protocol.Report
}

// Transition describes health changes from a report.
type Transition struct {
	Before, After       string
	Synced, ApplyFailed bool
	Applied, Attempted  int64
}

// Host states.
const (
	HostProvisioning = "provisioning"
	HostRunning      = "running"
	HostStopping     = "stopping"
	HostStopped      = "stopped"
	HostStarting     = "starting"
	HostResizing     = "resizing"
	HostDeleting     = "deleting"
	HostFailed       = "failed"
	HostDeleted      = "deleted"
)

// Account and user states.
const (
	AccountPending  = "pending"
	AccountActive   = "active"
	AccountFailed   = "failed"
	AccountRemoving = "removing"
	UserActive      = "active"
	UserLocked      = "locked"
	UserRemoving    = "removing"
)

// Health and alarm values.
const (
	HealthSync    = "sync"
	HealthPending = "pending"
	HealthError   = "error"
	HealthSilent  = "silent"
	HealthNone    = "none"
	AlarmNone     = ""
	AlarmSilent   = "silent"
	AlarmError    = "error"
)

// Operation statuses and kinds.
const (
	OpRunning   = "running"
	OpFailed    = "failed"
	OpDone      = "done"
	OpCancelled = "cancelled"
	OpCreate    = "create"
	OpStop      = "stop"
	OpStart     = "start"
	OpResize    = "resize"
	OpDelete    = "delete"
	OpPlace     = "place"
	OpUnplace   = "unplace"
)

// Store limits and timing.
const (
	SilentAfter = 5 * time.Minute
	GrowWindow  = 24 * time.Hour
	FirstID     = 20000
	BatchSize   = 50
	MaxGrows    = 4
)

// Store errors distinguish database failures, missing records and guards.
var (
	ErrUnreachable     = errors.New("accounts record is unreachable")
	ErrNotFound        = errors.New("record not found")
	ErrTaken           = errors.New("name is held")
	ErrConflict        = errors.New("record state conflict")
	ErrBusy            = fmt.Errorf("host already has an operation: %w", ErrConflict)
	ErrHostFailed      = fmt.Errorf("host failed: %w", ErrConflict)
	ErrNotFailed       = fmt.Errorf("host has not failed: %w", ErrConflict)
	ErrNotRunning      = fmt.Errorf("host is not running: %w", ErrConflict)
	ErrNotStopped      = fmt.Errorf("host is not stopped: %w", ErrConflict)
	ErrHasAccounts     = fmt.Errorf("host still has accounts: %w", ErrConflict)
	ErrHostFull        = fmt.Errorf("host account limit reached: %w", ErrConflict)
	ErrHostGone        = fmt.Errorf("host is gone: %w", ErrConflict)
	ErrNothingToChange = fmt.Errorf("host target is unchanged: %w", ErrConflict)
	ErrShrinks         = fmt.Errorf("target shrinks a volume: %w", ErrConflict)
	ErrGrowLimit       = fmt.Errorf("volume grow limit reached: %w", ErrConflict)
	ErrHasUsers        = fmt.Errorf("account still has users: %w", ErrConflict)
	ErrAccountRemoving = fmt.Errorf("account is being removed: %w", ErrConflict)
	ErrAccountFull     = fmt.Errorf("host user limit reached: %w", ErrConflict)
	ErrTooManyKeys     = fmt.Errorf("user key limit reached: %w", ErrConflict)
	ErrNotLocked       = fmt.Errorf("user is not locked: %w", ErrConflict)
	ErrUserRemoving    = fmt.Errorf("user is being removed: %w", ErrConflict)
	ErrTooManyUnbans   = fmt.Errorf("host pending unban limit reached: %w", ErrConflict)
	ErrAllowlistFull   = fmt.Errorf("host allowlist limit reached: %w", ErrConflict)
	ErrNotInAccount    = fmt.Errorf("user belongs to another account: %w", ErrNotFound)
	ErrTooManyInvites  = fmt.Errorf("account invite limit reached: %w", ErrConflict)
	ErrLastKey         = fmt.Errorf("user must keep at least one key: %w", ErrConflict)
	ErrNotActive       = fmt.Errorf("user is not active: %w", ErrConflict)
	ErrNotAdmin        = fmt.Errorf("user is not an active admin: %w", ErrConflict)
	ErrSelf            = fmt.Errorf("cannot perform this action on oneself: %w", ErrConflict)
	ErrKeyNotHeld      = fmt.Errorf("user does not hold the key: %w", ErrNotFound)
	ErrVolumeDeleting  = fmt.Errorf("volume is being deleted: %w", ErrConflict)
)

// Unreachable is the user-facing database failure line.
const Unreachable string = "accounts record cannot be reached"

// Store operates on an open database and coordinates version notifications.
type Store struct {
	recordReader
	d        *db.DB
	cfg      Config
	mu       sync.Mutex
	watchers map[string]*hostWatchers
}

type hostWatchers struct {
	channels map[chan struct{}]struct{}
	pending  bool
}

// New builds a store over the supplied handle without opening anything.
func New(d *db.DB, cfg Config) *Store {
	return &Store{
		recordReader: recordReader{runRead: d.Read, now: cfg.Now},
		d:            d, cfg: cfg, watchers: make(map[string]*hostWatchers),
	}
}

func (s *Store) stamp() time.Time { return s.cfg.Now().UTC().Truncate(time.Second) }

func cleanTime(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

type storedTime struct{ *time.Time }

func (t storedTime) Scan(src any) error {
	n, ok := src.(int64)
	if !ok {
		return fmt.Errorf("invalid stored timestamp: %T", src)
	}
	*t.Time = time.Unix(n, 0).UTC()
	return nil
}

func allGiven(values ...string) bool {
	for _, value := range values {
		if value == "" {
			return false
		}
	}
	return true
}

func validCaller(c identity.Caller) bool {
	return len(c.UserID) > 0 && len(c.UserID) <= valid.MaxUserIDBytes
}

func validLimit(limit int) bool { return limit >= 1 && limit <= paging.MaxLimit+1 }

func storeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrTaken), errors.Is(err, ErrConflict), errors.Is(err, ErrUnreachable):
		return err
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	default:
		return errors.Join(ErrUnreachable, err)
	}
}

type query struct {
	context.Context
	*sql.Tx
}

func (q query) exec(stmt string, args ...any) error {
	_, err := q.ExecContext(q.Context, stmt, args...)
	return err
}

func (q query) row(stmt string, args ...any) *sql.Row {
	return q.QueryRowContext(q.Context, stmt, args...)
}

type scanner interface{ Scan(...any) error }

func collect[T any](q query, scan func(scanner) (T, error), stmt string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(q.Context, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []T{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type recordReader struct {
	now     func() time.Time
	runRead func(context.Context, func(*sql.Tx) error) error
}

// Snapshot is a read-only view valid during a ReadSnapshot callback.
type Snapshot struct{ recordReader }

// ReadSnapshot runs reads against one point-in-time database snapshot.
func ReadSnapshot(ctx context.Context, s *Store, fn func(*Snapshot) error) error {
	if err := ctx.Err(); err != nil {
		return storeError(err)
	}
	var callbackErr error
	err := s.d.Read(ctx, func(tx *sql.Tx) error {
		snapshot := Snapshot{recordReader{now: s.cfg.Now, runRead: func(_ context.Context, read func(*sql.Tx) error) error {
			return read(tx)
		}}}
		callbackErr = fn(&snapshot)
		return callbackErr
	})
	if callbackErr != nil {
		return callbackErr
	}
	return storeError(err)
}

func read[T any](ctx context.Context, r *recordReader, valid bool, fn func(query) (T, error)) (T, error) {
	return transact(ctx, r.runRead, valid, fn)
}

type verifiedHostKey struct{}

type verifiedHost struct {
	name, instance string
	seq            int64
}

// WithVerifiedHost ties store calls to the authenticated incarnation of a host.
func WithVerifiedHost(ctx context.Context, h Host) context.Context {
	return context.WithValue(ctx, verifiedHostKey{}, verifiedHost{name: h.Name, instance: h.InstanceID, seq: h.Seq})
}

func checkVerifiedHost(q query) error {
	h, guarded := q.Value(verifiedHostKey{}).(verifiedHost)
	if !guarded {
		return nil
	}
	var found int
	return q.row("SELECT 1 FROM hosts WHERE name = ? AND seq = ? AND instance_id = ? AND state <> ?",
		h.name, h.seq, h.instance, HostDeleted).Scan(&found)
}

func transact[T any](ctx context.Context, run func(context.Context, func(*sql.Tx) error) error, valid bool, fn func(query) (T, error)) (out T, err error) {
	if err := ctx.Err(); err != nil {
		return out, storeError(err)
	}
	err = run(ctx, func(tx *sql.Tx) error {
		if !valid {
			return ErrUnreachable
		}
		q := query{ctx, tx}
		if err := checkVerifiedHost(q); err != nil {
			return err
		}
		var err error
		out, err = fn(q)
		return err
	})
	if err != nil {
		var zero T
		return zero, storeError(err)
	}
	return out, nil
}

type writing struct {
	query
	noteVersionChange func(string)
}

func write[T any](ctx context.Context, s *Store, valid bool, fn func(*writing) (T, error)) (T, error) {
	// Keep the commit and its notification ordered with registrations.
	s.mu.Lock()
	defer s.mu.Unlock()
	var pending bool
	noteVersionChange := func(host string) {
		// Keep a mark on the existing watcher entry until the transaction ends.
		if watchers := s.watchers[host]; watchers != nil {
			watchers.pending = true
			pending = true
		}
	}
	out, err := transact(ctx, s.d.Write, valid, func(q query) (T, error) {
		return fn(&writing{query: q, noteVersionChange: noteVersionChange})
	})
	if pending {
		for host, watchers := range s.watchers {
			if !watchers.pending {
				continue
			}
			watchers.pending = false
			if err != nil {
				continue
			}
			for ch := range watchers.channels {
				close(ch)
			}
			delete(s.watchers, host)
		}
	}
	return out, err
}

func change(ctx context.Context, s *Store, valid bool, fn func(*writing) error) error {
	_, err := write(ctx, s, valid, func(w *writing) (struct{}, error) {
		return struct{}{}, fn(w)
	})
	return err
}

func (w *writing) bump(host string) (int64, error) {
	var version int64
	err := w.row("UPDATE hosts SET version = version + 1 WHERE name = ? RETURNING version", host).Scan(&version)
	w.noteVersionChange(host)
	return version, err
}

func allocate(q query, name string, first int64) (int64, error) {
	var n int64
	err := q.row("INSERT INTO counters (name, value) VALUES (?, ?) ON CONFLICT (name) DO UPDATE SET value = value + 1 RETURNING value", name, first).Scan(&n)
	return n, err
}

// Watch subscribes to the next committed version change of one host.
func (s *Store) Watch(host string) (<-chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan struct{})
	watchers := s.watchers[host]
	if watchers == nil {
		watchers = &hostWatchers{channels: make(map[chan struct{}]struct{})}
		s.watchers[host] = watchers
	}
	watchers.channels[ch] = struct{}{}
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		watchers := s.watchers[host]
		if watchers == nil {
			return
		}
		delete(watchers.channels, ch)
		if len(watchers.channels) == 0 {
			delete(s.watchers, host)
		}
	}
}
