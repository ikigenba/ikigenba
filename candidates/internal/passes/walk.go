package passes

import (
	"context"
	"errors"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
)

// Walk records an explicit search of the named parks and works it in the background.
func (e *Engine) Walk(ctx context.Context, c identity.Caller, ids []string, again bool) (store.Pass, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	refuse := func(word, text string) error { return &Refusal{Word: word, Text: text} }
	if e.stopping.Load() {
		return store.Pass{}, refuse(RefusalStopping, "")
	}
	if e.cfg.Places == nil {
		return store.Pass{}, refuse(RefusalInvalid, NoPlacesKey)
	}
	if len(ids) == 0 || len(ids) > MaxWalkParks {
		return store.Pass{}, refuse(RefusalInvalid, "")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return store.Pass{}, refuse(RefusalInvalid, "")
		}
		seen[id] = true
	}
	for _, id := range ids {
		if !store.ValidParkID(id) {
			return store.Pass{}, refuse(RefusalNotFound, "")
		}
	}
	selected := make([]store.Park, 0, len(ids))
	gone, walked := []string{}, []string{}
	for _, id := range ids {
		k, err := e.cfg.Store.GetPark(e.ctx, id)
		if err != nil {
			if e.stopping.Load() {
				return store.Pass{}, refuse(RefusalStopping, "")
			}
			if errors.Is(err, store.ErrNotFound) {
				return store.Pass{}, refuse(RefusalNotFound, "")
			}
			return store.Pass{}, err
		}
		selected = append(selected, k)
		if k.State == parks.StateGone {
			gone = append(gone, id)
		}
		if !k.Walked.IsZero() {
			walked = append(walked, id)
		}
	}
	if len(gone) > 0 {
		return store.Pass{}, refuse(RefusalInvalid, ParkGone+" "+strings.Join(gone, ", "))
	}
	if !again && len(walked) > 0 {
		return store.Pass{}, refuse(RefusalInvalid, AlreadyWalked+" "+strings.Join(walked, ", "))
	}
	ceiling, err := e.cfg.Store.WalkCeiling(e.ctx)
	if err != nil {
		if e.stopping.Load() {
			return store.Pass{}, refuse(RefusalStopping, "")
		}
		return store.Pass{}, err
	}
	if e.stopping.Load() {
		return store.Pass{}, refuse(RefusalStopping, "")
	}
	p, err := e.cfg.Store.AddPass(e.ctx, store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: c.UserID, UserEmail: c.Email, RequestID: c.RequestID})
	if err != nil {
		if e.stopping.Load() {
			return store.Pass{}, refuse(RefusalStopping, "")
		}
		if errors.Is(err, store.ErrBusy) {
			return store.Pass{}, refuse(RefusalBusy, "")
		}
		return store.Pass{}, err
	}
	e.startWalk(ctx, p, c, selected, ceiling)
	return p, nil
}

func (e *Engine) startWalk(ctx context.Context, p store.Pass, c identity.Caller, selected []store.Park, ceiling int64) {
	finishCtx := context.WithoutCancel(ctx)
	trail.Record(finishCtx, e.cfg.Telemetry, e.cfg.Events, trail.Started, p)
	e.work.Add(1)
	go func() {
		defer e.work.Done()
		failure := &workFailure{}
		workCtx := context.WithValue(identity.NewContext(e.ctx, c), failureKey{}, failure)
		totals, cut, reason, detail := e.walk(workCtx, p.ID, selected, ceiling)
		if e.ctx.Err() != nil {
			reason = store.ReasonInterrupted
		}
		if e.cfg.Stderr != nil {
			if reason == store.ReasonStore {
				detail = failure.text()
			}
			if reason == store.ReasonStore || reason == store.ReasonPlaces {
				_, _ = e.cfg.Stderr.Write([]byte("candidates: pass " + p.ID + " failed: " + reason + "\n\n" + strings.ReplaceAll(detail, "\n", " ") + "\n"))
			}
		}
		var ended store.Pass
		var err error
		switch reason {
		case "":
			ended, err = e.cfg.Store.FinishWalk(e.ctx, p.ID, totals, cut)
		case store.ReasonInterrupted:
			ended, err = e.cfg.Store.FailWalk(finishCtx, p.ID, reason, totals)
		default:
			ended, err = e.cfg.Store.FailWalk(e.ctx, p.ID, reason, totals)
		}
		if err != nil && e.ctx.Err() != nil {
			ended, err = e.cfg.Store.FailWalk(finishCtx, p.ID, store.ReasonInterrupted, totals)
		}
		if err == nil {
			trail.Record(finishCtx, e.cfg.Telemetry, e.cfg.Events, trail.Finished, ended)
			e.follow(finishCtx, ended)
		}
	}()
}

func (e *Engine) walk(ctx context.Context, pass string, selected []store.Park, ceiling int64) (store.Counts, bool, string, string) {
	var total store.Counts
	var made int64
	for _, k := range selected {
		if made >= ceiling {
			return total, true, "", ""
		}
		sweep := e.cfg.Places.Sweep(ctx, places.First(k.Lat, k.Lon, k.Radius), ceiling-made)
		made += sweep.Requests
		spent := func() {
			total.Circles += sweep.Circles
			total.Requests += sweep.Requests
			total.Cost += sweep.CostNanos
		}
		if ctx.Err() != nil {
			spent()
			return total, false, store.ReasonInterrupted, ""
		}
		w, err := e.cfg.Store.RecordSweep(ctx, store.Sweep{Pass: pass, Park: k.ID, Found: sweep.Found, Circles: sweep.Circles, Requests: sweep.Requests, Cost: sweep.CostNanos, CeilingReached: sweep.CeilingReached})
		if err != nil {
			spent()
			return total, false, e.storeFailure(ctx, err), ""
		}
		total.Circles += w.Circles
		total.Requests += w.Requests
		total.Found += w.Found
		total.NewCandidates += w.NewCandidates
		total.Cost += w.Cost
		if !w.Walked.IsZero() {
			total.ParksWalked++
		}
		if sweep.Error != "" {
			detail := sweep.Error
			if sweep.Status != "" {
				detail += " " + sweep.Status
			}
			return total, false, store.ReasonPlaces, detail
		}
		if sweep.CeilingReached {
			return total, true, "", ""
		}
	}
	return total, false, "", ""
}
