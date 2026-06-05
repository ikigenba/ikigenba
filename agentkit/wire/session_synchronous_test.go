package wire_test

import (
	"bytes"
	"errors"
	"testing"

	"agentkit/wire"
)

// R-12IH-ILKT: tool execution is synchronous from the model's point of
// view — each tool_use is followed by exactly one tool_result before
// the next assistant turn. Session enforces this at the emission point:
//   - a second assistant event is rejected while any prior tool_use
//     remains unanswered
//   - a tool_result whose tool_use_id is not pending (unknown id, or a
//     duplicate answer for an already-answered id) is rejected
//   - two tool_result blocks for the same id within a single user
//     event are rejected
func TestR_12IH_ILKT_OneToolUseOneToolResult(t *testing.T) {
	t.Run("assistant_blocked_while_pending", func(t *testing.T) {
		var buf bytes.Buffer
		s := wire.NewSession(&buf)

		use, err := wire.NewToolUseBlock("toolu_1", "Read", map[string]any{"file_path": "/a"})
		if err != nil {
			t.Fatalf("NewToolUseBlock: %v", err)
		}
		if err := s.EmitAssistant(wire.NewAssistantEvent(use)); err != nil {
			t.Fatalf("first EmitAssistant: %v", err)
		}

		// No tool_result yet — a second assistant turn is forbidden.
		if err := s.EmitAssistant(wire.NewAssistantEvent(map[string]any{
			"type": "text", "text": "premature next turn",
		})); !errors.Is(err, wire.ErrAssistantWithPending) {
			t.Fatalf("second EmitAssistant with pending = %v, want ErrAssistantWithPending", err)
		}

		// After answering, the next assistant turn is permitted.
		res, err := wire.NewToolResultBlock("toolu_1", false, "ok")
		if err != nil {
			t.Fatalf("NewToolResultBlock: %v", err)
		}
		if err := s.EmitUser(wire.NewUserEvent(res)); err != nil {
			t.Fatalf("EmitUser: %v", err)
		}
		if err := s.EmitAssistant(wire.NewAssistantEvent(map[string]any{
			"type": "text", "text": "next turn",
		})); err != nil {
			t.Fatalf("EmitAssistant after answer: %v", err)
		}
	})

	t.Run("tool_result_for_unknown_id_rejected", func(t *testing.T) {
		var buf bytes.Buffer
		s := wire.NewSession(&buf)

		use, _ := wire.NewToolUseBlock("toolu_real", "Read", map[string]any{"file_path": "/a"})
		if err := s.EmitAssistant(wire.NewAssistantEvent(use)); err != nil {
			t.Fatalf("EmitAssistant: %v", err)
		}

		bogus, _ := wire.NewToolResultBlock("toolu_bogus", false, "ok")
		if err := s.EmitUser(wire.NewUserEvent(bogus)); !errors.Is(err, wire.ErrUnsolicitedToolResult) {
			t.Fatalf("EmitUser unsolicited = %v, want ErrUnsolicitedToolResult", err)
		}
		// The pending id must remain pending — the rejected event was
		// not applied.
		if pending := s.PendingToolUseIDs(); len(pending) != 1 || pending[0] != "toolu_real" {
			t.Fatalf("pending after rejection = %v, want [toolu_real]", pending)
		}
	})

	t.Run("duplicate_tool_result_rejected_across_events", func(t *testing.T) {
		var buf bytes.Buffer
		s := wire.NewSession(&buf)

		use, _ := wire.NewToolUseBlock("toolu_1", "Read", map[string]any{"file_path": "/a"})
		if err := s.EmitAssistant(wire.NewAssistantEvent(use)); err != nil {
			t.Fatalf("EmitAssistant: %v", err)
		}
		res, _ := wire.NewToolResultBlock("toolu_1", false, "ok")
		if err := s.EmitUser(wire.NewUserEvent(res)); err != nil {
			t.Fatalf("first EmitUser: %v", err)
		}
		// A second tool_result for the same id is no longer pending and
		// must be rejected as unsolicited.
		dup, _ := wire.NewToolResultBlock("toolu_1", false, "ok again")
		if err := s.EmitUser(wire.NewUserEvent(dup)); !errors.Is(err, wire.ErrUnsolicitedToolResult) {
			t.Fatalf("duplicate EmitUser = %v, want ErrUnsolicitedToolResult", err)
		}
	})

	t.Run("duplicate_tool_result_rejected_within_event", func(t *testing.T) {
		var buf bytes.Buffer
		s := wire.NewSession(&buf)

		use, _ := wire.NewToolUseBlock("toolu_1", "Read", map[string]any{"file_path": "/a"})
		if err := s.EmitAssistant(wire.NewAssistantEvent(use)); err != nil {
			t.Fatalf("EmitAssistant: %v", err)
		}
		resA, _ := wire.NewToolResultBlock("toolu_1", false, "first")
		resB, _ := wire.NewToolResultBlock("toolu_1", false, "second")
		if err := s.EmitUser(wire.NewUserEvent(resA, resB)); !errors.Is(err, wire.ErrUnsolicitedToolResult) {
			t.Fatalf("EmitUser with duplicate ids = %v, want ErrUnsolicitedToolResult", err)
		}
	})
}
