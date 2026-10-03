// Package maintenance schedules git's collection through the shared write limits.
package maintenance

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

// Config supplies the catalog, host git, shared limits and optional test hold.
type Config struct {
	Store     *store.Store
	Git       *git.Git
	Limits    *limits.Limits
	Telemetry *telemetry.Writer
	Hold      func(context.Context, string)
}

// Scheduler owns a sequential maintenance schedule.
type Scheduler struct {
	stop       chan struct{}
	done       chan struct{}
	waitCancel context.CancelFunc
	runCancel  context.CancelFunc
	once       sync.Once
}

// Start arms the first interval before returning, without running a cycle.
func Start(cfg Config) *Scheduler {
	waitCtx, waitCancel := context.WithCancel(context.Background())
	runCtx, runCancel := context.WithCancel(context.Background())
	s := &Scheduler{stop: make(chan struct{}), done: make(chan struct{}), waitCancel: waitCancel, runCancel: runCancel}
	interval := time.Duration(math.MaxInt64)
	if hours := cfg.Limits.Settings().MaintenanceHours; hours <= math.MaxInt64/int64(time.Hour) {
		interval = time.Duration(hours) * time.Hour
	}
	next := cfg.Limits.Clock().After(interval)
	go func() {
		defer close(s.done)
		defer waitCancel()
		defer runCancel()
		for {
			select {
			case <-s.stop:
				return
			case <-next:
			}
			if waitCtx.Err() != nil {
				return
			}
			next = cfg.Limits.Clock().After(interval)
			cycle(waitCtx, runCtx, cfg)
			if waitCtx.Err() != nil {
				return
			}
		}
	}()
	return s
}

// Stop ends new work immediately and lets active work finish until ctx ends.
func (s *Scheduler) Stop(ctx context.Context) {
	first := false
	s.once.Do(func() { first = true; close(s.stop); s.waitCancel() })
	if !first {
		return
	}
	select {
	case <-s.done:
		return
	case <-ctx.Done():
		s.runCancel()
	}
	<-s.done
}

// Cycle runs one sequential cycle with ctx ending both waiting and active work.
func Cycle(ctx context.Context, cfg Config) { cycle(ctx, ctx, cfg) }

func stopped(ctx context.Context, cfg Config) bool {
	return ctx.Err() != nil || cfg.Limits.Draining()
}

func cycle(waitCtx, runCtx context.Context, cfg Config) {
	repos, err := cfg.Store.All(waitCtx)
	if err != nil {
		return
	}
	for _, repo := range repos {
		if stopped(waitCtx, cfg) {
			return
		}
		if !repo.Available {
			continue
		}
		if !maintain(waitCtx, runCtx, cfg, repo) {
			return
		}
	}
}

func operation(cfg Config, name, id, limit string, waited time.Duration) {
	attrs := telemetry.Attrs{"repo": id, "operation": "maintenance"}
	if name == "operation.waited" {
		attrs["wait_us"] = int64(waited / time.Microsecond)
	} else {
		attrs["limit"] = limit
	}
	cfg.Telemetry.Emit(context.Background(), name, attrs)
}

func maintain(waitCtx, runCtx context.Context, cfg Config, repo store.Repo) bool {
	grant, err := cfg.Limits.Acquire(waitCtx, repo.ID, limits.Maintenance, true)
	if err != nil {
		limit := "draining"
		switch {
		case errors.Is(err, limits.ErrQueueFull):
			limit = "queue_length"
		case errors.Is(err, limits.ErrQueueTimeout):
			limit = "queue_seconds"
		}
		operation(cfg, "operation.rejected", repo.ID, limit, 0)
		return limit != "draining"
	}
	defer grant.Release()
	repo, err = cfg.Store.Find(runCtx, repo.Owner, repo.ID)
	if err != nil {
		return true
	}
	if grant.Waited() > 0 {
		operation(cfg, "operation.waited", repo.ID, "", grant.Waited())
	}
	if cfg.Hold != nil {
		cfg.Hold(runCtx, repo.ID)
	}
	if runCtx.Err() != nil {
		return false
	}
	before, err := cfg.Store.Size(runCtx, repo.ID)
	if err != nil {
		return true
	}
	deadline := grant.Deadline()
	if runCtx.Err() != nil {
		return false
	}
	select {
	case <-deadline:
		operation(cfg, "operation.timed_out", repo.ID, "operation_seconds", 0)
		return true
	default:
	}
	ctx, cancel := context.WithCancel(runCtx)
	defer cancel()
	cmd := cfg.Git.Command(ctx, cfg.Store.Dir(repo.ID), nil, "gc")
	start := cfg.Limits.Clock().Now()
	if err = cmd.Start(); err != nil {
		return true
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timedOut := false
	select {
	case err = <-done:
	case <-runCtx.Done():
		cancel()
		<-done
		return false
	case <-deadline:
		timedOut = true
		cancel()
		<-done
	}
	end := cfg.Limits.Clock().Now()
	if runCtx.Err() != nil {
		return false
	}
	if timedOut {
		operation(cfg, "operation.timed_out", repo.ID, "operation_seconds", 0)
		return true
	}
	if err != nil {
		return true
	}
	after, err := cfg.Store.Size(runCtx, repo.ID)
	if err != nil {
		return true
	}
	if runCtx.Err() != nil {
		return false
	}
	cfg.Telemetry.Emit(context.Background(), "maintenance.finished", telemetry.Attrs{
		"repo": repo.ID, "duration_us": int64(end.Sub(start) / time.Microsecond), "size_before": before, "size_after": after,
	})
	return true
}
