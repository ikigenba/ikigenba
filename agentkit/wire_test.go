package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"reflect"
	"slices"
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

type portableOutputSchemaWire interface {
	renderOutputSchema(json.RawMessage) (json.RawMessage, error)
}

func TestPortableOutputSchemaRetainsGrammarAndClosesObjects(t *testing.T) {
	// R-TW65-3I2H
	schema := json.RawMessage(`{"type":"object","description":"result","properties":{"choice":{"type":"string","description":"selected value","enum":["alpha","beta"],"const":"alpha"},"nested":{"type":"object","properties":{"flag":{"type":"boolean"}},"required":["flag"],"additionalProperties":false},"rows":{"type":"array","items":{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}},"maybe":{"anyOf":[{"type":"object","properties":{"label":{"type":"string"}},"required":["label"]},{"type":"null"}]},"definition":{"$ref":"#/$defs/Record"}},"required":["choice","nested","rows","maybe","definition"],"$defs":{"Record":{"type":"object","description":"stored record","properties":{"code":{"type":"string"}},"required":["code"]}}}`)
	want := decodeOutputSchemaTestDocument(t, json.RawMessage(`{"type":"object","description":"result","additionalProperties":false,"properties":{"choice":{"type":"string","description":"selected value","enum":["alpha","beta"],"const":"alpha"},"nested":{"type":"object","properties":{"flag":{"type":"boolean"}},"required":["flag"],"additionalProperties":false},"rows":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"integer"}},"required":["id"]}},"maybe":{"anyOf":[{"type":"object","additionalProperties":false,"properties":{"label":{"type":"string"}},"required":["label"]},{"type":"null"}]},"definition":{"$ref":"#/$defs/Record"}},"required":["choice","nested","rows","maybe","definition"],"$defs":{"Record":{"type":"object","description":"stored record","additionalProperties":false,"properties":{"code":{"type":"string"}},"required":["code"]}}}`))
	original := append([]byte(nil), schema...)

	for _, wire := range allTestWires() {
		renderer, ok := wire.(portableOutputSchemaWire)
		if !ok {
			t.Fatalf("%T does not expose the private portable output renderer", wire)
		}
		got, err := renderer.renderOutputSchema(schema)
		if err != nil {
			t.Fatalf("%T: %v", wire, err)
		}
		again, err := renderer.renderOutputSchema(schema)
		if err != nil {
			t.Fatalf("%T second render: %v", wire, err)
		}
		if !bytes.Equal(got, again) {
			t.Errorf("%T rendered nondeterministically:\n%s\n%s", wire, got, again)
		}
		if !bytes.Equal(schema, original) {
			t.Fatalf("%T mutated source bytes: %s, want %s", wire, schema, original)
		}
		document := decodeOutputSchemaTestDocument(t, got)
		if !reflect.DeepEqual(document, want) {
			t.Errorf("%T grammar render = %#v, want %#v", wire, document, want)
		}
		assertEveryOutputObjectClosed(t, document, "$")
	}
}

func TestPortableOutputSchemaMovesConstraintsToProse(t *testing.T) {
	// R-TXE1-H9T6
	schema := json.RawMessage(`{"type":"object","properties":{"number":{"type":"number","description":"Existing numeric rule.","minimum":-1e+400,"maximum":9.99e+999,"exclusiveMinimum":-2,"exclusiveMaximum":10,"multipleOf":1e-300},"text":{"type":"string","minLength":2,"maxLength":8,"pattern":"^[A-Z]+\\s?$","format":"custom/value"},"list":{"type":"array","minItems":1,"maxItems":4,"uniqueItems":true,"items":{"type":"string","description":"Item.","pattern":"^x+$"}},"repeatable":{"type":"array","uniqueItems":false,"items":{"type":"integer","minimum":-5}},"maybe":{"anyOf":[{"type":"number","maximum":1e+400},{"type":"null"}]},"defined":{"$ref":"#/$defs/Count"}},"required":["number","text","list","repeatable","maybe","defined"],"$defs":{"Count":{"type":"integer","minimum":0,"multipleOf":2}}}`)
	tests := []struct {
		name    string
		wire    WireFormat
		fixture string
	}{
		{"anthropic_messages", newAnthropicWire(nil), "testdata/anthropic_messages.output_schema.json"},
		{"openai_responses", newOpenAIResponsesWire(nil), "testdata/openai_responses.output_schema.json"},
		{"openai_chat_completions", newOpenAIChatWire(nil), "testdata/openai_chat_completions.output_schema.json"},
		{"gemini_generate_content", newGeminiWire(nil), "testdata/gemini_generate_content.output_schema.json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.wire.(portableOutputSchemaWire).renderOutputSchema(schema)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(test.fixture)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("rendered bytes = %s\nwant fixture = %s", got, want)
			}
			document := decodeOutputSchemaTestDocument(t, got)
			for _, keyword := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "minLength", "maxLength", "pattern", "format", "minItems", "maxItems", "uniqueItems"} {
				if containsJSONKey(document, keyword) {
					t.Errorf("rendered schema retains constraint keyword %q", keyword)
				}
			}
			assertPhase23ConstraintDescriptions(t, document)
		})
	}
}

func TestPortableOutputSchemaRejectsInvalidInputPerWire(t *testing.T) {
	invalid := json.RawMessage(`{"type":"object","properties":{"bad":{"allOf":[{"type":"string"}]}},"required":["bad"]}`)
	for _, wire := range allTestWires() {
		got, err := wire.(portableOutputSchemaWire).renderOutputSchema(invalid)
		if err == nil || got != nil {
			t.Errorf("%T rendered invalid output schema as %s with error %v", wire, got, err)
			continue
		}
		if !strings.Contains(err.Error(), "allOf") {
			t.Errorf("%T diagnostic %q does not name allOf", wire, err)
		}
	}
}

func TestAnthropicMessagesEmbedsNativeOutputContract(t *testing.T) {
	// R-TYLX-V1JV
	schema := json.RawMessage(`{"type":"object","description":"report","properties":{"profile":{"$ref":"#/$defs/Profile"},"items":{"type":"array","items":{"type":"object","properties":{"score":{"type":"integer","minimum":0},"label":{"type":"string"}},"required":["score","label"]}}},"required":["profile","items"],"$defs":{"Profile":{"type":"object","properties":{"name":{"type":"string","minLength":2},"contact":{"type":"object","properties":{"active":{"type":"boolean"}},"required":["active"]}},"required":["name","contact"]}}}`)
	original := append([]byte(nil), schema...)
	state := RequestState{
		Model:   "claude-sonnet-fixture",
		History: History{{Role: RoleUser, Blocks: []Block{Text{Text: "Return the report."}}}},
		Output:  &OutputContract{Schema: schema, MaxAttempts: 7},
	}

	body, err := newAnthropicWire(nil).EncodeRequest(state)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/anthropic_messages.output_contract.request.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, want) {
		t.Fatalf("encoded request = %s\nwant fixture = %s", body, want)
	}
	if len(body) == 0 || body[len(body)-1] != '\n' {
		t.Fatal("encoded request has no trailing newline")
	}
	if !bytes.Equal(schema, original) {
		t.Fatalf("EncodeRequest mutated source schema: %s, want %s", schema, original)
	}

	document := decodeOutputSchemaTestDocument(t, body)
	if got := sortedJSONKeys(document); !slices.Equal(got, []string{"messages", "model", "output_config"}) {
		t.Fatalf("top-level keys = %v", got)
	}
	outputConfig, ok := document["output_config"].(map[string]any)
	if !ok || !slices.Equal(sortedJSONKeys(outputConfig), []string{"format"}) {
		t.Fatalf("output_config = %#v", document["output_config"])
	}
	format, ok := outputConfig["format"].(map[string]any)
	if !ok || !slices.Equal(sortedJSONKeys(format), []string{"schema", "type"}) {
		t.Fatalf("output_config.format = %#v", outputConfig["format"])
	}
	if format["type"] != "json_schema" {
		t.Fatalf("output_config.format.type = %#v", format["type"])
	}
	rendered, ok := format["schema"].(map[string]any)
	if !ok {
		t.Fatalf("output_config.format.schema is not an object: %#v", format["schema"])
	}
	assertEveryOutputObjectClosed(t, rendered, "$.output_config.format.schema")
	properties := rendered["properties"].(map[string]any)
	if got := properties["profile"].(map[string]any)["$ref"]; got != "#/$defs/Profile" {
		t.Errorf("rendered reference = %#v", got)
	}
	score := properties["items"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["score"].(map[string]any)
	if containsJSONKey(rendered, "minimum") || score["description"] != "Value must be >= 0." {
		t.Errorf("rendered constrained score = %#v", score)
	}
	name := rendered["$defs"].(map[string]any)["Profile"].(map[string]any)["properties"].(map[string]any)["name"].(map[string]any)
	if containsJSONKey(rendered, "minLength") || name["description"] != "Length must be >= 2." {
		t.Errorf("rendered constrained name = %#v", name)
	}
	if containsJSONKey(document, "MaxAttempts") || containsJSONKey(document, "max_attempts") {
		t.Fatalf("request leaked MaxAttempts: %s", body)
	}

	endpoint, err := NewEndpoint("https://anthropic.invalid/v1/messages", wireLoopAuth{})
	if err != nil {
		t.Fatal(err)
	}
	provider := newComposedProvider(newAnthropicWire(nil), endpoint, Identity{})
	request, err := provider.BuildRequest(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	for name := range request.Header {
		if strings.EqualFold(name, "anthropic-beta") {
			t.Fatalf("ordinary Anthropic request emitted beta header %q", name)
		}
	}

	invalid := RequestState{Output: &OutputContract{Schema: json.RawMessage(`{"type":"object","properties":{"bad":{"allOf":[{"type":"string"}]}},"required":["bad"]}`)}}
	invalidBody, invalidErr := newAnthropicWire(nil).EncodeRequest(invalid)
	if invalidErr == nil || invalidBody != nil {
		t.Fatalf("invalid output schema encoded as %s with error %v", invalidBody, invalidErr)
	}
	if !strings.Contains(invalidErr.Error(), "Anthropic output schema") || !strings.Contains(invalidErr.Error(), "allOf") {
		t.Fatalf("invalid output schema error is not useful: %v", invalidErr)
	}
}

func sortedJSONKeys(document map[string]any) []string {
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func decodeOutputSchemaTestDocument(t *testing.T, data []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	return document
}

func assertEveryOutputObjectClosed(t *testing.T, schema map[string]any, path string) {
	t.Helper()
	if schema["type"] == "object" && schema["additionalProperties"] != false {
		t.Errorf("object %s is not closed: %#v", path, schema)
	}
	for _, keyword := range []string{"properties", "$defs"} {
		if children, ok := schema[keyword].(map[string]any); ok {
			for name, child := range children {
				assertEveryOutputObjectClosed(t, child.(map[string]any), path+"."+keyword+"."+name)
			}
		}
	}
	if item, ok := schema["items"].(map[string]any); ok {
		assertEveryOutputObjectClosed(t, item, path+".items")
	}
	if branches, ok := schema["anyOf"].([]any); ok {
		for index, branch := range branches {
			assertEveryOutputObjectClosed(t, branch.(map[string]any), fmt.Sprintf("%s.anyOf[%d]", path, index))
		}
	}
}

func assertPhase23ConstraintDescriptions(t *testing.T, document map[string]any) {
	t.Helper()
	properties := document["properties"].(map[string]any)
	descriptions := map[string]string{
		"number":     `Existing numeric rule. Value must be >= -1e+400. Value must be <= 9.99e+999. Value must be > -2. Value must be < 10. Value must be a multiple of 1e-300.`,
		"text":       `Length must be >= 2. Length must be <= 8. Value must match pattern "^[A-Z]+\\s?$". Value must use format "custom/value".`,
		"list":       `Item count must be >= 1. Item count must be <= 4. Items must be unique.`,
		"repeatable": `Items may repeat.`,
	}
	for name, want := range descriptions {
		if got := properties[name].(map[string]any)["description"]; got != want {
			t.Errorf("%s description = %q, want %q", name, got, want)
		}
	}
	if got := properties["list"].(map[string]any)["items"].(map[string]any)["description"]; got != `Item. Value must match pattern "^x+$".` {
		t.Errorf("list item description = %q", got)
	}
	if got := properties["repeatable"].(map[string]any)["items"].(map[string]any)["description"]; got != `Value must be >= -5.` {
		t.Errorf("repeatable item description = %q", got)
	}
	maybe := properties["maybe"].(map[string]any)["anyOf"].([]any)[0].(map[string]any)
	if got := maybe["description"]; got != `Value must be <= 1e+400.` {
		t.Errorf("nullable branch description = %q", got)
	}
	definition := document["$defs"].(map[string]any)["Count"].(map[string]any)
	if got := definition["description"]; got != `Value must be >= 0. Value must be a multiple of 2.` {
		t.Errorf("definition description = %q", got)
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
		assertExactRequestRoot(t, test.name, body, test.rootKey, state.Model)
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

func assertExactRequestRoot(t *testing.T, wireName string, body []byte, wantKey, wantModel string) {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("%s request body is invalid JSON: %v", wireName, err)
	}
	wantKeys := 1
	if wireName != "gemini" {
		wantKeys++
		var model string
		if err := json.Unmarshal(root["model"], &model); err != nil || model != wantModel {
			t.Fatalf("%s request model = %q, %v; want %q", wireName, model, err, wantModel)
		}
	}
	if len(root) != wantKeys || root[wantKey] == nil {
		t.Fatalf("%s request root keys = %v, want body-grammar key %q and its grammar-owned fields only", wireName, reflect.ValueOf(root).MapKeys(), wantKey)
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
		if bytes.Index(bodyTools, []byte(`"name":"search"`)) >= bytes.Index(bodyTools, []byte(`"name":"fetch"`)) {
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
			want:     `{"model":"opaque-model-has-no-capability-role","messages":[],"thinking":{"type":"enabled","budget_tokens":4096},"tool_choice":{"type":"tool","name":"lookup"}}` + "\n",
		},
		{
			name:     "responses effort and no tools",
			wire:     newOpenAIResponsesWire(nil),
			settings: Settings{Reasoning: ReasoningConfig{Mode: ReasoningEffort, Effort: EffortHigh}, ToolChoice: ToolChoice{Mode: ToolChoiceNone}},
			want:     `{"model":"opaque-model-has-no-capability-role","input":[],"reasoning":{"effort":"high"},"tool_choice":"none"}` + "\n",
		},
		{
			name:     "chat off and named tool",
			wire:     newOpenAIChatWire(nil),
			settings: Settings{Reasoning: ReasoningConfig{Mode: ReasoningOff}, ToolChoice: ToolChoice{Mode: ToolChoiceTool, Name: "lookup"}},
			want:     `{"model":"opaque-model-has-no-capability-role","messages":[],"reasoning_effort":"none","tool_choice":{"type":"function","function":{"name":"lookup"}}}` + "\n",
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

type wireLoopAuth struct{}

func (wireLoopAuth) Apply(context.Context, *http.Request, []byte) error { return nil }

type wireLoopTransport struct {
	responses [][]byte
	requests  [][]byte
}

func (transport *wireLoopTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	transport.requests = append(transport.requests, append([]byte(nil), body...))
	index := len(transport.requests) - 1
	if index >= len(transport.responses) {
		return nil, fmt.Errorf("unexpected HTTP round-trip %d", index+1)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(bytes.NewReader(transport.responses[index])),
	}, nil
}

type wireLoopCall struct {
	id     string
	name   string
	input  map[string]any
	result string
}

func TestEveryWireCompletesFixtureDrivenToolLoop(t *testing.T) {
	// R-TF3J-QPOR
	tests := []struct {
		name       string
		wire       KnownWire
		toolCalls  string
		final      string
		blocks     []mixedToolBlock
		assertBody func(*testing.T, []byte, []wireLoopCall)
	}{
		{"anthropic_messages", KnownWireAnthropicMessages, "testdata/anthropic_messages_tool_call.sse", "testdata/anthropic_messages.sse", anthropicMixedToolBlocks(), assertAnthropicLoopBody},
		{"openai_responses", KnownWireOpenAIResponses, "testdata/openai_responses_tool_call.sse", "testdata/openai_responses.sse", openAIResponsesMixedToolBlocks(), assertResponsesLoopBody},
		{"openai_chat_completions", KnownWireOpenAIChat, "testdata/openai_chat_completions_tool_call.sse", "testdata/openai_chat_completions.sse", openAIChatMixedToolBlocks(), assertChatLoopBody},
		{"gemini_generate_content", KnownWireGemini, "testdata/gemini_generate_content_tool_call.sse", "testdata/gemini_generate_content.sse", geminiMixedToolBlocks(), assertGeminiLoopBody},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := loopCalls(test.blocks)
			tools := make([]Tool, 0, len(calls))
			dispatches := make([]int, len(calls))
			for index, call := range calls {
				schema := schemaForLoopInput(t, call.input)
				tool, err := NewToolFromSchema(call.name, "offline fixture tool", schema, func(_ context.Context, raw json.RawMessage) (string, error) {
					var input map[string]any
					if err := json.Unmarshal(raw, &input); err != nil {
						return "", err
					}
					if !reflect.DeepEqual(input, call.input) {
						return "", fmt.Errorf("dispatch input = %#v, want %#v", input, call.input)
					}
					dispatches[index]++
					return call.result, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				tools = append(tools, tool)
			}

			first, err := os.ReadFile(test.toolCalls)
			if err != nil {
				t.Fatal(err)
			}
			second, err := os.ReadFile(test.final)
			if err != nil {
				t.Fatal(err)
			}
			transport := &wireLoopTransport{responses: [][]byte{first, second}}
			endpoint, err := NewEndpoint("https://offline.invalid/v1/generate", wireLoopAuth{}, WithHTTPClient(&http.Client{Transport: transport}))
			if err != nil {
				t.Fatal(err)
			}
			conversation, err := NewForWire(test.wire, endpoint, "fixture-model", Config{Tools: tools})
			if err != nil {
				t.Fatal(err)
			}

			var events []Event
			stream := conversation.Send(context.Background(), Text{Text: "use both fixture tools"})
			for event := range stream.Events() {
				events = append(events, event)
			}
			if stream.Err() != nil {
				t.Fatal(stream.Err())
			}
			assertLoopEvents(t, events, test.blocks, calls)
			for index, count := range dispatches {
				if count != 1 {
					t.Errorf("tool %q dispatched %d times, want once", calls[index].name, count)
				}
			}
			if len(transport.requests) != 2 {
				t.Fatalf("HTTP round-trips = %d, want exactly two", len(transport.requests))
			}
			test.assertBody(t, transport.requests[1], calls)
		})
	}
}

func loopCalls(blocks []mixedToolBlock) []wireLoopCall {
	var calls []wireLoopCall
	for _, block := range blocks {
		if block.id != "" {
			calls = append(calls, wireLoopCall{
				id: block.id, name: block.name, input: block.input,
				result: "offline result for " + block.id,
			})
		}
	}
	return calls
}

func schemaForLoopInput(t *testing.T, input map[string]any) json.RawMessage {
	t.Helper()
	properties := make(map[string]any, len(input))
	required := make([]string, 0, len(input))
	for name, value := range input {
		kind := ""
		switch value.(type) {
		case string:
			kind = "string"
		case bool:
			kind = "boolean"
		case float64:
			kind = "number"
		default:
			t.Fatalf("unsupported fixture argument %q of type %T", name, value)
		}
		properties[name] = map[string]any{"type": kind}
		required = append(required, name)
	}
	slices.Sort(required)
	schema, err := json.Marshal(map[string]any{"type": "object", "properties": properties, "required": required})
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func assertLoopEvents(t *testing.T, events []Event, blocks []mixedToolBlock, calls []wireLoopCall) {
	t.Helper()
	if len(events) != 2+len(calls)*2 {
		t.Fatalf("events = %#v, want MessageDone, calls, returns, MessageDone", events)
	}
	first, ok := events[0].(MessageDone)
	if !ok {
		t.Fatalf("event 0 = %T, want MessageDone", events[0])
	}
	assertMixedToolMessage(t, first.Message, blocks)
	for index, want := range calls {
		call, ok := events[index+1].(ToolCall)
		if !ok {
			t.Fatalf("event %d = %T, want ToolCall", index+1, events[index+1])
		}
		assertLoopToolUse(t, call.Use, want)
		returned, ok := events[index+1+len(calls)].(ToolReturn)
		if !ok {
			t.Fatalf("event %d = %T, want ToolReturn", index+1+len(calls), events[index+1+len(calls)])
		}
		if returned.Result.ToolUseID != want.id || returned.Result.Content != want.result || returned.Result.IsError {
			t.Errorf("tool return = %#v, want id %q content %q and no error", returned.Result, want.id, want.result)
		}
	}
	final, ok := events[len(events)-1].(MessageDone)
	if !ok || final.Message.Role != RoleAssistant || len(final.Message.Blocks) != 1 {
		t.Fatalf("final event = %#v, want one-block assistant MessageDone", events[len(events)-1])
	}
	text, ok := final.Message.Blocks[0].(Text)
	if !ok || text.Text != "Hello" {
		t.Fatalf("final block = %#v, want Text Hello", final.Message.Blocks[0])
	}
}

func assertLoopToolUse(t *testing.T, use ToolUse, want wireLoopCall) {
	t.Helper()
	var input map[string]any
	if err := json.Unmarshal(use.Input, &input); err != nil {
		t.Fatal(err)
	}
	if use.ID != want.id || use.Name != want.name || !reflect.DeepEqual(input, want.input) {
		t.Errorf("tool call = id %q name %q input %#v, want %q %q %#v", use.ID, use.Name, input, want.id, want.name, want.input)
	}
}

func loopBodyObject(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatalf("second request is not vendor JSON: %v\n%s", err, body)
	}
	return object
}

func objectSlice(t *testing.T, value any, context string) []map[string]any {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %T, want array", context, value)
	}
	objects := make([]map[string]any, len(items))
	for index, item := range items {
		objects[index], ok = item.(map[string]any)
		if !ok {
			t.Fatalf("%s[%d] = %T, want object", context, index, item)
		}
	}
	return objects
}

func assertAnthropicLoopBody(t *testing.T, body []byte, calls []wireLoopCall) {
	t.Helper()
	root := loopBodyObject(t, body)
	var uses, results []map[string]any
	for _, message := range objectSlice(t, root["messages"], "messages") {
		for _, block := range objectSlice(t, message["content"], "messages[].content") {
			switch block["type"] {
			case "tool_use":
				uses = append(uses, block)
			case "tool_result":
				results = append(results, block)
			}
		}
	}
	assertObjectCalls(t, uses, results, calls, "id", "name", "input", "tool_use_id", "content")
}

func assertResponsesLoopBody(t *testing.T, body []byte, calls []wireLoopCall) {
	t.Helper()
	root := loopBodyObject(t, body)
	var uses, results []map[string]any
	for _, item := range objectSlice(t, root["input"], "input") {
		switch item["type"] {
		case "function_call":
			uses = append(uses, item)
		case "function_call_output":
			results = append(results, item)
		}
	}
	assertStringArgumentCalls(t, uses, results, calls, "call_id", "name", "arguments", "call_id", "output")
}

func assertChatLoopBody(t *testing.T, body []byte, calls []wireLoopCall) {
	t.Helper()
	root := loopBodyObject(t, body)
	var uses, results []map[string]any
	for _, message := range objectSlice(t, root["messages"], "messages") {
		if message["role"] == "assistant" && message["tool_calls"] != nil {
			uses = append(uses, objectSlice(t, message["tool_calls"], "assistant.tool_calls")...)
		}
		if message["role"] == "tool" {
			results = append(results, message)
		}
	}
	if len(uses) != len(calls) || len(results) != len(calls) {
		t.Fatalf("chat second request has %d calls and %d results, want %d each", len(uses), len(results), len(calls))
	}
	for index, want := range calls {
		function, ok := uses[index]["function"].(map[string]any)
		if !ok {
			t.Fatalf("tool_calls[%d].function = %T, want object", index, uses[index]["function"])
		}
		assertStringArgumentCall(t, uses[index]["id"], function["name"], function["arguments"], want)
		if results[index]["tool_call_id"] != want.id || results[index]["content"] != want.result {
			t.Errorf("tool message %d = %#v, want id %q content %q", index, results[index], want.id, want.result)
		}
	}
}

func assertGeminiLoopBody(t *testing.T, body []byte, calls []wireLoopCall) {
	t.Helper()
	root := loopBodyObject(t, body)
	var uses, results []map[string]any
	for _, content := range objectSlice(t, root["contents"], "contents") {
		for _, part := range objectSlice(t, content["parts"], "contents[].parts") {
			if call, ok := part["functionCall"].(map[string]any); ok {
				uses = append(uses, call)
			}
			if result, ok := part["functionResponse"].(map[string]any); ok {
				results = append(results, result)
			}
		}
	}
	if len(uses) != len(calls) || len(results) != len(calls) {
		t.Fatalf("gemini second request has %d calls and %d results, want %d each", len(uses), len(results), len(calls))
	}
	for index, want := range calls {
		if uses[index]["id"] != want.id || uses[index]["name"] != want.name || !reflect.DeepEqual(uses[index]["args"], want.input) {
			t.Errorf("functionCall %d = %#v, want id/name/args for %#v", index, uses[index], want)
		}
		response, ok := results[index]["response"].(map[string]any)
		if !ok {
			t.Fatalf("functionResponse %d response = %T, want object", index, results[index]["response"])
		}
		if results[index]["id"] != want.id || results[index]["name"] != want.name || response["output"] != want.result {
			t.Errorf("functionResponse %d = %#v, want correlated %#v", index, results[index], want)
		}
	}
}

func assertObjectCalls(t *testing.T, uses, results []map[string]any, calls []wireLoopCall, useID, useName, useArgs, resultID, resultContent string) {
	t.Helper()
	if len(uses) != len(calls) || len(results) != len(calls) {
		t.Fatalf("second request has %d calls and %d results, want %d each", len(uses), len(results), len(calls))
	}
	for index, want := range calls {
		if uses[index][useID] != want.id || uses[index][useName] != want.name || !reflect.DeepEqual(uses[index][useArgs], want.input) {
			t.Errorf("call %d = %#v, want %#v", index, uses[index], want)
		}
		if results[index][resultID] != want.id || results[index][resultContent] != want.result || results[index]["is_error"] == true {
			t.Errorf("result %d = %#v, want %#v", index, results[index], want)
		}
	}
}

func assertStringArgumentCalls(t *testing.T, uses, results []map[string]any, calls []wireLoopCall, useID, useName, useArgs, resultID, resultContent string) {
	t.Helper()
	if len(uses) != len(calls) || len(results) != len(calls) {
		t.Fatalf("second request has %d calls and %d results, want %d each", len(uses), len(results), len(calls))
	}
	for index, want := range calls {
		assertStringArgumentCall(t, uses[index][useID], uses[index][useName], uses[index][useArgs], want)
		if results[index][resultID] != want.id || results[index][resultContent] != want.result {
			t.Errorf("result %d = %#v, want %#v", index, results[index], want)
		}
	}
}

func assertStringArgumentCall(t *testing.T, id, name, arguments any, want wireLoopCall) {
	t.Helper()
	argumentText, ok := arguments.(string)
	if !ok {
		t.Fatalf("arguments = %T, want JSON string", arguments)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(argumentText), &input); err != nil {
		t.Fatalf("arguments = %q, want JSON object string: %v", argumentText, err)
	}
	if id != want.id || name != want.name || !reflect.DeepEqual(input, want.input) {
		t.Errorf("call = id %#v name %#v input %#v, want %#v", id, name, input, want)
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
			got, err := wire.EncodeRequest(RequestState{Model: "vendor/model:latest", History: History{parsed}})
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

func TestWireRequestModelPlacementMatchesFixtures(t *testing.T) {
	const model = "vendor/model:latest"
	tests := []struct {
		fixture  wireFixture
		hasModel bool
	}{
		// R-TBFU-LEGO
		{wireFixtures()[0], true},
		// R-TCNQ-Z67D (OpenAI Responses)
		{wireFixtures()[1], true},
		// R-TCNQ-Z67D (OpenAI Chat Completions)
		{wireFixtures()[2], true},
		// R-TDVN-CXY2
		{wireFixtures()[3], false},
	}
	for _, test := range tests {
		t.Run(test.fixture.name, func(t *testing.T) {
			response, err := os.Open(test.fixture.response)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Close() }()
			wire := test.fixture.make(nil)
			var parsed Message
			for event, decodeErr := range wire.DecodeStream(SSEFrames(response)) {
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				parsed = event.(MessageDone).Message
			}
			got, err := wire.EncodeRequest(RequestState{Model: model, History: History{parsed}})
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(test.fixture.request)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("request bytes = %q, want fixture bytes %q", got, want)
			}
			var root map[string]json.RawMessage
			if err := json.Unmarshal(got, &root); err != nil {
				t.Fatal(err)
			}
			rawModel, exists := root["model"]
			if exists != test.hasModel {
				t.Fatalf("top-level model presence = %v, want %v", exists, test.hasModel)
			}
			if test.hasModel {
				var decodedModel string
				if err := json.Unmarshal(rawModel, &decodedModel); err != nil {
					t.Fatal(err)
				}
				if decodedModel != model {
					t.Fatalf("top-level model = %q, want verbatim %q", decodedModel, model)
				}
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
