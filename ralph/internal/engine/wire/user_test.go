package wire_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"ralph/internal/engine/wire"
)

// R-YLQR-3Z46: every `user` event has shape
// {"type":"user","message":{"role":"user","content":[<blocks>]}}.
// The constructor must fix message.role to "user" and content must
// always serialize as a JSON array (zero or more blocks), never null.
func TestR_YLQR_3Z46_UserEventShape(t *testing.T) {
	cases := []struct {
		name    string
		content []any
	}{
		{name: "empty", content: nil},
		{
			name: "two_blocks",
			content: []any{
				map[string]any{"type": "text", "text": "hello"},
				map[string]any{"type": "text", "text": "world"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := wire.NewUserEvent(tc.content...)

			var buf bytes.Buffer
			if err := wire.Encode(&buf, ev); err != nil {
				t.Fatalf("encode: %v", err)
			}
			line := strings.TrimSuffix(buf.String(), "\n")

			// Empty content must be `[]`, never `null`.
			if tc.content == nil && !strings.Contains(line, `"content":[]`) {
				t.Errorf("empty content not serialized as []: %s", line)
			}

			var got map[string]any
			if err := json.Unmarshal([]byte(line), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if got["type"] != "user" {
				t.Errorf("type = %v, want \"user\"", got["type"])
			}

			msg, ok := got["message"].(map[string]any)
			if !ok {
				t.Fatalf("message missing or not object: %v", got["message"])
			}
			if msg["role"] != "user" {
				t.Errorf("message.role = %v, want \"user\"", msg["role"])
			}
			content, ok := msg["content"].([]any)
			if !ok {
				t.Fatalf("message.content missing or not array: %v", msg["content"])
			}
			if len(content) != len(tc.content) {
				t.Errorf("len(content) = %d, want %d", len(content), len(tc.content))
			}

			// Top-level must contain only {type, message} (no sidecar on
			// events created via NewUserEvent).
			for k := range got {
				if k != "type" && k != "message" {
					t.Errorf("unexpected top-level key %q", k)
				}
			}
			// message must contain only {role, content}.
			for k := range msg {
				if k != "role" && k != "content" {
					t.Errorf("unexpected message key %q", k)
				}
			}
		})
	}
}

// R-CZWA-5X35: a user event carrying a tool_result block MAY also carry a
// top-level tool_use_result sidecar alongside message. Events created via
// NewUserEventWithSidecar include the field; events created via NewUserEvent
// omit it (omitempty).
func TestR_CZWA_5X35_UserEventSidecar(t *testing.T) {
	sidecar := map[string]any{
		"stdout":      "hello\n",
		"stderr":      "",
		"interrupted": false,
	}
	block := map[string]any{"type": "tool_result", "tool_use_id": "call_1", "is_error": false, "content": "hello\n\n[exit: 0]"}
	ev := wire.NewUserEventWithSidecar(sidecar, block)

	var buf bytes.Buffer
	if err := wire.Encode(&buf, ev); err != nil {
		t.Fatalf("encode: %v", err)
	}
	line := strings.TrimSuffix(buf.String(), "\n")

	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got["type"] != "user" {
		t.Errorf("type = %v, want \"user\"", got["type"])
	}
	tur, ok := got["tool_use_result"].(map[string]any)
	if !ok {
		t.Fatalf("tool_use_result missing or not object: %v", got["tool_use_result"])
	}
	if tur["stdout"] != "hello\n" {
		t.Errorf("tool_use_result.stdout = %v, want %q", tur["stdout"], "hello\n")
	}
	if tur["stderr"] != "" {
		t.Errorf("tool_use_result.stderr = %v, want empty string", tur["stderr"])
	}
	if tur["interrupted"] != false {
		t.Errorf("tool_use_result.interrupted = %v, want false", tur["interrupted"])
	}

	// Without sidecar, tool_use_result must be absent.
	ev2 := wire.NewUserEvent(block)
	var buf2 bytes.Buffer
	if err := wire.Encode(&buf2, ev2); err != nil {
		t.Fatalf("encode no-sidecar: %v", err)
	}
	var got2 map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(buf2.String(), "\n")), &got2); err != nil {
		t.Fatalf("decode no-sidecar: %v", err)
	}
	if _, present := got2["tool_use_result"]; present {
		t.Errorf("tool_use_result must be absent when no sidecar is set")
	}
}
