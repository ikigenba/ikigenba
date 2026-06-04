package wire_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"ralph/internal/engine/wire"
)

// R-XCMG-YGOH: tool invocations appear as content blocks of shape
// {"type":"tool_use","id":"<unique-id>","name":"<tool-name>","input":<json-value>}.
// Input is a JSON value (object, string, or null).
func TestR_XCMG_YGOH_ToolUseBlockShape(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		toolName  string
		input     any
		wantInput string // canonical JSON form
	}{
		{
			name:      "object_input",
			id:        "toolu_01",
			toolName:  "Read",
			input:     map[string]any{"path": "/tmp/x"},
			wantInput: `{"path":"/tmp/x"}`,
		},
		{
			name:      "string_input",
			id:        "toolu_02",
			toolName:  "Bash",
			input:     "ls -la",
			wantInput: `"ls -la"`,
		},
		{
			name:      "null_input",
			id:        "toolu_03",
			toolName:  "noop",
			input:     nil,
			wantInput: `null`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block, err := wire.NewToolUseBlock(tc.id, tc.toolName, tc.input)
			if err != nil {
				t.Fatalf("NewToolUseBlock: %v", err)
			}
			if block.Type != "tool_use" {
				t.Errorf("Type = %q, want \"tool_use\"", block.Type)
			}
			if block.ID != tc.id {
				t.Errorf("ID = %q, want %q", block.ID, tc.id)
			}
			if block.Name != tc.toolName {
				t.Errorf("Name = %q, want %q", block.Name, tc.toolName)
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
			content := got["message"].(map[string]any)["content"].([]any)
			if len(content) != 1 {
				t.Fatalf("len(content) = %d, want 1", len(content))
			}
			b, ok := content[0].(map[string]any)
			if !ok {
				t.Fatalf("content[0] not object: %v", content[0])
			}
			if b["type"] != "tool_use" {
				t.Errorf("block.type = %v, want \"tool_use\"", b["type"])
			}
			if b["id"] != tc.id {
				t.Errorf("block.id = %v, want %q", b["id"], tc.id)
			}
			if b["name"] != tc.toolName {
				t.Errorf("block.name = %v, want %q", b["name"], tc.toolName)
			}

			// Compare input via re-marshal of the decoded value to a
			// canonical JSON form, so map ordering doesn't matter.
			gotInputBytes, err := json.Marshal(b["input"])
			if err != nil {
				t.Fatalf("remarshal input: %v", err)
			}
			if string(gotInputBytes) != tc.wantInput {
				t.Errorf("block.input = %s, want %s", gotInputBytes, tc.wantInput)
			}
		})
	}
}
