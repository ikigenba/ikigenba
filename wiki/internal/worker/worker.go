// Package worker will run asynchronous wiki jobs.
package worker

import (
	"context"
	"time"

	wikidomain "wiki/internal/wiki"
)

// Task is a unit of background work.
type Task func(context.Context) error

// Service is the ingest queue surface consumed by the single worker.
type Service interface {
	ProcessNext(ctx context.Context) (bool, error)
	WaitForWork(ctx context.Context) error
}

type bootSweeper interface {
	RequeueWorking(ctx context.Context) (int, error)
}

type inboxProcessor interface {
	ProcessInbox(context.Context) wikidomain.Drain
}

type expiredReaper interface {
	ReapExpired(context.Context, time.Time) (int, error)
}

type embeddingCatchUpService interface {
	DrainEmbeddingCatchUp(context.Context) (int, error)
}

// Run processes pending ingest jobs with one worker loop.
func Run(ctx context.Context, services ...Service) error {
	if len(services) == 0 || services[0] == nil {
		<-ctx.Done()
		return nil
	}
	svc := services[0]
	if sweeper, ok := svc.(bootSweeper); ok {
		if _, err := sweeper.RequeueWorking(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
	if inbox, ok := svc.(inboxProcessor); ok {
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		errs := make(chan error, 2)
		go func() { errs <- runClaims(runCtx, svc) }()
		go func() { errs <- runInbox(runCtx, inbox) }()
		err := <-errs
		cancel()
		<-errs
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return runClaims(ctx, svc)
}

func runClaims(ctx context.Context, svc Service) error {
	for {
		processed, err := svc.ProcessNext(ctx)
		if err != nil {
			return err
		}
		if processed {
			continue
		}
		if err := svc.WaitForWork(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

func runInbox(ctx context.Context, svc inboxProcessor) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		svc.ProcessInbox(ctx)
		if reaper, ok := svc.(expiredReaper); ok {
			if _, err := reaper.ReapExpired(ctx, time.Now()); err != nil && ctx.Err() == nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// RunEmbeddingCatchUp periodically drains pages whose stored embedding is stale.
func RunEmbeddingCatchUp(ctx context.Context, svc embeddingCatchUpService) error {
	if svc == nil {
		<-ctx.Done()
		return nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if _, err := svc.DrainEmbeddingCatchUp(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
