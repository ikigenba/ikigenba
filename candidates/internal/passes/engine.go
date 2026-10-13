// Package passes resolves scraper evidence in background passes.
package passes

import (
	"context"
	"errors"
	"sync"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

// Config supplies the state, source registry, and sibling and event clients.
type Config struct {
	Store     *store.Store
	Sources   *sources.Registry
	Scrapes   *scrapes.Client
	Telemetry *telemetry.Writer
	Events    *events.Emitter
}

// Engine coordinates a single cancellable background pass.
type Engine struct {
	cfg      Config
	mu       sync.Mutex
	stopping bool
	ctx      context.Context
	cancel   context.CancelFunc
	work     sync.WaitGroup
}

// New creates an idle engine.
func New(cfg Config) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{cfg: cfg, ctx: ctx, cancel: cancel}
}

// Handlers declares the engine's accepted event.
func (e *Engine) Handlers() events.Handlers { return events.Handlers{trail.Accepted: e.Deliver} }

// Rebuild records a pass and starts its work without waiting.
func (e *Engine) Rebuild(ctx context.Context, c identity.Caller) (store.Pass, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping {
		return store.Pass{}, &Refusal{RefusalStopping}
	}
	if e.cfg.Scrapes == nil {
		return store.Pass{}, &Refusal{RefusalUnavailable}
	}
	p, err := e.cfg.Store.AddPass(context.WithoutCancel(ctx), store.Pass{Trigger: store.TriggerRebuild, UserID: c.UserID, UserEmail: c.Email, RequestID: c.RequestID})
	if errors.Is(err, store.ErrBusy) {
		return store.Pass{}, &Refusal{RefusalBusy}
	}
	if err != nil {
		return store.Pass{}, err
	}
	e.start(ctx, p, c, nil)
	return p, nil
}
func (e *Engine) start(ctx context.Context, p store.Pass, c identity.Caller, run *scrapes.Run) {
	trail.Record(context.WithoutCancel(ctx), e.cfg.Telemetry, e.cfg.Events, trail.Started, p)
	e.work.Add(1)
	finishCtx := context.WithoutCancel(ctx)
	go func() {
		defer e.work.Done()
		workCtx := identity.NewContext(e.ctx, c)
		totals, reason := e.resolve(workCtx, run, p.Trigger == store.TriggerRebuild)
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.ctx.Err() != nil {
			reason = store.ReasonInterrupted
		}
		var ended store.Pass
		var err error
		if reason != "" {
			_ = e.cfg.Store.RefreshShortlist(finishCtx)
			ended, err = e.cfg.Store.FailPass(finishCtx, p.ID, reason)
		} else {
			ended, err = e.cfg.Store.FinishPass(finishCtx, p.ID, totals)
		}
		if err == nil {
			trail.Record(context.WithoutCancel(ctx), e.cfg.Telemetry, e.cfg.Events, trail.Finished, ended)
		}
	}()
}

// Deliver handles a completed scraper run from the event bus.
func (e *Engine) Deliver(ctx context.Context, d events.Delivery) events.Outcome {
	e.mu.Lock()
	if e.stopping {
		e.mu.Unlock()
		return events.Fail(Stopping)
	}
	scraper, _ := d.Event.Attrs["scraper"].(string)
	runID, _ := d.Event.Attrs["run"].(string)
	status, _ := d.Event.Attrs["status"].(string)
	_, known := e.cfg.Sources.Get(scraper)
	if d.Event.Name != trail.Accepted || d.Event.ID == "" || status != store.StatusDone || !known || runID == "" || runID == store.TriggerRebuild {
		e.mu.Unlock()
		return events.Skip()
	}
	_, err := e.cfg.Store.FindDelivered(context.WithoutCancel(ctx), d.Event.ID, runID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		e.mu.Unlock()
		return events.Fail(store.Unreachable)
	}
	if err == nil {
		e.mu.Unlock()
		return events.Skip()
	}
	e.mu.Unlock()
	caller, _ := identity.FromContext(ctx)
	service := identity.Caller{UserID: "candidates", RequestID: caller.RequestID}
	var run scrapes.Run
	if e.cfg.Scrapes == nil {
		err = scrapes.ErrUnavailable
	} else {
		run, err = e.cfg.Scrapes.Record(identity.NewContext(e.ctx, service), runID)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping {
		return events.Fail(Stopping)
	}
	if errors.Is(err, scrapes.ErrNotFound) || (err == nil && run.Status != store.StatusDone) {
		return events.Skip()
	}
	p, writeErr := e.cfg.Store.AddPass(context.Background(), store.Pass{Trigger: runID, Delivery: d.Event.ID, UserID: caller.UserID, RequestID: caller.RequestID})
	switch {
	case errors.Is(writeErr, store.ErrDelivered):
		return events.Skip()
	case errors.Is(writeErr, store.ErrBusy):
		return events.Fail(Busy)
	case writeErr != nil:
		return events.Fail(store.Unreachable)
	}
	if err == nil {
		e.start(ctx, p, service, &run)
		return events.OK()
	}
	trail.Record(context.WithoutCancel(ctx), e.cfg.Telemetry, e.cfg.Events, trail.Started, p)
	_ = e.cfg.Store.RefreshShortlist(context.WithoutCancel(ctx))
	ended, writeErr := e.cfg.Store.FailPass(context.WithoutCancel(ctx), p.ID, store.ReasonScrapes)
	if writeErr != nil {
		return events.Fail(store.Unreachable)
	}
	trail.Record(context.WithoutCancel(ctx), e.cfg.Telemetry, e.cfg.Events, trail.Finished, ended)
	return events.OK()
}

// Recover settles passes left running by an earlier process.
func (e *Engine) Recover(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	ps, err := e.cfg.Store.RunningPasses(ctx)
	if err != nil {
		return err
	}
	for _, p := range ps {
		ended, err := e.cfg.Store.FailPass(ctx, p.ID, store.ReasonInterrupted)
		if err != nil {
			return err
		}
		trail.Record(context.Background(), e.cfg.Telemetry, e.cfg.Events, trail.Finished, ended)
	}
	return nil
}

// Drain prevents starts and cancels outstanding pass work.
func (e *Engine) Drain(ctx context.Context) {
	e.mu.Lock()
	e.stopping = true
	e.cancel()
	e.mu.Unlock()
	done := make(chan struct{})
	go func() { e.work.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
func addCounts(a *store.Counts, b store.Counts) {
	a.New += b.New
	a.Changed += b.Changed
	a.Unchanged += b.Unchanged
	a.Gone += b.Gone
	a.Created += b.Created
	a.Linked += b.Linked
	a.Rescored += b.Rescored
	a.Unranged += b.Unranged
}
func (e *Engine) resolve(ctx context.Context, delivery *scrapes.Run, rebuild bool) (store.Counts, string) {
	var total store.Counts
	var touched []string
	seen := map[string]bool{}
	remember := func(ids []string) {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				touched = append(touched, id)
			}
		}
	}
	process := func(run scrapes.Run, scraper string) string {
		if run.Status != store.StatusDone {
			return store.ReasonScrapes
		}
		source, _ := e.cfg.Sources.Get(scraper)
		batch := make([]store.Observation, 0, BatchSize)
		var rows int64
		var storeFailure bool
		flush := func() error {
			counts, ids, err := e.cfg.Store.Attach(ctx, run.ID, rebuild, batch)
			if err != nil {
				storeFailure = true
				return err
			}
			addCounts(&total, counts)
			remember(ids)
			batch = batch[:0]
			return nil
		}
		err := e.cfg.Scrapes.Rows(ctx, run.ID, func(row scrapes.Row) error {
			rows++
			total.Rows++
			batch = append(batch, store.Observation{Scraper: scraper, SourceID: row.SourceID, Digest: row.Digest, Reading: source.Read(row)})
			if len(batch) == BatchSize {
				return flush()
			}
			return nil
		})
		if storeFailure {
			return store.ReasonStore
		}
		if err != nil || rows != run.Rows {
			return store.ReasonScrapes
		}
		if len(batch) > 0 {
			if flush() != nil {
				return store.ReasonStore
			}
		}
		counts, ids, err := e.cfg.Store.MarkGone(ctx, scraper, run.ID)
		if err != nil {
			return store.ReasonStore
		}
		addCounts(&total, counts)
		remember(ids)
		return ""
	}
	if delivery != nil {
		if reason := process(*delivery, delivery.Scraper); reason != "" {
			return store.Counts{}, reason
		}
	} else {
		for _, name := range e.cfg.Sources.Names() {
			run, err := e.cfg.Scrapes.Current(ctx, name)
			if errors.Is(err, scrapes.ErrNotFound) {
				continue
			}
			if err != nil {
				return store.Counts{}, store.ReasonScrapes
			}
			if reason := process(run, name); reason != "" {
				return store.Counts{}, reason
			}
		}
	}
	var counts store.Counts
	var err error
	if rebuild {
		counts, err = e.cfg.Store.RescoreAll(ctx)
	} else {
		counts, err = e.cfg.Store.Rescore(ctx, touched)
	}
	if err != nil {
		return store.Counts{}, store.ReasonStore
	}
	addCounts(&total, counts)
	if err = e.cfg.Store.RefreshShortlist(ctx); err != nil {
		return store.Counts{}, store.ReasonStore
	}
	return total, ""
}
