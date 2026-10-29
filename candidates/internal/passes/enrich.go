package passes

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// Enrich records a pass over one kind, or every ready kind when kind is empty.
func (e *Engine) Enrich(ctx context.Context, c identity.Caller, kind string) (store.Pass, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	reg := enrichers.New(enrichers.Config{Website: e.cfg.Website, Prompts: e.cfg.Prompts, Routes: e.cfg.Routes})
	if kind != "" {
		en, ok := reg.Get(kind)
		_, promptsKind := en.(enrichers.Prompter)
		_, routeKind := en.(enrichers.Router)
		if !ok || promptsKind || routeKind {
			return store.Pass{}, &Refusal{Word: RefusalInvalid}
		}
		if f, ok := en.(enrichers.Fetcher); ok && !f.Ready() {
			return store.Pass{}, &Refusal{Word: RefusalUnavailable}
		}
	}
	if e.filling {
		return store.Pass{}, &Refusal{Word: RefusalBusy}
	}
	p, err := e.cfg.Store.AddPass(e.ctx, store.Pass{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: c.UserID, UserEmail: c.Email, RequestID: c.RequestID})
	if err != nil && e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	if errors.Is(err, store.ErrBusy) {
		return store.Pass{}, &Refusal{Word: RefusalBusy}
	}
	if err != nil {
		return store.Pass{}, err
	}
	e.start(ctx, p, c, nil, kind)
	return p, nil
}

func (e *Engine) follow(ctx context.Context, starter store.Pass) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() || starter.Status != store.StatusDone {
		return
	}
	kind := ""
	if starter.Kind == store.KindWalk || starter.Kind == store.KindIngest && (starter.Origin != store.OriginIngest || starter.Scraper != "") {
		kind = store.KindEnrich
	}
	if (starter.Kind == store.KindEnrich || starter.Kind == store.KindEnrichTarget) && starter.Derived+starter.HTTPS+starter.HTTP+starter.None > 0 {
		kind = store.KindJudge
	}
	if kind == "" {
		return
	}
	p, err := e.cfg.Store.AddPass(e.ctx, store.Pass{Kind: kind, Origin: store.OriginPass, Trigger: starter.ID, RequestID: starter.RequestID})
	if err != nil {
		return
	}
	e.start(ctx, p, identity.Caller{UserID: "candidates", RequestID: p.RequestID}, nil, "")
}

func (e *Engine) derive(ctx context.Context, kind string, force bool) (int64, error) {
	var total int64
	after := ""
	for {
		next, n, err := e.cfg.Store.Derive(ctx, kind, after, force, BatchSize)
		if err != nil {
			return total, err
		}
		total += n
		if next == "" {
			return total, nil
		}
		after = next
	}
}

func (e *Engine) enrich(ctx context.Context, kind string) (store.Counts, string) {
	var total store.Counts
	for _, en := range enrichers.New(enrichers.Config{Website: e.cfg.Website, Prompts: e.cfg.Prompts, Routes: e.cfg.Routes}).All() {
		if kind != "" && kind != en.Kind() {
			continue
		}
		if _, ok := en.(enrichers.Deriver); ok {
			n, err := e.derive(ctx, en.Kind(), false)
			if err != nil {
				return total, e.storeFailure(ctx, err)
			}
			total.Derived += n
			continue
		}
		fetcher, fetched := en.(enrichers.Fetcher)
		if !fetched {
			continue
		}
		if !fetcher.Ready() {
			continue
		}
		counts, reason := e.fetch(ctx, fetcher)
		total.Due += counts.Due
		total.Fetched += counts.Fetched
		total.HTTPS += counts.HTTPS
		total.HTTP += counts.HTTP
		total.None += counts.None
		if reason != "" {
			return total, reason
		}
	}
	return total, ""
}

func (e *Engine) fetch(ctx context.Context, fetcher enrichers.Fetcher) (store.Counts, string) {
	var total store.Counts
	mark, err := e.cfg.Store.EnrichmentMark(ctx)
	if err != nil {
		return total, e.storeFailure(ctx, err)
	}
	after := ""
	for {
		selection, err := e.enrichSelection(ctx, after)
		if err != nil {
			return total, e.storeFailure(ctx, err)
		}
		if selection.after == "" {
			return total, ""
		}
		after = selection.after
		total.Due += selection.due
		groups, domains := selection.groups, selection.domains
		// Keep only one batch of candidate ids and results in memory. Results from
		// earlier batches are read back through the store's recording mark.
		var resultMu sync.Mutex
		failed := false
		writing := false
		type completedCheck struct {
			input  string
			result enrichers.Result
			done   chan struct{}
		}
		var pending []completedCheck
		write := func(dom string, r enrichers.Result) {
			job := completedCheck{input: dom, result: r, done: make(chan struct{})}
			resultMu.Lock()
			if failed || ctx.Err() != nil {
				resultMu.Unlock()
				return
			}
			pending = append(pending, job)
			if writing {
				resultMu.Unlock()
				<-job.done
				return
			}
			writing = true
			resultMu.Unlock()
			// The first completed check writes immediately. Other completed
			// checks queue while that write runs and share the next transaction;
			// a check still fetching never holds back a completed result.
			for {
				resultMu.Lock()
				batch := pending
				pending = nil
				if len(batch) == 0 {
					writing = false
					resultMu.Unlock()
					return
				}
				stop := failed || ctx.Err() != nil
				resultMu.Unlock()
				if !stop {
					n := 0
					for _, check := range batch {
						n += len(groups[check.input])
					}
					es := make([]store.Enrichment, 0, n)
					for _, check := range batch {
						for _, id := range groups[check.input] {
							r := check.result
							es = append(es, store.Enrichment{Candidate: id, Kind: sources.WebsiteCheck, Inputs: check.input, Source: r.Source, Found: r.Found, Cost: r.Cost})
						}
					}
					var saved []store.Enrichment
					var err error
					if ctx.Err() == nil {
						saved, err = e.cfg.Store.AddEnrichments(ctx, es)
					}
					resultMu.Lock()
					if err != nil {
						failed = true
						e.storeFailure(ctx, err)
					} else {
						for _, v := range saved {
							var found website.Result
							_ = json.Unmarshal(v.Found, &found)
							switch found.State {
							case sources.WebsiteHTTPS:
								total.HTTPS++
							case sources.WebsiteHTTP:
								total.HTTP++
							case sources.WebsiteNone:
								total.None++
							}
						}
					}
					resultMu.Unlock()
				}
				for _, check := range batch {
					close(check.done)
				}
			}
		}
		unchecked := domains[:0]
		for _, dom := range domains {
			prev, err := e.cfg.Store.EnrichmentAfter(ctx, sources.WebsiteCheck, dom, mark)
			if errors.Is(err, store.ErrNotFound) {
				unchecked = append(unchecked, dom)
				continue
			}
			if err != nil {
				return total, e.storeFailure(ctx, err)
			}
			write(dom, enrichers.Result{Source: prev.Source, Found: prev.Found, Cost: prev.Cost})
		}
		if failed || ctx.Err() != nil {
			return total, e.storeFailure(ctx, err)
		}
		total.Fetched += int64(len(unchecked))
		for start := 0; start < len(unchecked); start += website.Concurrency {
			if start > 0 {
				after := e.cfg.Website.After
				if after == nil {
					after = time.After
				}
				select {
				case <-after(website.Interval):
				case <-ctx.Done():
					return total, e.storeFailure(ctx, err)
				}
			}
			end := min(start+website.Concurrency, len(unchecked))
			fetcher.Fetch(ctx, unchecked[start:end], write)
			if failed || ctx.Err() != nil {
				return total, e.storeFailure(ctx, err)
			}
		}
	}
}

// enrichSelection keeps full candidate records off the checking stack. Only
// this batch's checked domains and candidate ids live while callbacks write.
type enrichmentSelection struct {
	after   string
	groups  map[string][]string
	domains []string
	due     int64
}

func (e *Engine) enrichSelection(ctx context.Context, after string) (enrichmentSelection, error) {
	next, batch, err := e.cfg.Store.DueEnrichments(ctx, sources.WebsiteCheck, after, BatchSize)
	if err != nil {
		return enrichmentSelection{}, err
	}
	out := enrichmentSelection{after: next, groups: make(map[string][]string), domains: make([]string, 0, len(batch))}
	if len(batch) == 0 {
		return out, nil
	}

	for _, c := range batch {
		dom := c.Inputs
		if dom == "" {
			continue
		}
		out.due++
		if _, ok := out.groups[dom]; !ok {
			out.domains = append(out.domains, dom)
		}
		out.groups[dom] = append(out.groups[dom], c.Candidate)
	}
	return out, nil
}
