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

// EnrichTarget starts the requested enrichment kinds over a fixed target.
func (e *Engine) EnrichTarget(ctx context.Context, c identity.Caller, kind string, t Target, force bool) (store.Pass, error) {
	if e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	reg := enrichers.New(enrichers.Config{Website: e.cfg.Website, Prompts: e.cfg.Prompts, Routes: e.cfg.Routes})
	if _, ok := reg.Get(kind); kind != "" && !ok {
		return store.Pass{}, &Refusal{Word: RefusalInvalid}
	}
	if (t.ID == "" && len(t.IDs) == 0) || (t.ID != "" && len(t.IDs) != 0) || len(t.IDs) > MaxTargetIDs {
		return store.Pass{}, &Refusal{Word: RefusalInvalid}
	}
	targets := append([]string(nil), t.IDs...)
	if t.ID != "" {
		switch {
		case store.ValidProspectListID(t.ID):
			read, err := e.cfg.Store.ReadProspectList(e.ctx, t.ID)
			if err != nil {
				return store.Pass{}, targetReadError(err)
			}
			for _, m := range read.Members {
				targets = append(targets, m.Candidate.ID)
			}
		case store.ValidCandidateID(t.ID):
			cand, err := e.cfg.Store.GetCandidate(e.ctx, t.ID)
			if err != nil {
				return store.Pass{}, targetReadError(err)
			}
			if cand.Status == store.StatusMerged {
				return store.Pass{}, &Refusal{Word: RefusalInvalid}
			}
			targets = append(targets, t.ID)
		default:
			return store.Pass{}, &Refusal{Word: RefusalNotFound}
		}
	}
	kinds := targetKinds(reg, kind, e.cfg.Routes != nil)
	for _, en := range kinds {
		if _, ok := en.(enrichers.Router); ok && e.cfg.Routes == nil {
			return store.Pass{}, &Refusal{Word: RefusalInvalid, Text: NoRouteKey}
		}
		if ready, ok := en.(interface{ Ready() bool }); ok && !ready.Ready() {
			return store.Pass{}, &Refusal{Word: RefusalUnavailable}
		}
	}
	for _, en := range kinds {
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
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	if e.filling {
		return store.Pass{}, &Refusal{Word: RefusalBusy}
	}
	p, claimed, err := e.cfg.Store.AddTargetPass(e.ctx, store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Target: t.ID, TargetIDs: append([]string(nil), t.IDs...), EnrichKind: kind, UserID: c.UserID, UserEmail: c.Email, RequestID: c.RequestID}, targets)
	if err != nil && e.stopping.Load() {
		return store.Pass{}, &Refusal{Word: RefusalStopping}
	}
	if err != nil {
		return store.Pass{}, err
	}
	// Named kinds begin with the recorded pass. Capture even queued subjects
	// before returning, so a later worker sees the same input as the first.
	var selection targetSelection
	if kind != "" {
		selection, err = e.selectTargets(identity.NewContext(e.ctx, c), kinds[0], claimed, force, false)
	}
	e.startWork(ctx, p, c, func(work context.Context) (store.Counts, string) {
		if err != nil {
			return store.Counts{}, e.storeFailure(work, err)
		}
		if kind != "" {
			return e.targeted(work, kinds[0], claimed, force, selection)
		}
		return e.targetKinds(work, p.ID, kinds, claimed, force)
	})
	return p, nil
}

func targetKinds(reg *enrichers.Registry, kind string, routes bool) []enrichers.Enricher {
	if kind != "" {
		en, _ := reg.Get(kind)
		return []enrichers.Enricher{en}
	}
	out := []enrichers.Enricher{}
	for _, en := range reg.All() {
		if _, ok := en.(enrichers.Deriver); ok {
			continue
		}
		if _, ok := en.(enrichers.Router); ok && !routes {
			continue
		}
		out = append(out, en)
	}
	return out
}

func targetReadError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return &Refusal{Word: RefusalNotFound}
	}
	return err
}

type targetSelection struct {
	due      []store.Due
	subjects map[string]enrichers.Subject
	previous map[string]int
	entry    store.KindEntry
}

func (e *Engine) selectTargets(ctx context.Context, en enrichers.Enricher, targets []string, force, progress bool) (targetSelection, error) {
	sel := targetSelection{subjects: map[string]enrichers.Subject{}, previous: map[string]int{}, entry: store.KindEntry{Kind: en.Kind()}}
	var err error
	sel.due, err = e.cfg.Store.DueAmong(ctx, en.Kind(), targets, force)
	if err != nil {
		return sel, err
	}
	if progress {
		applicable, err := e.cfg.Store.DueAmong(ctx, en.Kind(), targets, true)
		if err != nil {
			return sel, err
		}
		var active int64
		for _, id := range targets {
			c, err := e.cfg.Store.GetCandidate(ctx, id)
			if err != nil {
				return sel, err
			}
			if c.Status == store.StatusActive {
				active++
			}
		}
		sel.entry.NotApplicable = active - int64(len(applicable))
		sel.entry.NotDue = int64(len(applicable) - len(sel.due))
	}
	_, prompt := en.(enrichers.Prompter)
	_, route := en.(enrichers.Router)
	for _, d := range sel.due {
		if prompt || route {
			s, err := e.cfg.Store.Subject(ctx, d.Candidate)
			if err != nil {
				return sel, err
			}
			sel.subjects[d.Candidate] = s
		}
		if progress {
			es, err := e.cfg.Store.Enrichments(ctx, d.Candidate)
			if err != nil {
				return sel, err
			}
			sel.previous[d.Candidate] = len(es)
		}
	}
	return sel, nil
}

func (e *Engine) targetKinds(ctx context.Context, id string, kinds []enrichers.Enricher, targets []string, force bool) (store.Counts, string) {
	var total store.Counts
	for _, en := range kinds {
		if ctx.Err() != nil {
			return total, store.ReasonInterrupted
		}
		if _, err := e.cfg.Store.StartPassKind(ctx, id, en.Kind()); err != nil {
			return total, e.storeFailure(ctx, err)
		}
		sel, err := e.selectTargets(ctx, en, targets, force, true)
		if err != nil {
			return total, e.storeFailure(ctx, err)
		}
		counts, reason := e.targeted(ctx, en, targets, force, sel)
		if reason != "" {
			return total, reason
		}
		if ctx.Err() != nil {
			return total, store.ReasonInterrupted
		}
		entry := sel.entry
		for _, d := range sel.due {
			es, err := e.cfg.Store.Enrichments(ctx, d.Candidate)
			if err != nil {
				return total, e.storeFailure(ctx, err)
			}
			for _, r := range es[:max(0, len(es)-sel.previous[d.Candidate])] {
				if r.Kind != en.Kind() {
					continue
				}
				f, ok := en.(enrichers.Failer)
				if ok && f.Failed(r.Found) {
					entry.Failed++
				} else {
					entry.NewResults++
				}
			}
		}
		entry.Cost = counts.Cost
		if _, err := e.cfg.Store.EndPassKind(ctx, id, entry); err != nil {
			return total, e.storeFailure(ctx, err)
		}
		total.Due += counts.Due
		total.Fetched += counts.Fetched
		total.HTTPS += counts.HTTPS
		total.HTTP += counts.HTTP
		total.None += counts.None
		total.Runs += counts.Runs
		total.RunsFailed += counts.RunsFailed
		total.Pages += counts.Pages
		total.Cost += counts.Cost
		total.SearchDifferent += counts.SearchDifferent
		total.SearchStands += counts.SearchStands
		total.SearchNone += counts.SearchNone
	}
	return total, ""
}

func (e *Engine) targeted(ctx context.Context, en enrichers.Enricher, targets []string, force bool, sel targetSelection) (store.Counts, string) {
	if _, ok := en.(enrichers.Deriver); ok {
		n, err := e.cfg.Store.DeriveAmong(ctx, en.Kind(), targets, force)
		if err != nil {
			return store.Counts{}, e.storeFailure(ctx, err)
		}
		return store.Counts{Derived: n}, ""
	}
	if pr, ok := en.(enrichers.Prompter); ok {
		return e.promptTargets(ctx, pr, sel.due, sel.subjects)
	}
	if rt, ok := en.(enrichers.Router); ok {
		return e.routeTargets(ctx, rt, sel.due, sel.subjects)
	}
	return e.fetchTargets(ctx, en.(enrichers.Fetcher), sel.due)
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
		if failed || ctx.Err() != nil {
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

func (e *Engine) promptTargets(ctx context.Context, pr enrichers.Prompter, ds []store.Due, subjects map[string]enrichers.Subject) (store.Counts, string) {
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
			go func() {
				counts, reason := e.promptOne(ctx, pr, d, subjects[d.Candidate])
				results <- promptResult{counts, reason}
			}()
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

func (e *Engine) promptOne(ctx context.Context, pr enrichers.Prompter, d store.Due, subject enrichers.Subject) (store.Counts, string) {
	var total store.Counts
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
