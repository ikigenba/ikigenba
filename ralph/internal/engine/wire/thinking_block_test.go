package wire_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"ralph/internal/engine/wire"
)

// R-SA9P-R1H4: extended-thinking / reasoning output is forwarded to
// stdout as content blocks of shape
// {"type":"thinking","thinking":"<string>"}.
func TestR_SA9P_R1H4_ThinkingBlockShape(t *testing.T) {
	cases := []struct {
		name     string
		thinking string
	}{
		{name: "ascii", thinking: "let me reason about this"},
		{name: "empty", thinking: ""},
		{name: "utf8", thinking: "考える — 思考"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block := wire.NewThinkingBlock(tc.thinking)
			if block.Type != "thinking" {
				t.Errorf("Type = %q, want \"thinking\"", block.Type)
			}

			ev := wire.NewAssistantEvent(block)

			var buf bytes.Buffer
			if err := wire.Encode(&buf, ev); err != nil {
				t.Fatalf("encode: %v", err)
			}
			line := strings.TrimSuffix(buf.String(), "\n")

			var got map[string]any
			if err := json.Unmarshal([]byte(line), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}

			msg := got["message"].(map[string]any)
			content := msg["content"].([]any)
			if len(content) != 1 {
				t.Fatalf("len(content) = %d, want 1", len(content))
			}
			b, ok := content[0].(map[string]any)
			if !ok {
				t.Fatalf("content[0] not object: %v", content[0])
			}
			if b["type"] != "thinking" {
				t.Errorf("block.type = %v, want \"thinking\"", b["type"])
			}
			if b["thinking"] != tc.thinking {
				t.Errorf("block.thinking = %v, want %q", b["thinking"], tc.thinking)
			}
			for k := range b {
				if k != "type" && k != "thinking" {
					t.Errorf("unexpected field %q in block", k)
				}
			}
		})
	}
}
