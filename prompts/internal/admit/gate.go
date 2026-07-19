// Package admit bounds concurrent inference work.
package admit

import (
	"context"
	"sync"
)

// Gate maintains independent pools for provider calls and agent-session runs.
type Gate struct {
	callCap int
	runs    chan struct{}

	mu    sync.Mutex
	calls map[string]chan struct{}
}

// New constructs a gate with per-provider call capacity and global run
// capacity.
func New(callCap, runCap int) *Gate {
	if callCap < 1 {
		panic("admit: call capacity must be positive")
	}
	if runCap < 1 {
		panic("admit: run capacity must be positive")
	}
	return &Gate{
		callCap: callCap,
		runs:    make(chan struct{}, runCap),
		calls:   make(map[string]chan struct{}),
	}
}

// AcquireCall waits for capacity in provider's call pool. Each provider has a
// distinct pool, created on first use.
func (g *Gate) AcquireCall(ctx context.Context, provider string) (func(), error) {
	g.mu.Lock()
	sem := g.calls[provider]
	if sem == nil {
		sem = make(chan struct{}, g.callCap)
		g.calls[provider] = sem
	}
	g.mu.Unlock()
	return acquire(ctx, sem)
}

// AcquireRun waits for capacity in the global agent-session run pool.
func (g *Gate) AcquireRun(ctx context.Context) (func(), error) {
	return acquire(ctx, g.runs)
}

func acquire(ctx context.Context, sem chan struct{}) (func(), error) {
	select {
	case sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-sem }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
