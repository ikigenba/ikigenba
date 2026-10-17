// Package passes resolves scraper evidence in background passes.
package passes

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// Config supplies the state, source registry, and sibling and event clients.
type Config struct {
	Store     *store.Store
	Sources   *sources.Registry
	Scrapes   *scrapes.Client
	Prompts   *prompts.Client
	Telemetry *telemetry.Writer
	Events    *events.Emitter
	Website   *website.Checker
	After     func(time.Duration) <-chan time.Time
}

// Engine coordinates cancellable enrichment and resolution passes.
type Engine struct {
	cfg      Config
	mu       sync.Mutex
	stopping atomic.Bool
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
func (e *Engine) Handlers() events.Handlers {
	return events.Handlers{trail.Accepted: e.Deliver, trail.AcceptedJudge: e.Deliver}
}

// Rebuild records a pass and starts its work without waiting.
func (e *Engine) Rebuild(ctx context.Context, c identity.Caller) (store.Pass, error) {
	if e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	if e.cfg.Scrapes == nil {
		return store.Pass{}, &Refusal{Word: RefusalUnavailable}
	}
	p, err := e.cfg.Store.AddPass(e.ctx, store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: c.UserID, UserEmail: c.Email, RequestID: c.RequestID})
	if err != nil && e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	if errors.Is(err, store.ErrBusy) {
		return store.Pass{}, &Refusal{Word: RefusalBusy}
	}
	if err != nil {
		return store.Pass{}, err
	}
	e.start(ctx, p, c, nil, "")
	return p, nil
}
func (e *Engine) start(ctx context.Context, p store.Pass, c identity.Caller, run *scrapes.Run, kind string) {
	e.startWork(ctx, p, c, func(workCtx context.Context) (store.Counts, string) {
		if p.Kind == store.KindEnrich {
			return e.enrich(workCtx, kind)
		}
		return e.resolve(workCtx, run, p.Kind)
	})
}

func (e *Engine) startWork(ctx context.Context, p store.Pass, c identity.Caller, job func(context.Context) (store.Counts, string)) {
	trail.Record(context.WithoutCancel(ctx), e.cfg.Telemetry, e.cfg.Events, trail.Started, p)
	e.work.Add(1)
	finishCtx := context.WithoutCancel(ctx)
	go func() {
		defer e.work.Done()
		totals, reason := job(identity.NewContext(e.ctx, c))
		ended, err := e.finish(finishCtx, p.ID, totals, reason)
		if err == nil {
			trail.Record(finishCtx, e.cfg.Telemetry, e.cfg.Events, trail.Finished, ended)
			e.follow(finishCtx, ended)
		}
	}()
}

// finish cancels an uncommitted ending when the drain arrives, then records
// interruption independently of the cancelled work context.
func (e *Engine) finish(ctx context.Context, id string, counts store.Counts, reason string) (store.Pass, error) {
	if e.ctx.Err() != nil {
		return e.cfg.Store.FailPass(ctx, id, store.ReasonInterrupted)
	}
	var ended store.Pass
	var err error
	if reason == "" {
		ended, err = e.cfg.Store.FinishPass(e.ctx, id, counts)
	} else {
		ended, err = e.cfg.Store.FailPass(e.ctx, id, reason)
	}
	if err != nil && e.ctx.Err() != nil {
		return e.cfg.Store.FailPass(ctx, id, store.ReasonInterrupted)
	}
	return ended, err
}

// Deliver handles a completed scraper run from the event bus.
func (e *Engine) Deliver(ctx context.Context, d events.Delivery) events.Outcome {
	if e.stopping.Load() {
		return events.Fail(Stopping)
	}
	e.mu.Lock()
	if e.stopping.Load() {
		e.mu.Unlock()
		return events.Fail(Stopping)
	}
	scraper, _ := d.Event.Attrs["scraper"].(string)
	runID, _ := d.Event.Attrs["run"].(string)
	status, _ := d.Event.Attrs["status"].(string)
	_, known := e.cfg.Sources.Get(scraper)
	judge := d.Event.Name == trail.AcceptedJudge && d.Event.ID != ""
	if !judge && (d.Event.Name != trail.Accepted || d.Event.ID == "" || status != store.StatusDone || !known || runID == "" || runID == store.TriggerRebuild) {
		e.mu.Unlock()
		return events.Skip()
	}
	if judge {
		runID = ""
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
	switch {
	case judge:
		err = nil
	case e.cfg.Scrapes == nil:
		err = scrapes.ErrUnavailable
	default:
		run, err = e.cfg.Scrapes.Record(identity.NewContext(e.ctx, service), runID)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() {
		return events.Fail(Stopping)
	}
	if !judge && (errors.Is(err, scrapes.ErrNotFound) || (err == nil && run.Status != store.StatusDone)) {
		return events.Skip()
	}
	kind, origin, trigger := store.KindIngest, store.OriginRun, runID
	if judge {
		kind, origin, trigger = store.KindJudge, store.OriginCron, store.TriggerJudge
	}
	p, writeErr := e.cfg.Store.AddPass(e.ctx, store.Pass{Kind: kind, Origin: origin, Trigger: trigger, Delivery: d.Event.ID, UserID: caller.UserID, RequestID: caller.RequestID})
	switch {
	case writeErr != nil && e.stopping.Load():
		return events.Fail(Stopping)
	case errors.Is(writeErr, store.ErrDelivered):
		return events.Skip()
	case errors.Is(writeErr, store.ErrBusy):
		return events.Fail(Busy)
	case writeErr != nil:
		return events.Fail(store.Unreachable)
	}
	if err == nil {
		if judge {
			e.start(ctx, p, service, nil, "")
		} else {
			run.Scraper = scraper
			e.start(ctx, p, service, &run, "")
		}
		return events.OK()
	}
	trail.Record(context.WithoutCancel(ctx), e.cfg.Telemetry, e.cfg.Events, trail.Started, p)
	ended, writeErr := e.finish(context.WithoutCancel(ctx), p.ID, store.Counts{}, store.ReasonScrapes)
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
	e.stopping.Store(true)
	e.cancel()
	done := make(chan struct{})
	go func() {
		// Synchronize with starts admitted before stopping was set.
		e.mu.Lock()
		wait := e.work.Wait
		e.mu.Unlock()
		wait()
		close(done)
	}()
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
	a.Judged += b.Judged
	a.Unranged += b.Unranged
	a.Derived += b.Derived
}
func (e *Engine) resolve(ctx context.Context, delivery *scrapes.Run, kind string) (store.Counts, string) {
	var total store.Counts
	rebuild := kind == store.KindRebuild
	process := func(run scrapes.Run, scraper string) string {
		if run.Status != store.StatusDone {
			return store.ReasonScrapes
		}
		source, _ := e.cfg.Sources.Get(scraper)
		batch := make([]store.Observation, 0, BatchSize)
		var rows int64
		var storeFailure bool
		flush := func() error {
			counts, _, err := e.cfg.Store.Attach(ctx, run.ID, rebuild, batch)
			if err != nil {
				storeFailure = true
				return err
			}
			addCounts(&total, counts)
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
		counts, _, err := e.cfg.Store.MarkGone(ctx, scraper, run.ID)
		if err != nil {
			return store.ReasonStore
		}
		addCounts(&total, counts)
		return ""
	}
	if delivery != nil {
		if reason := process(*delivery, delivery.Scraper); reason != "" {
			return store.Counts{}, reason
		}
	} else if rebuild {
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
	if rebuild {
		for _, en := range enrichers.New(enrichers.Config{Website: e.cfg.Website, Prompts: e.cfg.Prompts}).All() {
			if _, ok := en.(enrichers.Deriver); ok {
				n, err := e.derive(ctx, en.Kind(), true)
				if err != nil {
					return store.Counts{}, store.ReasonStore
				}
				total.Derived += n
			}
		}
	}
	if kind == store.KindRebuild || kind == store.KindJudge {
		counts, err := e.cfg.Store.JudgeAll(ctx)
		if err != nil {
			return store.Counts{}, store.ReasonStore
		}
		addCounts(&total, counts)
	}
	return total, ""
}
