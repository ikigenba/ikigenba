// Package job is agentkit's generic async agent-job lifecycle: spawn a run on a
// goroutine with context cancellation + TTL, gate single-flight per key, write
// terminal state, and sweep crash-orphaned runs at boot. Persistence and the
// actual work are supplied by the consumer via Store and Job.
//
// The seam: agentkit owns the lifecycle; the consumer owns persistence (a Store
// over its own DB) and the work (a Job). Status and Record types live here so
// every consumer agrees on them. agentkit is store-agnostic and domain-agnostic:
// it never sees the consumer's owner/collection/session columns — those live in
// the consumer's own row, keyed on Record.ID / Record.FlightKey.
package job

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Status is a run's lifecycle state. Mirrors ralph's session.Run* values so a
// later ralph retrofit is a rename, not a remodel.
type Status string

const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Terminal reports whether s is an end state (anything but running).
func (s Status) Terminal() bool { return s != StatusRunning }

// Record is the persisted shape of one run. The consumer's Store maps it to its
// own table (ralph: runs; wiki: a job-record table from 002_wiki.sql). Generic
// fields only — no session/collection/owner columns here; the consumer carries
// those in its own row and joins on ID / FlightKey.
type Record struct {
	ID        string    // run id (consumer-minted; ULID in the suite)
	FlightKey string    // single-flight key: at most one running run per key
	Status    Status    // running | succeeded | failed | cancelled
	StartedAt time.Time
	EndedAt   time.Time // zero until terminal
	UsageJSON string    // opaque accounting blob captured from the result stream; "" if none
	Error     string    // terminal error message; "" on success
}

// Store is the persistence seam the consumer implements over its own DB. All
// methods are called by the Runner; the consumer never calls them directly. The
// single-flight guarantee rests on Insert rejecting a duplicate FlightKey under
// one serialized writer (ralph relies on SQLite's single connection; wiki
// inherits the same db.Open).
type Store interface {
	// Insert persists a new record in StatusRunning. It MUST fail (return a
	// non-nil error, conventionally ErrFlightInUse) if another record with the
	// same FlightKey is already StatusRunning — this is the single-flight gate.
	// Returning that error makes Runner.Spawn report rejection without launching
	// a goroutine.
	Insert(ctx context.Context, rec Record) error

	// Load returns the record by id, or ErrNotFound if absent. Consumers layer
	// owner-scoping on top (a foreign-owned id reads as ErrNotFound), as ralph's
	// store already does.
	Load(ctx context.Context, id string) (Record, error)

	// UpdateTerminal writes the run's end state: status (one of the terminal
	// values), endedAt, the usage blob, and an error message. Called on a fresh
	// background context because the run's own ctx may be cancelled/expired by
	// the time we persist.
	UpdateTerminal(ctx context.Context, id string, status Status, endedAt time.Time, usageJSON, errMsg string) error

	// SweepRunning is boot-time crash recovery: every record still StatusRunning
	// (orphaned by a crash) is flipped to StatusFailed with endedAt + a
	// "interrupted by restart" error, transactionally, returning the count
	// swept. Mirrors session.Store.SweepRunning.
	SweepRunning(ctx context.Context) (int, error)
}

// Job is the unit of work the consumer supplies for one run. Run executes the
// agent (ralph: agent.Run over a session sandbox; wiki: the ingest integration
// pass), honoring ctx for cancellation/TTL. It returns the usage blob to persist
// and an error (nil = succeeded). The Runner classifies the terminal Status from
// (ctx state, user-cancel, err). Job is an interface, not a func, so the
// consumer can carry state (raw bytes, store handle, reindex closure, client
// factory, …).
type Job interface {
	Run(ctx context.Context) (usageJSON string, err error)
}

// Sentinel errors. ErrFlightInUse is the contract a Store.Insert uses to signal
// the single-flight conflict; ErrNotFound is what Store.Load returns for an
// absent (or, with consumer-side scoping, foreign-owned) id.
var (
	ErrNotFound    = errors.New("job: not found")
	ErrFlightInUse = errors.New("job: a run is already in flight for this key")
)

// Runner owns the lifecycle. It holds the Store, the per-run TTL, and the
// in-flight cancel registry; it is the only thing that writes terminal state.
type Runner struct {
	store Store
	ttl   time.Duration
	now   func() time.Time

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	// userCancelled records run ids whose in-flight run was cancelled by an
	// explicit Cancel call (as opposed to a TTL expiry), so the goroutine can
	// classify the terminal status as cancelled rather than failed.
	userCancelled map[string]bool

	// done is closed-per-id signalling used only by tests to await goroutine
	// completion deterministically; nil in production.
	wg sync.WaitGroup
}

// New builds a Runner over store with a per-run wall-clock ttl. A ttl <= 0
// means no deadline (the run is bounded only by Cancel and the Job itself).
func New(store Store, ttl time.Duration) *Runner {
	return &Runner{
		store:         store,
		ttl:           ttl,
		now:           func() time.Time { return time.Now().UTC() },
		cancels:       make(map[string]context.CancelFunc),
		userCancelled: make(map[string]bool),
	}
}

// Spawn gates single-flight (via Store.Insert on rec.FlightKey), and on success
// launches job on a goroutine bounded by ttl, returning the persisted Record
// (forced to StatusRunning with StartedAt stamped if unset). It returns
// ErrFlightInUse (without spawning) when FlightKey already has a running record
// — or whatever error the Store's Insert returns. Spawn returns immediately;
// terminal state is written by the goroutine via Store.UpdateTerminal.
func (r *Runner) Spawn(rec Record, job Job) (Record, error) {
	rec.Status = StatusRunning
	if rec.StartedAt.IsZero() {
		rec.StartedAt = r.now()
	}
	rec.EndedAt = time.Time{}

	// The single-flight gate is the Store's responsibility: Insert must reject a
	// duplicate running FlightKey. We react to that rejection; we never launch a
	// goroutine on failure, so the rejected caller's state is untouched.
	if err := r.store.Insert(context.Background(), rec); err != nil {
		return Record{}, err
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if r.ttl > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), r.ttl)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}

	r.mu.Lock()
	r.cancels[rec.ID] = cancel
	r.mu.Unlock()

	r.wg.Add(1)
	go r.execute(ctx, cancel, rec, job)

	return rec, nil
}

// execute runs the job and persists its terminal outcome. It always cleans up
// the cancel registry for rec.ID and always writes a terminal record (success,
// failure, TTL, or cancel).
func (r *Runner) execute(ctx context.Context, cancel context.CancelFunc, rec Record, job Job) {
	defer r.wg.Done()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.cancels, rec.ID)
		delete(r.userCancelled, rec.ID)
		r.mu.Unlock()
	}()

	usageJSON, runErr := job.Run(ctx)

	// Classify the terminal status: an explicit user cancel wins over a TTL
	// expiry, TTL over a generic job error, and a clean return is success.
	r.mu.Lock()
	userCancelled := r.userCancelled[rec.ID]
	r.mu.Unlock()

	// Persistence uses a fresh background context: the run ctx may be
	// cancelled/expired by the time we write the terminal state.
	bg := context.Background()
	endedAt := r.now()

	switch {
	case userCancelled:
		_ = r.store.UpdateTerminal(bg, rec.ID, StatusCancelled, endedAt, usageJSON, "cancelled")
	case ctx.Err() == context.DeadlineExceeded:
		_ = r.store.UpdateTerminal(bg, rec.ID, StatusFailed, endedAt, usageJSON, "run TTL exceeded")
	case runErr != nil:
		_ = r.store.UpdateTerminal(bg, rec.ID, StatusFailed, endedAt, usageJSON, runErr.Error())
	default:
		_ = r.store.UpdateTerminal(bg, rec.ID, StatusSucceeded, endedAt, usageJSON, "")
	}
}

// Cancel signals the in-flight run for id, marking it user-cancelled (so it is
// classified Cancelled, not Failed) and triggering context cancellation. Returns
// whether a run was in flight. Idempotent: cancelling an unknown or
// already-finished id is a no-op returning false.
func (r *Runner) Cancel(id string) bool {
	r.mu.Lock()
	cancel, ok := r.cancels[id]
	if ok {
		r.userCancelled[id] = true
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

// Recover is the boot-time crash sweep; it delegates to Store.SweepRunning,
// which flips every record left StatusRunning by a crash to a terminal failed
// state. Call it once on startup, before accepting new Spawns. Returns the
// number of records swept.
func (r *Runner) Recover(ctx context.Context) (int, error) {
	return r.store.SweepRunning(ctx)
}

// wait blocks until all in-flight goroutines spawned by this Runner have
// finished writing their terminal state. It is intended for tests and graceful
// shutdown; production callers typically let goroutines run to completion.
func (r *Runner) wait() { r.wg.Wait() }
