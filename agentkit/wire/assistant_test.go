package wire_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"agentkit/wire"
)

// R-ZRNK-LQRK: assistant events must NOT carry a message.usage field.
// Per-message usage from the provider is partial and unreliable; the
// authoritative token totals live exclusively in the result event (R-Y5QZ-UNB2).
func TestR_ZRNK_LQRK_AssistantEventHasNoMessageUsage(t *testing.T) {
	ev := wire.NewAssistantEvent(map[string]any{"type": "text", "text": "hello"})

	var buf bytes.Buffer
	if err := wire.Encode(&buf, ev); err != nil {
		t.Fatalf("encode: %v", err)
	}
	line := strings.TrimSuffix(buf.String(), "\n")

	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	msg, ok := got["message"].(map[string]any)
	if !ok {
		t.Fatalf("message missing or not object: %v", got["message"])
	}
	if _, hasUsage := msg["usage"]; hasUsage {
		t.Errorf("message.usage must not be present, got: %v", msg["usage"])
	}
}

// R-VUYW-4K1X: every `assistant` event has shape
// {"type":"assistant","message":{"role":"assistant","content":[<blocks>]}}.
// The constructor must fix message.role to "assistant" and content
// must always serialize as a JSON array (zero or more blocks).
func TestR_VUYW_4K1X_AssistantEventShape(t *testing.T) {
	cases := []struct {
		name    string
		content []any
	}{
		{name: "empty", content: nil},
		{
			name: "two_blocks",
			content: []any{
				map[string]any{"type": "text", "text": "hi"},
				map[string]any{"type": "text", "text": "there"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := wire.NewAssistantEvent(tc.content...)

			var buf bytes.Buffer
			if err := wire.Encode(&buf, ev); err != nil {
				t.Fatalf("encode: %v", err)
			}
			line := strings.TrimSuffix(buf.String(), "\n")

			var got map[string]any
			if err := json.Unmarshal([]byte(line), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if got["type"] != "assistant" {
				t.Errorf("type = %v, want \"assistant\"", got["type"])
			}

			msg, ok := got["message"].(map[string]any)
			if !ok {
				t.Fatalf("message missing or not object: %v", got["message"])
			}
			if msg["role"] != "assistant" {
				t.Errorf("message.role = %v, want \"assistant\"", msg["role"])
			}
			content, ok := msg["content"].([]any)
			if !ok {
				t.Fatalf("message.content missing or not array: %v", msg["content"])
			}
			if len(content) != len(tc.content) {
				t.Errorf("len(content) = %d, want %d", len(content), len(tc.content))
			}
		})
	}
}
