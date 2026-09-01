package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

var (
	_ WireFormat = (*anthropicWire)(nil)
	_ WireFormat = (*openAIResponsesWire)(nil)
	_ WireFormat = (*openAIChatWire)(nil)
	_ WireFormat = (*geminiWire)(nil)
	_ Framer     = SSEFrames
)

type fixtureTool struct {
	name        string
	description string
	schema      json.RawMessage
}

func (t fixtureTool) Name() string            { return t.name }
func (t fixtureTool) Description() string     { return t.description }
func (t fixtureTool) Schema() json.RawMessage { return t.schema }
func (fixtureTool) isTool()                   {}
func (fixtureTool) Call(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

func allTestWires() []WireFormat {
	return []WireFormat{
		newAnthropicWire(nil),
		newOpenAIResponsesWire(nil),
		newOpenAIChatWire(nil),
		newGeminiWire(nil),
	}
}

func TestWireSelectionIsConstructorOnly(t *testing.T) {
	// R-2WCZ-48BW
	conversationType := reflect.TypeFor[Conversation]()
	wireType := reflect.TypeFor[WireFormat]()
	for index := range conversationType.NumField() {
		field := conversationType.Field(index)
		if field.Type == wireType || field.Type.Implements(wireType) {
			t.Fatalf("Conversation has assignable wire field %q", field.Name)
		}
	}
	for _, wire := range allTestWires() {
		if reflect.TypeOf(wire).Name() != "" || reflect.TypeOf(wire).Elem().Name() == "" {
			t.Fatalf("constructor returned unexpected wire type %T", wire)
		}
	}
}

func TestWireOwnsOnlyBodyGrammar(t *testing.T) {
	// R-YB1L-L7DS
	assertEndpointConcernsAreOutsideWireInterface(t)
	state := RequestState{Model: "endpoint-owned-model", History: History{
		{Role: RoleAssistant, Blocks: []Block{
			Text{Text: "Hello"},
			Reasoning{Text: "think", Provider: json.RawMessage(`{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"think"}]}`)},
			ToolUse{ID: "call_1", Name: "lookup", Input: json.RawMessage(`{"q":"value"}`)},
		}},
		{Role: RoleTool, Blocks: []Block{ToolResult{ToolUseID: "call_1", Content: "answer", IsError: true}}},
	}}
	tool := fixtureTool{name: "lookup", description: "look up", schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)}
	tests := []struct {
		name      string
		wire      WireFormat
		replay    json.RawMessage
		rootKey   string
		want      []string
		wantTools []string
		wantUsage Usage
	}{
		{"anthropic", newAnthropicWire(nil), json.RawMessage(`{"type":"thinking","thinking":"replayed","signature":"sig"}`), "messages", []string{`"thinking":"replayed"`, `"signature":"sig"`, `"type":"tool_use"`, `"type":"tool_result"`, `"is_error":true`}, []string{`"name":"lookup"`, `"input_schema":`}, Usage{InputTokens: 10, CachedTokens: 2, OutputTokens: 4}},
		{"responses", newOpenAIResponsesWire(nil), json.RawMessage(`{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"replayed"}]}`), "input", []string{`"type":"reasoning"`, `"id":"rs_1"`, `"text":"replayed"`, `"arguments":"{\"q\":\"value\"}"`}, []string{`"type":"function"`, `"name":"lookup"`, `"parameters":`}, Usage{InputTokens: 10, CachedTokens: 2, OutputTokens: 4, ReasoningTokens: 3}},
		{"chat", newOpenAIChatWire(nil), json.RawMessage(`{"reasoning_content":"replayed"}`), "messages", []string{`"reasoning_content":"replayed"`, `"tool_calls"`, `"arguments":"{\"q\":\"value\"}"`, `"tool_call_id":"call_1"`}, []string{`"type":"function"`, `"function":{"name":"lookup"`, `"parameters":`}, Usage{InputTokens: 10, CachedTokens: 2, OutputTokens: 4, ReasoningTokens: 3}},
		{"gemini", newGeminiWire(nil), json.RawMessage(`{"text":"replayed","thought":true,"thoughtSignature":"sig"}`), "contents", []string{`"text":"replayed"`, `"thought":true`, `"thoughtSignature":"sig"`, `"functionCall"`, `"args":{"q":"value"}`, `"functionResponse"`, `"isError":true`}, []string{`"functionDeclarations":`, `"name":"lookup"`, `"parameters":`}, Usage{InputTokens: 10, CachedTokens: 2, OutputTokens: 4, ReasoningTokens: 3}},
	}
	for _, test := range tests {
		wire := test.wire
		wireState := state
		wireState.History = append(History(nil), state.History...)
		wireState.History[0].Blocks = append([]Block(nil), state.History[0].Blocks...)
		reasoning := wireState.History[0].Blocks[1].(Reasoning)
		reasoning.Provider = test.replay
		wireState.History[0].Blocks[1] = reasoning
		body, err := wire.EncodeRequest(wireState)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		assertExactRequestRoot(t, test.name, body, test.rootKey)
		for _, fragment := range test.want {
			if !bytes.Contains(body, []byte(fragment)) {
				t.Errorf("%s body %s does not own required grammar fragment %s", test.name, body, fragment)
			}
		}
		if test.name == "responses" {
			assertResponsesCallItemTypes(t, body)
		}
		if test.name == "anthropic" {
			assertAnthropicToolInputIsObject(t, body)
		}
		declarations, err := wire.RenderTools([]Tool{tool})
		if err != nil || !json.Valid(declarations) {
			t.Fatalf("%T tool grammar = %s, %v", wire, declarations, err)
		}
		for _, fragment := range test.wantTools {
			if !bytes.Contains(declarations, []byte(fragment)) {
				t.Errorf("%s tool declarations %s lack wire-owned fragment %s", test.name, declarations, fragment)
			}
		}
		if len(wire.ReservedKeys()) == 0 {
			t.Fatalf("%T did not identify its provider option grammar", wire)
		}
		fixture := wireFixturesByName()[test.name]
		response, err := os.Open(fixture.response)
		if err != nil {
			t.Fatal(err)
		}
		var decoded []Message
		for event, decodeErr := range wire.DecodeStream(SSEFrames(response)) {
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			decoded = append(decoded, event.(Message))
		}
		_ = response.Close()
		if len(decoded) != 1 || decoded[0].Blocks[0].(Text).Text != "Hello" {
			t.Errorf("%s streaming vocabulary decoded %#v, want one completed message", test.name, decoded)
		}
		if got := decodedWireUsage(wire); got != test.wantUsage {
			t.Errorf("%s normalized vendor usage topology to %+v", test.name, got)
		}
	}
}

func assertEndpointConcernsAreOutsideWireInterface(t *testing.T) {
	t.Helper()
	wireType := reflect.TypeFor[WireFormat]()
	endpointType := reflect.TypeFor[endpointConfig]()
	concerns := []struct {
		name  string
		field string
	}{
		{"base URL", "baseURL"},
		{"auth", "auth"},
		{"headers", "headers"},
		{"error envelope", "classifier"},
	}
	for _, concern := range concerns {
		field, ok := endpointType.FieldByName(concern.field)
		if !ok {
			t.Fatalf("endpointConfig lacks independently expected %s field %q", concern.name, concern.field)
		}
		for index := range wireType.NumMethod() {
			method := wireType.Method(index)
			if strings.Contains(strings.ToLower(method.Name), strings.ToLower(concern.field)) {
				t.Errorf("WireFormat method %q owns endpoint %s", method.Name, concern.name)
			}
			for parameter := range method.Type.NumIn() {
				if method.Type.In(parameter) == field.Type {
					t.Errorf("WireFormat.%s accepts endpoint-owned %s type %s", method.Name, concern.name, field.Type)
				}
			}
			for result := range method.Type.NumOut() {
				if method.Type.Out(result) == field.Type {
					t.Errorf("WireFormat.%s returns endpoint-owned %s type %s", method.Name, concern.name, field.Type)
				}
			}
		}
	}
}

func assertExactRequestRoot(t *testing.T, wireName string, body []byte, wantKey string) {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("%s request body is invalid JSON: %v", wireName, err)
	}
	if len(root) != 1 || root[wantKey] == nil {
		t.Fatalf("%s request root keys = %v, want only body-grammar key %q (no base URL, auth, headers, or error envelope)", wireName, reflect.ValueOf(root).MapKeys(), wantKey)
	}
}

func assertAnthropicToolInputIsObject(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Messages []struct {
			Content []struct {
				Type  string         `json:"type"`
				Input map[string]any `json:"input"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	for _, message := range request.Messages {
		for _, content := range message.Content {
			if content.Type == "tool_use" {
				if !reflect.DeepEqual(content.Input, map[string]any{"q": "value"}) {
					t.Fatalf("Anthropic tool_use input = %#v, want object-valued input containing q=value", content.Input)
				}
				return
			}
		}
	}
	t.Fatal("Anthropic request lacks tool_use content with object-valued input")
}

func assertResponsesCallItemTypes(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Input []struct {
			Type string `json:"type"`
		} `json:"input"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"function_call": 1, "function_call_output": 1}
	got := make(map[string]int)
	for _, item := range request.Input {
		if _, relevant := want[item.Type]; relevant {
			got[item.Type]++
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Responses call item types = %v, want exact distinct types %v", got, want)
	}
}

func decodedWireUsage(wire WireFormat) Usage {
	switch wire := wire.(type) {
	case *anthropicWire:
		return wire.lastUsage
	case *openAIResponsesWire:
		return wire.lastUsage
	case *openAIChatWire:
		return wire.lastUsage
	case *geminiWire:
		return wire.lastUsage
	default:
		panic("unknown test wire")
	}
}

func TestDecodeStreamYieldsOnlyCompletedMessages(t *testing.T) {
	// R-2YSR-VRTA
	for _, test := range wireFixtures() {
		wire := test.make(nil)
		response, err := os.Open(test.response)
		if err != nil {
			t.Fatal(err)
		}
		var events []Event
		for event, decodeErr := range wire.DecodeStream(SSEFrames(response)) {
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			events = append(events, event)
		}
		_ = response.Close()
		if len(events) != 1 {
			t.Fatalf("%s yielded %d events, want one completed message", test.name, len(events))
		}
		message, ok := events[0].(Message)
		if !ok || len(message.Blocks) != 1 || message.Blocks[0].(Text).Text != "Hello" {
			t.Fatalf("%s event = %#v, want completed Message", test.name, events[0])
		}
	}
}

func TestDecodeStreamMergesAbsoluteUsageFieldWise(t *testing.T) {
	// R-300O-9JJZ
	wire := newAnthropicWire(nil).(*anthropicWire)
	frames := sequenceFrames(
		`{"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":25}}}`,
		`{"type":"message_delta","delta":{"usage":{"output_tokens":9}}}`,
		`{"type":"message_start","message":{"usage":{"input_tokens":120}}}`,
		`{"type":"message_stop"}`,
	)
	for _, err := range wire.DecodeStream(frames) {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := Usage{InputTokens: 95, CachedTokens: 25, OutputTokens: 9}
	if wire.lastUsage != want {
		t.Fatalf("usage = %+v, want field-wise absolute merge %+v", wire.lastUsage, want)
	}
}

func TestDecodeStreamUsesClassifierForInBandError(t *testing.T) {
	// R-318K-NBAO
	want := &Error{Category: CategoryRateLimit, Status: http.StatusOK, Code: "slow", Message: "wait"}
	called := 0
	classifier := func(status int, _ http.Header, body []byte) error {
		called++
		if status != http.StatusOK || !bytes.Equal(body, []byte(`{"vendor_notice":{"code":"slow"}}`)) {
			return nil
		}
		return want
	}
	wire := newOpenAIResponsesWire(classifier)
	var got error
	for event, err := range wire.DecodeStream(sequenceFrames(`{"vendor_notice":{"code":"slow"}}`, `{"type":"response.output_text.delta","delta":"ignored"}`)) {
		if event != nil {
			t.Fatalf("in-band error became event %#v", event)
		}
		got = err
	}
	if called != 1 || !errors.Is(got, want) {
		t.Fatalf("classifier calls = %d, error = %v, want one call and authoritative %v", called, got, want)
	}
}

func TestFramingIsSeparableAndErrorsPropagate(t *testing.T) {
	// R-0UPN-4AP7
	want := errors.New("binary framing failed")
	alternate := Framer(func(io.Reader) iter.Seq2[[]byte, error] {
		return func(yield func([]byte, error) bool) {
			if !yield([]byte(`{"type":"response.output_text.delta","delta":"Hello"}`), nil) {
				return
			}
			if !yield([]byte(`{"type":"response.completed","response":{"usage":{}}}`), nil) {
				return
			}
			yield(nil, want)
		}
	})
	wire := newOpenAIResponsesWire(nil)
	var got error
	var messages []Message
	for event, err := range wire.DecodeStream(alternate(strings.NewReader("not SSE"))) {
		if err != nil {
			got = err
			continue
		}
		messages = append(messages, event.(Message))
	}
	if len(messages) != 1 || messages[0].Blocks[0].(Text).Text != "Hello" {
		t.Fatalf("alternate framer messages = %#v, want one completed unchanged-wire message", messages)
	}
	if !errors.Is(got, want) {
		t.Fatalf("decode error = %v, want alternate-framer error %v", got, want)
	}
}

func TestRenderToolsRejectsOutsideCanonicalSubset(t *testing.T) {
	// R-34W9-SMIR
	tool := fixtureTool{name: "recursive", schema: json.RawMessage(`{"type":"object","properties":{"child":{"$ref":"#"}}}`)}
	for _, wire := range allTestWires() {
		if rendered, err := wire.RenderTools([]Tool{tool}); err == nil || rendered != nil {
			t.Fatalf("%T RenderTools = %s, %v; want pre-send rejection", wire, rendered, err)
		}
	}
}

func TestSupportedSettingsAreEncodedByOwningWireGrammar(t *testing.T) {
	// R-3VQ2-7KU1
	tests := []struct {
		name     string
		wire     WireFormat
		settings Settings
		want     string
	}{
		{
			name:     "anthropic budget and named tool",
			wire:     newAnthropicWire(nil),
			settings: Settings{Reasoning: ReasoningConfig{Mode: ReasoningBudget, Budget: 4096}, ToolChoice: ToolChoice{Mode: ToolChoiceTool, Name: "lookup"}},
			want:     `{"messages":[],"thinking":{"type":"enabled","budget_tokens":4096},"tool_choice":{"type":"tool","name":"lookup"}}` + "\n",
		},
		{
			name:     "responses effort and no tools",
			wire:     newOpenAIResponsesWire(nil),
			settings: Settings{Reasoning: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortHigh}, ToolChoice: ToolChoice{Mode: ToolChoiceNone}},
			want:     `{"input":[],"reasoning":{"effort":"high"},"tool_choice":"none"}` + "\n",
		},
		{
			name:     "chat off and named tool",
			wire:     newOpenAIChatWire(nil),
			settings: Settings{Reasoning: ReasoningConfig{Mode: ReasoningOff}, ToolChoice: ToolChoice{Mode: ToolChoiceTool, Name: "lookup"}},
			want:     `{"messages":[],"reasoning_effort":"none","tool_choice":{"type":"function","function":{"name":"lookup"}}}` + "\n",
		},
		{
			name:     "gemini bare on and required tool",
			wire:     newGeminiWire(nil),
			settings: Settings{Reasoning: ReasoningConfig{Mode: ReasoningOn}, ToolChoice: ToolChoice{Mode: ToolChoiceRequired}},
			want:     `{"contents":[],"generationConfig":{"thinkingConfig":{"thinkingBudget":-1}},"toolConfig":{"functionCallingConfig":{"mode":"ANY"}}}` + "\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator, ok := test.wire.(interface{ validateSettings(Settings) error })
			if !ok {
				t.Fatalf("%T has no body-grammar capability declaration", test.wire)
			}
			if err := validator.validateSettings(test.settings); err != nil {
				t.Fatalf("supported settings rejected: %v", err)
			}
			body, err := test.wire.EncodeRequest(RequestState{Model: "opaque-model-has-no-capability-role", Settings: test.settings})
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != test.want {
				t.Fatalf("encoded body = %s, want exact owning-wire grammar %s", body, test.want)
			}
		})
	}
}

type wireFixture struct {
	name     string
	response string
	request  string
	make     func(wireClassifier) WireFormat
}

func wireFixtures() []wireFixture {
	return []wireFixture{
		{"anthropic_messages", "testdata/anthropic_messages.sse", "testdata/anthropic_messages.request.json", newAnthropicWire},
		{"openai_responses", "testdata/openai_responses.sse", "testdata/openai_responses.request.json", newOpenAIResponsesWire},
		{"openai_chat_completions", "testdata/openai_chat_completions.sse", "testdata/openai_chat_completions.request.json", newOpenAIChatWire},
		{"gemini_generate_content", "testdata/gemini_generate_content.sse", "testdata/gemini_generate_content.request.json", newGeminiWire},
	}
}

func wireFixturesByName() map[string]wireFixture {
	fixtures := wireFixtures()
	return map[string]wireFixture{
		"anthropic": fixtures[0],
		"responses": fixtures[1],
		"chat":      fixtures[2],
		"gemini":    fixtures[3],
	}
}

func TestEveryWireRoundTripsCapturedMessageBytes(t *testing.T) {
	// R-3646-6E9G
	for _, test := range wireFixtures() {
		t.Run(test.name, func(t *testing.T) {
			response, err := os.Open(test.response)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Close() }()
			wire := test.make(nil)
			var parsed Message
			for event, decodeErr := range wire.DecodeStream(SSEFrames(response)) {
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				parsed = event.(Message)
			}
			got, err := wire.EncodeRequest(RequestState{History: History{parsed}})
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(test.request)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("request bytes = %q, want fixture bytes %q", got, want)
			}
		})
	}
}

func sequenceFrames(frames ...string) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		for _, frame := range frames {
			if !yield([]byte(frame), nil) {
				return
			}
		}
	}
}
