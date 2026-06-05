package wire_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"agentkit/wire"
)

// R-5DMN-M3F2: tool_use and tool_result blocks are correlated by
// `id` / `tool_use_id`, not by ordering. Within a single user event,
// tool_result blocks may appear in any order relative to the
// originating tool_use blocks; ralph-loops looks them up by id.
//
// This test pins the wire layer's behavior to that contract:
//   - tool_use blocks carry an `id`.
//   - tool_result blocks carry a `tool_use_id` that names the id.
//   - The encoder/parser does NOT enforce that tool_result blocks
//     appear in the same order as the tool_use blocks they answer.
//     Reversing the order is a valid, faithfully round-tripped event.
func TestR_5DMN_M3F2_ToolUseToolResultCorrelationByID(t *testing.T) {
	useA, err := wire.NewToolUseBlock("toolu_A", "Read", map[string]any{"path": "/a"})
	if err != nil {
		t.Fatalf("NewToolUseBlock A: %v", err)
	}
	useB, err := wire.NewToolUseBlock("toolu_B", "Bash", map[string]any{"cmd": "echo b"})
	if err != nil {
		t.Fatalf("NewToolUseBlock B: %v", err)
	}
	resA, err := wire.NewToolResultBlock("toolu_A", false, "ok-A")
	if err != nil {
		t.Fatalf("NewToolResultBlock A: %v", err)
	}
	resB, err := wire.NewToolResultBlock("toolu_B", false, "ok-B")
	if err != nil {
		t.Fatalf("NewToolResultBlock B: %v", err)
	}

	if useA.ID != resA.ToolUseID || useB.ID != resB.ToolUseID {
		t.Fatalf("correlation field mismatch: useA.ID=%q resA.ToolUseID=%q useB.ID=%q resB.ToolUseID=%q",
			useA.ID, resA.ToolUseID, useB.ID, resB.ToolUseID)
	}

	assistant := wire.NewAssistantEvent(useA, useB)

	// Results in REVERSE order vs. the originating tool_use blocks.
	// The wire layer must accept this — order carries no meaning.
	userReversed := wire.NewUserEvent(resB, resA)
	userInOrder := wire.NewUserEvent(resA, resB)

	var buf bytes.Buffer
	if err := wire.Encode(&buf, assistant); err != nil {
		t.Fatalf("encode assistant: %v", err)
	}
	if err := wire.Encode(&buf, userReversed); err != nil {
		t.Fatalf("encode user (reversed): %v", err)
	}
	if err := wire.Encode(&buf, userInOrder); err != nil {
		t.Fatalf("encode user (in-order): %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	idsFromContent := func(line string) []string {
		var ev struct {
			Message struct {
				Content []map[string]any `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("decode: %v", err)
		}
		out := make([]string, 0, len(ev.Message.Content))
		for _, b := range ev.Message.Content {
			if v, ok := b["id"].(string); ok {
				out = append(out, v)
			} else if v, ok := b["tool_use_id"].(string); ok {
				out = append(out, v)
			}
		}
		return out
	}

	gotAssistant := idsFromContent(lines[0])
	gotReversed := idsFromContent(lines[1])
	gotInOrder := idsFromContent(lines[2])

	wantAssistant := []string{"toolu_A", "toolu_B"}
	if !equalStrings(gotAssistant, wantAssistant) {
		t.Errorf("assistant ids = %v, want %v", gotAssistant, wantAssistant)
	}

	// Reversed order must round-trip as reversed (encoder does not
	// silently re-sort to match the tool_use order).
	wantReversed := []string{"toolu_B", "toolu_A"}
	if !equalStrings(gotReversed, wantReversed) {
		t.Errorf("user (reversed) tool_use_ids = %v, want %v", gotReversed, wantReversed)
	}
	wantInOrder := []string{"toolu_A", "toolu_B"}
	if !equalStrings(gotInOrder, wantInOrder) {
		t.Errorf("user (in-order) tool_use_ids = %v, want %v", gotInOrder, wantInOrder)
	}

	// Both orderings name the same set of tool_use ids — correlation
	// is by id-set membership, not by position.
	if !equalSet(gotReversed, gotInOrder) {
		t.Errorf("reversed and in-order events name different id sets: %v vs %v", gotReversed, gotInOrder)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
		if m[s] < 0 {
			return false
		}
	}
	return true
}
