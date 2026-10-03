// Package limits bounds operations and coordinates repository holds.
package limits

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/repos/internal/settings"
)

// Clock supplies operation timing. Nil functions use the standard clock.
type Clock struct {
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time
}

// Op names the operation requesting a slot.
type Op string

// Operation names select the independent read and write lanes.
const (
	Fetch       Op = "fetch"
	Push        Op = "push"
	Maintenance Op = "maintenance"
)

var (
	// ErrQueueFull reports a queue with no room for another waiter.
	ErrQueueFull = errors.New("operation queue is full")
	// ErrQueueTimeout reports an expired queue wait.
	ErrQueueTimeout = errors.New("operation queue wait expired")
	// ErrDraining reports a stop refusing new or waiting operations.
	ErrDraining = errors.New("repos is draining")
)

// Usage is a snapshot of one kind of operations.
type Usage struct{ Slots, Active, Queued int64 }

// Pressure holds the independent read and write usage snapshots.
type Pressure struct{ Read, Write Usage }

type lane struct {
	slots, active int64
	queue         []*waiter
}
type repository struct {
	active       int64
	locked, held bool
}
type waiter struct {
	ctx     context.Context
	repo    string
	lock    bool
	start   time.Time
	arbiter *timerArbiter
	arming  bool
	done    chan struct{}
	grant   *Grant
	err     error
}

// Limits coordinates slots, queues, repository locks, and holds.
type Limits struct {
	commands    chan func()
	wake        chan struct{}
	settings    settings.Settings
	clock       Clock
	read, write lane
	repos       map[string]*repository
	draining    bool
	handoffs    []chan struct{}
}

// New creates limits using the supplied settings and timing functions.
func New(s settings.Settings, c Clock) *Limits {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.After == nil {
		c.After = time.After
	}
	l := &Limits{settings: s, clock: c, read: lane{slots: s.ReadSlots}, write: lane{slots: s.WriteSlots}, repos: make(map[string]*repository), commands: make(chan func()), wake: make(chan struct{}, 1)}
	go l.dispatch()
	return l
}

// Settings returns the settings supplied to New.
func (l *Limits) Settings() settings.Settings { return l.settings }

// Clock returns the resolved timing functions supplied to New.
func (l *Limits) Clock() Clock { return l.clock }
func (l *Limits) kind(op Op) *lane {
	if op == Fetch {
		return &l.read
	}
	return &l.write
}
func (l *Limits) available(k *lane, repo string, lock bool) bool {
	r := l.repos[repo]
	return k.active < k.slots && (r == nil || (!r.held && (!lock || !r.locked)))
}
func (l *Limits) record(repo string) *repository {
	r := l.repos[repo]
	if r == nil {
		r = &repository{}
		l.repos[repo] = r
	}
	return r
}
func (l *Limits) grant(k *lane, repo string, lock bool, waited time.Duration) *Grant {
	k.active++
	r := l.record(repo)
	r.active++
	if lock {
		r.locked = true
	}
	return &Grant{limits: l, lane: k, repo: repo, lock: lock, waited: waited}
}

// invoke serializes state changes. Timer arbiters make each grant decision
// within the same loop that receives that waiter's timer and cancellation.
func (l *Limits) invoke(fn func()) {
	done := make(chan struct{})
	l.commands <- func() { fn(); close(done) }
	<-done
}
func (l *Limits) signal() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}
func (l *Limits) dispatch() {
	for {
		select {
		case command := <-l.commands:
			command()
		case <-l.wake:
			l.reap()
			l.assign()
		}
	}
}

type arbitration struct {
	grant func() *Grant
	reply chan arbitrationResult
}
type arbitrationResult struct {
	grant *Grant
	err   error
}
type timerArbiter struct{ requests chan arbitration }

func (a *timerArbiter) decide(grant func() *Grant) arbitrationResult {
	if a.requests == nil {
		if grant != nil {
			return arbitrationResult{grant: grant()}
		}
		return arbitrationResult{}
	}
	reply := make(chan arbitrationResult)
	a.requests <- arbitration{grant: grant, reply: reply}
	return <-reply
}
func (l *Limits) arm(w *waiter, timer <-chan time.Time) *timerArbiter {
	a := &timerArbiter{}
	// With no timer or cancellation channel there is nothing to receive; no
	// goroutine or select cases are needed for this valid injected clock.
	if timer == nil && w.ctx.Done() == nil {
		return a
	}
	a.requests = make(chan arbitration)
	go func() {
		var terminal error
		ctxDone := w.ctx.Done()
		for {
			select {
			case <-timer:
				terminal = ErrQueueTimeout
				timer = nil
				l.signal()
			case <-ctxDone:
				terminal = context.Cause(w.ctx)
				ctxDone = nil
				l.signal()
			case <-w.done:
				return
			case request := <-a.requests:
				if terminal == nil {
					select {
					case <-ctxDone:
						terminal = context.Cause(w.ctx)
						ctxDone = nil
					default:
					}
					if terminal == nil {
						select {
						case <-timer:
							terminal = ErrQueueTimeout
							timer = nil
						default:
						}
					}
				}
				result := arbitrationResult{err: terminal}
				if terminal == nil && request.grant != nil {
					result.grant = request.grant()
				}
				request.reply <- result
				if result.grant != nil {
					return
				}
			}
		}
	}()
	return a
}

// Acquire obtains a slot and an optional repository lock, or waits for them.
func (l *Limits) Acquire(ctx context.Context, repo string, op Op, lock bool) (*Grant, error) {
	var g *Grant
	var err error
	var w *waiter
	l.invoke(func() {
		if l.draining {
			err = ErrDraining
			return
		}
		k := l.kind(op)
		eligible := false
		if l.available(k, repo, lock) {
			for _, q := range k.queue {
				if l.available(k, q.repo, q.lock) {
					eligible = true
					break
				}
			}
		}
		if !eligible && l.available(k, repo, lock) {
			g = l.grant(k, repo, lock, 0)
			return
		}
		if int64(len(k.queue)) >= l.settings.QueueLength {
			err = ErrQueueFull
			return
		}
		w = &waiter{ctx: ctx, repo: repo, lock: lock, start: l.clock.Now(), done: make(chan struct{}), arming: true}
		k.queue = append(k.queue, w)
	})
	if w == nil {
		return g, err
	}
	timer := l.clock.After(seconds(l.settings.QueueSeconds))
	// Publish the arbiter inside the dispatcher before it can consume a wake.
	// A pre-fired timer may notify as arm starts its goroutine, but dispatch
	// cannot process that notification until this registration has completed.
	l.invoke(func() { w.arbiter = l.arm(w, timer); w.arming = false; l.assign() })
	<-w.done
	return w.grant, w.err
}

func (l *Limits) remove(k *lane, i int) {
	copy(k.queue[i:], k.queue[i+1:])
	k.queue[len(k.queue)-1] = nil
	k.queue = k.queue[:len(k.queue)-1]
}
func waitingError(w *waiter) error {
	if w.arming {
		select {
		case <-w.ctx.Done():
			return context.Cause(w.ctx)
		default:
			return nil
		}
	}
	return w.arbiter.decide(nil).err
}

// reap handles timer notifications in one pass, without a queue-sized select.
func (l *Limits) reap() {
	for _, k := range []*lane{&l.read, &l.write} {
		kept := k.queue[:0]
		for _, w := range k.queue {
			if err := waitingError(w); err != nil {
				w.err = err
				close(w.done)
			} else {
				kept = append(kept, w)
			}
		}
		for i := len(kept); i < len(k.queue); i++ {
			k.queue[i] = nil
		}
		k.queue = kept
	}
}

// handoff lets the dispatcher keep answering snapshots while a pending
// After call publishes its timer. A release is acknowledged only after all
// eligible waiters have their timer checked and their slot assigned.
func (l *Limits) handoff(fn func()) {
	done := make(chan struct{})
	l.commands <- func() {
		fn()
		if l.assign() {
			l.handoffs = append(l.handoffs, done)
		} else {
			close(done)
		}
	}
	<-done
}
func (l *Limits) assign() bool {
	pending := false
	if !l.draining {
		for _, k := range []*lane{&l.read, &l.write} {
			for i := 0; k.active < k.slots && i < len(k.queue); {
				w := k.queue[i]
				if !l.available(k, w.repo, w.lock) {
					i++
					continue
				}
				if w.arming {
					pending = true
					break
				}
				result := w.arbiter.decide(func() *Grant {
					waited := l.clock.Now().Sub(w.start)
					if waited < 0 {
						waited = 0
					}
					return l.grant(k, w.repo, w.lock, waited)
				})
				l.remove(k, i)
				w.grant, w.err = result.grant, result.err
				close(w.done)
			}
		}
	}
	if !pending {
		for _, done := range l.handoffs {
			close(done)
		}
		l.handoffs = nil
	}
	return pending
}

// Pressure returns current counts without waiting for operation resources.
func (l *Limits) Pressure() Pressure {
	var p Pressure
	l.invoke(func() {
		p = Pressure{Read: Usage{l.read.slots, l.read.active, int64(len(l.read.queue))}, Write: Usage{l.write.slots, l.write.active, int64(len(l.write.queue))}}
	})
	return p
}

// Draining reports whether Drain has begun refusing operations.
func (l *Limits) Draining() bool {
	var draining bool
	l.invoke(func() { draining = l.draining })
	return draining
}

// Drain refuses waiters and future calls while preserving active grants.
func (l *Limits) Drain() {
	l.invoke(func() {
		if l.draining {
			return
		}
		l.draining = true
		for _, k := range []*lane{&l.read, &l.write} {
			for _, w := range k.queue {
				w.err = ErrDraining
				close(w.done)
			}
			k.queue = nil
		}
		l.assign()
	})
}

// Busy reports an active grant or hold on the repository.
func (l *Limits) Busy(repo string) bool {
	var busy bool
	l.invoke(func() { r := l.repos[repo]; busy = r != nil && (r.active > 0 || r.held) })
	return busy
}

// TryHold excludes all operations on an idle repository until release.
func (l *Limits) TryHold(repo string) (func(), bool) {
	release := func() {}
	var ok bool
	l.invoke(func() {
		r := l.record(repo)
		if r.active > 0 || r.held {
			return
		}
		r.held = true
		ok = true
		var once sync.Once
		release = func() { once.Do(func() { l.handoff(func() { r.held = false }) }) }
	})
	return release, ok
}

// Grant owns an operation slot and, when requested, its repository lock.
type Grant struct {
	limits   *Limits
	lane     *lane
	repo     string
	lock     bool
	waited   time.Duration
	release  sync.Once
	deadline sync.Once
	timer    <-chan time.Time
}

// Waited returns the fixed, nonnegative time spent waiting for this grant.
func (g *Grant) Waited() time.Duration { return g.waited }

// Deadline arms and returns the operation timer exactly once.
func (g *Grant) Deadline() <-chan time.Time {
	g.deadline.Do(func() { g.timer = g.limits.clock.After(seconds(g.limits.settings.OperationSeconds)) })
	return g.timer
}

// Release frees this grant and assigns resources to eligible waiters once.
func (g *Grant) Release() {
	g.release.Do(func() {
		l := g.limits
		l.handoff(func() {
			g.lane.active--
			r := l.repos[g.repo]
			r.active--
			if g.lock {
				r.locked = false
			}
		})
	})
}
func seconds(n int64) time.Duration {
	if n > math.MaxInt64/int64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(n) * time.Second
}
