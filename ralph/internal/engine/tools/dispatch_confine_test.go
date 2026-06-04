package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ralph/internal/engine/tools"
	"ralph/internal/engine/wire"
)

func mustToolUse(t *testing.T, id, name string, input any) wire.ToolUseBlock {
	t.Helper()
	b, err := wire.NewToolUseBlock(id, name, input)
	if err != nil {
		t.Fatalf("NewToolUseBlock: %v", err)
	}
	return b
}

func resultContent(t *testing.T, b wire.ToolResultBlock) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(b.Content, &s); err != nil {
		return string(b.Content)
	}
	return s
}

// Dispatch must surface a sandbox escape as an is_error tool_result (not
// a Go error) for read/write/edit, so an escape attempt reaches the model
// as a correlatable failure rather than tearing down the loop.
func TestDispatch_ConfinesReadWriteEdit(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		tool  string
		input any
	}{
		{"Read", map[string]any{"file_path": "/etc/passwd"}},
		{"Read", map[string]any{"file_path": "../escape.txt"}},
		{"Write", map[string]any{"file_path": "/tmp/evil-ralph-out.txt", "content": "x"}},
		{"Edit", map[string]any{"file_path": "../escape.txt", "old_string": "a", "new_string": "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			block := mustToolUse(t, "id1", tc.tool, tc.input)
			tr, _, err := tools.Dispatch(context.Background(), root, block)
			if err != nil {
				t.Fatalf("Dispatch returned Go error, want is_error block: %v", err)
			}
			if !tr.IsError {
				t.Fatalf("%s escape not flagged is_error; content=%q", tc.tool, resultContent(t, tr))
			}
			if !strings.Contains(resultContent(t, tr), "escapes sandbox") {
				t.Fatalf("%s error content = %q, want 'escapes sandbox'", tc.tool, resultContent(t, tr))
			}
		})
	}
}

// A legitimate in-root write then read round-trips under a sandbox root.
func TestDispatch_AllowsInRootWriteRead(t *testing.T) {
	root := t.TempDir()

	wblock := mustToolUse(t, "w1", "Write", map[string]any{
		"file_path": "hello.txt",
		"content":   "in-root-content",
	})
	tr, _, err := tools.Dispatch(context.Background(), root, wblock)
	if err != nil {
		t.Fatalf("Dispatch write: %v", err)
	}
	if tr.IsError {
		t.Fatalf("in-root write flagged is_error: %q", resultContent(t, tr))
	}
	if _, err := os.Stat(filepath.Join(root, "hello.txt")); err != nil {
		t.Fatalf("file not created in root: %v", err)
	}

	rblock := mustToolUse(t, "r1", "Read", map[string]any{"file_path": "hello.txt"})
	tr, _, err = tools.Dispatch(context.Background(), root, rblock)
	if err != nil {
		t.Fatalf("Dispatch read: %v", err)
	}
	if tr.IsError {
		t.Fatalf("in-root read flagged is_error: %q", resultContent(t, tr))
	}
	if !strings.Contains(resultContent(t, tr), "in-root-content") {
		t.Fatalf("read content = %q, want in-root-content", resultContent(t, tr))
	}
}
