package wire_test

import (
	"bytes"
	"testing"

	"ralph/internal/engine/wire"
)

// R-Z8NW-UZJB: tool invocations from the model arrive as `tool_use`
// content blocks in `assistant` events on stdout, and ikigai-cli emits
// the corresponding `tool_result` content blocks in `user` events on
// stdout, in the exact stream-json shape Claude Code produces. This
// test drives a full assistant -> user -> result iteration through a
// Session with a tool round trip and pins the byte-for-byte shape of
// each emitted line.
func TestR_Z8NW_UZJB_ToolRoundTripWireShape(t *testing.T) {
	var buf bytes.Buffer
	s := wire.NewSession(&buf)

	tu, err := wire.NewToolUseBlock("toolu_01abc", "Read", map[string]any{
		"file_path": "/tmp/x",
	})
	if err != nil {
		t.Fatalf("NewToolUseBlock: %v", err)
	}
	if err := s.EmitAssistant(wire.NewAssistantEvent(tu)); err != nil {
		t.Fatalf("EmitAssistant: %v", err)
	}

	tr, err := wire.NewToolResultBlock("toolu_01abc", false, "1\thello\n")
	if err != nil {
		t.Fatalf("NewToolResultBlock: %v", err)
	}
	if err := s.EmitUser(wire.NewUserEvent(tr)); err != nil {
		t.Fatalf("EmitUser: %v", err)
	}

	res, err := wire.NewResultEvent(map[string]string{"status": "DONE"}, false)
	if err != nil {
		t.Fatalf("NewResultEvent: %v", err)
	}
	if err := s.EmitResult(res); err != nil {
		t.Fatalf("EmitResult: %v", err)
	}

	want := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_01abc","name":"Read","input":{"file_path":"/tmp/x"}}]}}` + "\n" +
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01abc","is_error":false,"content":"1\thello\n"}]}}` + "\n" +
		`{"type":"result","structured_output":{"status":"DONE"},"is_error":false}` + "\n"

	got := buf.String()
	if got != want {
		t.Errorf("wire shape mismatch\n got: %q\nwant: %q", got, want)
	}
}
