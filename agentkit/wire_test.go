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
			decoded = append(decoded, event.(MessageDone).Message)
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
	// R-4ZYQ-U0AY
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
		completed, ok := events[0].(MessageDone)
		if !ok || len(completed.Message.Blocks) != 1 || completed.Message.Blocks[0].(Text).Text != "Hello" {
			t.Fatalf("%s event = %#v, want MessageDone", test.name, events[0])
		}
	}
}

func TestAnthropicDecodeStreamEmitsToolUseFromGolden(t *testing.T) {
	// R-T44G-AS0I
	response, err := os.Open("testdata/anthropic_messages_tool_call.sse")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Close() }()

	var messages []Message
	for event, decodeErr := range newAnthropicWire(nil).DecodeStream(SSEFrames(response)) {
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		completed, ok := event.(MessageDone)
		if !ok {
			t.Fatalf("event type = %T, want MessageDone", event)
		}
		messages = append(messages, completed.Message)
	}
	if len(messages) != 1 {
		t.Fatalf("decoded %d messages, want one", len(messages))
	}
	assertMixedToolMessage(t, messages[0], anthropicMixedToolBlocks())
}

func TestOpenAIResponsesDecodeStreamEmitsToolUseFromGolden(t *testing.T) {
	// R-T5CC-OJR7
	response, err := os.Open("testdata/openai_responses_tool_call.sse")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Close() }()

	var messages []Message
	for event, decodeErr := range newOpenAIResponsesWire(nil).DecodeStream(SSEFrames(response)) {
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		completed, ok := event.(MessageDone)
		if !ok {
			t.Fatalf("event type = %T, want MessageDone", event)
		}
		messages = append(messages, completed.Message)
	}
	if len(messages) != 1 {
		t.Fatalf("decoded %d messages, want one", len(messages))
	}
	assertMixedToolMessage(t, messages[0], openAIResponsesMixedToolBlocks())
}

func TestOpenAIChatDecodeStreamEmitsToolUseFromGolden(t *testing.T) {
	// R-T6K9-2BHW
	response, err := os.Open("testdata/openai_chat_completions_tool_call.sse")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Close() }()

	var messages []Message
	for event, decodeErr := range newOpenAIChatWire(nil).DecodeStream(SSEFrames(response)) {
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		completed, ok := event.(MessageDone)
		if !ok {
			t.Fatalf("event type = %T, want MessageDone", event)
		}
		messages = append(messages, completed.Message)
	}
	if len(messages) != 1 {
		t.Fatalf("decoded %d messages, want one", len(messages))
	}
	assertMixedToolMessage(t, messages[0], openAIChatMixedToolBlocks())
}

func TestGeminiDecodeStreamEmitsToolUseFromGolden(t *testing.T) {
	// R-T7S5-G38L
	response, err := os.Open("testdata/gemini_generate_content_tool_call.sse")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Close() }()

	var messages []Message
	for event, decodeErr := range newGeminiWire(nil).DecodeStream(SSEFrames(response)) {
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		completed, ok := event.(MessageDone)
		if !ok {
			t.Fatalf("event type = %T, want MessageDone", event)
		}
		messages = append(messages, completed.Message)
	}
	if len(messages) != 1 {
		t.Fatalf("decoded %d messages, want one", len(messages))
	}
	assertMixedToolMessage(t, messages[0], geminiMixedToolBlocks())
}

type mixedToolBlock struct {
	text  string
	id    string
	name  string
	input map[string]any
}

func anthropicMixedToolBlocks() []mixedToolBlock {
	return mixedToolBlocks(
		"toolu_weather_AQID_01", "lookup_weather", map[string]any{"city": "Chicago", "units": "metric"},
		"toolu_route_verbatim-02", "plan_route", map[string]any{"destination": "Museum Campus", "avoid_tolls": true},
	)
}

func openAIResponsesMixedToolBlocks() []mixedToolBlock {
	return mixedToolBlocks(
		"call_weather_verbatim-01", "lookup_weather", map[string]any{"city": "Chicago", "units": "metric"},
		"call_route_AQID_02", "plan_route", map[string]any{"destination": "Museum Campus", "avoid_tolls": true},
	)
}

func openAIChatMixedToolBlocks() []mixedToolBlock {
	return mixedToolBlocks(
		"chatcall_weather_verbatim-01", "lookup_weather_chat", map[string]any{"city": "Chicago", "units": "metric"},
		"chatcall_route_AQID_02", "plan_route_chat", map[string]any{"destination": "Museum Campus", "avoid_tolls": true},
	)
}

func geminiMixedToolBlocks() []mixedToolBlock {
	return mixedToolBlocks(
		"gemini_weather_verbatim-01", "lookup_weather_gemini", map[string]any{"city": "Chicago", "days": float64(3)},
		"gemini_route_AQID_02", "plan_route_gemini", map[string]any{"destination": "Museum Campus", "avoid_tolls": true},
	)
}

func mixedToolBlocks(firstID, firstName string, firstInput map[string]any, secondID, secondName string, secondInput map[string]any) []mixedToolBlock {
	return []mixedToolBlock{
		{text: "Before weather. "},
		{id: firstID, name: firstName, input: firstInput},
		{text: "Then route. "},
		{id: secondID, name: secondName, input: secondInput},
		{text: "Ready."},
	}
}

func assertMixedToolMessage(t *testing.T, message Message, want []mixedToolBlock) {
	t.Helper()
	if message.Role != RoleAssistant {
		t.Errorf("message role = %v, want assistant", message.Role)
	}
	if len(message.Blocks) != len(want) {
		t.Fatalf("decoded %d blocks, want exact mixed sequence of %d: %#v", len(message.Blocks), len(want), message.Blocks)
	}
	for index, expected := range want {
		block := message.Blocks[index]
		if expected.text != "" {
			text, ok := block.(Text)
			if !ok || text.Text != expected.text {
				t.Errorf("block %d = %#v, want Text %q", index, block, expected.text)
			}
			continue
		}
		toolUse, ok := block.(ToolUse)
		if !ok {
			t.Fatalf("block %d type = %T, want ToolUse", index, block)
		}
		if toolUse.ID != expected.id || toolUse.Name != expected.name {
			t.Errorf("block %d id/name = %q/%q, want %q/%q", index, toolUse.ID, toolUse.Name, expected.id, expected.name)
		}
		var input any
		if err := json.Unmarshal(toolUse.Input, &input); err != nil {
			t.Fatalf("block %d input %q is invalid JSON: %v", index, toolUse.Input, err)
		}
		object, ok := input.(map[string]any)
		if !ok {
			t.Fatalf("block %d input decoded as %T, want JSON object (not a JSON-encoded string)", index, input)
		}
		if !reflect.DeepEqual(object, expected.input) {
			t.Errorf("block %d input = %#v, want %#v", index, object, expected.input)
		}
	}
}

func TestEveryWireDecodesMixedToolCallsInVendorOrderWithObjectInput(t *testing.T) {
	// R-T901-TUZA
	// R-TA7Y-7MPZ
	tests := []struct {
		name     string
		wire     WireFormat
		response string
		want     []mixedToolBlock
	}{
		{"anthropic", newAnthropicWire(nil), "testdata/anthropic_messages_tool_call.sse", anthropicMixedToolBlocks()},
		{"responses", newOpenAIResponsesWire(nil), "testdata/openai_responses_tool_call.sse", openAIResponsesMixedToolBlocks()},
		{"chat", newOpenAIChatWire(nil), "testdata/openai_chat_completions_tool_call.sse", openAIChatMixedToolBlocks()},
		{"gemini", newGeminiWire(nil), "testdata/gemini_generate_content_tool_call.sse", geminiMixedToolBlocks()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, err := os.Open(test.response)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Close() }()

			var messages []Message
			for event, decodeErr := range test.wire.DecodeStream(SSEFrames(response)) {
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				completed, ok := event.(MessageDone)
				if !ok {
					t.Fatalf("event type = %T, want MessageDone", event)
				}
				messages = append(messages, completed.Message)
			}
			if len(messages) != 1 {
				t.Fatalf("decoded %d messages, want exactly one", len(messages))
			}
			assertMixedToolMessage(t, messages[0], test.want)
		})
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
		messages = append(messages, event.(MessageDone).Message)
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

func TestCanonicalToolSchemaRendersPortablyAcrossEveryWire(t *testing.T) {
	// R-46P5-NIIA
	schema := json.RawMessage(`{"type":"object","description":"portable input","properties":{"filter":{"type":"object","properties":{"name":{"type":"string","enum":["red","green"],"minLength":3,"maxLength":5,"pattern":"^[a-z]+$"}},"required":["name"]},"scores":{"type":"array","items":{"type":"number","minimum":0,"maximum":1},"minItems":1,"maxItems":3}},"required":["filter","scores"]}`)
	if err := ValidateToolSchema(schema); err != nil {
		t.Fatalf("representative canonical schema rejected: %v", err)
	}
	tool, err := NewToolFromSchema("portable", "all wires", schema, func(context.Context, json.RawMessage) (string, error) {
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var semanticSchema any
	if err := json.Unmarshal(schema, &semanticSchema); err != nil {
		t.Fatal(err)
	}
	for _, wire := range allTestWires() {
		rendered, renderErr := wire.RenderTools([]Tool{tool})
		if renderErr != nil || !json.Valid(rendered) {
			t.Errorf("%T rejected canonical schema: %s, %v", wire, rendered, renderErr)
			continue
		}
		var envelope any
		if err := json.Unmarshal(rendered, &envelope); err != nil {
			t.Errorf("%T rendered invalid JSON: %v", wire, err)
			continue
		}
		if _, trimming := wire.(*geminiWire); trimming {
			schemas := renderedToolSchemas(t, wire, rendered)
			if len(schemas) != 1 {
				t.Errorf("%T rendered %d schemas, want one", wire, len(schemas))
			} else {
				assertJSONNarrowing(t, schemas[0], semanticSchema, "$")
			}
		} else if !containsJSONValue(envelope, semanticSchema) {
			t.Errorf("%T rendering widened or discarded schema semantics: %s", wire, rendered)
		}
	}

	invalid := fixtureTool{name: "invalid", schema: json.RawMessage(`{"type":"object","properties":{"nested":{"type":"object","additionalProperties":false}}}`)}
	var commonDiagnostic string
	for _, wire := range allTestWires() {
		rendered, renderErr := wire.RenderTools([]Tool{invalid})
		if renderErr == nil || rendered != nil {
			t.Errorf("%T rendered invalid schema as %s with error %v", wire, rendered, renderErr)
			continue
		}
		if !strings.Contains(renderErr.Error(), "additionalProperties") {
			t.Errorf("%T diagnostic %q does not name rejected construct", wire, renderErr)
		}
		if commonDiagnostic == "" {
			commonDiagnostic = renderErr.Error()
		} else if renderErr.Error() != commonDiagnostic {
			t.Errorf("%T failed differently: %q, want %q", wire, renderErr, commonDiagnostic)
		}
	}
}

func phase16Tools() []Tool {
	return []Tool{
		fixtureTool{
			name:        "search",
			description: "search ranked records",
			schema:      json.RawMessage(`{"type":"object","description":"search input","properties":{"query":{"type":"string","description":"search phrase","enum":["alpha","beta"],"minLength":2,"maxLength":20,"pattern":"^[a-z]+$","format":"hostname"},"filter":{"type":"object","description":"ranking filter","properties":{"scores":{"type":"array","description":"accepted scores","items":{"type":"number","description":"normalized score","minimum":0,"maximum":1,"exclusiveMinimum":0,"exclusiveMaximum":1,"multipleOf":0.1},"minItems":1,"maxItems":3,"uniqueItems":true}},"required":["scores"]}},"required":["query","filter"]}`),
		},
		fixtureTool{
			name:        "fetch",
			description: "fetch records by id",
			schema:      json.RawMessage(`{"type":"object","description":"fetch input","properties":{"ids":{"type":"array","description":"record ids","items":{"type":"string","description":"record id","enum":["one","two"],"minLength":3,"maxLength":3,"pattern":"^[a-z]+$"},"minItems":1,"maxItems":2}},"required":["ids"]}`),
		},
	}
}

func TestPerWireToolDeclarationGoldenAndSchemaOwnership(t *testing.T) {
	// R-47X2-1A8Z
	// R-494Y-F1ZO
	// R-4E0J-Y4YG
	tests := []struct {
		name    string
		wire    WireFormat
		fixture string
	}{
		{"openai_responses", newOpenAIResponsesWire(nil), "testdata/openai_responses.tools.json"},
		{"openai_chat_completions", newOpenAIChatWire(nil), "testdata/openai_chat_completions.tools.json"},
		{"anthropic_messages", newAnthropicWire(nil), "testdata/anthropic_messages.tools.json"},
		{"gemini_generate_content", newGeminiWire(nil), "testdata/gemini_generate_content.tools.json"},
	}
	tools := phase16Tools()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.wire.RenderTools(tools)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(test.fixture)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("RenderTools bytes = %s\nwant fixture = %s", got, want)
			}
		})
	}
}

func TestRenderToolsNeverWidensCanonicalSchemas(t *testing.T) {
	// R-4BKR-6LH2
	tools := phase16Tools()
	for _, wire := range allTestWires() {
		rendered, err := wire.RenderTools(tools)
		if err != nil {
			t.Fatal(err)
		}
		gotSchemas := renderedToolSchemas(t, wire, rendered)
		for index, tool := range tools {
			var canonical any
			if err := json.Unmarshal(tool.Schema(), &canonical); err != nil {
				t.Fatal(err)
			}
			if _, trimming := wire.(*geminiWire); trimming {
				assertJSONNarrowing(t, gotSchemas[index], canonical, "$")
			} else if !reflect.DeepEqual(gotSchemas[index], canonical) {
				t.Errorf("%T schema %d changed: %#v, want %#v", wire, index, gotSchemas[index], canonical)
			}
		}
	}
}

func TestGeminiOwnsRecursiveSchemaNarrowing(t *testing.T) {
	// R-4ACU-STQD
	tools := phase16Tools()
	originals := make([][]byte, len(tools))
	for index, tool := range tools {
		originals[index] = append([]byte(nil), tool.Schema()...)
	}
	gemini := newGeminiWire(nil)
	rendered, err := gemini.RenderTools(tools)
	if err != nil {
		t.Fatal(err)
	}
	geminiSchemas := renderedToolSchemas(t, gemini, rendered)
	for index, schema := range geminiSchemas {
		for _, rejected := range []string{"exclusiveMinimum", "exclusiveMaximum", "multipleOf", "uniqueItems", "oneOf"} {
			if containsJSONKey(schema, rejected) {
				t.Errorf("Gemini schema %d retains rejected keyword %q: %#v", index, rejected, schema)
			}
		}
		if !containsJSONKey(schema, "description") || !containsJSONKey(schema, "enum") || !containsJSONKey(schema, "minItems") {
			t.Errorf("Gemini schema %d was replaced instead of recursively narrowed: %#v", index, schema)
		}
	}
	if !containsJSONKey(geminiSchemas[0], "minimum") {
		t.Errorf("Gemini recursive narrowing discarded accepted nested numeric constraints: %#v", geminiSchemas[0])
	}
	for index, tool := range tools {
		if !bytes.Equal(tool.Schema(), originals[index]) {
			t.Errorf("Gemini rendering mutated source schema %d: %s, want %s", index, tool.Schema(), originals[index])
		}
	}
	for _, wire := range []WireFormat{newOpenAIResponsesWire(nil), newOpenAIChatWire(nil), newAnthropicWire(nil)} {
		other, err := wire.RenderTools(tools)
		if err != nil {
			t.Fatal(err)
		}
		schemas := renderedToolSchemas(t, wire, other)
		if !containsJSONKey(schemas[0], "pattern") || !containsJSONKey(schemas[0], "uniqueItems") || !containsJSONKey(schemas[0], "multipleOf") {
			t.Errorf("%T leaked Gemini narrowing into schema: %#v", wire, schemas[0])
		}
	}
}

func TestRequestBodiesEmbedRenderedToolsOnceAndInOrder(t *testing.T) {
	// R-47X2-1A8Z
	tools := phase16Tools()
	tests := []struct {
		wire    WireFormat
		fixture string
	}{
		{newAnthropicWire(nil), "testdata/anthropic_messages.tools.json"},
		{newOpenAIResponsesWire(nil), "testdata/openai_responses.tools.json"},
		{newOpenAIChatWire(nil), "testdata/openai_chat_completions.tools.json"},
		{newGeminiWire(nil), "testdata/gemini_generate_content.tools.json"},
	}
	for _, test := range tests {
		wire := test.wire
		body, err := wire.EncodeRequest(RequestState{Tools: tools})
		if err != nil {
			t.Fatal(err)
		}
		var request map[string]json.RawMessage
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		bodyTools, present := request["tools"]
		if !present {
			t.Fatalf("%T request omitted tools: %s", wire, body)
		}
		rendered, err := os.ReadFile(test.fixture)
		if err != nil {
			t.Fatal(err)
		}
		var declaration map[string]json.RawMessage
		if _, gemini := wire.(*geminiWire); gemini {
			if err := json.Unmarshal(rendered, &declaration); err != nil {
				t.Fatal(err)
			}
			rendered = declaration["tools"]
		}
		if !bytes.Equal(bodyTools, rendered) {
			t.Errorf("%T request tools = %s, want its RenderTools shape %s", wire, bodyTools, rendered)
		}
		schemas := renderedToolSchemas(t, wire, func() json.RawMessage {
			if _, gemini := wire.(*geminiWire); gemini {
				wrapped, _ := json.Marshal(map[string]json.RawMessage{"tools": bodyTools})
				return wrapped
			}
			return bodyTools
		}())
		if len(schemas) != 2 {
			t.Fatalf("%T request declaration count = %d, want 2", wire, len(schemas))
		}
		if !bytes.Contains(bodyTools, []byte(`"name":"search"`)) || bytes.Index(bodyTools, []byte(`"name":"search"`)) >= bytes.Index(bodyTools, []byte(`"name":"fetch"`)) {
			t.Errorf("%T reordered tools in request: %s", wire, bodyTools)
		}
	}
}

func TestAnthropicPreservesSuppliedToolOrderInRenderingAndRequest(t *testing.T) {
	// R-5WW1-5TBP
	tools := []Tool{
		fixtureTool{name: "z_last_alphabetically", description: "z", schema: json.RawMessage(`{"type":"object","properties":{}}`)},
		fixtureTool{name: "a_first_alphabetically", description: "a", schema: json.RawMessage(`{"type":"object","properties":{}}`)},
		fixtureTool{name: "m_middle_alphabetically", description: "m", schema: json.RawMessage(`{"type":"object","properties":{}}`)},
	}
	wire := newAnthropicWire(nil)
	rendered, err := wire.RenderTools(tools)
	if err != nil {
		t.Fatal(err)
	}
	var declarations []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rendered, &declarations); err != nil {
		t.Fatal(err)
	}
	var renderedNames []string
	for _, declaration := range declarations {
		renderedNames = append(renderedNames, declaration.Name)
	}
	want := []string{"z_last_alphabetically", "a_first_alphabetically", "m_middle_alphabetically"}
	if !reflect.DeepEqual(renderedNames, want) {
		t.Fatalf("RenderTools order = %v, want %v", renderedNames, want)
	}
	body, err := wire.EncodeRequest(RequestState{Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	var requestNames []string
	for _, declaration := range request.Tools {
		requestNames = append(requestNames, declaration.Name)
	}
	if !reflect.DeepEqual(requestNames, want) {
		t.Fatalf("EncodeRequest order = %v, want %v", requestNames, want)
	}
}

func renderedToolSchemas(t *testing.T, wire WireFormat, rendered json.RawMessage) []any {
	t.Helper()
	var declarations []map[string]any
	switch wire.(type) {
	case *geminiWire:
		var root struct {
			Tools []struct {
				Declarations []map[string]any `json:"functionDeclarations"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(rendered, &root); err != nil {
			t.Fatal(err)
		}
		if len(root.Tools) != 1 {
			t.Fatalf("Gemini tools groups = %d, want exactly one", len(root.Tools))
		}
		declarations = root.Tools[0].Declarations
	default:
		if err := json.Unmarshal(rendered, &declarations); err != nil {
			t.Fatal(err)
		}
	}
	schemas := make([]any, len(declarations))
	for index, declaration := range declarations {
		switch wire.(type) {
		case *openAIResponsesWire:
			schemas[index] = declaration["parameters"]
		case *openAIChatWire:
			schemas[index] = declaration["function"].(map[string]any)["parameters"]
		case *anthropicWire:
			schemas[index] = declaration["input_schema"]
		case *geminiWire:
			schemas[index] = declaration["parameters"]
		}
	}
	return schemas
}

func assertJSONNarrowing(t *testing.T, got, canonical any, path string) {
	t.Helper()
	switch got := got.(type) {
	case map[string]any:
		want, ok := canonical.(map[string]any)
		if !ok {
			t.Fatalf("%s widened object over %#v", path, canonical)
		}
		for key, value := range got {
			canonicalValue, present := want[key]
			if !present {
				t.Errorf("%s introduced keyword %q", path, key)
				continue
			}
			assertJSONNarrowing(t, value, canonicalValue, path+"."+key)
		}
	case []any:
		want, ok := canonical.([]any)
		if !ok || len(got) != len(want) {
			t.Errorf("%s changed array shape from %#v to %#v", path, canonical, got)
			return
		}
		for index := range got {
			assertJSONNarrowing(t, got[index], want[index], path)
		}
	default:
		if !reflect.DeepEqual(got, canonical) {
			t.Errorf("%s changed value from %#v to %#v", path, canonical, got)
		}
	}
}

func containsJSONKey(value any, key string) bool {
	switch value := value.(type) {
	case map[string]any:
		if _, present := value[key]; present {
			return true
		}
		for _, child := range value {
			if containsJSONKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if containsJSONKey(child, key) {
				return true
			}
		}
	}
	return false
}

func containsJSONValue(value, want any) bool {
	if reflect.DeepEqual(value, want) {
		return true
	}
	switch value := value.(type) {
	case map[string]any:
		for _, child := range value {
			if containsJSONValue(child, want) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if containsJSONValue(child, want) {
				return true
			}
		}
	}
	return false
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
				parsed = event.(MessageDone).Message
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
