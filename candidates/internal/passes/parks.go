package passes

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// EditParkRules atomically records the owner's edit and its derivation pass.
func (e *Engine) EditParkRules(ctx context.Context, c identity.Caller, edit store.ParkRulesEdit) (store.Pass, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	if e.cfg.Scrapes == nil {
		return store.Pass{}, &Refusal{Word: RefusalUnavailable}
	}
	p, err := e.cfg.Store.SetParkRules(e.ctx, edit, store.Pass{Kind: store.KindParks, Origin: store.OriginRules, Trigger: store.TriggerParkRules, UserID: c.UserID, UserEmail: c.Email, RequestID: c.RequestID})
	if err != nil {
		word := RefusalUnavailable
		switch {
		case e.stopping.Load():
			word = RefusalStopping
		case errors.Is(err, store.ErrInvalid):
			word = RefusalInvalid
		case errors.Is(err, store.ErrBusy):
			word = RefusalBusy
		}
		return store.Pass{}, &Refusal{Word: word}
	}
	e.start(ctx, p, c, nil, "")
	return p, nil
}

// StartParks starts a derivation only when the current parcel run differs.
func (e *Engine) StartParks(ctx context.Context, requestID string) (store.Pass, bool) {
	if e.stopping.Load() || e.cfg.Scrapes == nil {
		return store.Pass{}, false
	}
	c := identity.Caller{UserID: "candidates", RequestID: requestID}
	readCtx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	run, err := e.cfg.Scrapes.Current(identity.NewContext(readCtx, c), parks.Scraper)
	if err != nil || run.Status != store.StatusDone {
		return store.Pass{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() {
		return store.Pass{}, false
	}
	last, err := e.cfg.Store.ParksRun(e.ctx)
	if err != nil || last == run.ID {
		return store.Pass{}, false
	}
	p, err := e.cfg.Store.AddPass(e.ctx, store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: run.ID, RequestID: requestID})
	if err != nil {
		return store.Pass{}, false
	}
	e.start(ctx, p, c, &run, "")
	return p, true
}

type parkSnapshot struct {
	rules parks.Rules
	keys  map[string]bool
	prior []parks.Prior
}

func (e *Engine) parkSnapshot(ctx context.Context) (parkSnapshot, error) {
	rules, _, err := e.cfg.Store.ParkRules(ctx)
	if err != nil {
		return parkSnapshot{}, err
	}
	keys := make(map[string]bool)
	after := ""
	for {
		page, next, err := e.cfg.Store.ActiveAddressKeys(ctx, after, BatchSize)
		if err != nil {
			return parkSnapshot{}, err
		}
		for _, key := range page {
			keys[key] = true
		}
		if next == "" {
			break
		}
		after = next
	}
	prior, err := e.cfg.Store.PriorParks(ctx)
	return parkSnapshot{rules, keys, prior}, err
}

func (e *Engine) parks(ctx context.Context, supplied *scrapes.Run, p store.Pass, snapshot parkSnapshot) (store.Counts, string) {
	rules, keys, prior := snapshot.rules, snapshot.keys, snapshot.prior
	var err error
	var run scrapes.Run
	if supplied != nil {
		run = *supplied
	} else {
		run, err = e.cfg.Scrapes.Current(ctx, parks.Scraper)
		if err != nil {
			return store.Counts{}, store.ReasonScrapes
		}
	}
	if run.Status != store.StatusDone {
		return store.Counts{}, store.ReasonScrapes
	}
	var kept []parks.Parcel
	counties := make(map[string]bool)
	var counts store.Counts
	err = e.cfg.Scrapes.Rows(ctx, run.ID, func(row scrapes.Row) error {
		counts.ParcelsRead++
		parcel := parks.ParcelOf(row)
		if parcel.County != "" {
			counties[parcel.County] = true
		}
		parcel.Rule = parks.Classify(parcel, rules, keys)
		switch parcel.Rule {
		case parks.RuleLabel:
			counts.ByLabel++
		case parks.RuleValue:
			counts.ByValue++
		case parks.RuleCandidate:
			counts.ByCandidate++
		}
		if parcel.Rule != "" {
			kept = append(kept, parcel)
		}
		return nil
	})
	if err != nil || counts.ParcelsRead != run.Rows {
		return store.Counts{}, store.ReasonScrapes
	}
	plan := parks.Match(parks.Clusters(kept, rules), prior)
	counts.ParksCreated = int64(len(plan.News))
	counts.ParksUpdated = int64(len(plan.Updates))
	counts.ParksGone = int64(len(plan.Gone))
	counts.ParksGrown = plan.Grown
	cs := make([]string, 0, len(counties))
	for county := range counties {
		cs = append(cs, county)
	}
	sort.Strings(cs)
	_, err = e.cfg.Store.ApplyParks(ctx, p.ID, run.ID, cs, plan, counts)
	if err != nil {
		return store.Counts{}, e.storeFailure(ctx, err)
	}
	return counts, ""
}

// The full-ingest sentinel is a valid parcel run id too. FindDelivered
// deliberately omits that sentinel for candidate ingests.
func (e *Engine) parcelTriggerRecorded(ctx context.Context, run string) error {
	started, id := "", ""
	for {
		page, err := e.cfg.Store.ListPasses(ctx, started, id, BatchSize)
		if err != nil {
			return err
		}
		for _, p := range page {
			if p.Trigger == run {
				return nil
			}
		}
		if len(page) < BatchSize {
			return store.ErrNotFound
		}
		last := page[len(page)-1]
		started, id = last.Started.UTC().Format(time.RFC3339), last.ID
	}
}
