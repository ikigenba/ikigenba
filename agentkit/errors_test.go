package agentkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestSentinelDeclarations(t *testing.T) {
	// R-ZE9Y-O5TC
	wantMessages := map[string]string{
		"ErrInvalidConfig": "agentkit: invalid configuration",
		"ErrClosed":        "agentkit: conversation closed",
	}
	sentinels := map[string]error{
		"ErrInvalidConfig": ErrInvalidConfig,
		"ErrClosed":        ErrClosed,
	}
	checkSentinelValues(t, wantMessages, sentinels)
	checkDirectSentinelDeclarations(t, wantMessages)
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
	if errors.Is(ErrInvalidConfig, ErrClosed) || errors.Is(ErrClosed, ErrInvalidConfig) {
		t.Fatal("ErrInvalidConfig and ErrClosed are the same sentinel")
	}
}

func checkDirectSentinelDeclarations(t *testing.T, wantMessages map[string]string) {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]bool, len(wantMessages))
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value := specification.(*ast.ValueSpec)
			if len(value.Names) != 1 {
				continue
			}
			name := value.Names[0].Name
			wantMessage, wanted := wantMessages[name]
			if !wanted {
				continue
			}
			if !ast.IsExported(name) || found[name] {
				t.Fatalf("%s is not one uniquely declared exported package variable", name)
			}
			if value.Type != nil {
				identifier, isError := value.Type.(*ast.Ident)
				if !isError || identifier.Name != "error" {
					t.Fatalf("%s explicit static type is %T, want error", name, value.Type)
				}
			}
			if !isDirectErrorsNew(value, wantMessage) {
				t.Fatalf("%s is not initialized directly by errors.New with exact message %q", name, wantMessage)
			}
			found[name] = true
		}
	}
	for name := range wantMessages {
		if !found[name] {
			t.Fatalf("%s direct errors.New package variable declaration not found", name)
		}
	}
}

func TestInvalidOutputSentinel(t *testing.T) {
	// R-TRAJ-KF3P
	t.Run("direct declaration", testInvalidOutputDeclaration)
	t.Run("identity and wrapping", testInvalidOutputIdentityAndWrapping)
}

func testInvalidOutputDeclaration(t *testing.T) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			value := specification.(*ast.ValueSpec)
			if len(value.Names) != 1 || value.Names[0].Name != "ErrInvalidOutput" {
				continue
			}
			found++
			if !ast.IsExported(value.Names[0].Name) || !isDirectErrorsNew(value, "agentkit: structured output rejected") {
				t.Fatal("ErrInvalidOutput is not declared directly with the exact errors.New message")
			}
		}
	}
	if found != 1 {
		t.Fatalf("ErrInvalidOutput package declarations = %d, want exactly one", found)
	}
}

func isDirectErrorsNew(value *ast.ValueSpec, wantMessage string) bool {
	if len(value.Values) != 1 {
		return false
	}
	call, ok := value.Values[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "New" {
		return false
	}
	packageName, ok := selector.X.(*ast.Ident)
	message, literal := call.Args[0].(*ast.BasicLit)
	return ok && literal && packageName.Name == "errors" && message.Kind == token.STRING && message.Value == fmt.Sprintf("%q", wantMessage)
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
	return &Error{
		Category:   classifyStatus(status),
		Status:     status,
		Code:       envelope.Error.Code,
		Message:    envelope.Error.Message,
		RetryAfter: retryAfter,
	}
}

func TestClassifierReceivesFullResponseAndLiftsRetryHint(t *testing.T) {
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
