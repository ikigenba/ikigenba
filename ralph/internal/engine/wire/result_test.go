package wire_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"ralph/internal/engine/wire"
)

// R-Y5QZ-UNB2: the result event carries num_turns, duration_ms,
// total_cost_usd, usage, and modelUsage when constructed via
// NewResultEventFull. Fields are omitted when zero (not
// zero-padded), so the MVP 3-key shape from R-13ZB-EZZK still works.
func TestR_Y5QZ_UNB2_ResultEventHasUsageFields(t *testing.T) {
	stats := wire.IterationStats{
		NumTurns:   2,
		DurationMs: 1234,
		Usage: wire.UsageTotals{
			InputTokens:              100,
			OutputTokens:             50,
			CacheReadInputTokens:     10,
			CacheCreationInputTokens: 5,
		},
		ModelUsage: map[string]wire.ModelUsageEntry{
			"claude-haiku-4-5": {
				InputTokens:              100,
				OutputTokens:             50,
				CacheReadInputTokens:     10,
				CacheCreationInputTokens: 5,
				CostUSD:                  0.000123,
				ContextWindow:            200000,
				MaxOutputTokens:          8192,
			},
		},
	}

	ev, err := wire.NewResultEventFull(map[string]string{"status": "DONE"}, false, stats)
	if err != nil {
		t.Fatalf("NewResultEventFull: %v", err)
	}

	var buf bytes.Buffer
	if err := wire.Encode(&buf, ev); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// All top-level accounting fields must be present.
	for _, k := range []string{"type", "structured_output", "is_error", "num_turns", "duration_ms", "total_cost_usd", "usage", "modelUsage"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}

	if got["type"] != "result" {
		t.Errorf("type = %v, want %q", got["type"], "result")
	}
	if got["is_error"] != false {
		t.Errorf("is_error = %v, want false", got["is_error"])
	}
	if int(got["num_turns"].(float64)) != 2 {
		t.Errorf("num_turns = %v, want 2", got["num_turns"])
	}
	if int(got["duration_ms"].(float64)) != 1234 {
		t.Errorf("duration_ms = %v, want 1234", got["duration_ms"])
	}
	if got["total_cost_usd"].(float64) != 0.000123 {
		t.Errorf("total_cost_usd = %v, want 0.000123", got["total_cost_usd"])
	}

	// usage sub-object
	usage, ok := got["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage not an object: %v", got["usage"])
	}
	if int(usage["input_tokens"].(float64)) != 100 {
		t.Errorf("usage.input_tokens = %v, want 100", usage["input_tokens"])
	}
	if int(usage["output_tokens"].(float64)) != 50 {
		t.Errorf("usage.output_tokens = %v, want 50", usage["output_tokens"])
	}
	if int(usage["cache_read_input_tokens"].(float64)) != 10 {
		t.Errorf("usage.cache_read_input_tokens = %v, want 10", usage["cache_read_input_tokens"])
	}
	if int(usage["cache_creation_input_tokens"].(float64)) != 5 {
		t.Errorf("usage.cache_creation_input_tokens = %v, want 5", usage["cache_creation_input_tokens"])
	}

	// modelUsage sub-object
	modelUsage, ok := got["modelUsage"].(map[string]any)
	if !ok {
		t.Fatalf("modelUsage not an object: %v", got["modelUsage"])
	}
	entry, ok := modelUsage["claude-haiku-4-5"].(map[string]any)
	if !ok {
		t.Fatalf("modelUsage[claude-haiku-4-5] not an object")
	}
	if int(entry["inputTokens"].(float64)) != 100 {
		t.Errorf("modelUsage.inputTokens = %v, want 100", entry["inputTokens"])
	}
	if int(entry["outputTokens"].(float64)) != 50 {
		t.Errorf("modelUsage.outputTokens = %v, want 50", entry["outputTokens"])
	}
	if entry["costUSD"].(float64) != 0.000123 {
		t.Errorf("modelUsage.costUSD = %v, want 0.000123", entry["costUSD"])
	}
	if int(entry["contextWindow"].(float64)) != 200000 {
		t.Errorf("modelUsage.contextWindow = %v, want 200000", entry["contextWindow"])
	}
	if int(entry["maxOutputTokens"].(float64)) != 8192 {
		t.Errorf("modelUsage.maxOutputTokens = %v, want 8192", entry["maxOutputTokens"])
	}

	// NewResultEvent (no stats) must still produce exactly the 3 MVP keys.
	mvpEv, err := wire.NewResultEvent(map[string]string{"status": "CONTINUE"}, false)
	if err != nil {
		t.Fatalf("NewResultEvent: %v", err)
	}
	buf.Reset()
	if err := wire.Encode(&buf, mvpEv); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var mvp map[string]any
	if err := json.Unmarshal(buf.Bytes(), &mvp); err != nil {
		t.Fatalf("decode mvp: %v", err)
	}
	for k := range mvp {
		switch k {
		case "type", "structured_output", "is_error":
		default:
			t.Errorf("NewResultEvent emitted unexpected key %q (accounting fields must be omitted when zero)", k)
		}
	}
}

// R-13ZB-EZZK: the result event has shape
// {"type":"result","structured_output":<json-value>,"is_error":<bool>}.
// structured_output is required; is_error flags iteration-level
// failure. Optional fields (num_turns, duration_ms, total_cost_usd,
// usage) are out of MVP scope and must not leak into the encoded
// object as zero-valued required fields.
func TestR_13ZB_EZZK_ResultEventShape(t *testing.T) {
	ev, err := wire.NewResultEvent(map[string]string{"status": "CONTINUE"}, false)
	if err != nil {
		t.Fatalf("NewResultEvent: %v", err)
	}
	if ev.Type != "result" {
		t.Errorf("Type = %q, want %q", ev.Type, "result")
	}

	var buf bytes.Buffer
	if err := wire.Encode(&buf, ev); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	line := strings.TrimSuffix(buf.String(), "\n")

	// Decode generically and assert exactly the three MVP keys.
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["type"] != "result" {
		t.Errorf("type = %v, want %q", got["type"], "result")
	}
	so, ok := got["structured_output"].(map[string]any)
	if !ok {
		t.Fatalf("structured_output not an object: %v", got["structured_output"])
	}
	if so["status"] != "CONTINUE" {
		t.Errorf("structured_output.status = %v, want %q", so["status"], "CONTINUE")
	}
	if got["is_error"] != false {
		t.Errorf("is_error = %v, want false", got["is_error"])
	}

	wantKeys := map[string]bool{"type": true, "structured_output": true, "is_error": true}
	for k := range got {
		if !wantKeys[k] {
			t.Errorf("unexpected key in MVP result event: %q", k)
		}
	}
	for k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("missing required key: %q", k)
		}
	}

	// is_error: true must round-trip with a structured_output value
	// that the schema would reject, per R-1OPL-X3LD's pattern (the
	// value here doesn't matter — we just assert is_error encodes).
	errEv, err := wire.NewResultEvent(map[string]string{"oops": "schema-fail"}, true)
	if err != nil {
		t.Fatalf("NewResultEvent: %v", err)
	}
	buf.Reset()
	if err := wire.Encode(&buf, errEv); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var got2 map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got2["is_error"] != true {
		t.Errorf("is_error = %v, want true", got2["is_error"])
	}

	// structured_output accepts arbitrary JSON values, not just objects.
	scalarEv, err := wire.NewResultEvent("DONE", false)
	if err != nil {
		t.Fatalf("NewResultEvent scalar: %v", err)
	}
	buf.Reset()
	if err := wire.Encode(&buf, scalarEv); err != nil {
		t.Fatalf("Encode scalar: %v", err)
	}
	var got3 map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got3); err != nil {
		t.Fatalf("decode scalar: %v", err)
	}
	if got3["structured_output"] != "DONE" {
		t.Errorf("structured_output scalar = %v, want %q", got3["structured_output"], "DONE")
	}
}
