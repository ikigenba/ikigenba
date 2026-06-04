package wire_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"ralph/internal/engine/wire"
)

// R-RCS9-92FK: stdout is newline-delimited JSON. Each line is exactly
// one complete JSON object with no pretty-printing, no leading or
// trailing whitespace inside the line, and no embedded newlines that
// would split a logical event across lines.
func TestR_RCS9_92FK_EncodeIsOneLinePerEvent(t *testing.T) {
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	// String value contains characters that a careless encoder might
	// pretty-print or pass through unescaped: real newline, tab, and
	// non-ASCII text.
	event := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":    "assistant",
			"content": []textBlock{{Type: "text", Text: "line one\nline two\tend é"}},
		},
	}

	var buf bytes.Buffer
	if err := wire.Encode(&buf, event); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out := buf.String()

	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("output not newline-terminated: %q", out)
	}
	body := strings.TrimSuffix(out, "\n")
	if strings.Contains(body, "\n") {
		t.Errorf("encoded line contains an embedded newline (R-RCS9-92FK): %q", body)
	}
	if body != strings.TrimSpace(body) {
		t.Errorf("encoded line has leading/trailing whitespace inside the line: %q", body)
	}
	// Two events back-to-back must produce two distinct lines.
	buf.Reset()
	if err := wire.Encode(&buf, event); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := wire.Encode(&buf, event); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines for 2 events, got %d: %q", len(lines), buf.String())
	}
	// Each line must round-trip as a JSON object preserving the
	// original string value (newline survives as-is after decoding).
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("decode line: %v", err)
	}
	gotText := got["message"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if gotText != "line one\nline two\tend é" {
		t.Errorf("round-trip text mismatch: %q", gotText)
	}
}
