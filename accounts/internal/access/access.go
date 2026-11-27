package access

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
)

const (
	// ReissueAfter is both the maximum age for reuse and the minimum time left.
	ReissueAfter = 15 * time.Minute
	// IssueBudget limits the entire credential issue, including caller identity.
	IssueBudget = 8 * time.Second
	// IssueParallel caps concurrent AssumeRole calls within one issue.
	IssueParallel = 8
)

var (
	errInvalidIdentity = errors.New("STS identity ARN has no partition")
	errNoCredentials   = errors.New("STS answered no credentials")
)

// Config supplies the backing STS, backup settings, and cache clock.
type Config struct {
	STS   cloud.STS
	Hosts settings.Hosts
	Now   func() time.Time
}

// issueError preserves the cause for errors.Is without rendering it. Even
// a local diagnostic could happen to match a credential issued earlier.
type issueError struct{ cause error }

func (e issueError) Error() string { return "" }
func (e issueError) Unwrap() error { return e.cause }

type identity struct {
	partition, account string
}

type cacheKey struct {
	host, account, prefix string
}

type cacheEntry struct {
	credentials  protocol.Credentials
	issued       time.Time
	lastAsked    time.Time
	activeIssues int
	succeeded    bool
}

// Access issues and caches credentials using the configured STS.
type Access struct {
	cfg     Config
	mu      sync.Mutex
	id      identity
	entries map[cacheKey]*cacheEntry
}

// New constructs an issuer without consulting the clock or STS.
func New(cfg Config) *Access {
	return &Access{cfg: cfg, entries: make(map[cacheKey]*cacheEntry)}
}

// Cached returns the number of successful entries still held.
func (a *Access) Cached() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, entry := range a.entries {
		if entry.succeeded {
			count++
		}
	}
	return count
}

type request struct {
	key      cacheKey
	entry    *cacheEntry
	result   protocol.Credentials
	fallback protocol.Credentials
	refresh  bool
	err      error
}

// Issue returns the host and account credential sets available within one budget.
func (a *Access) Issue(ctx context.Context, host, hostPrefix string, accounts []string) (protocol.Credentials, []protocol.AccountCredentials, error) {
	ctx, cancel := context.WithTimeout(ctx, IssueBudget)
	defer cancel()
	now := a.cfg.Now()
	slugs := slices.Clone(accounts)
	slices.Sort(slugs)
	slugs = slices.Compact(slugs)
	if len(slugs) > 0 && slugs[0] == "" {
		slugs = slugs[1:]
	}

	results := make([]protocol.AccountCredentials, len(slugs))
	requests := make([]request, len(slugs)+1)
	requests[0].key = cacheKey{host: host, prefix: hostPrefix}
	for i, slug := range slugs {
		results[i].Account = slug
		requests[i+1].key = cacheKey{host: host, account: slug}
	}
	active := requests
	if hostPrefix == "" {
		active = requests[1:]
	}

	id := a.prepare(now, active)
	if len(active) == 0 {
		return protocol.Credentials{}, results, nil
	}
	defer a.release(active)
	if id.partition == "" {
		var err error
		id, err = a.identify(ctx)
		if err != nil {
			return protocol.Credentials{}, results, issueError{cause: err}
		}
	}

	a.refresh(ctx, id, now, active)
	var err error
	for i := range active {
		item := &active[i]
		if item.result != (protocol.Credentials{}) {
			continue
		}
		if item.err != nil {
			err = item.err
		} else if err == nil {
			err = errNoCredentials
		}
	}
	for i := range results {
		results[i].Credentials = requests[i+1].result
	}
	if err != nil {
		err = issueError{cause: err}
	}
	return requests[0].result, results, err
}

func (a *Access) prepare(now time.Time, requests []request) identity {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range requests {
		item := &requests[i]
		entry := a.entries[item.key]
		if entry == nil {
			entry = &cacheEntry{lastAsked: now}
			a.entries[item.key] = entry
		}
		if now.After(entry.lastAsked) {
			entry.lastAsked = now
		}
		entry.activeIssues++
		item.entry = entry

		if entry.succeeded && now.Sub(entry.issued) < ReissueAfter && entry.credentials.Expires.After(now.Add(ReissueAfter)) {
			item.result = entry.credentials
			continue
		}
		item.refresh = true
		if entry.succeeded && entry.credentials.Expires.After(now) {
			item.fallback = entry.credentials
		}
	}
	unaskedSince := now.Add(-time.Hour)
	for key, entry := range a.entries {
		if !entry.lastAsked.After(unaskedSince) {
			delete(a.entries, key)
		}
	}
	return a.id
}

func (a *Access) release(requests []request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range requests {
		item := &requests[i]
		item.entry.activeIssues--
		if a.entries[item.key] == item.entry && item.entry.activeIssues == 0 && !item.entry.succeeded {
			delete(a.entries, item.key)
		}
	}
}

type identityAnswer struct {
	id  identity
	err error
}

func (a *Access) identify(ctx context.Context) (identity, error) {
	answer := make(chan identityAnswer, 1)
	go func() {
		caller, err := a.cfg.STS.Identity(ctx)
		if err != nil {
			answer <- identityAnswer{err: err}
			return
		}
		prefixed, arn := strings.CutPrefix(caller.Arn, "arn:")
		partition, _, found := strings.Cut(prefixed, ":")
		if !arn || !found || partition == "" {
			answer <- identityAnswer{err: errInvalidIdentity}
			return
		}
		id := identity{partition: partition, account: caller.Account}
		a.mu.Lock()
		if a.id.partition == "" {
			a.id = id
		}
		a.mu.Unlock()
		answer <- identityAnswer{id: id}
	}()
	select {
	case received := <-answer:
		return received.id, received.err
	case <-ctx.Done():
		return identity{}, ctx.Err()
	}
}

type roleAnswer struct {
	index       int
	credentials cloud.Credentials
	err         error
}

func (a *Access) refresh(ctx context.Context, id identity, now time.Time, requests []request) {
	due := make([]int, 0, len(requests))
	for i := range requests {
		if requests[i].refresh {
			due = append(due, i)
		}
	}
	results := make(chan roleAnswer, IssueParallel)
	inFlight := 0
	for len(due) > 0 || inFlight > 0 {
		for inFlight < IssueParallel && len(due) > 0 && ctx.Err() == nil {
			index := due[0]
			due = due[1:]
			inFlight++
			go a.assume(ctx, id, index, requests[index].key, results)
		}
		select {
		case received := <-results:
			a.record(now, &requests[received.index], received)
			inFlight--
		case <-ctx.Done():
			a.finishCanceled(now, requests, results, ctx.Err())
			return
		}
	}
}

// Preserve answers that arrived before cancellation alongside unfinished entries.
func (a *Access) finishCanceled(now time.Time, requests []request, results <-chan roleAnswer, err error) {
	for {
		select {
		case received := <-results:
			a.record(now, &requests[received.index], received)
		default:
			for i := range requests {
				item := &requests[i]
				if item.refresh {
					item.result, item.err = item.fallback, err
				}
			}
			return
		}
	}
}

func (a *Access) assume(ctx context.Context, id identity, index int, key cacheKey, answers chan<- roleAnswer) {
	prefix := key.prefix
	if key.account != "" {
		prefix = protocol.AccountPrefix(key.account)
	}
	credentials, err := a.cfg.STS.AssumeRole(ctx, cloud.RoleRequest{
		RoleARN:     RoleARN(id.partition, id.account, a.cfg.Hosts),
		SessionName: SessionName(key.host, key.account),
		Policy:      Policy(a.cfg.Hosts.BackupBucket(), id.partition, prefix),
		Duration:    cloud.RoleSessionDuration,
	})
	if err == nil {
		err = ctx.Err()
	}
	answers <- roleAnswer{index: index, credentials: credentials, err: err}
}

func (a *Access) record(now time.Time, item *request, answer roleAnswer) {
	item.refresh = false
	if answer.err != nil {
		item.result, item.err = item.fallback, answer.err
		return
	}
	item.result = protocol.Credentials(answer.credentials)
	a.mu.Lock()
	defer a.mu.Unlock()
	entry := item.entry
	if a.entries[item.key] == entry && (!entry.succeeded || !now.Before(entry.issued)) {
		entry.credentials, entry.issued, entry.succeeded = item.result, now, true
	}
}
