package passes

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// Enrich records a website pass and starts it without waiting for its checks.
func (e *Engine) Enrich(ctx context.Context, c identity.Caller, olderThanDays *int64) (store.Pass, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() {
		return store.Pass{}, &Refusal{RefusalStopping}
	}
	if e.cfg.Website == nil {
		return store.Pass{}, &Refusal{RefusalUnavailable}
	}
	if olderThanDays != nil && *olderThanDays < 0 {
		return store.Pass{}, &Refusal{RefusalInvalid}
	}
	p, err := e.cfg.Store.AddPass(e.ctx, store.Pass{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: c.UserID, UserEmail: c.Email, RequestID: c.RequestID})
	if errors.Is(err, store.ErrBusy) {
		return store.Pass{}, &Refusal{RefusalBusy}
	}
	if err != nil {
		return store.Pass{}, err
	}
	var ages *int64
	if olderThanDays != nil {
		v := *olderThanDays
		ages = &v
	}
	e.start(ctx, p, c, nil, ages)
	return p, nil
}

func (e *Engine) follow(ctx context.Context, starter store.Pass) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() || starter.Status != store.StatusDone {
		return
	}
	kind := ""
	if starter.Kind == store.KindIngest && e.cfg.Website != nil {
		kind = store.KindEnrich
	}
	if starter.Kind == store.KindEnrich && starter.HTTPS+starter.HTTP+starter.None > 0 {
		kind = store.KindJudge
	}
	if kind == "" {
		return
	}
	p, err := e.cfg.Store.AddPass(e.ctx, store.Pass{Kind: kind, Origin: store.OriginPass, Trigger: starter.ID, RequestID: starter.RequestID})
	if err != nil {
		return
	}
	e.start(ctx, p, identity.Caller{UserID: "candidates", RequestID: p.RequestID}, nil, nil)
}

func (e *Engine) enrich(ctx context.Context, ages *int64) (store.Counts, string) {
	var total store.Counts
	live, none := int64(website.RefreshLive), int64(website.RefreshNone)
	if ages != nil {
		live, none = *ages, *ages
	}
	mark, err := e.cfg.Store.EnrichmentMark(ctx)
	if err != nil {
		return total, store.ReasonStore
	}
	after := ""
	for {
		selection, err := e.enrichSelection(ctx, after, live, none)
		if err != nil {
			return total, store.ReasonStore
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
		write := func(dom string, r website.Result) {
			resultMu.Lock()
			defer resultMu.Unlock()
			if failed {
				return
			}
			es := make([]store.Enrichment, 0, len(groups[dom]))
			for _, id := range groups[dom] {
				es = append(es, store.Enrichment{Candidate: id, Kind: sources.WebsiteCheck, Source: dom, State: r.State, URL: r.URL, Title: r.Title, StatusCode: r.StatusCode, Size: r.Size, Error: r.Error})
			}
			e.mu.Lock()
			if e.stopping.Load() {
				e.mu.Unlock()
				return
			}
			saved, err := e.cfg.Store.AddEnrichments(ctx, es)
			e.mu.Unlock()
			if err != nil {
				failed = true
				return
			}
			for _, v := range saved {
				switch v.State {
				case sources.WebsiteHTTPS:
					total.HTTPS++
				case sources.WebsiteHTTP:
					total.HTTP++
				case sources.WebsiteNone:
					total.None++
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
				return total, store.ReasonStore
			}
			write(dom, website.Result{State: prev.State, URL: prev.URL, Title: prev.Title, StatusCode: prev.StatusCode, Size: prev.Size, Error: prev.Error})
		}
		if failed || ctx.Err() != nil {
			return total, store.ReasonStore
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
					return total, store.ReasonStore
				}
			}
			end := min(start+website.Concurrency, len(unchecked))
			e.cfg.Website.CheckAll(ctx, unchecked[start:end], write)
			if failed || ctx.Err() != nil {
				return total, store.ReasonStore
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

func (e *Engine) enrichSelection(ctx context.Context, after string, live, none int64) (enrichmentSelection, error) {
	batch, err := e.cfg.Store.DueEnrichments(ctx, sources.WebsiteCheck, after, live, none, BatchSize)
	if err != nil {
		return enrichmentSelection{}, err
	}
	out := enrichmentSelection{groups: make(map[string][]string), domains: make([]string, 0, len(batch))}
	if len(batch) == 0 {
		return out, nil
	}
	out.after = batch[len(batch)-1].ID
	for _, c := range batch {
		dom := website.Domain(c.Email)
		if dom == "" {
			continue
		}
		out.due++
		if _, ok := out.groups[dom]; !ok {
			out.domains = append(out.domains, dom)
		}
		out.groups[dom] = append(out.groups[dom], c.ID)
	}
	return out, nil
}
