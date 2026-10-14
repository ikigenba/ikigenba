// Package runs owns background scraping and its lifecycle.
package runs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/pace"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/trail"
)

// Store is the persistence seam used by the engine.
type Store interface {
	AddRun(context.Context, store.Run) (store.Run, error)
	Collect(context.Context, string, store.Step) (*store.Collection, error)
	FinishRun(context.Context, string, store.Counts) (store.Run, error)
	FailRun(context.Context, string, string) (store.Run, error)
	RunningRuns(context.Context) ([]store.Run, error)
	TidyFiles(context.Context) error
}

// Database write retry policy and diagnostic copy.
const (
	WriteRetries = 5
	WriteBackoff = time.Second
	Retrying     = "retrying"
	GaveUp       = "gave up"
)

// Config supplies the engine dependencies.
type Config struct {
	Store         Store
	Scrapers      *scrapers.Registry
	Telemetry     *telemetry.Writer
	Events        *events.Emitter
	Sleep         func(context.Context, time.Duration)
	Stderr        io.Writer
	Now           func() time.Time
	Budget        int64
	PressureLimit int64
	Pressure      string
}

// Refusal is a machine-readable refusal word.
type Refusal struct{ Word string }

func (r *Refusal) Error() string { return r.Word }

// Refusal words and delivery constants.
const (
	RefusalNotFound    = "not_found"
	RefusalBusy        = "busy"
	RefusalNoRun       = "no_run"
	RefusalStopping    = "stopping"
	RefusalUnavailable = "unavailable"
	RefusalMissing     = "missing"
	TriggerPrefix      = "scrape_"
	Stopping           = "The service is stopping."
)

// Record is the public JSON representation of a run.
type Record struct {
	ID        string `json:"id"`
	Scraper   string `json:"scraper"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Started   string `json:"started"`
	Finished  string `json:"finished,omitempty"`
	Rows      int64  `json:"rows"`
	New       int64  `json:"new"`
	Changed   int64  `json:"changed"`
	Unchanged int64  `json:"unchanged"`
	Gone      int64  `json:"gone"`
	User      string `json:"user,omitempty"`
	Email     string `json:"email,omitempty"`
	Trigger   string `json:"trigger,omitempty"`
}

// RecordOf returns the public representation of a stored run.
func RecordOf(r store.Run) Record {
	v := Record{ID: r.ID, Scraper: r.Scraper, Status: r.Status, Reason: r.Reason, Started: r.Started.UTC().Format(time.RFC3339), Rows: r.Rows, New: r.New, Changed: r.Changed, Unchanged: r.Unchanged, Gone: r.Gone, Trigger: r.Trigger}
	if !r.Finished.IsZero() {
		v.Finished = r.Finished.UTC().Format(time.RFC3339)
	}
	if r.Trigger == "" {
		v.User, v.Email = r.UserID, r.UserEmail
	}
	return v
}

// Engine starts, settles and cancels background runs.
type Engine struct {
	starting  map[uint64]context.CancelFunc
	nextStart uint64
	builds    map[string]*store.Collection
	load      chan struct{}
	contexts  map[string]context.Context
	ending    map[string]bool
	endctx    context.Context
	endcancel context.CancelFunc
	diagMu    sync.Mutex
	drained   atomic.Bool
	cfg       Config
	mu        sync.Mutex
	stopping  bool
	active    map[string]context.CancelFunc
	changed   chan struct{}
}

// New constructs an engine.
func New(cfg Config) *Engine {
	if cfg.Sleep == nil {
		cfg.Sleep = func(ctx context.Context, d time.Duration) {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	endctx, endcancel := context.WithCancel(context.Background())
	return &Engine{cfg: cfg, starting: make(map[uint64]context.CancelFunc), builds: make(map[string]*store.Collection), load: make(chan struct{}, 1), endctx: endctx, endcancel: endcancel, ending: make(map[string]bool), active: make(map[string]context.CancelFunc), contexts: make(map[string]context.Context), changed: make(chan struct{})}
}

// Start records a manual run and starts its work in the background.
func (e *Engine) Start(ctx context.Context, c identity.Caller, scraper string) (store.Run, error) {
	sc, ok := e.cfg.Scrapers.Get(scraper)
	if !ok {
		return store.Run{}, &Refusal{Word: RefusalNotFound}
	}
	r, err := e.start(ctx, sc, store.Run{Scraper: scraper, UserID: c.UserID, UserEmail: c.Email, RequestID: c.RequestID})
	var refusal *Refusal
	if errors.As(err, &refusal) {
		return store.Run{}, refusal
	}
	if errors.Is(err, store.ErrBusy) {
		return store.Run{}, &Refusal{Word: RefusalBusy}
	}
	if err != nil {
		return store.Run{}, store.ErrUnreachable
	}
	return r, nil
}

// Deliver accepts a cron fire or answers a retry-safe outcome.
func (e *Engine) Deliver(ctx context.Context, d events.Delivery) events.Outcome {
	e.mu.Lock()
	stopping := e.stopping
	e.mu.Unlock()
	if stopping {
		return events.Fail(Stopping)
	}
	words := strings.Split(d.Event.Name, ".")
	if len(words) != 3 || words[0] != "cron" || words[2] != "fired" || !strings.HasPrefix(words[1], TriggerPrefix) || d.Event.ID == "" {
		return events.Skip()
	}
	name := strings.TrimPrefix(words[1], TriggerPrefix)
	sc, ok := e.cfg.Scrapers.Get(name)
	if !ok {
		return events.Skip()
	}
	c, _ := identity.FromContext(ctx)
	ctx = context.WithoutCancel(ctx)
	_, err := e.start(ctx, sc, store.Run{Scraper: name, Trigger: words[1], Delivery: d.Event.ID, UserID: c.UserID, RequestID: c.RequestID})
	var refusal *Refusal
	if errors.As(err, &refusal) {
		return events.Fail(Stopping)
	}
	if errors.Is(err, store.ErrBusy) || errors.Is(err, store.ErrDelivered) {
		return events.Skip()
	}
	if err != nil {
		return events.Fail(store.Unreachable)
	}
	return events.OK()
}

// start keeps pending catalog writes cancellable without blocking the drain lock.
func (e *Engine) start(ctx context.Context, sc source.Scraper, r store.Run) (store.Run, error) {
	e.mu.Lock()
	if e.stopping {
		e.mu.Unlock()
		return store.Run{}, &Refusal{Word: RefusalStopping}
	}
	e.nextStart++
	key := e.nextStart
	ctx, cancel := context.WithCancel(ctx)
	e.starting[key] = cancel
	e.mu.Unlock()
	defer cancel()
	r, err := e.cfg.Store.AddRun(ctx, r)
	e.mu.Lock()
	delete(e.starting, key)
	close(e.changed)
	e.changed = make(chan struct{})
	var launch func()
	if err == nil {
		launch = e.launch(ctx, sc, r)
	}
	e.mu.Unlock()
	if launch != nil {
		launch()
	}
	return r, err
}

// FiredEvent returns the exact cron event name for a scraper.
func FiredEvent(scraper string) string { return "cron." + TriggerPrefix + scraper + ".fired" }

// Handlers returns one accepted event name per registered scraper.
func (e *Engine) Handlers() events.Handlers {
	handlers := make(events.Handlers)
	for _, name := range e.cfg.Scrapers.Names() {
		handlers[FiredEvent(name)] = e.Deliver
	}
	return handlers
}

// launch is called with mu held, keeping start and stop ordered.
func (e *Engine) launch(ctx context.Context, sc source.Scraper, r store.Run) func() {
	c := identity.Caller{UserID: r.UserID, Email: r.UserEmail, RequestID: r.RequestID}
	ctx, cancel := context.WithCancel(identity.NewContext(context.WithoutCancel(ctx), c))
	e.active[r.ID] = cancel
	e.contexts[r.ID] = ctx
	if e.stopping {
		cancel()
	}
	return func() {
		trail.Record(ctx, e.cfg.Telemetry, e.cfg.Events, trail.Started, r)
		go func() {
			defer cancel()
			e.work(ctx, sc, r)
		}()
	}
}
func (e *Engine) diagnostic(ctx context.Context, id, op, wording string, err error) {
	e.diagMu.Lock()
	defer e.diagMu.Unlock()
	if e.drained.Load() || ctx.Err() != nil || e.cfg.Stderr == nil {
		return
	}
	line := "scrapes: " + id + " " + op + ": " + wording + ": " + strings.ReplaceAll(err.Error(), "\n", "; ") + "\n"
	_, _ = fmt.Fprint(e.cfg.Stderr, line)
}

func (e *Engine) write(ctx context.Context, id, op string, call func() error) error {
	for attempt := 0; ; attempt++ {
		err := call()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		if !errors.Is(err, store.ErrLocked) || attempt == WriteRetries {
			e.diagnostic(ctx, id, op, GaveUp, err)
			return err
		}
		e.diagnostic(ctx, id, op, Retrying, err)
		e.cfg.Sleep(ctx, WriteBackoff<<attempt)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (e *Engine) release(id string) {
	e.active[id]()
	delete(e.active, id)
	delete(e.contexts, id)
	delete(e.builds, id)
	delete(e.ending, id)
	close(e.changed)
	e.changed = make(chan struct{})
}

func (e *Engine) phase() *pace.Phase {
	return pace.Start(pace.Config{Budget: e.cfg.Budget, Limit: e.cfg.PressureLimit, Pressure: e.cfg.Pressure, Now: e.cfg.Now, Sleep: e.cfg.Sleep})
}

func (e *Engine) work(ctx context.Context, sc source.Scraper, r store.Run) {
	var collect *pace.Phase
	build, err := e.cfg.Store.Collect(ctx, r.ID, func(ctx context.Context, bytes int64, took time.Duration) error {
		return collect.Step(ctx, bytes, took)
	})
	buildFailed := err != nil
	if err != nil {
		e.diagnostic(ctx, r.ID, "Collect", GaveUp, err)
	}
	var counts store.Counts
	loaded := false
	if err == nil {
		e.mu.Lock()
		if _, active := e.active[r.ID]; !active {
			e.mu.Unlock()
			_ = build.Discard()
			return
		}
		e.builds[r.ID] = build
		e.mu.Unlock()
		collect = e.phase()
		var batchErr error
		err = sc.Scrape(ctx, func(rows []source.Row) error {
			if batchErr != nil {
				return batchErr
			}
			batchErr = build.Add(ctx, rows)
			if batchErr != nil {
				buildFailed = true
				e.diagnostic(ctx, r.ID, "Add", GaveUp, batchErr)
			}
			return batchErr
		})
		if batchErr != nil {
			err = batchErr
		}
		if err == nil && ctx.Err() == nil {
			err = build.Seal(ctx)
			if err != nil {
				buildFailed = true
				e.diagnostic(ctx, r.ID, "Seal", GaveUp, err)
			}
			if err == nil && ctx.Err() == nil {
				bytes, elapsed, paused := collect.Totals()
				trail.Phase(ctx, e.cfg.Telemetry, trail.Collected, r, bytes, elapsed, paused)
				select {
				case <-ctx.Done():
				case e.load <- struct{}{}:
					if ctx.Err() == nil {
						load := e.phase()
						counts, err = build.Load(ctx, load.Step)
						if err != nil {
							buildFailed = true
							e.diagnostic(ctx, r.ID, "Load", GaveUp, err)
						}
						if err == nil {
							loaded = true
							bytes, elapsed, paused := load.Totals()
							trail.Phase(ctx, e.cfg.Telemetry, trail.Loaded, r, bytes, elapsed, paused)
						}
					}
					<-e.load
				}
			}
		}
	}
	if !loaded && build != nil {
		_ = build.Discard()
	}
	e.mu.Lock()
	if _, active := e.active[r.ID]; !active {
		e.mu.Unlock()
		return
	}
	e.ending[r.ID] = true
	e.mu.Unlock()
	endctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	// A successful AddRun response can arrive after the drain deadline. Its
	// canceled work still needs an ending, as the deadline cannot settle a
	// record whose id the engine did not yet know.
	callctx := endctx
	if e.endctx.Err() == nil {
		stop := context.AfterFunc(e.endctx, cancel)
		defer stop()
	} else {
		// Permit its first settlement write, but the expired drain still
		// forbids any retry or pause if that write is refused.
		callctx = context.WithoutCancel(ctx)
		cancel()
	}
	defer cancel()
	var ended store.Run
	op := "FailRun"
	reason := store.ReasonSource
	switch {
	case loaded:
		op = "FinishRun"
	case ctx.Err() != nil:
		reason = store.ReasonInterrupted
	case buildFailed:
		reason = store.ReasonStore
	}
	enderr := e.write(endctx, r.ID, op, func() error {
		var err error
		if op == "FinishRun" {
			ended, err = e.cfg.Store.FinishRun(callctx, r.ID, counts)
		} else {
			ended, err = e.cfg.Store.FailRun(callctx, r.ID, reason)
		}
		return err
	})
	if enderr == nil {
		trail.Record(endctx, e.cfg.Telemetry, e.cfg.Events, trail.Finished, ended)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, active := e.active[r.ID]; active {
		e.release(r.ID)
	}
}

// Recover settles runs left running by an earlier process.
func (e *Engine) Recover(ctx context.Context) error {
	list, err := e.cfg.Store.RunningRuns(ctx)
	if err != nil {
		return store.ErrUnreachable
	}
	for _, r := range list {
		ended, err := e.cfg.Store.FailRun(ctx, r.ID, store.ReasonInterrupted)
		if err != nil {
			return store.ErrUnreachable
		}
		trail.Record(context.Background(), e.cfg.Telemetry, e.cfg.Events, trail.Finished, ended)
	}
	if err := e.cfg.Store.TidyFiles(ctx); err != nil {
		return store.ErrUnreachable
	}
	return nil
}

// Drain stops new work, cancels active work and waits within ctx.
func (e *Engine) Drain(ctx context.Context) {
	stop := context.AfterFunc(ctx, e.endcancel)
	defer stop()
	defer e.drained.Store(true)
	e.mu.Lock()
	e.stopping = true
	for _, cancel := range e.starting {
		cancel()
	}
	for id, cancel := range e.active {
		if !e.ending[id] {
			cancel()
		}
	}
	for len(e.active) > 0 || len(e.starting) > 0 {
		changed := e.changed
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			e.endcancel()
			e.mu.Lock()
			pending := make(map[string]context.Context)
			builds := make(map[string]*store.Collection)
			for id := range e.active {
				if !e.ending[id] {
					pending[id] = e.contexts[id]
					builds[id] = e.builds[id]
				}
				e.release(id)
			}
			e.mu.Unlock()
			settled := make(chan struct{})
			go func() {
				defer close(settled)
				for id, workctx := range pending {
					if build := builds[id]; build != nil {
						_ = build.Discard()
					}
					endctx := context.WithoutCancel(workctx)
					ended, err := e.cfg.Store.FailRun(endctx, id, store.ReasonInterrupted)
					if err == nil {
						trail.Record(endctx, e.cfg.Telemetry, e.cfg.Events, trail.Finished, ended)
					}
				}
			}()
			timer := time.NewTimer(time.Second / 2)
			defer timer.Stop()
			select {
			case <-settled:
			case <-timer.C:
			}
			return
		case <-changed:
		}
		e.mu.Lock()
	}
	e.mu.Unlock()
}
