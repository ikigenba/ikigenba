// Package runs owns background scraping and its lifecycle.
package runs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/trail"
)

// Config supplies the engine dependencies.
type Config struct {
	Store     *store.Store
	Scrapers  *scrapers.Registry
	Telemetry *telemetry.Writer
	Events    *events.Emitter
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
	TriggerPrefix      = "scrape_"
	Accepts            = "cron.*.fired"
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
	contexts map[string]context.Context
	cfg      Config
	mu       sync.Mutex
	stopping bool
	active   map[string]context.CancelFunc
	changed  chan struct{}
}

// New constructs an engine.
func New(cfg Config) *Engine {
	return &Engine{cfg: cfg, active: make(map[string]context.CancelFunc), contexts: make(map[string]context.Context), changed: make(chan struct{})}
}

// Start records a manual run and starts its work in the background.
func (e *Engine) Start(ctx context.Context, c identity.Caller, scraper string) (store.Run, error) {
	sc, ok := e.cfg.Scrapers.Get(scraper)
	if !ok {
		return store.Run{}, &Refusal{Word: RefusalNotFound}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping {
		return store.Run{}, &Refusal{Word: RefusalStopping}
	}
	r, err := e.cfg.Store.AddRun(ctx, store.Run{Scraper: scraper, UserID: c.UserID, UserEmail: c.Email, RequestID: c.RequestID})
	if errors.Is(err, store.ErrBusy) {
		return store.Run{}, &Refusal{Word: RefusalBusy}
	}
	if err != nil {
		return store.Run{}, store.ErrUnreachable
	}
	e.launch(ctx, sc, r)
	return r, nil
}

// Deliver accepts a cron fire or answers a retry-safe outcome.
func (e *Engine) Deliver(ctx context.Context, d events.Delivery) events.Outcome {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping {
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
	r, err := e.cfg.Store.AddRun(ctx, store.Run{Scraper: name, Trigger: words[1], Delivery: d.Event.ID, UserID: c.UserID, RequestID: c.RequestID})
	if errors.Is(err, store.ErrBusy) || errors.Is(err, store.ErrDelivered) {
		return events.Skip()
	}
	if err != nil {
		return events.Fail(store.Unreachable)
	}
	e.launch(ctx, sc, r)
	return events.OK()
}

// Handlers returns the accepted event patterns.
func (e *Engine) Handlers() events.Handlers { return events.Handlers{Accepts: e.Deliver} }

// launch is called with mu held, keeping start and stop ordered.
func (e *Engine) launch(ctx context.Context, sc source.Scraper, r store.Run) {
	c := identity.Caller{UserID: r.UserID, Email: r.UserEmail, RequestID: r.RequestID}
	ctx, cancel := context.WithCancel(identity.NewContext(context.WithoutCancel(ctx), c))
	e.active[r.ID] = cancel
	e.contexts[r.ID] = ctx
	trail.Record(ctx, e.cfg.Telemetry, e.cfg.Events, trail.Started, r)
	go func() {
		defer cancel()
		e.work(ctx, sc, r)
	}()
}
func (e *Engine) work(ctx context.Context, sc source.Scraper, r store.Run) {
	batchFailed := false
	err := sc.Scrape(ctx, func(rows []source.Row) error {
		err := e.cfg.Store.PutRows(ctx, r.ID, rows)
		if err != nil {
			batchFailed = true
		}
		return err
	})
	interrupted := ctx.Err() != nil
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, active := e.active[r.ID]; !active {
		return
	}
	endctx := context.WithoutCancel(ctx)
	var ended store.Run
	var enderr error
	switch {
	case interrupted:
		ended, enderr = e.cfg.Store.FailRun(endctx, r.ID, store.ReasonInterrupted)
	case batchFailed:
		ended, enderr = e.cfg.Store.FailRun(endctx, r.ID, store.ReasonStore)
	case err != nil:
		ended, enderr = e.cfg.Store.FailRun(endctx, r.ID, store.ReasonSource)
	default:
		ended, enderr = e.cfg.Store.FinishRun(endctx, r.ID)
	}
	if enderr == nil {
		trail.Record(endctx, e.cfg.Telemetry, e.cfg.Events, trail.Finished, ended)
	}
	e.active[r.ID]()
	delete(e.active, r.ID)
	delete(e.contexts, r.ID)
	close(e.changed)
	e.changed = make(chan struct{})
}

// Recover settles runs left running by an earlier process.
func (e *Engine) Recover(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
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
	return nil
}

// Drain stops new work, cancels active work and waits within ctx.
func (e *Engine) Drain(ctx context.Context) {
	e.mu.Lock()
	e.stopping = true
	for _, cancel := range e.active {
		cancel()
	}
	for len(e.active) > 0 {
		changed := e.changed
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			e.mu.Lock()
			for id := range e.active {
				ended, err := e.cfg.Store.FailRun(context.WithoutCancel(ctx), id, store.ReasonInterrupted)
				if err == nil {
					trail.Record(context.WithoutCancel(e.contexts[id]), e.cfg.Telemetry, e.cfg.Events, trail.Finished, ended)
				}
				delete(e.active, id)
				delete(e.contexts, id)
			}
			close(e.changed)
			e.changed = make(chan struct{})
			e.mu.Unlock()
			return
		case <-changed:
		}
		e.mu.Lock()
	}
	e.mu.Unlock()
}
