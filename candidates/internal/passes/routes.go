package passes

import (
	"context"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// routeTargets keeps at most one request per worker in flight. A completed
// worker admits the next subject immediately, without an engine timer.
func (e *Engine) routeTargets(ctx context.Context, rt enrichers.Router, ds []store.Due) (store.Counts, string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan promptResult, RoutesInFlight)
	total := store.Counts{Due: int64(len(ds))}
	next, active := 0, 0
	reason := ""
	for next < len(ds) || active > 0 {
		for reason == "" && ctx.Err() == nil && active < RoutesInFlight && next < len(ds) {
			d := ds[next]
			next++
			active++
			go func() { counts, why := e.routeOne(ctx, rt, d); results <- promptResult{counts, why} }()
		}
		if ctx.Err() != nil && reason == "" {
			reason = store.ReasonInterrupted
		}
		if active == 0 {
			break
		}
		r := <-results
		active--
		total.Runs += r.counts.Runs
		total.RunsFailed += r.counts.RunsFailed
		total.Cost += r.counts.Cost
		if reason == "" && r.reason != "" {
			reason = r.reason
			cancel()
		}
		if reason != "" {
			next = len(ds)
		}
	}
	return total, reason
}

func (e *Engine) routeOne(ctx context.Context, rt enrichers.Router, d store.Due) (store.Counts, string) {
	s, err := e.cfg.Store.Subject(ctx, d.Candidate)
	if err != nil {
		return store.Counts{}, store.ReasonStore
	}
	res, tally := rt.Route(ctx, s)
	counts := store.Counts{Runs: tally.Sent, RunsFailed: tally.Failed, Cost: tally.CostNanos}
	if ctx.Err() != nil {
		return counts, store.ReasonInterrupted
	}
	_, err = e.cfg.Store.AddEnrichments(ctx, []store.Enrichment{{Candidate: d.Candidate, Kind: rt.Kind(), Inputs: d.Inputs, Source: res.Source, Found: res.Found, Cost: res.Cost}})
	if err != nil {
		return counts, store.ReasonStore
	}
	return counts, ""
}
