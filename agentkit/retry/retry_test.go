package retry

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestClockHasExactMethodSet(t *testing.T) {
	// R-0HAQ-WTJK
	// A stub with only Now and Sleep satisfies Clock, so Clock requires no
	// other method.
	var clock Clock = exactClockStub{}

	// Any Clock converts to an interface with exactly Now and Sleep, so
	// Clock has at least those methods with those signatures.
	type nowSleeper interface {
		Now() time.Time
		Sleep(ctx context.Context, d time.Duration) error
	}
	var ns nowSleeper = clock
	if got := ns.Now(); !got.Equal(exactClockNow) {
		t.Errorf("Now() = %v, want %v", got, exactClockNow)
	}
	if err := ns.Sleep(context.Background(), time.Second); err != nil {
		t.Errorf("Sleep() error = %v, want nil", err)
	}
}

var exactClockNow = time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)

type exactClockStub struct{}

func (exactClockStub) Now() time.Time { return exactClockNow }

func (exactClockStub) Sleep(context.Context, time.Duration) error { return nil }

func TestPolicyHasExactFieldsInOrder(t *testing.T) {
	// R-0IIN-ALA9
	// Conversion from an identical anonymous struct compiles only when field
	// names, types, and order all match exactly.
	type policyShape = struct {
		MaxAttempts int
		Base        time.Duration
		Max         time.Duration
		Jitter      float64
		Clock       Clock
		Rand        func() float64
		Retryable   func(err error) bool
		RetryAfter  func(err error) time.Duration
	}
	clock := exactClockStub{}
	policy := Policy(policyShape{
		MaxAttempts: 3,
		Base:        time.Second,
		Max:         time.Minute,
		Jitter:      0.5,
		Clock:       clock,
		Rand:        func() float64 { return 0.25 },
		Retryable:   func(error) bool { return true },
		RetryAfter:  func(error) time.Duration { return 2 * time.Second },
	})
	back := policyShape(policy)
	if back.MaxAttempts != 3 || back.Base != time.Second || back.Max != time.Minute ||
		back.Jitter != 0.5 || back.Clock != Clock(clock) {
		t.Errorf("Policy round trip = %+v, want the constructed values", back)
	}
	if policy.Rand() != 0.25 || !policy.Retryable(errors.New("x")) ||
		policy.RetryAfter(nil) != 2*time.Second {
		t.Errorf("Policy function fields did not return the constructed values")
	}
}

func TestDoHasExactGenericDeclaration(t *testing.T) {
	// R-0JQJ-OD0Y
	assertCallable := func(func(
		context.Context,
		Policy,
		func(context.Context) (int, error),
		func(int, error, time.Duration),
	) (int, error)) {
	}
	assertCallable(Do[int])

	type result struct{ Name string }
	got, err := Do(context.Background(), Policy{MaxAttempts: 1}, func(context.Context) (result, error) {
		return result{Name: "done"}, nil
	}, func(int, error, time.Duration) {})
	if err != nil || got.Name != "done" {
		t.Fatalf("Do[result] = %+v, %v; want done, nil", got, err)
	}
}

func TestDoHonorsRetryDecisionAndAttemptLimit(t *testing.T) {
	// R-57A5-4MR4
	retryableErr := errors.New("retryable")
	terminalErr := errors.New("terminal")
	tests := []struct {
		name        string
		maxAttempts int
		retryable   func(error) bool
		failures    []error
		wantCalls   int
		wantErr     error
	}{
		{
			name:        "retryable error reaches success",
			maxAttempts: 3,
			retryable:   func(err error) bool { return errors.Is(err, retryableErr) },
			failures:    []error{retryableErr, retryableErr},
			wantCalls:   3,
		},
		{
			name:        "terminal error stops immediately",
			maxAttempts: 4,
			retryable:   func(err error) bool { return errors.Is(err, retryableErr) },
			failures:    []error{terminalErr, retryableErr},
			wantCalls:   1,
			wantErr:     terminalErr,
		},
		{
			name:        "positive limit is exhausted",
			maxAttempts: 2,
			retryable:   func(error) bool { return true },
			failures:    []error{retryableErr, retryableErr, retryableErr},
			wantCalls:   2,
			wantErr:     retryableErr,
		},
		{
			name:        "nil retryable is terminal",
			maxAttempts: 3,
			failures:    []error{retryableErr, retryableErr},
			wantCalls:   1,
			wantErr:     retryableErr,
		},
		{
			name:        "zero max attempts permits one",
			maxAttempts: 0,
			retryable:   func(error) bool { return true },
			failures:    []error{retryableErr, retryableErr},
			wantCalls:   1,
			wantErr:     retryableErr,
		},
		{
			name:        "negative max attempts permits one",
			maxAttempts: -2,
			retryable:   func(error) bool { return true },
			failures:    []error{retryableErr, retryableErr},
			wantCalls:   1,
			wantErr:     retryableErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &fakeClock{}
			calls := 0
			got, err := Do(context.Background(), Policy{
				MaxAttempts: test.maxAttempts,
				Base:        time.Millisecond,
				Max:         time.Second,
				Clock:       clock,
				Retryable:   test.retryable,
			}, func(context.Context) (string, error) {
				calls++
				if calls <= len(test.failures) {
					return "", test.failures[calls-1]
				}
				return "ok", nil
			}, nil)
			if calls != test.wantCalls {
				t.Errorf("operation calls = %d, want %d", calls, test.wantCalls)
			}
			if !errors.Is(err, test.wantErr) {
				t.Errorf("error = %v, want identical %v", err, test.wantErr)
			}
			if test.wantErr == nil && got != "ok" {
				t.Errorf("value = %q, want ok", got)
			}
		})
	}
}

func TestDoSelectsExactCappedJitteredBackoffAndRetryAfterFloor(t *testing.T) {
	// R-58I1-IEHT
	operationErr := errors.New("transient")
	tests := []struct {
		name       string
		retryAfter time.Duration
		want       []time.Duration
	}{
		{
			name: "exponential cap is applied before jitter",
			want: []time.Duration{750 * time.Millisecond, 1500 * time.Millisecond, 2250 * time.Millisecond, 2250 * time.Millisecond},
		},
		{
			name:       "server delay is larger",
			retryAfter: 2 * time.Second,
			want:       []time.Duration{2 * time.Second, 2 * time.Second, 2250 * time.Millisecond, 2250 * time.Millisecond},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &fakeClock{}
			calls := 0
			_, err := Do(context.Background(), Policy{
				MaxAttempts: 5,
				Base:        time.Second,
				Max:         3 * time.Second,
				Jitter:      0.5,
				Clock:       clock,
				Rand:        func() float64 { return 0.5 },
				Retryable:   func(error) bool { return true },
				RetryAfter:  func(error) time.Duration { return test.retryAfter },
			}, func(context.Context) (struct{}, error) {
				calls++
				return struct{}{}, operationErr
			}, nil)
			if !sameError(err, operationErr) {
				t.Fatalf("error = %v, want identical operation error", err)
			}
			if !reflect.DeepEqual(clock.sleeps, test.want) {
				t.Errorf("delays = %v, want %v", clock.sleeps, test.want)
			}
		})
	}
}

func TestDoUsesFakeClockForFullSequenceWithoutRealWaiting(t *testing.T) {
	// R-59PX-W68I
	start := time.Date(2040, time.January, 2, 3, 4, 5, 0, time.UTC)
	clock := &fakeClock{now: start}
	calls := 0
	got, err := Do(context.Background(), Policy{
		MaxAttempts: 4,
		Base:        time.Hour,
		Max:         4 * time.Hour,
		Clock:       clock,
		Retryable:   func(error) bool { return true },
	}, func(context.Context) (int, error) {
		calls++
		if calls < 4 {
			return 0, errors.New("try again")
		}
		return 42, nil
	}, nil)
	if err != nil || got != 42 {
		t.Fatalf("Do() = (%d, %v), want (42, nil)", got, err)
	}
	wantSleeps := []time.Duration{time.Hour, 2 * time.Hour, 4 * time.Hour}
	if !reflect.DeepEqual(clock.sleeps, wantSleeps) {
		t.Errorf("sleeps = %v, want %v", clock.sleeps, wantSleeps)
	}
	if wantNow := start.Add(7 * time.Hour); !clock.Now().Equal(wantNow) {
		t.Errorf("fake now = %v, want %v", clock.Now(), wantNow)
	}
}

func TestDoReturnsContextErrorWhenCancelledDuringWait(t *testing.T) {
	// R-5AXU-9XZ7
	ctx, cancel := context.WithCancel(context.Background())
	clock := &cancellingClock{entered: make(chan struct{})}
	providerErr := errors.New("provider failed")
	calls := 0
	go func() {
		<-clock.entered
		cancel()
	}()

	_, err := Do(ctx, Policy{
		MaxAttempts: 3,
		Base:        time.Hour,
		Max:         time.Hour,
		Clock:       clock,
		Retryable:   func(error) bool { return true },
	}, func(context.Context) (struct{}, error) {
		calls++
		return struct{}{}, providerErr
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if errors.Is(err, providerErr) {
		t.Errorf("error %v matches provider error", err)
	}
	if calls != 1 {
		t.Errorf("operation calls = %d, want 1", calls)
	}
}

func TestDoReturnsFinalOperationErrorVerbatim(t *testing.T) {
	// R-5C5Q-NPPW
	sentinel := errors.New("sentinel")
	typed := &testOperationError{cause: sentinel}
	tests := []struct {
		name      string
		policy    Policy
		wantCalls int
	}{
		{
			name:      "terminal",
			policy:    Policy{MaxAttempts: 3, Retryable: func(error) bool { return false }},
			wantCalls: 1,
		},
		{
			name: "exhausted",
			policy: Policy{
				MaxAttempts: 2,
				Max:         time.Second,
				Clock:       &fakeClock{},
				Retryable:   func(error) bool { return true },
			},
			wantCalls: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			_, err := Do(context.Background(), test.policy, func(context.Context) (int, error) {
				calls++
				return 0, typed
			}, nil)
			if !sameError(err, typed) {
				t.Fatalf("error identity = %p, want %p", err, typed)
			}
			if !errors.Is(err, sentinel) {
				t.Errorf("errors.Is(error, sentinel) = false")
			}
			var gotTyped *testOperationError
			if !errors.As(err, &gotTyped) || gotTyped != typed {
				t.Errorf("errors.As = (%p, %v), want original typed error", gotTyped, gotTyped != nil)
			}
			if calls != test.wantCalls {
				t.Errorf("operation calls = %d, want %d", calls, test.wantCalls)
			}
		})
	}
}

func TestDoCallsOnRetryOnceBeforeEachWaitWithExactArguments(t *testing.T) {
	// R-5DDN-1HGL
	errorsByAttempt := []error{errors.New("first"), errors.New("second")}
	events := make([]string, 0, 4)
	clock := &fakeClock{beforeSleep: func(delay time.Duration) {
		events = append(events, "sleep:"+delay.String())
	}}
	callbackCalls := 0
	operationCalls := 0
	got, err := Do(context.Background(), Policy{
		MaxAttempts: 3,
		Base:        10 * time.Millisecond,
		Max:         20 * time.Millisecond,
		Clock:       clock,
		Retryable:   func(error) bool { return true },
		RetryAfter: func(err error) time.Duration {
			if sameError(err, errorsByAttempt[0]) {
				return 15 * time.Millisecond
			}
			return 0
		},
	}, func(context.Context) (string, error) {
		operationCalls++
		if operationCalls <= len(errorsByAttempt) {
			return "", errorsByAttempt[operationCalls-1]
		}
		return "done", nil
	}, func(attempt int, err error, delay time.Duration) {
		callbackCalls++
		wantDelay := []time.Duration{15 * time.Millisecond, 20 * time.Millisecond}[attempt-1]
		if !sameError(err, errorsByAttempt[attempt-1]) {
			t.Errorf("callback attempt %d error = %v, want identical %v", attempt, err, errorsByAttempt[attempt-1])
		}
		if delay != wantDelay {
			t.Errorf("callback attempt %d delay = %v, want %v", attempt, delay, wantDelay)
		}
		events = append(events, "callback:"+delay.String())
	})
	if err != nil || got != "done" {
		t.Fatalf("Do() = (%q, %v), want (done, nil)", got, err)
	}
	if callbackCalls != 2 {
		t.Errorf("callback calls = %d, want 2", callbackCalls)
	}
	wantEvents := []string{"callback:15ms", "sleep:15ms", "callback:20ms", "sleep:20ms"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Errorf("event order = %v, want %v", events, wantEvents)
	}

	nilCallbackClock := &fakeClock{}
	nilCallbackCalls := 0
	_, err = Do(context.Background(), Policy{
		MaxAttempts: 2,
		Max:         time.Millisecond,
		Clock:       nilCallbackClock,
		Retryable:   func(error) bool { return true },
	}, func(context.Context) (struct{}, error) {
		nilCallbackCalls++
		if nilCallbackCalls == 1 {
			return struct{}{}, errors.New("retry")
		}
		return struct{}{}, nil
	}, nil)
	if err != nil || nilCallbackCalls != 2 || len(nilCallbackClock.sleeps) != 1 {
		t.Errorf("nil callback run: calls=%d sleeps=%v err=%v", nilCallbackCalls, nilCallbackClock.sleeps, err)
	}
}

type fakeClock struct {
	now         time.Time
	sleeps      []time.Duration
	beforeSleep func(time.Duration)
}

func (f *fakeClock) Now() time.Time {
	return f.now
}

func (f *fakeClock) Sleep(ctx context.Context, delay time.Duration) error {
	if f.beforeSleep != nil {
		f.beforeSleep(delay)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	f.sleeps = append(f.sleeps, delay)
	f.now = f.now.Add(delay)
	return nil
}

type cancellingClock struct {
	entered chan struct{}
}

func (*cancellingClock) Now() time.Time {
	return time.Time{}
}

func (c *cancellingClock) Sleep(ctx context.Context, _ time.Duration) error {
	close(c.entered)
	<-ctx.Done()
	return ctx.Err()
}

type testOperationError struct {
	cause error
}

func (e *testOperationError) Error() string {
	return "operation: " + e.cause.Error()
}

func (e *testOperationError) Unwrap() error {
	return e.cause
}

func sameError(got, want error) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	gotValue := reflect.ValueOf(got)
	wantValue := reflect.ValueOf(want)
	return gotValue.Kind() == reflect.Pointer &&
		wantValue.Kind() == reflect.Pointer &&
		gotValue.Type() == wantValue.Type() &&
		gotValue.Pointer() == wantValue.Pointer()
}
