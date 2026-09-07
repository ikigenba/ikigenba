package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
	"time"
)

func TestLimitsShape(t *testing.T) {
	// R-TILY-AHD5
	typeOfLimits := reflect.TypeOf(Limits{})
	if typeOfLimits.NumField() != 2 {
		t.Fatalf("Limits has %d fields, want exactly 2", typeOfLimits.NumField())
	}
	want := []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "MaxToolCalls", typeOf: reflect.TypeOf(int(0))},
		{name: "MaxContextTokens", typeOf: reflect.TypeOf(int64(0))},
	}
	for index, expected := range want {
		field := typeOfLimits.Field(index)
		if field.Name != expected.name || field.Type != expected.typeOf {
			t.Fatalf("Limits field %d = (%s, %s), want (%s, %s)", index, field.Name, field.Type, expected.name, expected.typeOf)
		}
	}
}

func TestLimitKindValues(t *testing.T) {
	// R-TJTU-O93U
	if reflect.TypeOf(LimitKind("")).Kind() != reflect.String {
		t.Fatalf("LimitKind underlying kind = %s, want string", reflect.TypeOf(LimitKind("")).Kind())
	}
	if got := string(LimitToolCalls); got != "tool_calls" {
		t.Fatalf("LimitToolCalls = %q, want %q", got, "tool_calls")
	}
	if got := string(LimitContextTokens); got != "context_tokens" {
		t.Fatalf("LimitContextTokens = %q, want %q", got, "context_tokens")
	}
}

func TestLimitInfoShapeAndJSON(t *testing.T) {
	// R-TL1R-20UJ
	typeOfInfo := reflect.TypeOf(LimitInfo{})
	want := []struct {
		name    string
		typeOf  reflect.Type
		jsonTag string
	}{
		{name: "Kind", typeOf: reflect.TypeOf(LimitKind("")), jsonTag: "kind"},
		{name: "Max", typeOf: reflect.TypeOf(int64(0)), jsonTag: "max"},
		{name: "Actual", typeOf: reflect.TypeOf(int64(0)), jsonTag: "actual"},
	}
	if typeOfInfo.NumField() != len(want) {
		t.Fatalf("LimitInfo has %d fields, want exactly %d", typeOfInfo.NumField(), len(want))
	}
	for index, expected := range want {
		field := typeOfInfo.Field(index)
		if field.Name != expected.name || field.Type != expected.typeOf || field.Tag.Get("json") != expected.jsonTag {
			t.Fatalf("LimitInfo field %d = (%s, %s, json:%q), want (%s, %s, json:%q)", index, field.Name, field.Type, field.Tag.Get("json"), expected.name, expected.typeOf, expected.jsonTag)
		}
	}

	wantInfo := LimitInfo{Kind: LimitContextTokens, Max: 100, Actual: 101}
	encoded, err := json.Marshal(wantInfo)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		t.Fatal(err)
	}
	wantKeys := map[string]bool{"kind": true, "max": true, "actual": true}
	if len(keys) != len(wantKeys) {
		t.Fatalf("marshaled LimitInfo keys = %v, want exactly %v", keys, wantKeys)
	}
	for key := range wantKeys {
		if _, ok := keys[key]; !ok {
			t.Fatalf("marshaled LimitInfo is missing key %q: %s", key, encoded)
		}
	}
	var decoded LimitInfo
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != wantInfo {
		t.Fatalf("LimitInfo JSON round trip = %#v, want %#v", decoded, wantInfo)
	}
}

func TestErrLimitExceededDeclarationAndBehavior(t *testing.T) {
	// R-TNHJ-TKBX
	if ErrLimitExceeded == nil {
		t.Fatal("ErrLimitExceeded is nil")
	}
	if got := ErrLimitExceeded.Error(); got != "agentkit: limit exceeded" {
		t.Fatalf("ErrLimitExceeded.Error() = %q, want %q", got, "agentkit: limit exceeded")
	}
	if !errors.Is(ErrLimitExceeded, ErrLimitExceeded) {
		t.Fatal("errors.Is(ErrLimitExceeded, ErrLimitExceeded) = false")
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "limits.go", nil, 0)
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
			if len(value.Names) != 1 || value.Names[0].Name != "ErrLimitExceeded" {
				continue
			}
			found++
			if !ast.IsExported(value.Names[0].Name) || !isDirectErrorsNew(value, "agentkit: limit exceeded") {
				t.Fatal("ErrLimitExceeded is not declared directly with the exact errors.New message")
			}
		}
	}
	if found != 1 {
		t.Fatalf("ErrLimitExceeded package declarations = %d, want exactly one", found)
	}

	var providerError *Error
	if errors.As(ErrLimitExceeded, &providerError) {
		t.Fatal("errors.As(ErrLimitExceeded, *Error) = true")
	}
	if Retryable(ErrLimitExceeded) {
		t.Fatal("Retryable(ErrLimitExceeded) = true")
	}
}

// R-TOPG-7C2M
func TestZeroLimitsNeverRefuseWork(t *testing.T) {
	call := ToolUse{ID: "lookup", Name: "lookup", Input: json.RawMessage(`{"city":"Oslo"}`)}
	provider := &phase15Provider{model: "model", responses: [][]Event{
		{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{call}}}},
		{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}}},
	}}
	transportCalls := 0
	toolCalls := 0
	var output bytes.Buffer
	conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
		Log: NewLog(&output, func() time.Time { return time.Time{} }, ""),
		Tools: []Tool{MustTool("lookup", "", func(context.Context, phase15Input) (string, error) {
			toolCalls++
			return "found", nil
		})},
	})
	stream := conversation.Send(context.Background(), Text{Text: "lookup"})
	drainStream(stream)
	if stream.Err() != nil || errors.Is(stream.Err(), ErrLimitExceeded) {
		t.Fatalf("zero-limit Send error = %v, want nil", stream.Err())
	}
	if transportCalls != 2 || toolCalls != 1 {
		t.Fatalf("zero-limit calls: transport=%d tool=%d, want 2 and 1", transportCalls, toolCalls)
	}
	for _, record := range decodeLogRecords(t, output.Bytes()) {
		if record.Type == RecordLimit {
			t.Fatalf("zero Limits wrote a limit record: %#v", record)
		}
	}
}

// R-TPXC-L3TB
func TestNegativeLimitsFailBeforeProviderAndPreserveHistory(t *testing.T) {
	tests := []struct {
		name   string
		limits Limits
	}{
		{name: "tool calls", limits: Limits{MaxToolCalls: -1}},
		{name: "context tokens", limits: Limits{MaxContextTokens: -1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &phase15Provider{model: "model"}
			transportCalls := 0
			conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{Limits: test.limits})
			conversation.history = History{{Role: RoleSystem, Blocks: []Block{Text{Text: "stable"}}}}
			before, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			stream := conversation.Send(context.Background(), Text{Text: "refuse"})
			drainStream(stream)
			if !errors.Is(stream.Err(), ErrInvalidConfig) {
				t.Fatalf("Send error = %v, want ErrInvalidConfig", stream.Err())
			}
			if transportCalls != 0 || len(provider.states) != 0 {
				t.Fatalf("provider boundary calls: transport=%d states=%d, want zero", transportCalls, len(provider.states))
			}
			after, err := json.Marshal(conversation.history)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("history changed: before=%s after=%s", before, after)
			}
		})
	}
}

// R-6LYP-SXIT
// R-TUSY-46S3
func TestToolCallLimitRefusesWholeDispatchAndTracksLifetime(t *testing.T) {
	t.Run("one round exceeds budget", func(t *testing.T) {
		first := Message{Role: RoleAssistant, Blocks: []Block{
			ToolUse{ID: "one", Name: "lookup", Input: json.RawMessage(`{"city":"Oslo"}`)},
			ToolUse{ID: "two", Name: "lookup", Input: json.RawMessage(`{"city":"Oslo"}`)},
		}}
		provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}}}
		transportCalls := 0
		toolCalls := 0
		var output bytes.Buffer
		conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
			Limits: Limits{MaxToolCalls: 1},
			Log:    NewLog(&output, func() time.Time { return time.Time{} }, ""),
			Tools: []Tool{MustTool("lookup", "", func(context.Context, phase15Input) (string, error) {
				toolCalls++
				return "found", nil
			})},
		})
		before := marshalLimitTestHistory(t, conversation.history)
		stream := conversation.Send(context.Background(), Text{Text: "lookup"})
		drainStream(stream)
		if !errors.Is(stream.Err(), ErrLimitExceeded) || transportCalls != 1 || toolCalls != 0 {
			t.Fatalf("refusal error=%v transport=%d tool=%d, want ErrLimitExceeded, 1, 0", stream.Err(), transportCalls, toolCalls)
		}
		after := marshalLimitTestHistory(t, conversation.history)
		if !bytes.Equal(before, after) {
			t.Fatalf("history changed: before=%s after=%s", before, after)
		}
		assertLimitLog(t, decodeLogRecords(t, output.Bytes()), LimitInfo{Kind: LimitToolCalls, Max: 1, Actual: 2})
	})

	t.Run("prior dispatch counts toward budget", func(t *testing.T) {
		first := Message{Role: RoleAssistant, Blocks: []Block{ToolUse{ID: "first", Name: "first", Input: json.RawMessage(`{"city":"Oslo"}`)}}}
		second := Message{Role: RoleAssistant, Blocks: []Block{
			ToolUse{ID: "second-a", Name: "second", Input: json.RawMessage(`{"city":"Oslo"}`)},
			ToolUse{ID: "second-b", Name: "second", Input: json.RawMessage(`{"city":"Oslo"}`)},
		}}
		provider := &phase15Provider{model: "model", responses: [][]Event{{MessageDone{Message: first}}, {MessageDone{Message: second}}}}
		transportCalls := 0
		firstCalls, secondCalls := 0, 0
		var output bytes.Buffer
		conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
			Limits: Limits{MaxToolCalls: 2},
			Log:    NewLog(&output, func() time.Time { return time.Time{} }, ""),
			Tools: []Tool{
				MustTool("first", "", func(context.Context, phase15Input) (string, error) { firstCalls++; return "ok", nil }),
				MustTool("second", "", func(context.Context, phase15Input) (string, error) { secondCalls++; return "ok", nil }),
			},
		})
		stream := conversation.Send(context.Background(), Text{Text: "lookup"})
		drainStream(stream)
		if !errors.Is(stream.Err(), ErrLimitExceeded) || transportCalls != 2 || firstCalls != 1 || secondCalls != 0 {
			t.Fatalf("lifetime refusal error=%v transport=%d first=%d second=%d", stream.Err(), transportCalls, firstCalls, secondCalls)
		}
		assertLimitLog(t, decodeLogRecords(t, output.Bytes()), LimitInfo{Kind: LimitToolCalls, Max: 2, Actual: 3})
	})
}

// R-8EVL-UD8C
// R-TTL1-QF1E
func TestContextLimitRefusesOnlySubsequentWork(t *testing.T) {
	const maxContext = int64(20)
	wantLimit := LimitInfo{Kind: LimitContextTokens, Max: maxContext, Actual: 21}

	t.Run("next Send", func(t *testing.T) {
		final := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: "done"}}}
		provider := &phase15Provider{
			model:      "model",
			responses:  [][]Event{{MessageDone{Message: final}}},
			accounting: []providerAccounting{{usage: Usage{InputTokens: 1, CachedTokens: 2, CacheWrite5mTokens: 3, CacheWrite1hTokens: 4, OutputTokens: 5, ReasoningTokens: 6}}},
		}
		transportCalls := 0
		var output bytes.Buffer
		conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
			Limits: Limits{MaxContextTokens: maxContext},
			Log:    NewLog(&output, func() time.Time { return time.Time{} }, ""),
		})
		first := conversation.Send(context.Background(), Text{Text: "first"})
		drainStream(first)
		if first.Err() != nil {
			t.Fatalf("over-context no-tool turn error = %v, want nil", first.Err())
		}
		if len(conversation.history) != 2 || !reflect.DeepEqual(conversation.history[1], final) {
			t.Fatalf("over-context no-tool turn did not commit: %#v", conversation.history)
		}
		for _, record := range decodeLogRecords(t, output.Bytes()) {
			if record.Type == RecordLimit {
				t.Fatalf("completing over-context turn wrote premature limit: %#v", record)
			}
		}
		before := marshalLimitTestHistory(t, conversation.history)
		statesBefore, transportBefore := len(provider.states), transportCalls
		second := conversation.Send(context.Background(), Text{Text: "second"})
		drainStream(second)
		if !errors.Is(second.Err(), ErrLimitExceeded) || transportCalls != transportBefore || len(provider.states) != statesBefore {
			t.Fatalf("next-Send refusal error=%v transport=%d/%d states=%d/%d", second.Err(), transportCalls, transportBefore, len(provider.states), statesBefore)
		}
		after := marshalLimitTestHistory(t, conversation.history)
		if !bytes.Equal(before, after) {
			t.Fatalf("history changed: before=%s after=%s", before, after)
		}
		assertLimitLog(t, decodeLogRecords(t, output.Bytes()), wantLimit)
	})

	t.Run("tool dispatch", func(t *testing.T) {
		call := ToolUse{ID: "call", Name: "lookup", Input: json.RawMessage(`{"city":"Oslo"}`)}
		provider := &phase15Provider{
			model:      "model",
			responses:  [][]Event{{MessageDone{Message: Message{Role: RoleAssistant, Blocks: []Block{call}}}}},
			accounting: []providerAccounting{{usage: Usage{InputTokens: 21}}},
		}
		transportCalls, toolCalls := 0, 0
		var output bytes.Buffer
		conversation := newConversation(provider, successfulPhase15Client(&transportCalls), Config{
			Limits: Limits{MaxContextTokens: maxContext},
			Log:    NewLog(&output, func() time.Time { return time.Time{} }, ""),
			Tools: []Tool{MustTool("lookup", "", func(context.Context, phase15Input) (string, error) {
				toolCalls++
				return "found", nil
			})},
		})
		before := marshalLimitTestHistory(t, conversation.history)
		stream := conversation.Send(context.Background(), Text{Text: "lookup"})
		drainStream(stream)
		if !errors.Is(stream.Err(), ErrLimitExceeded) || transportCalls != 1 || toolCalls != 0 {
			t.Fatalf("dispatch refusal error=%v transport=%d tool=%d", stream.Err(), transportCalls, toolCalls)
		}
		after := marshalLimitTestHistory(t, conversation.history)
		if !bytes.Equal(before, after) {
			t.Fatalf("history changed: before=%s after=%s", before, after)
		}
		assertLimitLog(t, decodeLogRecords(t, output.Bytes()), wantLimit)
	})
}

func assertLimitLog(t *testing.T, records []LogRecord, want LimitInfo) {
	t.Helper()
	limitCount, errorCount := 0, 0
	limitIndex, turnEndIndex := -1, -1
	for index, record := range records {
		switch record.Type {
		case RecordLimit:
			limitCount++
			limitIndex = index
			if record.Limit == nil || *record.Limit != want {
				t.Fatalf("limit record = %#v, want %#v", record.Limit, want)
			}
		case RecordError:
			errorCount++
		case RecordTurnEnd:
			turnEndIndex = index
		}
	}
	if limitCount != 1 || errorCount != 0 || limitIndex < 0 || turnEndIndex < 0 || limitIndex >= turnEndIndex {
		t.Fatalf("limit log counts/order: limits=%d errors=%d limitIndex=%d turnEndIndex=%d records=%#v", limitCount, errorCount, limitIndex, turnEndIndex, records)
	}
}

func marshalLimitTestHistory(t *testing.T, history History) []byte {
	t.Helper()
	encoded, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
