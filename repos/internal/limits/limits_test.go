package limits_test

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
)

type alarm struct {
	duration         time.Duration
	fire             chan time.Time
	pressure         limits.Pressure
	nowCalls         int
	observerNowCalls int
}
type clock struct {
	mu     sync.Mutex
	now    time.Time
	calls  int
	alarms chan alarm
	l      *limits.Limits
}

func newClock(s settings.Settings) (*limits.Limits, *clock) {
	c := &clock{now: time.Unix(100, 0), alarms: make(chan alarm, 100)}
	c.l = limits.New(s, limits.Clock{Now: c.read, After: c.after})
	return c.l, c
}
func (c *clock) read() time.Time { c.mu.Lock(); defer c.mu.Unlock(); c.calls++; return c.now }
func (c *clock) set(n time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.now = n }
func (c *clock) count() int      { c.mu.Lock(); defer c.mu.Unlock(); return c.calls }
func (c *clock) after(d time.Duration) <-chan time.Time {
	before := c.count()
	pressure := c.l.Pressure()
	a := alarm{duration: d, fire: make(chan time.Time), pressure: pressure, nowCalls: before, observerNowCalls: c.count() - before}
	c.alarms <- a
	return a.fire
}
func config() settings.Settings {
	s := settings.Defaults()
	s.ReadSlots = 1
	s.WriteSlots = 1
	s.QueueLength = 1
	return s
}

type outcome struct {
	g   *limits.Grant
	err error
}

func acquire(ctx context.Context, l *limits.Limits, repo string, op limits.Op, lock bool) <-chan outcome {
	ch := make(chan outcome, 1)
	go func() { g, e := l.Acquire(ctx, repo, op, lock); ch <- outcome{g, e} }()
	return ch
}
func grant(t *testing.T, l *limits.Limits, repo string, op limits.Op, lock bool) *limits.Grant {
	t.Helper()
	g, e := l.Acquire(t.Context(), repo, op, lock)
	if e != nil || g == nil {
		t.Fatalf("Acquire: %v %v", g, e)
	}
	return g
}
func success(t *testing.T, ch <-chan outcome) *limits.Grant {
	t.Helper()
	r := <-ch
	if r.err != nil || r.g == nil {
		t.Fatalf("wait: %v %v", r.g, r.err)
	}
	return r.g
}
func failure(t *testing.T, ch <-chan outcome, want error) {
	t.Helper()
	r := <-ch
	if r.g != nil || !errors.Is(r.err, want) {
		t.Fatalf("result: %v %v want %v", r.g, r.err, want)
	}
}
func noAlarm(t *testing.T, c *clock) {
	t.Helper()
	select {
	case a := <-c.alarms:
		t.Fatalf("unexpected After(%v)", a.duration)
	default:
	}
}

// R-7YDA-Y1DH R-7ZL7-BT46 R-3RDS-6HC8 R-80T3-PKUV
func TestSurfaceAndLargeCounts(t *testing.T) {
	var _ limits.Grant
	for i, a := range []error{limits.ErrQueueFull, limits.ErrQueueTimeout, limits.ErrDraining} {
		if a == nil {
			t.Fatal("nil sentinel")
		}
		for j, b := range []error{limits.ErrQueueFull, limits.ErrQueueTimeout, limits.ErrDraining} {
			if i != j && errors.Is(a, b) {
				t.Fatal("sentinels match")
			}
		}
	}
	for _, n := range []int64{1, 2, math.MaxInt64 / 2, math.MaxInt64} {
		s := config()
		s.ReadSlots = n
		s.WriteSlots = n
		s.QueueLength = n
		s.QueueSeconds = n
		s.OperationSeconds = n
		l, _ := newClock(s)
		want := limits.Pressure{Read: limits.Usage{Slots: n}, Write: limits.Usage{Slots: n}}
		if p := l.Pressure(); p != want {
			t.Fatalf("pressure %v", p)
		}
		if l.Draining() {
			t.Fatal("new limits draining")
		}
	}
}

// R-3GEO-QJNZ R-3HML-4BEO R-8FFW-ATR7 R-X8RR-8QHC R-JOTR-6LE9
func TestImmediateIndependentKindsAndLocks(t *testing.T) {
	s := config()
	s.WriteSlots = 3
	s.ReadSlots = 2
	l, c := newClock(s)
	gs := []*limits.Grant{}
	for _, op := range []limits.Op{limits.Push, limits.Fetch, limits.Maintenance, limits.Push} {
		before := c.count()
		g := grant(t, l, "a", op, len(gs) == 0)
		if calls := c.count() - before; calls > 1 {
			t.Fatalf("immediate Acquire called Now %d times", calls)
		}
		gs = append(gs, g)
	}
	if p := l.Pressure(); p.Read.Active != 1 || p.Write.Active != 3 || p.Read.Queued != 0 || p.Write.Queued != 0 {
		t.Fatalf("pressure %v", p)
	}
	for _, g := range gs {
		baseline := c.count()
		if g.Waited() != 0 {
			t.Fatal("immediate grant did not have zero wait")
		}
		if c.count() != baseline {
			t.Fatal("Waited read Now")
		}
		if !l.Busy("a") {
			t.Fatal("immediate grant not busy")
		}
	}
	noAlarm(t, c)
	// A write lock does not hold up reads, nor another repository's writes.
	baseline := c.count()
	gs[3].Release()
	if c.count() != baseline {
		t.Fatal("release without assignment read Now")
	}
	before := c.count()
	other := grant(t, l, "b", limits.Push, true)
	if calls := c.count() - before; calls > 1 {
		t.Fatalf("immediate Acquire called Now %d times", calls)
	}
	baseline = c.count()
	other.Release()
	for _, g := range gs {
		g.Release()
		g.Release()
	}
	if c.count() != baseline {
		t.Fatal("release read Now without assignment")
	}
	if l.Busy("a") || l.Busy("b") || l.Pressure().Read.Active != 0 || l.Pressure().Write.Active != 0 {
		t.Fatal("release leaked")
	}
}

// R-HS9H-ACRV R-3IUH-I35D R-89CE-DZ1Q R-JOTR-6LE9 R-8MRA-LG7D
func TestQueueSyncWaitDurationAndIndependentQueues(t *testing.T) {
	for _, delta := range []time.Duration{4 * time.Second, -time.Second} {
		t.Run(delta.String(), func(t *testing.T) {
			s := config()
			l, c := newClock(s)
			read := grant(t, l, "a", limits.Fetch, false)
			write := grant(t, l, "a", limits.Push, true)
			baseline := c.count()
			rc := acquire(t.Context(), l, "b", limits.Fetch, false)
			ra := <-c.alarms
			if ra.duration != time.Duration(s.QueueSeconds)*time.Second || ra.pressure.Read.Queued != 1 || ra.nowCalls != baseline+1 {
				t.Fatalf("queue sync: %v", ra)
			}
			baseline = c.count()
			wc := acquire(t.Context(), l, "c", limits.Maintenance, true)
			wa := <-c.alarms
			if wa.pressure.Write.Queued != 1 || wa.pressure.Read.Queued != 1 || wa.nowCalls != baseline+1 {
				t.Fatalf("independent queues: %v", wa)
			}
			before := l.Pressure()
			g, e := l.Acquire(t.Context(), "d", limits.Fetch, false)
			if g != nil || !errors.Is(e, limits.ErrQueueFull) || l.Pressure() != before {
				t.Fatal("queue full changed pressure")
			}
			noAlarm(t, c)
			c.set(time.Unix(100, 0).Add(delta))
			// The refused Acquire and snapshot calls may read Now. Start a fresh
			// baseline for each restricted method, after those unrestricted calls.
			baseline = c.count()
			read.Release()
			if c.count() != baseline+1 {
				t.Fatal("read handoff did not read Now exactly once")
			}
			if p := l.Pressure(); p.Read.Active != 1 || p.Read.Queued != 0 || p.Write.Queued != 1 {
				t.Fatalf("synchronous grant: %v", p)
			}
			rg := success(t, rc)
			want := delta
			if want < 0 {
				want = 0
			}
			baseline = c.count()
			firstWait, secondWait := rg.Waited(), rg.Waited()
			if firstWait != want || secondWait != want {
				t.Fatalf("Waited %v", firstWait)
			}
			if c.count() != baseline {
				t.Fatal("Waited read Now")
			}
			baseline = c.count()
			write.Release()
			if c.count() != baseline+1 {
				t.Fatal("write handoff did not read Now exactly once")
			}
			wg := success(t, wc)
			baseline = c.count()
			if wg.Waited() != want {
				t.Fatal("write wait")
			}
			rg.Release()
			wg.Release()
			if c.count() != baseline {
				t.Fatal("Waited or releases without assignment read Now")
			}

		})
	}
}

// R-X7JU-UYQN R-HTHD-O4IK R-8KBH-TWPZ R-8J3L-G4ZA
func TestHoldsGrantEveryEligibleWaiter(t *testing.T) {
	s := config()
	s.WriteSlots = 3
	s.ReadSlots = 3
	s.QueueLength = 4
	l, c := newClock(s)
	initial := l.Pressure()
	release, ok := l.TryHold("a")
	if !ok || release == nil || !l.Busy("a") || l.Pressure() != initial {
		t.Fatal("hold contract")
	}
	noop, ok := l.TryHold("a")
	if ok || noop == nil {
		t.Fatal("busy hold succeeded")
	}
	noop()
	if !l.Busy("a") {
		t.Fatal("failed hold changed busy")
	}
	// Every kind and lock flag is held; each owns only queue space.
	pending := []<-chan outcome{}
	for _, op := range []limits.Op{limits.Fetch, limits.Push, limits.Maintenance} {
		for _, locked := range []bool{false, true} {
			pending = append(pending, acquire(t.Context(), l, "a", op, locked))
			<-c.alarms
		}
	}
	if p := l.Pressure(); p.Read.Active != 0 || p.Write.Active != 0 || p.Read.Queued != 2 || p.Write.Queued != 4 {
		t.Fatalf("held queue %v", p)
	}
	// A blocked earlier waiter cannot delay an eligible later repository.
	b := grant(t, l, "b", limits.Push, true)
	b.Release()
	baseline := c.count()
	c.set(time.Unix(102, 0))
	release()
	if c.count() != baseline+4 {
		t.Fatalf("hold-release assignment Now calls %d", c.count()-baseline)
	}
	baseline = c.count()
	release()
	if c.count() != baseline {
		t.Fatal("duplicate hold release read Now")
	}
	p := l.Pressure()
	readWins := p.Read.Active == 2 && p.Write.Active == 2 && p.Read.Queued == 0 && p.Write.Queued == 2
	writeWins := p.Read.Active == 1 && p.Write.Active == 3 && p.Read.Queued == 1 && p.Write.Queued == 1
	if !readWins && !writeWins {
		t.Fatalf("hold handoff did not assign all eligible waiters: %v", p)
	}
	// Neither kind has priority over the other kind's repository lock. Drain
	// the returned grants in whichever order the compliant implementation
	// assigned them; their releases make every remaining waiter eligible.
	ready := make(chan outcome, len(pending))
	for _, ch := range pending {
		go func(ch <-chan outcome) { ready <- <-ch }(ch)
	}
	for range pending {
		r := <-ready
		if r.g == nil || r.err != nil {
			t.Fatalf("held waiter: %v %v", r.g, r.err)
		}
		r.g.Release()
	}
	if l.Busy("a") || l.Busy("b") {
		t.Fatal("released hold or grants stayed busy")
	}
}

// R-X7JU-UYQN
func TestEligibleFIFOAndSynchronousAssignment(t *testing.T) {
	s := config()
	s.QueueLength = 4
	l, c := newClock(s)
	first := grant(t, l, "occupied", limits.Push, true)
	hold, _ := l.TryHold("held")
	blocked := acquire(t.Context(), l, "held", limits.Push, true)
	<-c.alarms
	oldest := acquire(t.Context(), l, "older", limits.Push, true)
	<-c.alarms
	youngest := acquire(t.Context(), l, "younger", limits.Push, true)
	<-c.alarms
	first.Release()
	if !l.Busy("older") || l.Busy("younger") || l.Pressure().Write.Queued != 2 {
		t.Fatal("oldest eligible was not assigned inside release")
	}
	og := success(t, oldest)
	og.Release()
	if !l.Busy("younger") {
		t.Fatal("next eligible not assigned")
	}
	yg := success(t, youngest)
	yg.Release()
	if l.Busy("held") != true {
		t.Fatal("hold lost")
	}
	hold()
	bg := success(t, blocked)
	bg.Release()
}

// R-AEIH-37FK R-AFQD-GZ69 R-8E7Z-X20I R-8J3L-G4ZA
func TestWaitErrorsLeaveNothingHeld(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			l, c := newClock(config())
			g := grant(t, l, "a", limits.Fetch, false)
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			pending := acquire(ctx, l, "b", limits.Fetch, true)
			a := <-c.alarms
			if l.Busy("b") {
				t.Fatal("waiting operation busy")
			}
			cause := errors.New("caller left")
			want := limits.ErrQueueTimeout
			if mode == "timeout" {
				a.fire <- time.Unix(130, 0)
			} else {
				want = cause
				cancel(cause)
			}
			failure(t, pending, want)
			if l.Pressure().Read.Queued != 0 || l.Pressure().Read.Active != 1 || l.Busy("b") {
				t.Fatal("wait failure leaked")
			}
			release, ok := l.TryHold("b")
			if !ok {
				t.Fatal("wait failure left lock")
			}
			release()
			g.Release()
			grant(t, l, "b", limits.Fetch, true).Release()
		})
	}
}

// R-BAWA-CECU R-XB7K-09YQ
func TestDrainRefusesWaitersAndPreservesGrants(t *testing.T) {
	l, c := newClock(config())
	if l.Draining() {
		t.Fatal("premature draining")
	}
	g := grant(t, l, "a", limits.Fetch, false)
	w := acquire(t.Context(), l, "b", limits.Fetch, false)
	<-c.alarms
	l.Drain()
	l.Drain()
	if !l.Draining() {
		t.Fatal("draining not visible")
	}
	failure(t, w, limits.ErrDraining)
	if !l.Busy("a") || l.Pressure().Read.Active != 1 || l.Pressure().Read.Queued != 0 {
		t.Fatal("drain changed active grant")
	}
	for _, op := range []limits.Op{limits.Fetch, limits.Push, limits.Maintenance} {
		q, e := l.Acquire(t.Context(), "c", op, false)
		if q != nil || !errors.Is(e, limits.ErrDraining) {
			t.Fatal("late acquire accepted")
		}
	}
	noAlarm(t, c)
	g.Release()
	if l.Busy("a") {
		t.Fatal("grant failed to release during drain")
	}
	empty, _ := newClock(config())
	empty.Drain()
	if !empty.Draining() {
		t.Fatal("empty Drain invisible")
	}
}

// R-AEIH-37FK R-AFQD-GZ69 R-BAWA-CECU
func TestAssignmentWinsLaterRefusal(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel", "drain"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{})
			proceed := make(chan struct{})
			timer := make(chan time.Time, 1)
			l := limits.New(config(), limits.Clock{Now: func() time.Time { return time.Unix(100, 0) }, After: func(time.Duration) <-chan time.Time { close(entered); <-proceed; return timer }})
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			g := grant(t, l, "a", limits.Fetch, false)
			w := acquire(ctx, l, "b", limits.Fetch, false)
			<-entered
			close(proceed)
			g.Release()
			if !l.Busy("b") || l.Pressure().Read.Queued != 0 || l.Pressure().Read.Active != 1 {
				t.Fatal("not assigned synchronously")
			}
			switch mode {
			case "timeout":
				timer <- time.Unix(130, 0)
			case "cancel":
				cancel(errors.New("late cancellation"))
			case "drain":
				l.Drain()
			}
			success(t, w).Release()
		})
	}
}

// R-HS9H-ACRV R-8GNS-OLHW R-JOTR-6LE9
func TestSaturatedTimersAndOnceDeadline(t *testing.T) {
	for _, n := range []int64{1, math.MaxInt64 / int64(time.Second), math.MaxInt64/int64(time.Second) + 1, math.MaxInt64} {
		t.Run(string(rune(n%26+'a')), func(t *testing.T) {
			s := config()
			s.QueueSeconds = n
			s.OperationSeconds = n
			l, c := newClock(s)
			g := grant(t, l, "a", limits.Fetch, false)
			baseline := c.count()
			want := time.Duration(math.MaxInt64)
			if n <= math.MaxInt64/int64(time.Second) {
				want = time.Duration(n) * time.Second
			}
			ch := g.Deadline()
			a := <-c.alarms
			if a.nowCalls != baseline || c.count() != baseline+a.observerNowCalls {
				t.Fatal("Deadline read Now")
			}
			if ch != a.fire || a.duration != want || a.pressure.Read.Active != 1 || !l.Busy("a") {
				t.Fatal("deadline arming")
			}
			baseline = c.count()
			if g.Deadline() != ch {
				t.Fatal("deadline changed")
			}
			if c.count() != baseline {
				t.Fatal("repeat Deadline read Now")
			}
			noAlarm(t, c)
			baseline = c.count()
			w := acquire(t.Context(), l, "b", limits.Fetch, false)
			qa := <-c.alarms
			if qa.duration != want || qa.pressure.Read.Queued != 1 || qa.nowCalls != baseline+1 {
				t.Fatal("queue duration or sync")
			}
			qa.fire <- time.Unix(100, 0)
			failure(t, w, limits.ErrQueueTimeout)
			baseline = c.count()
			g.Release()
			if c.count() != baseline {
				t.Fatal("release without assignment read Now")
			}

		})
	}
}

// R-8P73-CZOR R-8MRA-LG7D
func TestConcurrentMethods(t *testing.T) {
	s := config()
	s.ReadSlots = 100
	s.WriteSlots = 100
	l, c := newClock(s)
	g := grant(t, l, "shared", limits.Fetch, false)
	hold, _ := l.TryHold("held")
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			_ = l.Settings()
			_ = l.Clock()
			_ = l.Pressure()
			_ = l.Busy("shared")
			_ = l.Draining()
			_ = g.Waited()
			_ = g.Deadline()
			noop, ok := l.TryHold("held")
			if ok {
				t.Error("duplicate hold")
			}
			noop()
			h, e := l.Acquire(t.Context(), "parallel", limits.Maintenance, false)
			if e != nil {
				t.Error(e)
			} else {
				h.Release()
				h.Release()
			}
		})
	}
	wg.Wait()
	a := <-c.alarms
	if a.duration != time.Duration(s.OperationSeconds)*time.Second {
		t.Fatal("deadline duration")
	}
	noAlarm(t, c)
	for range 16 {
		wg.Go(func() { g.Release(); hold(); l.Drain(); _ = l.Pressure(); _ = g.Deadline() })
	}
	wg.Wait()
	if l.Pressure().Read.Active != 0 || l.Pressure().Write.Active != 0 || l.Busy("shared") || l.Busy("held") {
		t.Fatal("concurrent release leaked")
	}
}

// R-AEIH-37FK R-8E7Z-X20I
func TestDeliveredQueueTimerPrecedesSlotRelease(t *testing.T) {
	// An unbuffered timer send establishes receipt before Release begins.
	// The channel remains open; the receiver and Release can then race, but
	// the expired waiter must never gain the slot.
	for range 32 {
		l, c := newClock(config())
		active := grant(t, l, "occupied", limits.Fetch, false)
		waiting := acquire(t.Context(), l, "expired", limits.Fetch, true)
		a := <-c.alarms
		a.fire <- time.Unix(130, 0)
		active.Release()
		failure(t, waiting, limits.ErrQueueTimeout)
		if p := l.Pressure(); p.Read.Active != 0 || p.Read.Queued != 0 {
			t.Fatalf("expired waiter acquired resources: %v", p)
		}
		release, ok := l.TryHold("expired")
		if !ok {
			t.Fatal("expired waiter left repository busy")
		}
		release()
	}
}

// R-AEIH-37FK R-X7JU-UYQN R-8E7Z-X20I
func TestBufferedQueueTimerBeforeRegistration(t *testing.T) {
	for range 16 {
		timer := make(chan time.Time, 1)
		afterEntered := make(chan struct{})
		returnTimer := make(chan struct{})
		var l *limits.Limits
		l = limits.New(config(), limits.Clock{
			Now: func() time.Time { return time.Unix(100, 0) },
			After: func(time.Duration) <-chan time.Time {
				// This observes the documented queue-before-After sync point while
				// withholding the channel from Acquire. A buffered delivery is now
				// possible before its channel can be registered with the limits.
				if l.Pressure().Read.Queued != 1 {
					t.Error("waiter not queued at After")
				}
				close(afterEntered)
				<-returnTimer
				return timer
			},
		})
		active := grant(t, l, "occupied", limits.Fetch, false)
		waiting := acquire(t.Context(), l, "expired", limits.Fetch, true)
		<-afterEntered
		timer <- time.Unix(130, 0)
		released := make(chan struct{})
		go func() { active.Release(); close(released) }()
		close(returnTimer)
		<-released
		failure(t, waiting, limits.ErrQueueTimeout)
		if p := l.Pressure(); p.Read.Active != 0 || p.Read.Queued != 0 || l.Busy("expired") {
			t.Fatal("buffered timeout gained resources")
		}
	}
}

// R-HS9H-ACRV R-80T3-PKUV R-BAWA-CECU
func TestQueueBeyondSelectCaseLimit(t *testing.T) {
	// QueueLength is a count, including when it exceeds the number of channel
	// cases a single runtime select can hold. This uses only injected timing.
	const waiters = 32768
	s := config()
	s.QueueLength = math.MaxInt64
	after := make(chan time.Duration, waiters)
	l := limits.New(s, limits.Clock{
		Now:   func() time.Time { return time.Unix(100, 0) },
		After: func(d time.Duration) <-chan time.Time { after <- d; return nil },
	})
	g := grant(t, l, "occupied", limits.Fetch, false)
	results := make(chan outcome, waiters)
	for range waiters {
		go func() {
			h, e := l.Acquire(context.Background(), "queued", limits.Fetch, false)
			results <- outcome{h, e}
		}()
	}
	for range waiters {
		if d := <-after; d != time.Duration(s.QueueSeconds)*time.Second {
			t.Fatalf("queue After(%v)", d)
		}
	}
	if p := l.Pressure(); p.Read.Active != 1 || p.Read.Queued != waiters {
		t.Fatalf("large queue %v", p)
	}
	l.Drain()
	for range waiters {
		r := <-results
		if r.g != nil || !errors.Is(r.err, limits.ErrDraining) {
			t.Fatalf("drained large queue: %v %v", r.g, r.err)
		}
	}
	g.Release()
	if p := l.Pressure(); p.Read.Active != 0 || p.Read.Queued != 0 {
		t.Fatalf("large queue leaked: %v", p)
	}
}

// R-AEIH-37FK R-8E7Z-X20I
func TestPrefiredQueueTimerClearsWithoutSlotRelease(t *testing.T) {
	// The open buffered channel is already ready when After returns. Its wake
	// can occur as the timer arbiter is published; the occupied slot stays held
	// until timeout and queue cleanup have both been observed.
	for range 64 {
		timer := make(chan time.Time, 1)
		timer <- time.Unix(130, 0)
		var l *limits.Limits
		l = limits.New(config(), limits.Clock{
			Now: func() time.Time { return time.Unix(100, 0) },
			After: func(time.Duration) <-chan time.Time {
				if p := l.Pressure(); p.Read.Queued != 1 || p.Read.Active != 1 {
					t.Errorf("queue-before-After snapshot %v", p)
				}
				return timer
			},
		})
		active := grant(t, l, "occupied", limits.Fetch, false)
		waiting := acquire(t.Context(), l, "expired", limits.Fetch, true)
		failure(t, waiting, limits.ErrQueueTimeout)
		if p := l.Pressure(); p.Read.Queued != 0 || p.Read.Active != 1 || !l.Busy("occupied") || l.Busy("expired") {
			t.Fatalf("prefired timer cleanup %v", p)
		}
		release, ok := l.TryHold("expired")
		if !ok {
			t.Fatal("prefired timeout left lock")
		}
		release()
		active.Release()
	}
}
