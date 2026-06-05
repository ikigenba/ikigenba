package wire_test

import (
	"testing"

	"agentkit/wire"
)

type countingWriter struct {
	writes int
	bytes  int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.writes++
	c.bytes += len(p)
	return len(p), nil
}

// R-T6MT-WOH2: stdout writes are line-flushed. Each event must be
// handed to the underlying writer in a single Write call so ralph-loops
// reads complete lines as they're produced, never partial events glued
// to the next iteration's output.
func TestR_T6MT_WOH2_LineFlushedWrites(t *testing.T) {
	event := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "hello"},
			},
		},
	}

	cw := &countingWriter{}
	if err := wire.Encode(cw, event); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if cw.writes != 1 {
		t.Errorf("Encode made %d Write calls, want 1 (R-T6MT-WOH2)", cw.writes)
	}
	if cw.bytes == 0 || cw.bytes < len("\n") {
		t.Errorf("Encode wrote %d bytes, want non-empty line", cw.bytes)
	}

	// Three back-to-back events: one Write per event, no batching.
	cw = &countingWriter{}
	for i := 0; i < 3; i++ {
		if err := wire.Encode(cw, event); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}
	if cw.writes != 3 {
		t.Errorf("3 Encodes made %d Write calls, want 3 (R-T6MT-WOH2)", cw.writes)
	}
}
