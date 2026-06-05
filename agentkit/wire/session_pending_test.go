package wire_test

import (
	"bytes"
	"errors"
	"testing"

	"agentkit/wire"
)

// R-5ZKU-HYRK: every tool_use block emitted in an assistant event must
// be answered by exactly one tool_result block in a subsequent user
// event before the iteration's result event. Session refuses
// EmitResult while any tool_use id remains unanswered.
func TestR_5ZKU_HYRK_ResultRefusedWithPendingToolUse(t *testing.T) {
	var buf bytes.Buffer
	s := wire.NewSession(&buf)

	useA, err := wire.NewToolUseBlock("toolu_A", "Read", map[string]any{"path": "/a"})
	if err != nil {
		t.Fatalf("NewToolUseBlock A: %v", err)
	}
	useB, err := wire.NewToolUseBlock("toolu_B", "Bash", map[string]any{"cmd": "echo b"})
	if err != nil {
		t.Fatalf("NewToolUseBlock B: %v", err)
	}

	if err := s.EmitAssistant(wire.NewAssistantEvent(useA, useB)); err != nil {
		t.Fatalf("EmitAssistant: %v", err)
	}

	pending := s.PendingToolUseIDs()
	if len(pending) != 2 || pending[0] != "toolu_A" || pending[1] != "toolu_B" {
		t.Fatalf("PendingToolUseIDs after assistant = %v, want [toolu_A toolu_B]", pending)
	}

	res, err := wire.NewResultEvent(map[string]string{"status": "CONTINUE"}, false)
	if err != nil {
		t.Fatalf("NewResultEvent: %v", err)
	}
	if err := s.EmitResult(res); !errors.Is(err, wire.ErrPendingToolUse) {
		t.Fatalf("EmitResult with two pending = %v, want ErrPendingToolUse", err)
	}
	if s.Finished() {
		t.Fatalf("Finished true after rejected EmitResult")
	}

	// Answer one of two; result still refused.
	resA, err := wire.NewToolResultBlock("toolu_A", false, "ok-A")
	if err != nil {
		t.Fatalf("NewToolResultBlock A: %v", err)
	}
	if err := s.EmitUser(wire.NewUserEvent(resA)); err != nil {
		t.Fatalf("EmitUser A: %v", err)
	}
	if pending := s.PendingToolUseIDs(); len(pending) != 1 || pending[0] != "toolu_B" {
		t.Fatalf("PendingToolUseIDs after answering A = %v, want [toolu_B]", pending)
	}
	if err := s.EmitResult(res); !errors.Is(err, wire.ErrPendingToolUse) {
		t.Fatalf("EmitResult with one pending = %v, want ErrPendingToolUse", err)
	}

	// Answer the second; result now permitted.
	resB, err := wire.NewToolResultBlock("toolu_B", false, "ok-B")
	if err != nil {
		t.Fatalf("NewToolResultBlock B: %v", err)
	}
	if err := s.EmitUser(wire.NewUserEvent(resB)); err != nil {
		t.Fatalf("EmitUser B: %v", err)
	}
	if pending := s.PendingToolUseIDs(); len(pending) != 0 {
		t.Fatalf("PendingToolUseIDs after answering B = %v, want []", pending)
	}
	if err := s.EmitResult(res); err != nil {
		t.Fatalf("EmitResult after all answered: %v", err)
	}
	if !s.Finished() {
		t.Fatalf("Finished false after EmitResult")
	}
}

// Out-of-order tool_result blocks (within a single user event) must
// still satisfy the pending set — correlation is by id, not order
// (R-5DMN-M3F2 in concert with R-5ZKU-HYRK).
func TestR_5ZKU_HYRK_OutOfOrderToolResultsClearPending(t *testing.T) {
	var buf bytes.Buffer
	s := wire.NewSession(&buf)

	useA, _ := wire.NewToolUseBlock("toolu_A", "Read", map[string]any{"path": "/a"})
	useB, _ := wire.NewToolUseBlock("toolu_B", "Bash", map[string]any{"cmd": "echo b"})
	if err := s.EmitAssistant(wire.NewAssistantEvent(useA, useB)); err != nil {
		t.Fatalf("EmitAssistant: %v", err)
	}

	resA, _ := wire.NewToolResultBlock("toolu_A", false, "ok-A")
	resB, _ := wire.NewToolResultBlock("toolu_B", false, "ok-B")
	// Reverse order of the originating tool_use blocks.
	if err := s.EmitUser(wire.NewUserEvent(resB, resA)); err != nil {
		t.Fatalf("EmitUser: %v", err)
	}
	if pending := s.PendingToolUseIDs(); len(pending) != 0 {
		t.Fatalf("PendingToolUseIDs = %v, want []", pending)
	}

	res, _ := wire.NewResultEvent(map[string]string{"status": "CONTINUE"}, false)
	if err := s.EmitResult(res); err != nil {
		t.Fatalf("EmitResult: %v", err)
	}
}

// A generic map-shaped tool_use block (as a driver might construct)
// must be tracked the same as a typed ToolUseBlock.
func TestR_5ZKU_HYRK_MapShapedBlocksTracked(t *testing.T) {
	var buf bytes.Buffer
	s := wire.NewSession(&buf)

	useMap := map[string]any{
		"type":  "tool_use",
		"id":    "toolu_X",
		"name":  "Read",
		"input": map[string]any{"path": "/x"},
	}
	if err := s.EmitAssistant(wire.NewAssistantEvent(useMap)); err != nil {
		t.Fatalf("EmitAssistant: %v", err)
	}
	if pending := s.PendingToolUseIDs(); len(pending) != 1 || pending[0] != "toolu_X" {
		t.Fatalf("PendingToolUseIDs = %v, want [toolu_X]", pending)
	}

	resMap := map[string]any{
		"type":        "tool_result",
		"tool_use_id": "toolu_X",
		"is_error":    false,
		"content":     "ok",
	}
	if err := s.EmitUser(wire.NewUserEvent(resMap)); err != nil {
		t.Fatalf("EmitUser: %v", err)
	}
	if pending := s.PendingToolUseIDs(); len(pending) != 0 {
		t.Fatalf("PendingToolUseIDs = %v, want []", pending)
	}
	res, _ := wire.NewResultEvent(map[string]string{"status": "DONE"}, false)
	if err := s.EmitResult(res); err != nil {
		t.Fatalf("EmitResult: %v", err)
	}
}
