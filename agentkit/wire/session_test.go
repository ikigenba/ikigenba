package wire_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agentkit/wire"
)

// R-0I14-J4N2: every iteration ends with exactly one `result` event
// emitted on stdout, and no assistant or user events follow it.
func TestR_0I14_J4N2_ExactlyOneResultTerminator(t *testing.T) {
	var buf bytes.Buffer
	s := wire.NewSession(&buf)

	if err := s.EmitAssistant(wire.NewAssistantEvent(map[string]any{
		"type": "text",
		"text": "hi",
	})); err != nil {
		t.Fatalf("EmitAssistant: %v", err)
	}
	if err := s.EmitUser(wire.NewUserEvent()); err != nil {
		t.Fatalf("EmitUser: %v", err)
	}
	if s.Finished() {
		t.Fatalf("Finished true before result")
	}

	res, err := wire.NewResultEvent(map[string]string{"status": "CONTINUE"}, false)
	if err != nil {
		t.Fatalf("NewResultEvent: %v", err)
	}
	if err := s.EmitResult(res); err != nil {
		t.Fatalf("EmitResult: %v", err)
	}
	if !s.Finished() {
		t.Fatalf("Finished false after result")
	}

	// A second result must be rejected.
	if err := s.EmitResult(res); !errors.Is(err, wire.ErrResultAlreadyEmitted) {
		t.Errorf("second EmitResult err = %v, want ErrResultAlreadyEmitted", err)
	}

	// No assistant or user event may follow the result.
	if err := s.EmitAssistant(wire.NewAssistantEvent()); !errors.Is(err, wire.ErrAfterResult) {
		t.Errorf("post-result EmitAssistant err = %v, want ErrAfterResult", err)
	}
	if err := s.EmitUser(wire.NewUserEvent()); !errors.Is(err, wire.ErrAfterResult) {
		t.Errorf("post-result EmitUser err = %v, want ErrAfterResult", err)
	}

	// Wire output must contain exactly one result line and it must be
	// the last line.
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	resultCount := 0
	resultIdx := -1
	for i, line := range lines {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %d not JSON: %v (%q)", i, err, line)
		}
		if ev["type"] == "result" {
			resultCount++
			resultIdx = i
		}
	}
	if resultCount != 1 {
		t.Errorf("result event count = %d, want 1", resultCount)
	}
	if resultIdx != len(lines)-1 {
		t.Errorf("result at index %d, want last (%d)", resultIdx, len(lines)-1)
	}
}
