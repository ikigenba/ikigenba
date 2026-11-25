// Package join serves the guest enrollment page.
package join

import (
	"container/list"
	"sync"
	"time"
)

// Enrollment limits and route prefix.
const (
	Prefix       = "/join/"
	Burst        = 10
	Every        = 6 * time.Second
	MaxAddresses = 4096
	MaxBodyBytes = 4096
)

// Limiter holds one token bucket per address in order of most recent use.
type Limiter struct {
	mu     sync.Mutex
	now    func() time.Time
	recent list.List
	byKey  map[string]*list.Element
}

type bucket struct {
	key   string
	stamp time.Time
	// Credit is measured in time, with Every equivalent to one token.
	credit time.Duration
}

// NewLimiter uses now to measure the time available to refill buckets.
func NewLimiter(now func() time.Time) *Limiter {
	return &Limiter{now: now, byKey: make(map[string]*list.Element)}
}

// Allow takes one token or returns the time until the next one is available.
func (l *Limiter) Allow(addr string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	elem := l.byKey[addr]
	if elem == nil {
		if len(l.byKey) == MaxAddresses {
			oldest := l.recent.Back()
			delete(l.byKey, oldest.Value.(*bucket).key)
			l.recent.Remove(oldest)
		}
		elem = l.recent.PushFront(&bucket{key: addr, stamp: now, credit: Burst * Every})
		l.byKey[addr] = elem
	} else {
		l.recent.MoveToFront(elem)
	}
	b := elem.Value.(*bucket)
	if now.After(b.stamp) {
		b.credit += min(Burst*Every-b.credit, now.Sub(b.stamp))
		b.stamp = now
	}
	if b.credit < Every {
		return false, Every - b.credit
	}
	b.credit -= Every
	return true, 0
}
