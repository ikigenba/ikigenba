package agentkit_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

// inBandTerminal serves body as an HTTP 200 SSE stream on loopback, decodes it
// with wire, and returns the stream's terminal *Error. It fails the test when
// the stream does not end with an *Error or yields anything after it.
func inBandTerminal(t *testing.T, wire agentkit.WireFormat, body string) *agentkit.Error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fixture status = %d, want 200", response.StatusCode)
	}

	var terminal error
	for event, decodeErr := range wire.DecodeStream(agentkit.SSEFrames(response.Body)) {
		if terminal != nil {
			t.Fatalf("stream continued after terminal error %v: event=%#v err=%v", terminal, event, decodeErr)
		}
		if decodeErr != nil {
			terminal = decodeErr
		}
	}
	var providerErr *agentkit.Error
	if !errors.As(terminal, &providerErr) {
		t.Fatalf("stream ended with %T %v, want terminal *Error", terminal, terminal)
	}
	return providerErr
}

func dataFrame(payload string) string { return "data: " + payload + "\n\n" }

func anthropicErrorStream(errorObject string) string {
	return "event: message_start\n" + dataFrame(`{"type":"message_start","message":{"usage":{"input_tokens":1}}}`) +
		"event: content_block_start\n" + dataFrame(`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`) +
		"event: content_block_delta\n" + dataFrame(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`) +
		"event: error\n" + dataFrame(`{"type":"error","error":`+errorObject+`}`) +
		"event: message_stop\n" + dataFrame(`{"type":"message_stop"}`)
}

func responsesErrorStream(errorEvent string) string {
	return dataFrame(`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"delta":"partial"}`) +
		dataFrame(errorEvent) +
		dataFrame(`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"delta":"after"}`)
}

func chatErrorStream(errorFrame string) string {
	return dataFrame(`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`) +
		dataFrame(errorFrame) +
		dataFrame(`{"choices":[{"index":0,"delta":{"content":"after"}}]}`)
}

func responsesWires() map[string]func() agentkit.WireFormat {
	return map[string]func() agentkit.WireFormat{
		"ResponsesWire":       agentkit.ResponsesWire,
		"OpenAIResponsesWire": agentkit.OpenAIResponsesWire,
		"XAIResponsesWire":    agentkit.XAIResponsesWire,
	}
}

func chatWires() map[string]func() agentkit.WireFormat {
	return map[string]func() agentkit.WireFormat{
		"ChatWire":       agentkit.ChatWire,
		"OpenAIChatWire": agentkit.OpenAIChatWire,
		"XAIChatWire":    agentkit.XAIChatWire,
	}
}

func TestAnthropicInBandErrorEventEndsStreamWithError(t *testing.T) {
	// R-ISHO-CITZ
	got := inBandTerminal(t, agentkit.AnthropicMessagesWire(), anthropicErrorStream(`{"type":"overloaded_error","message":"Overloaded"}`))
	if got.Status != http.StatusOK || got.Code != "overloaded_error" || got.Message != "Overloaded" {
		t.Fatalf("terminal error = %+v, want Status 200, Code overloaded_error, Message Overloaded", got)
	}
}

func TestAnthropicInBandErrorCategoryFollowsPairedStatus(t *testing.T) {
	// R-ITPK-QAKO
	cases := []struct {
		errorType string
		want      agentkit.Category
	}{
		{"invalid_request_error", agentkit.CategoryInvalidRequest},
		{"authentication_error", agentkit.CategoryAuth},
		{"billing_error", agentkit.CategoryInsufficientQuota},
		{"permission_error", agentkit.CategoryAuth},
		{"not_found_error", agentkit.CategoryInvalidRequest},
		{"conflict_error", agentkit.CategoryInvalidRequest},
		{"request_too_large", agentkit.CategoryInvalidRequest},
		{"rate_limit_error", agentkit.CategoryRateLimit},
		{"api_error", agentkit.CategoryOverloaded},
		{"timeout_error", agentkit.CategoryTimeout},
		{"overloaded_error", agentkit.CategoryOverloaded},
		{"unlisted_error", agentkit.CategoryUnknown},
	}
	for _, test := range cases {
		t.Run(test.errorType, func(t *testing.T) {
			got := inBandTerminal(t, agentkit.AnthropicMessagesWire(), anthropicErrorStream(`{"type":"`+test.errorType+`","message":"m"}`))
			if got.Category != test.want {
				t.Fatalf("Category = %v, want %v", got.Category, test.want)
			}
		})
	}
}

func TestResponsesErrorEventEndsStreamWithError(t *testing.T) {
	// R-IW5D-HU22
	cases := []struct {
		name, event, code string
	}{
		{"string code", `{"type":"error","code":"server_error","message":"boom","param":null,"sequence_number":2}`, "server_error"},
		{"null code", `{"type":"error","code":null,"message":"boom","param":null,"sequence_number":2}`, ""},
	}
	for wireName, newWire := range responsesWires() {
		for _, test := range cases {
			t.Run(wireName+"/"+test.name, func(t *testing.T) {
				got := inBandTerminal(t, newWire(), responsesErrorStream(test.event))
				if got.Status != http.StatusOK || got.Code != test.code || got.Message != "boom" {
					t.Fatalf("terminal error = %+v, want Status 200, Code %q, Message boom", got, test.code)
				}
			})
		}
	}
}

func TestResponsesFailedEventEndsStreamWithError(t *testing.T) {
	// R-IXD9-VLSR
	cases := []struct {
		name, code, want string
	}{
		{"string code", `"server_error"`, "server_error"},
		{"null code", `null`, ""},
	}
	for wireName, newWire := range responsesWires() {
		for _, test := range cases {
			t.Run(wireName+"/"+test.name, func(t *testing.T) {
				event := `{"type":"response.failed","sequence_number":3,"response":{"id":"resp_1","status":"failed","error":{"code":` + test.code + `,"message":"failed hard"}}}`
				got := inBandTerminal(t, newWire(), responsesErrorStream(event))
				if got.Status != http.StatusOK || got.Code != test.want || got.Message != "failed hard" {
					t.Fatalf("terminal error = %+v, want Status 200, Code %q, Message %q", got, test.want, "failed hard")
				}
			})
		}
	}
}

func TestChatErrorFrameEndsStreamWithError(t *testing.T) {
	// R-IYL6-9DJG
	cases := []struct {
		name, frame, code, message string
	}{
		{"string fields", `{"error":{"code":"server_error","message":"stream broke","param":null,"type":"server_error"}}`, "server_error", "stream broke"},
		{"non-string fields", `{"error":{"code":503,"message":{"text":"no"}}}`, "", ""},
		{"missing fields", `{"error":{}}`, "", ""},
		{"non-object error", `{"error":"stream broke"}`, "", ""},
	}
	for wireName, newWire := range chatWires() {
		for _, test := range cases {
			t.Run(wireName+"/"+test.name, func(t *testing.T) {
				got := inBandTerminal(t, newWire(), chatErrorStream(test.frame))
				if got.Status != http.StatusOK || got.Code != test.code || got.Message != test.message {
					t.Fatalf("terminal error = %+v, want Status 200, Code %q, Message %q", got, test.code, test.message)
				}
			})
		}
	}
}

func TestOpenAIFamilyInBandErrorCategoryFollowsPairedStatus(t *testing.T) {
	// R-IZT2-N5A5
	cases := []struct {
		code string
		want agentkit.Category
	}{
		{"slow_down", agentkit.CategoryRateLimit},
		{"server_is_overloaded", agentkit.CategoryOverloaded},
		{"credit_balance_exhausted", agentkit.CategoryRateLimit},
		{"organization_spend_limit_exceeded", agentkit.CategoryRateLimit},
		{"project_spend_limit_exceeded", agentkit.CategoryRateLimit},
		{"organization_usage_limit_exceeded", agentkit.CategoryRateLimit},
		{"server_error", agentkit.CategoryUnknown},
		{"", agentkit.CategoryUnknown},
	}
	codeJSON := func(code string) string {
		if code == "" {
			return "null"
		}
		return fmt.Sprintf("%q", code)
	}
	type family struct {
		wires  map[string]func() agentkit.WireFormat
		stream func(code string) string
	}
	families := map[string]family{
		"responses error": {responsesWires(), func(code string) string {
			return responsesErrorStream(`{"type":"error","code":` + codeJSON(code) + `,"message":"m"}`)
		}},
		"responses failed": {responsesWires(), func(code string) string {
			return responsesErrorStream(`{"type":"response.failed","response":{"error":{"code":` + codeJSON(code) + `,"message":"m"}}}`)
		}},
		"chat": {chatWires(), func(code string) string {
			return chatErrorStream(`{"error":{"code":` + codeJSON(code) + `,"message":"m"}}`)
		}},
	}
	for familyName, family := range families {
		for wireName, newWire := range family.wires {
			for _, test := range cases {
				t.Run(strings.Join([]string{familyName, wireName, test.code}, "/"), func(t *testing.T) {
					got := inBandTerminal(t, newWire(), family.stream(test.code))
					if got.Category != test.want {
						t.Fatalf("Category = %v, want %v", got.Category, test.want)
					}
				})
			}
		}
	}
}
