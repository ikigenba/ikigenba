package ask

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type providerFunc func(context.Context, string, string, string) (Answer, error)

func (f providerFunc) Ask(ctx context.Context, scope, owner, question string) (Answer, error) {
	return f(ctx, scope, owner, question)
}

type doneObservedContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *doneObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestCacheHitServesStoredAnswer(t *testing.T) {
	// R-02RF-HMOW
	var calls atomic.Int32
	want := Answer{Found: true, Text: "cached", Citations: []Citation{{Path: "entity/acme", Title: "Acme"}}}
	cache := NewCache(providerFunc(func(context.Context, string, string, string) (Answer, error) {
		calls.Add(1)
		return want, nil
	}), 3)

	first, err := cache.Ask(context.Background(), "s", "owner-one", "question")
	if err != nil {
		t.Fatalf("first Ask: %v", err)
	}
	second, err := cache.Ask(context.Background(), "s", "owner-two", "question")
	if err != nil {
		t.Fatalf("second Ask: %v", err)
	}
	if calls.Load() != 1 || !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, first) {
		t.Fatalf("calls = %d, answers = %#v and %#v, want one computation and %#v", calls.Load(), first, second, want)
	}
}

func TestCacheKeyNormalizesOnlySurroundingWhitespace(t *testing.T) {
	// R-03ZB-VEFL
	var calls atomic.Int32
	cache := NewCache(providerFunc(func(context.Context, string, string, string) (Answer, error) {
		return Answer{Found: true, Text: fmt.Sprintf("answer-%d", calls.Add(1))}, nil
	}), 3)

	for _, question := range []string{"who is acme", "  who is acme \n", "Who is acme"} {
		if _, err := cache.Ask(context.Background(), "s", "", question); err != nil {
			t.Fatalf("Ask(%q): %v", question, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want trimmed forms to collide and case change to compute", calls.Load())
	}
}

func TestCacheKeyKeepsScopesIndependent(t *testing.T) {
	// R-0578-966A
	var calls atomic.Int32
	cache := NewCache(providerFunc(func(_ context.Context, scope, _, _ string) (Answer, error) {
		calls.Add(1)
		return Answer{Found: true, Text: "answer-" + scope}, nil
	}), 3)

	for _, scope := range []string{"s1", "s2", "s1", "s2"} {
		got, err := cache.Ask(context.Background(), scope, "", "same question")
		if err != nil {
			t.Fatalf("Ask(%q): %v", scope, err)
		}
		if got.Text != "answer-"+scope {
			t.Fatalf("Ask(%q) = %#v, want its scope-specific answer", scope, got)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want one per scope", calls.Load())
	}
}

func TestCacheEvictsLeastRecentlyUsedAfterHitRefresh(t *testing.T) {
	// R-06F4-MXWZ
	var mu sync.Mutex
	calls := make(map[string]int)
	cache := NewCache(providerFunc(func(_ context.Context, _, _, question string) (Answer, error) {
		mu.Lock()
		defer mu.Unlock()
		calls[question]++
		return Answer{Found: true, Text: question}, nil
	}), 3)
	ask := func(question string) {
		t.Helper()
		if _, err := cache.Ask(context.Background(), "s", "", question); err != nil {
			t.Fatalf("Ask(%q): %v", question, err)
		}
	}
	for _, question := range []string{"A", "B", "C", "A", "D", "A", "B"} {
		ask(question)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["A"] != 1 || calls["B"] != 2 || calls["C"] != 1 || calls["D"] != 1 {
		t.Fatalf("provider calls = %#v, want A refreshed and B evicted", calls)
	}
}

func TestCacheCoalescesConcurrentIdenticalAsks(t *testing.T) {
	// R-07N1-0PNO
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	want := Answer{Found: true, Text: "shared"}
	cache := NewCache(providerFunc(func(context.Context, string, string, string) (Answer, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return want, nil
	}), 5)

	const n = 12
	results := make(chan Answer, n)
	errs := make(chan error, n)
	start := make(chan struct{})
	entered := make([]chan struct{}, n)
	for i := range n {
		entered[i] = make(chan struct{})
		ctx := &doneObservedContext{Context: context.Background(), observed: entered[i]}
		go func() {
			<-start
			answer, err := cache.Ask(ctx, "s", "owner", "question")
			results <- answer
			errs <- err
		}()
	}
	close(start)
	awaitSignal(t, started, "provider start")
	for _, waiter := range entered {
		awaitSignal(t, waiter, "cache waiter entry")
	}
	close(release)
	for range n {
		if err := <-errs; err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if got := <-results; !reflect.DeepEqual(got, want) {
			t.Fatalf("Ask = %#v, want %#v", got, want)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", calls.Load())
	}
}

func TestCacheDetachedComputationSurvivesLeaderCancellation(t *testing.T) {
	// R-08UX-EHED
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	want := Answer{Found: true, Text: "completed"}
	cache := NewCache(providerFunc(func(context.Context, string, string, string) (Answer, error) {
		calls.Add(1)
		close(started)
		<-release
		return want, nil
	}), 5)

	leaderCtx, cancel := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := cache.Ask(leaderCtx, "s", "leader", "question")
		leaderErr <- err
	}()
	awaitSignal(t, started, "provider start")
	waiterResult := make(chan Answer, 1)
	waiterErr := make(chan error, 1)
	go func() {
		answer, err := cache.Ask(context.Background(), "s", "waiter", "question")
		waiterResult <- answer
		waiterErr <- err
	}()
	cancel()
	if err := awaitError(t, leaderErr, "leader return"); !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want context.Canceled", err)
	}
	close(release)
	if err := awaitError(t, waiterErr, "waiter return"); err != nil {
		t.Fatalf("waiter error: %v", err)
	}
	if got := <-waiterResult; !reflect.DeepEqual(got, want) {
		t.Fatalf("waiter answer = %#v, want %#v", got, want)
	}
	got, err := cache.Ask(context.Background(), "s", "later", "question")
	if err != nil || !reflect.DeepEqual(got, want) || calls.Load() != 1 {
		t.Fatalf("cached Ask = %#v, %v; calls = %d, want cached result", got, err, calls.Load())
	}
}

func TestCacheStoresHonestEmptyAnswer(t *testing.T) {
	// R-0A2T-S952
	var calls atomic.Int32
	want := Answer{Found: false, Text: honestEmptyText}
	cache := NewCache(providerFunc(func(context.Context, string, string, string) (Answer, error) {
		calls.Add(1)
		return want, nil
	}), 2)
	for range 2 {
		got, err := cache.Ask(context.Background(), "s", "", "unknown")
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("Ask = %#v, %v, want %#v", got, err, want)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1", calls.Load())
	}
}

func TestCacheSharesErrorsAndRecomputesAfterFailure(t *testing.T) {
	// R-0BAQ-60VR
	wantErr := errors.New("transient failure")
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	cache := NewCache(providerFunc(func(context.Context, string, string, string) (Answer, error) {
		call := calls.Add(1)
		if call == 1 {
			close(started)
			<-release
			return Answer{}, wantErr
		}
		return Answer{Found: true, Text: "recovered"}, nil
	}), 2)

	const n = 8
	errs := make(chan error, n)
	start := make(chan struct{})
	entered := make([]chan struct{}, n)
	for i := range n {
		entered[i] = make(chan struct{})
		ctx := &doneObservedContext{Context: context.Background(), observed: entered[i]}
		go func() {
			<-start
			_, err := cache.Ask(ctx, "s", "", "question")
			errs <- err
		}()
	}
	close(start)
	awaitSignal(t, started, "provider start")
	for _, waiter := range entered {
		awaitSignal(t, waiter, "cache waiter entry")
	}
	close(release)
	for range n {
		if err := <-errs; err != wantErr {
			t.Fatalf("coalesced error = %v, want identical error %v", err, wantErr)
		}
	}
	got, err := cache.Ask(context.Background(), "s", "", "question")
	if err != nil || got.Text != "recovered" || calls.Load() != 2 {
		t.Fatalf("retry = %#v, %v; calls = %d, want successful recomputation", got, err, calls.Load())
	}
}

func TestCacheInvalidateDropsOnlyNamedScope(t *testing.T) {
	// R-0CIM-JSMG
	var mu sync.Mutex
	calls := make(map[string]int)
	cache := NewCache(providerFunc(func(_ context.Context, scope, _, _ string) (Answer, error) {
		mu.Lock()
		defer mu.Unlock()
		calls[scope]++
		return Answer{Found: true, Text: fmt.Sprintf("%s-%d", scope, calls[scope])}, nil
	}), 4)
	for _, scope := range []string{"s1", "s2"} {
		if _, err := cache.Ask(context.Background(), scope, "", "question"); err != nil {
			t.Fatalf("initial Ask(%q): %v", scope, err)
		}
	}
	cache.Invalidate("s1")
	s1, err := cache.Ask(context.Background(), "s1", "", "question")
	if err != nil {
		t.Fatalf("second s1 Ask: %v", err)
	}
	s2, err := cache.Ask(context.Background(), "s2", "", "question")
	if err != nil {
		t.Fatalf("second s2 Ask: %v", err)
	}
	if s1.Text != "s1-2" || s2.Text != "s2-1" {
		t.Fatalf("answers = %q, %q, want s1 recomputed and s2 cached", s1.Text, s2.Text)
	}
}

func TestCacheZeroCapacityDisablesCachingAndCoalescing(t *testing.T) {
	// R-0JU0-UF2M
	var calls atomic.Int32
	cache := NewCache(providerFunc(func(context.Context, string, string, string) (Answer, error) {
		return Answer{Found: true, Text: fmt.Sprint(calls.Add(1))}, nil
	}), 0)
	for range 2 {
		if _, err := cache.Ask(context.Background(), "s", "", "same"); err != nil {
			t.Fatalf("sequential Ask: %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("sequential provider calls = %d, want 2", calls.Load())
	}

	started := make(chan struct{}, 5)
	release := make(chan struct{})
	cache = NewCache(providerFunc(func(context.Context, string, string, string) (Answer, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return Answer{Found: true, Text: "done"}, nil
	}), 0)
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.Ask(context.Background(), "s", "", "same"); err != nil {
				t.Errorf("concurrent Ask: %v", err)
			}
		}()
	}
	for range 5 {
		awaitSignal(t, started, "disabled provider invocation")
	}
	close(release)
	wg.Wait()
	if calls.Load() != 7 {
		t.Fatalf("total provider calls = %d, want 7", calls.Load())
	}
}

func awaitSignal(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func awaitError(t *testing.T, ch <-chan error, description string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}
