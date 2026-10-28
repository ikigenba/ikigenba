package passes

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// EnrichTarget starts one enrichment kind over a fixed prospect list or candidate.
func (e *Engine) EnrichTarget(ctx context.Context, c identity.Caller, kind, target string, force bool) (store.Pass, error) {
	if e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	en, ok := enrichers.New(enrichers.Config{Website: e.cfg.Website, Prompts: e.cfg.Prompts, Routes: e.cfg.Routes}).Get(kind)
	if !ok {
		return store.Pass{}, &Refusal{Word: RefusalInvalid}
	}
	targets := []string{}
	switch {
	case store.ValidProspectListID(target):
		read, err := e.cfg.Store.ReadProspectList(e.ctx, target)
		if err != nil {
			return store.Pass{}, targetReadError(err)
		}
		for _, m := range read.Members {
			targets = append(targets, m.Candidate.ID)
		}
	case store.ValidCandidateID(target):
		cand, err := e.cfg.Store.GetCandidate(e.ctx, target)
		if err != nil {
			return store.Pass{}, targetReadError(err)
		}
		if cand.Status == store.StatusMerged {
			return store.Pass{}, &Refusal{Word: RefusalInvalid}
		}
		targets = append(targets, target)
	default:
		return store.Pass{}, &Refusal{Word: RefusalNotFound}
	}
	if _, ok := en.(enrichers.Router); ok && e.cfg.Routes == nil {
		return store.Pass{}, &Refusal{Word: RefusalInvalid, Text: NoRouteKey}
	}
	if ready, ok := en.(interface{ Ready() bool }); ok && !ready.Ready() {
		return store.Pass{}, &Refusal{Word: RefusalUnavailable}
	}
	if pr, ok := en.(enrichers.Prompter); ok {
		_, err := e.cfg.Prompts.Show(identity.NewContext(e.ctx, c), pr.Prompt())
		if e.stopping.Load() {
			return store.Pass{}, &Refusal{Word: RefusalStopping}
		}
		if err != nil {
			var rf *prompts.Refusal
			if errors.As(err, &rf) {
				text := NoPrompt + " " + pr.Prompt()
				if rf.Text != "" {
					text += ": " + rf.Text
				}
				return store.Pass{}, &Refusal{Word: RefusalInvalid, Text: text}
			}
			return store.Pass{}, &Refusal{Word: RefusalUnavailable}
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	p, err := e.cfg.Store.AddPass(e.ctx, store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Target: target, EnrichKind: kind, UserID: c.UserID, UserEmail: c.Email, RequestID: c.RequestID})
	if err != nil && e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	if errors.Is(err, store.ErrBusy) {
		return store.Pass{}, &Refusal{Word: RefusalBusy}
	}
	if err != nil {
		return store.Pass{}, err
	}
	e.startWork(ctx, p, c, func(work context.Context) (store.Counts, string) { return e.targeted(work, en, targets, force) })
	return p, nil
}

func targetReadError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return &Refusal{Word: RefusalNotFound}
	}
	return err
}

func (e *Engine) targeted(ctx context.Context, en enrichers.Enricher, targets []string, force bool) (store.Counts, string) {
	if _, ok := en.(enrichers.Deriver); ok {
		n, err := e.cfg.Store.DeriveAmong(ctx, en.Kind(), targets, force)
		if err != nil {
			return store.Counts{}, e.storeFailure(ctx, err)
		}
		return store.Counts{Derived: n}, ""
	}
	ds, err := e.cfg.Store.DueAmong(ctx, en.Kind(), targets, force)
	if err != nil {
		return store.Counts{}, e.storeFailure(ctx, err)
	}
	if pr, ok := en.(enrichers.Prompter); ok {
		return e.promptTargets(ctx, pr, ds)
	}
	if rt, ok := en.(enrichers.Router); ok {
		return e.routeTargets(ctx, rt, ds)
	}
	return e.fetchTargets(ctx, en.(enrichers.Fetcher), ds)
}

func (e *Engine) fetchTargets(ctx context.Context, f enrichers.Fetcher, ds []store.Due) (store.Counts, string) {
	total := store.Counts{Due: int64(len(ds))}
	groups := map[string][]string{}
	inputs := []string{}
	for _, d := range ds {
		if _, ok := groups[d.Inputs]; !ok {
			inputs = append(inputs, d.Inputs)
		}
		groups[d.Inputs] = append(groups[d.Inputs], d.Candidate)
	}
	total.Fetched = int64(len(inputs))
	var mu sync.Mutex
	failed := false
	f.Fetch(ctx, inputs, func(in string, r enrichers.Result) {
		mu.Lock()
		defer mu.Unlock()
		if failed {
			return
		}
		es := make([]store.Enrichment, 0, len(groups[in]))
		for _, id := range groups[in] {
			es = append(es, store.Enrichment{Candidate: id, Kind: f.Kind(), Inputs: in, Source: r.Source, Found: r.Found, Cost: r.Cost})
		}
		saved, err := e.cfg.Store.AddEnrichments(ctx, es)
		if err != nil {
			failed = true
			e.storeFailure(ctx, err)
			return
		}
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
	})
	if failed {
		return total, store.ReasonStore
	}
	return total, ""
}

func (e *Engine) pause(ctx context.Context, d time.Duration) bool {
	after := e.cfg.After
	if after == nil {
		after = time.After
	}
	select {
	case <-ctx.Done():
		return false
	case <-after(d):
		return ctx.Err() == nil
	}
}

type promptResult struct {
	counts store.Counts
	reason string
}

func (e *Engine) promptTargets(ctx context.Context, pr enrichers.Prompter, ds []store.Due) (store.Counts, string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan promptResult, RunsInFlight)
	total := store.Counts{Due: int64(len(ds))}
	next, active := 0, 0
	for next < len(ds) || active > 0 {
		for active < RunsInFlight && next < len(ds) {
			d := ds[next]
			next++
			active++
			go func() { counts, reason := e.promptOne(ctx, pr, d); results <- promptResult{counts, reason} }()
		}
		select {
		case <-ctx.Done():
			return total, store.ReasonInterrupted
		case r := <-results:
			active--
			if r.reason != "" {
				cancel()
				for active > 0 {
					<-results
					active--
				}
				return total, r.reason
			}
			total.Runs += r.counts.Runs
			total.RunsFailed += r.counts.RunsFailed
			total.Pages += r.counts.Pages
			total.Cost += r.counts.Cost
			total.SearchDifferent += r.counts.SearchDifferent
			total.SearchStands += r.counts.SearchStands
			total.SearchNone += r.counts.SearchNone
		}
	}
	return total, ""
}

func (e *Engine) promptOne(ctx context.Context, pr enrichers.Prompter, d store.Due) (store.Counts, string) {
	var total store.Counts
	subject, err := e.cfg.Store.Subject(ctx, d.Candidate)
	if err != nil {
		return total, e.storeFailure(ctx, err)
	}
	before, err := e.cfg.Store.CountPages(ctx, d.Candidate)
	if err != nil {
		return total, e.storeFailure(ctx, err)
	}
	var start prompts.Start
	for attempt := 0; attempt < StartTries; attempt++ {
		if ctx.Err() != nil {
			return total, store.ReasonInterrupted
		}
		start, err = e.cfg.Prompts.Run(ctx, pr.Prompt(), pr.Input(subject))
		if err == nil {
			break
		}
		var rf *prompts.Refusal
		if !errors.As(err, &rf) || attempt == StartTries-1 {
			return total, store.ReasonPrompts
		}
		if !e.pause(ctx, QueueWait) {
			return total, store.ReasonInterrupted
		}
		if _, err = e.cfg.Prompts.Show(ctx, pr.Prompt()); err != nil {
			return total, store.ReasonPrompts
		}
	}
	total.Runs = 1
	for {
		if !e.pause(ctx, PollInterval) {
			return total, store.ReasonInterrupted
		}
		out, err := e.cfg.Prompts.Result(ctx, start.ID)
		if err != nil {
			return total, store.ReasonPrompts
		}
		if out.Status == enrichers.RunQueued || out.Status == enrichers.RunRunning {
			continue
		}
		after, err := e.cfg.Store.CountPages(ctx, d.Candidate)
		if err != nil {
			return total, e.storeFailure(ctx, err)
		}
		r := pr.Found(ctx, subject, out, after.URLs)
		if !pr.Valid(r.Found) {
			return total, store.ReasonPrompts
		}
		if ctx.Err() != nil {
			return total, store.ReasonInterrupted
		}
		_, err = e.cfg.Store.AddEnrichments(ctx, []store.Enrichment{{Candidate: d.Candidate, Kind: pr.Kind(), Inputs: d.Inputs, Source: r.Source, Found: r.Found, Cost: r.Cost}})
		if err != nil {
			return total, e.storeFailure(ctx, err)
		}
		if pr.Kind() == enrichers.KindWebsiteSearch {
			var found enrichers.WebsiteSearchFound
			_ = json.Unmarshal(r.Found, &found)
			switch found.Case {
			case enrichers.SearchCaseDifferent:
				total.SearchDifferent = 1
			case enrichers.SearchCaseStands:
				total.SearchStands = 1
			case enrichers.SearchCaseNone:
				total.SearchNone = 1
			}
		}
		if pr.Failed(r.Found) {
			total.RunsFailed = 1
		}
		if pr.Kind() == enrichers.KindSiteText {
			total.Pages = max(int64(0), after.Copies-before.Copies)
		}
		total.Cost = out.CostNanos
		return total, ""
	}
}
