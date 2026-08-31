package agentkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestErrorHasOnePublicShapeAndWrapsCause(t *testing.T) {
	// R-2K5Z-AIWY
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
		{name: "err", typeName: "error", exported: false},
	}
	errorType := reflect.TypeOf(Error{})
	if errorType.NumField() != len(want) {
		t.Fatalf("Error has %d fields, want exact D4 shape of %d", errorType.NumField(), len(want))
	}
	for index, expected := range want {
		actual := errorType.Field(index)
		if actual.Name != expected.name || actual.Type.String() != expected.typeName || actual.IsExported() != expected.exported {
			t.Fatalf("Error field %d = (%s, %s, exported=%t), want %#v", index, actual.Name, actual.Type, actual.IsExported(), expected)
		}
	}

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
	// R-2SP9-YX3T
	tests := []struct {
		name     string
		sentinel error
	}{
		{name: "invalid config", sentinel: ErrInvalidConfig},
		{name: "closed", sentinel: ErrClosed},
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

type syntheticEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func syntheticClassify(status int, header http.Header, body []byte) error {
	var envelope syntheticEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return &Error{Category: CategoryUnknown, Status: status, Message: err.Error(), err: err}
	}

	retryAfter, _ := time.ParseDuration(header.Get("Retry-After"))
	category := CategoryInvalidRequest
	if strings.Contains(envelope.Error.Message, "credential") {
		category = CategoryAuth
	}

	return &Error{
		Category:   category,
		Status:     status,
		Code:       envelope.Error.Code,
		Message:    envelope.Error.Message,
		RetryAfter: retryAfter,
	}
}

func TestClassifierReceivesFullResponseAndLiftsRetryHint(t *testing.T) {
	// R-2LDV-OANN
	// R-2MLS-22EC
	header := http.Header{
		"Retry-After":  []string{"2250ms"},
		"X-Request-Id": []string{"request-123"},
	}
	body := []byte(`{"error":{"code":"shared-code","message":"bad credential supplied"}}`)

	err := syntheticClassify(http.StatusTooManyRequests, header, body)
	var providerError *Error
	if !errors.As(err, &providerError) {
		t.Fatalf("classifier error type = %T, want *Error", err)
	}
	if providerError.Status != http.StatusTooManyRequests || providerError.Code != "shared-code" || providerError.Message != "bad credential supplied" {
		t.Fatalf("classifier result lost status or exact body fields: %#v", providerError)
	}
	if providerError.RetryAfter != 2250*time.Millisecond {
		t.Fatalf("RetryAfter = %v, want typed 2.25s from response header", providerError.RetryAfter)
	}
}

func TestClassifierMayDisambiguateSharedStatusAndCodeByMessage(t *testing.T) {
	// R-2NTO-FU51
	tests := []struct {
		message  string
		category Category
	}{
		{message: "bad credential supplied", category: CategoryAuth},
		{message: "unknown model requested", category: CategoryInvalidRequest},
	}
	for _, test := range tests {
		body := []byte(fmt.Sprintf(`{"error":{"code":"shared-code","message":%q}}`, test.message))
		err := syntheticClassify(http.StatusBadRequest, make(http.Header), body)
		var providerError *Error
		if !errors.As(err, &providerError) {
			t.Fatalf("classifier error type = %T, want *Error", err)
		}
		if providerError.Status != http.StatusBadRequest || providerError.Code != "shared-code" || providerError.Category != test.category {
			t.Fatalf("classification for %q = %#v, want shared status/code and category %v", test.message, providerError, test.category)
		}
	}
}
