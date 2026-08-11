package ask

import (
	"container/list"
	"context"
	"crypto/sha256"
	"strings"
	"sync"
	"time"
)

// Provider is the computation the cache fronts.
type Provider interface {
	Ask(ctx context.Context, scope, owner, question string) (Answer, error)
}

// ComputeTimeout bounds one detached miss computation.
const ComputeTimeout = 2 * time.Minute

// Cache fronts a Provider with a bounded response cache.
type Cache struct {
	mu      sync.Mutex
	next    Provider
	cap     int
	entries map[[sha256.Size]byte]*cacheEntry
	lru     list.List
}

type cacheEntry struct {
	scope       string
	answer      Answer
	err         error
	ready       chan struct{}
	lruElement  *list.Element
	invalidated bool
}

// NewCache fronts next with a response cache of at most cap answers.
func NewCache(next Provider, cap int) *Cache {
	if cap < 0 {
		panic("ask: negative cache capacity")
	}
	return &Cache{
		next:    next,
		cap:     cap,
		entries: make(map[[sha256.Size]byte]*cacheEntry),
	}
}

// Ask returns a cached answer or shares one detached computation on a miss.
func (c *Cache) Ask(ctx context.Context, scope, owner, question string) (Answer, error) {
	if c.cap == 0 {
		return c.next.Ask(ctx, scope, owner, question)
	}

	key := cacheKey(scope, question)
	c.mu.Lock()
	if entry, ok := c.entries[key]; ok {
		if entry.lruElement != nil {
			c.lru.MoveToFront(entry.lruElement)
			answer, err := entry.answer, entry.err
			c.mu.Unlock()
			return answer, err
		}
		c.mu.Unlock()
		return waitForEntry(ctx, entry)
	}

	entry := &cacheEntry{scope: scope, ready: make(chan struct{})}
	c.entries[key] = entry
	c.mu.Unlock()

	computeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ComputeTimeout)
	go func() {
		defer cancel()
		answer, err := c.next.Ask(computeCtx, scope, owner, question)
		c.finish(key, entry, answer, err)
	}()

	return waitForEntry(ctx, entry)
}

func (c *Cache) finish(key [sha256.Size]byte, entry *cacheEntry, answer Answer, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry.answer = answer
	entry.err = err
	if current, ok := c.entries[key]; ok && current == entry {
		if err != nil || entry.invalidated {
			delete(c.entries, key)
		} else {
			entry.lruElement = c.lru.PushFront(key)
			if c.lru.Len() > c.cap {
				oldest := c.lru.Back()
				delete(c.entries, oldest.Value.([sha256.Size]byte))
				c.lru.Remove(oldest)
			}
		}
	}
	close(entry.ready)
}

func waitForEntry(ctx context.Context, entry *cacheEntry) (Answer, error) {
	select {
	case <-entry.ready:
		return entry.answer, entry.err
	case <-ctx.Done():
		return Answer{}, ctx.Err()
	}
}

// Invalidate drops every stored and in-flight entry for scope.
func (c *Cache) Invalidate(scope string) {
	if c.cap == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if entry.scope != scope {
			continue
		}
		entry.invalidated = true
		delete(c.entries, key)
		if entry.lruElement != nil {
			c.lru.Remove(entry.lruElement)
		}
	}
}

func cacheKey(scope, question string) [sha256.Size]byte {
	return sha256.Sum256([]byte(scope + "\x00" + strings.TrimSpace(question)))
}
