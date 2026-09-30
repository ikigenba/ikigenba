package agentkit

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

func TestSentinelDeclarations(t *testing.T) {
	// R-B5TT-UVF1
	wantMessages := map[string]string{
		"ErrInvalidConfig":   "agentkit: invalid configuration",
		"ErrClosed":          "agentkit: conversation closed",
		"ErrInvalidArgument": "agentkit: invalid argument",
	}
	sentinels := map[string]error{
		"ErrInvalidConfig":   ErrInvalidConfig,
		"ErrClosed":          ErrClosed,
		"ErrInvalidArgument": ErrInvalidArgument,
	}
	checkSentinelValues(t, wantMessages, sentinels)
}

func TestSavepointSentinelDeclarationsAndWrapping(t *testing.T) {
	// R-BAPF-DYDT
	wantMessages := map[string]string{
		"ErrSavepointActive": "agentkit: savepoint active",
		"ErrTurnInFlight":    "agentkit: turn in flight",
	}
	sentinels := map[string]error{
		"ErrSavepointActive": ErrSavepointActive,
		"ErrTurnInFlight":    ErrTurnInFlight,
	}
	checkSentinelValues(t, wantMessages, sentinels)
	allOthers := []error{
		ErrInvalidConfig,
		ErrClosed,
		ErrInvalidArgument,
		ErrInvalidOutput,
		ErrLimitExceeded,
		ErrNotFound,
	}
	for name, sentinel := range sentinels {
		if !errors.Is(sentinel, sentinel) {
			t.Fatalf("errors.Is(%s, itself) = false", name)
		}
		for _, other := range allOthers {
			if errors.Is(sentinel, other) || errors.Is(other, sentinel) {
				t.Fatalf("%s is not distinct from %v", name, other)
			}
		}
		wrapped := &Error{err: fmt.Errorf("lifecycle refusal: %w", sentinel)}
		if !errors.Is(wrapped, sentinel) {
			t.Fatalf("%s is not discoverable through *Error wrapping", name)
		}
	}
}

func checkSentinelValues(t *testing.T, wantMessages map[string]string, sentinels map[string]error) {
	t.Helper()
	for name, sentinel := range sentinels {
		if sentinel == nil {
			t.Fatalf("%s is nil", name)
		}
		if got := sentinel.Error(); got != wantMessages[name] {
			t.Fatalf("%s.Error() = %q, want %q", name, got, wantMessages[name])
		}
	}
	for name, sentinel := range sentinels {
		for otherName, otherSentinel := range sentinels {
			if name != otherName && errors.Is(sentinel, otherSentinel) {
				t.Fatalf("%s and %s are the same sentinel", name, otherName)
			}
		}
	}
}

func TestInvalidOutputSentinel(t *testing.T) {
	// R-B71Q-8N5Q
	t.Run("identity and wrapping", testInvalidOutputIdentityAndWrapping)
}

func testInvalidOutputIdentityAndWrapping(t *testing.T) {
	if ErrInvalidOutput == nil || ErrInvalidOutput.Error() != "agentkit: structured output rejected" {
		t.Fatalf("ErrInvalidOutput = %v, want exact non-nil sentinel", ErrInvalidOutput)
	}
	if !errors.Is(ErrInvalidOutput, ErrInvalidOutput) {
		t.Fatal("errors.Is(ErrInvalidOutput, itself) = false")
	}
	if errors.Is(ErrInvalidOutput, ErrInvalidConfig) || errors.Is(ErrInvalidOutput, ErrClosed) {
		t.Fatal("ErrInvalidOutput is not distinct from existing sentinels")
	}
	wrapped := &Error{err: fmt.Errorf("invalid document: %w", ErrInvalidOutput)}
	if !errors.Is(wrapped, ErrInvalidOutput) {
		t.Fatal("ErrInvalidOutput is not discoverable through *Error wrapping")
	}
}

func TestErrorHasOnePublicShapeAndWrapsCause(t *testing.T) {
	// R-2K5Z-AIWY
	t.Run("category values", testCategoryValues)
	t.Run("error shape", testErrorShape)
	t.Run("error and unwrap", testErrorTextAndUnwrap)
}

func testCategoryValues(t *testing.T) {
	categories := []Category{
		CategoryUnknown,
		CategoryAuth,
		CategoryInvalidRequest,
		CategoryRateLimit,
		CategoryOverloaded,
		CategoryInsufficientQuota,
		CategoryTimeout,
		CategoryTransport,
	}
	for value, category := range categories {
		if int(category) != value {
			t.Fatalf("category at index %d has value %d, want exact value %d", value, category, value)
		}
	}
}

func testErrorShape(t *testing.T) {
	type field struct {
		name     string
		typeName string
		exported bool
	}
	want := []field{
		{name: "Category", typeName: "agentkit.Category", exported: true},
		{name: "Status", typeName: "int", exported: true},
		{name: "Code", typeName: "string", exported: true},
		{name: "Message", typeName: "string", exported: true},
		{name: "RetryAfter", typeName: "time.Duration", exported: true},
		{name: "Endpoint", typeName: "agentkit.Identity", exported: true},
	}
	errorType := reflect.TypeOf(Error{})
	var exported []reflect.StructField
	for index := range errorType.NumField() {
		if field := errorType.Field(index); field.IsExported() {
			exported = append(exported, field)
		}
	}
	if len(exported) != len(want) {
		t.Fatalf("Error has %d exported fields, want exact D4 shape of %d", len(exported), len(want))
	}
	for index, expected := range want {
		actual := exported[index]
		if actual.Name != expected.name || actual.Type.String() != expected.typeName || actual.IsExported() != expected.exported {
			t.Fatalf("Error exported field %d = (%s, %s, exported=%t), want %#v", index, actual.Name, actual.Type, actual.IsExported(), expected)
		}
	}
}

func testErrorTextAndUnwrap(t *testing.T) {
	cause := errors.New("socket closed")
	providerError := &Error{
		Category: CategoryTransport,
		Status:   0,
		Message:  "request failed",
		err:      cause,
	}
	if got, wantText := providerError.Error(), "transport: request failed (status 0)"; got != wantText {
		t.Fatalf("Error() = %q, want %q", got, wantText)
	}
	if providerError.Unwrap().Error() != cause.Error() || !errors.Is(providerError, cause) {
		t.Fatalf("Unwrap() = %v, want original cause %v", providerError.Unwrap(), cause)
	}
}

func TestRetryableIsTheOnlyCategoryPolicy(t *testing.T) {
	// R-2RHD-L5D4
	tests := []struct {
		name      string
		category  Category
		retryable bool
	}{
		{name: "unknown", category: CategoryUnknown, retryable: false},
		{name: "auth", category: CategoryAuth, retryable: false},
		{name: "invalid request", category: CategoryInvalidRequest, retryable: false},
		{name: "rate limit", category: CategoryRateLimit, retryable: true},
		{name: "overloaded", category: CategoryOverloaded, retryable: true},
		{name: "insufficient quota", category: CategoryInsufficientQuota, retryable: false},
		{name: "timeout", category: CategoryTimeout, retryable: true},
		{name: "transport", category: CategoryTransport, retryable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			providerError := &Error{Category: test.category}
			if got := Retryable(providerError); got != test.retryable {
				t.Fatalf("Retryable(direct %v) = %t, want %t", test.category, got, test.retryable)
			}
			wrapped := fmt.Errorf("outer: %w", fmt.Errorf("middle: %w", providerError))
			if got := Retryable(wrapped); got != test.retryable {
				t.Fatalf("Retryable(multiply wrapped %v) = %t, want %t", test.category, got, test.retryable)
			}
		})
	}
	if Retryable(errors.New("ordinary error")) {
		t.Fatal("ordinary error is retryable, want false")
	}
	if Retryable(nil) {
		t.Fatal("nil is retryable, want false")
	}
}

func TestConfigurationAndLifecycleSentinelsSurviveErrorWrapping(t *testing.T) {
	// R-CJTD-QX2U
	tests := []struct {
		name     string
		sentinel error
	}{
		{name: "invalid config", sentinel: ErrInvalidConfig},
		{name: "closed", sentinel: ErrClosed},
		{name: "invalid argument", sentinel: ErrInvalidArgument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			providerError := &Error{
				Category: CategoryInvalidRequest,
				Message:  test.sentinel.Error(),
				err:      fmt.Errorf("context: %w", test.sentinel),
			}
			if !errors.Is(providerError, test.sentinel) {
				t.Fatalf("errors.Is(%v, %v) = false", providerError, test.sentinel)
			}
		})
	}
}

func TestBuiltInStatusClassification(t *testing.T) {
	// R-OHYJ-3C6O
	tests := []struct {
		status   int
		category Category
	}{
		{status: 401, category: CategoryAuth},
		{status: 403, category: CategoryAuth},
		{status: 400, category: CategoryInvalidRequest},
		{status: 404, category: CategoryInvalidRequest},
		{status: 409, category: CategoryInvalidRequest},
		{status: 413, category: CategoryInvalidRequest},
		{status: 415, category: CategoryInvalidRequest},
		{status: 422, category: CategoryInvalidRequest},
		{status: 402, category: CategoryInsufficientQuota},
		{status: 429, category: CategoryRateLimit},
		{status: 408, category: CategoryTimeout},
		{status: 504, category: CategoryTimeout},
		{status: 500, category: CategoryOverloaded},
		{status: 502, category: CategoryOverloaded},
		{status: 503, category: CategoryOverloaded},
		{status: 529, category: CategoryOverloaded},
		{status: 300, category: CategoryUnknown},
		{status: 418, category: CategoryUnknown},
		{status: 599, category: CategoryUnknown},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("status_%d", test.status), func(t *testing.T) {
			if got := classifyStatus(test.status); got != test.category {
				t.Fatalf("classifyStatus(%d) = %v, want %v", test.status, got, test.category)
			}
		})
	}

	known := map[int]Category{}
	for _, test := range tests {
		if test.category != CategoryUnknown {
			known[test.status] = test.category
		}
	}
	for status := 100; status <= 599; status++ {
		if status >= http.StatusOK && status < http.StatusMultipleChoices {
			continue
		}
		want := CategoryUnknown
		if category, ok := known[status]; ok {
			want = category
		}
		if got := classifyStatus(status); got != want {
			t.Fatalf("classifyStatus(%d) = %v, want %v", status, got, want)
		}
	}
}
