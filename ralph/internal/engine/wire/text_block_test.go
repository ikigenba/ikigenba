package wire_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"ralph/internal/engine/wire"
)

// R-WQOA-2LBZ: text output from the model appears as a content block of
// shape {"type":"text","text":"<string>"}. Multiple text blocks within a
// single assistant turn are permitted.
func TestR_WQOA_2LBZ_TextBlockShape(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "ascii", text: "hello world"},
		{name: "empty", text: ""},
		{name: "utf8", text: "héllo — 世界"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block := wire.NewTextBlock(tc.text)
			if block.Type != "text" {
				t.Errorf("Type = %q, want \"text\"", block.Type)
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
			if b["type"] != "text" {
				t.Errorf("block.type = %v, want \"text\"", b["type"])
			}
			if b["text"] != tc.text {
				t.Errorf("block.text = %v, want %q", b["text"], tc.text)
			}
		})
	}

	t.Run("multiple_text_blocks", func(t *testing.T) {
		ev := wire.NewAssistantEvent(
			wire.NewTextBlock("first"),
			wire.NewTextBlock("second"),
		)
		var buf bytes.Buffer
		if err := wire.Encode(&buf, ev); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		content := got["message"].(map[string]any)["content"].([]any)
		if len(content) != 2 {
			t.Fatalf("len(content) = %d, want 2", len(content))
		}
		want := []string{"first", "second"}
		for i, w := range want {
			b := content[i].(map[string]any)
			if b["type"] != "text" || b["text"] != w {
				t.Errorf("content[%d] = %v, want type=text text=%q", i, b, w)
			}
		}
	})
}
