package retry

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/build"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClockHasExactMethodSet(t *testing.T) {
	// R-0HAQ-WTJK
	typ := reflect.TypeOf((*Clock)(nil)).Elem()
	if typ.Name() != "Clock" || typ.Kind() != reflect.Interface {
		t.Fatalf("Clock is %q of kind %v, want defined interface Clock", typ.Name(), typ.Kind())
	}

	want := []struct {
		name string
		in   []reflect.Type
		out  []reflect.Type
	}{
		{name: "Now", out: []reflect.Type{reflect.TypeOf(time.Time{})}},
		{
			name: "Sleep",
			in: []reflect.Type{
				reflect.TypeOf((*context.Context)(nil)).Elem(),
				reflect.TypeOf(time.Duration(0)),
			},
			out: []reflect.Type{reflect.TypeOf((*error)(nil)).Elem()},
		},
	}
	if typ.NumMethod() != len(want) {
		t.Fatalf("Clock has %d methods, want %d", typ.NumMethod(), len(want))
	}
	for i, expected := range want {
		method := typ.Method(i)
		if method.Name != expected.name {
			t.Errorf("method %d name = %q, want %q", i, method.Name, expected.name)
		}
		assertFunctionSignature(t, method.Type, expected.in, expected.out)
	}
}

func TestPolicyHasExactFieldsInOrder(t *testing.T) {
	// R-0IIN-ALA9
	typ := reflect.TypeOf(Policy{})
	want := []struct {
		name string
		typ  reflect.Type
	}{
		{name: "MaxAttempts", typ: reflect.TypeOf(int(0))},
		{name: "Base", typ: reflect.TypeOf(time.Duration(0))},
		{name: "Max", typ: reflect.TypeOf(time.Duration(0))},
		{name: "Jitter", typ: reflect.TypeOf(float64(0))},
		{name: "Clock", typ: reflect.TypeOf((*Clock)(nil)).Elem()},
		{name: "Rand", typ: reflect.TypeOf((func() float64)(nil))},
		{name: "Retryable", typ: reflect.TypeOf((func(error) bool)(nil))},
		{name: "RetryAfter", typ: reflect.TypeOf((func(error) time.Duration)(nil))},
	}
	if typ.Name() != "Policy" || typ.Kind() != reflect.Struct {
		t.Fatalf("Policy is %q of kind %v, want defined struct Policy", typ.Name(), typ.Kind())
	}
	if typ.NumField() != len(want) {
		t.Fatalf("Policy has %d fields, want %d", typ.NumField(), len(want))
	}
	for i, expected := range want {
		field := typ.Field(i)
		if field.Name != expected.name || field.Type != expected.typ || !field.IsExported() {
			t.Errorf("field %d = exported=%v %s %v, want exported %s %v",
				i, field.IsExported(), field.Name, field.Type, expected.name, expected.typ)
		}
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

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "retry.go", nil, 0)
	if err != nil {
		t.Fatalf("parse retry.go: %v", err)
	}
	var declaration *ast.FuncDecl
	for _, item := range file.Decls {
		if function, ok := item.(*ast.FuncDecl); ok && function.Name.Name == "Do" {
			declaration = function
			break
		}
	}
	if declaration == nil {
		t.Fatal("Do declaration not found")
	}
	if declaration.Recv != nil {
		t.Fatal("Do is a method, want package function")
	}
	assertASTFields(t, fileSet, declaration.Type.TypeParams, []astField{
		{names: []string{"T"}, typ: "any"},
	})
	assertASTFields(t, fileSet, declaration.Type.Params, []astField{
		{names: []string{"ctx"}, typ: "context.Context"},
		{names: []string{"p"}, typ: "Policy"},
		{names: []string{"op"}, typ: "func(ctx context.Context) (T, error)"},
		{names: []string{"onRetry"}, typ: "func(attempt int, err error, delay time.Duration)"},
	})
	assertASTFields(t, fileSet, declaration.Type.Results, []astField{
		{typ: "T"},
		{typ: "error"},
	})
}

func TestRetryProductionImportsOnlyStandardLibrary(t *testing.T) {
	// R-5628-QV0F
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read retry package: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", entry.Name(), parseErr)
		}
		for _, imported := range file.Imports {
			path, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("unquote import %s in %s: %v", imported.Path.Value, entry.Name(), unquoteErr)
			}
			if path == "github.com/ikigenba/ikigenba/agentkit" {
				t.Errorf("production file %s imports agentkit root", entry.Name())
			}
			pkg, importErr := build.Default.Import(path, ".", build.FindOnly)
			if importErr != nil {
				t.Errorf("resolve import %q in %s: %v", path, entry.Name(), importErr)
				continue
			}
			if !pkg.Goroot {
				t.Errorf("production import %q in %s is not from the standard library", path, entry.Name())
			}
		}
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

func assertFunctionSignature(t *testing.T, got reflect.Type, in, out []reflect.Type) {
	t.Helper()
	if got.NumIn() != len(in) || got.NumOut() != len(out) || got.IsVariadic() {
		t.Errorf("signature = %v, want %d inputs, %d outputs, non-variadic", got, len(in), len(out))
		return
	}
	for i, expected := range in {
		if got.In(i) != expected {
			t.Errorf("input %d = %v, want %v", i, got.In(i), expected)
		}
	}
	for i, expected := range out {
		if got.Out(i) != expected {
			t.Errorf("output %d = %v, want %v", i, got.Out(i), expected)
		}
	}
}

type astField struct {
	names []string
	typ   string
}

func assertASTFields(t *testing.T, fileSet *token.FileSet, got *ast.FieldList, want []astField) {
	t.Helper()
	if got == nil || len(got.List) != len(want) {
		if got == nil {
			t.Fatalf("field list is nil, want %d fields", len(want))
		}
		t.Fatalf("field list has %d fields, want %d", len(got.List), len(want))
	}
	for i, expected := range want {
		field := got.List[i]
		names := make([]string, len(field.Names))
		for j, name := range field.Names {
			names[j] = name.Name
		}
		if len(names) != len(expected.names) {
			t.Errorf("field %d names = %v, want %v", i, names, expected.names)
		} else {
			for j := range names {
				if names[j] != expected.names[j] {
					t.Errorf("field %d names = %v, want %v", i, names, expected.names)
					break
				}
			}
		}
		var rendered bytes.Buffer
		if err := format.Node(&rendered, fileSet, field.Type); err != nil {
			t.Fatalf("format field %d type: %v", i, err)
		}
		if rendered.String() != expected.typ {
			t.Errorf("field %d type = %q, want %q", i, rendered.String(), expected.typ)
		}
	}
}
