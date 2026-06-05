package wire_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"agentkit/wire"
)

// R-Z6H1-M2PZ: tool results appear as user content blocks of shape
// {"type":"tool_result","tool_use_id":"<id>","is_error":<bool>,"content":<json-value>}.
// Content is a JSON value (string, structured object/array, or null).
func TestR_Z6H1_M2PZ_ToolResultBlockShape(t *testing.T) {
	cases := []struct {
		name        string
		toolUseID   string
		isError     bool
		content     any
		wantContent string // canonical JSON form
	}{
		{
			name:        "string_content",
			toolUseID:   "toolu_01",
			isError:     false,
			content:     "hello",
			wantContent: `"hello"`,
		},
		{
			name:        "object_content",
			toolUseID:   "toolu_02",
			isError:     false,
			content:     map[string]any{"path": "/tmp/x"},
			wantContent: `{"path":"/tmp/x"}`,
		},
		{
			name:        "error_string_content",
			toolUseID:   "toolu_03",
			isError:     true,
			content:     "exit 1",
			wantContent: `"exit 1"`,
		},
		{
			name:        "null_content",
			toolUseID:   "toolu_04",
			isError:     false,
			content:     nil,
			wantContent: `null`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block, err := wire.NewToolResultBlock(tc.toolUseID, tc.isError, tc.content)
			if err != nil {
				t.Fatalf("NewToolResultBlock: %v", err)
			}
			if block.Type != "tool_result" {
				t.Errorf("Type = %q, want \"tool_result\"", block.Type)
			}
			if block.ToolUseID != tc.toolUseID {
				t.Errorf("ToolUseID = %q, want %q", block.ToolUseID, tc.toolUseID)
			}
			if block.IsError != tc.isError {
				t.Errorf("IsError = %v, want %v", block.IsError, tc.isError)
			}

			ev := wire.NewUserEvent(block)

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

			wantKeys := map[string]bool{
				"type": true, "tool_use_id": true, "is_error": true, "content": true,
			}
			for k := range b {
				if !wantKeys[k] {
					t.Errorf("unexpected key %q in block", k)
				}
			}
			for k := range wantKeys {
				if _, ok := b[k]; !ok {
					t.Errorf("missing key %q in block", k)
				}
			}

			if b["type"] != "tool_result" {
				t.Errorf("block.type = %v, want \"tool_result\"", b["type"])
			}
			if b["tool_use_id"] != tc.toolUseID {
				t.Errorf("block.tool_use_id = %v, want %q", b["tool_use_id"], tc.toolUseID)
			}
			if b["is_error"] != tc.isError {
				t.Errorf("block.is_error = %v, want %v", b["is_error"], tc.isError)
			}

			gotContentBytes, err := json.Marshal(b["content"])
			if err != nil {
				t.Fatalf("remarshal content: %v", err)
			}
			if string(gotContentBytes) != tc.wantContent {
				t.Errorf("block.content = %s, want %s", gotContentBytes, tc.wantContent)
			}
		})
	}
}
