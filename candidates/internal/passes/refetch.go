package passes

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// StartRefetch repairs the migration's page work list beside ordinary passes.
func (e *Engine) StartRefetch(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping.Load() || e.refetching || e.cfg.Website == nil {
		close(done)
		return done
	}
	e.refetching = true
	e.work.Add(1)
	workCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.ctx, cancel)
	go func() {
		defer close(done)
		defer cancel()
		defer stop()
		counts, err := e.refetch(workCtx)
		e.mu.Lock()
		cut := workCtx.Err() != nil || e.stopping.Load()
		e.refetching = false
		e.work.Done()
		e.mu.Unlock()
		if cut || e.cfg.Stderr == nil {
			return
		}
		line := fmt.Sprintf("candidates: pages refetched: %d read, %d scripted, %d redirect, %d empty, %d failed\n", counts[0], counts[1], counts[2], counts[3], counts[4])
		if err != nil {
			line = "candidates: pages refetch failed\n\n" + strings.ReplaceAll(err.Error(), "\n", " ") + "\n"
		}
		_, _ = e.cfg.Stderr.Write([]byte(line))
	}()
	return done
}

func (e *Engine) refetch(ctx context.Context) ([5]int, error) {
	var counts [5]int
	var after int64
	first := true
	for ctx.Err() == nil && !e.stopping.Load() {
		entries, err := e.cfg.Store.PendingRefetches(ctx, after, BatchSize)
		if err != nil || len(entries) == 0 {
			return counts, err
		}
		for _, entry := range entries {
			if ctx.Err() != nil || e.stopping.Load() || (!first && !e.pause(ctx, website.Interval)) {
				return counts, ctx.Err()
			}
			if ctx.Err() != nil || e.stopping.Load() {
				return counts, ctx.Err()
			}
			first = false
			page := e.cfg.Website.Get(ctx, entry.URL)
			if ctx.Err() != nil || e.stopping.Load() {
				return counts, ctx.Err()
			}
			outcome, index := store.RefetchFailed, 4
			if page.Error == "" && website.OnSite(entry.URL, page.URL) {
				_, err = e.cfg.Store.AddPage(ctx, store.Page{Candidate: entry.Candidate, URL: entry.URL, Title: page.Title, Text: page.Text, Reason: page.Reason, Size: page.Size, UserID: entry.UserID, UserEmail: entry.UserEmail})
				if errors.Is(err, store.ErrUnreachable) {
					return counts, err
				}
				if err == nil {
					outcome, index = store.RefetchRead, 0
					if page.Reason != "" {
						outcome = store.RefetchUnreadable
						switch page.Reason {
						case website.ReasonScripted:
							index = 1
						case website.ReasonRedirect:
							index = 2
						case website.ReasonEmpty:
							index = 3
						}
					}
				}
			}
			if ctx.Err() != nil || e.stopping.Load() {
				return counts, ctx.Err()
			}
			if err = e.cfg.Store.SettleRefetch(ctx, entry.Number, outcome); err != nil {
				return counts, err
			}
			counts[index]++
			after = entry.Number
		}
	}
	return counts, ctx.Err()
}
