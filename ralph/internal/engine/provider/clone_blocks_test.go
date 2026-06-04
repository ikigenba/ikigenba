package provider_test

import (
	"encoding/json"
	"strings"
	"testing"

	"ralph/internal/engine/provider"
)

// TestR_ROBI_V64M_ThinkingBlocksRoundTripWithToolUse pins the
// provider-side thinking/reasoning preservation contract at the
// abstraction layer. When an iteration uses tools, the agent loop
// must feed the prior assistant turn — thinking blocks with their
// Signature, and the matching tool_use block — back into the next
// Request unchanged, or Anthropic 400-rejects the call.
//
// R-ROBI-V64M: provider-side thinking / reasoning state must be
// preserved across all in-iteration round-trips with the same
// provider, including signed thinking paired with tool_use.
func TestR_ROBI_V64M_ThinkingBlocksRoundTripWithToolUse(t *testing.T) {
	originalInput := json.RawMessage(`{"path":"/tmp/x"}`)
	src := []provider.Block{
		provider.ThinkingBlock{Text: "consider the path", Signature: "sig-abc-123"},
		provider.ToolUseBlock{ID: "tu_1", Name: "read", Input: originalInput},
		provider.TextBlock{Text: "ok"},
	}

	cloned := provider.CloneBlocks(src)

	if len(cloned) != len(src) {
		t.Fatalf("CloneBlocks length: got %d, want %d", len(cloned), len(src))
	}

	tb, ok := cloned[0].(provider.ThinkingBlock)
	if !ok {
		t.Fatalf("cloned[0]: got %T, want ThinkingBlock", cloned[0])
	}
	if tb.Signature != "sig-abc-123" {
		t.Errorf("ThinkingBlock.Signature: got %q, want %q", tb.Signature, "sig-abc-123")
	}
	if tb.Text != "consider the path" {
		t.Errorf("ThinkingBlock.Text: got %q, want %q", tb.Text, "consider the path")
	}

	tu, ok := cloned[1].(provider.ToolUseBlock)
	if !ok {
		t.Fatalf("cloned[1]: got %T, want ToolUseBlock", cloned[1])
	}
	if tu.ID != "tu_1" || tu.Name != "read" {
		t.Errorf("ToolUseBlock identity: got (%q,%q), want (tu_1,read)", tu.ID, tu.Name)
	}
	if string(tu.Input) != string(originalInput) {
		t.Errorf("ToolUseBlock.Input: got %q, want %q", tu.Input, originalInput)
	}

	if _, ok := cloned[2].(provider.TextBlock); !ok {
		t.Errorf("cloned[2]: got %T, want TextBlock", cloned[2])
	}

	// Mutating the clone's tool_use Input must not corrupt the
	// source — required because the loop appends assistant blocks
	// to the next Request.Messages and any deep-copy-by-share would
	// let later editing trample the prior turn.
	for i := range tu.Input {
		tu.Input[i] = 'X'
	}
	if !strings.Contains(string(originalInput), "/tmp/x") {
		t.Errorf("mutating clone Input corrupted source: %q", originalInput)
	}

	// Mutating the source slice must not corrupt the clone — same
	// reason in reverse, the agent loop reuses the assistant turn.
	src[0] = provider.TextBlock{Text: "wiped"}
	if tb2, ok := cloned[0].(provider.ThinkingBlock); !ok || tb2.Signature != "sig-abc-123" {
		t.Errorf("mutating source corrupted clone: got %#v", cloned[0])
	}

	if got := provider.CloneBlocks(nil); got != nil {
		t.Errorf("CloneBlocks(nil): got %v, want nil", got)
	}
}
