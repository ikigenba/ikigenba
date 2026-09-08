package agentkit

import (
	"encoding"
	"encoding/json"
	"testing"
)

var (
	_ encoding.TextMarshaler   = ToolChoiceMode(0)
	_ encoding.TextUnmarshaler = (*ToolChoiceMode)(nil)
)

func TestToolChoiceModeTextAndJSON(t *testing.T) {
	// R-6ANQ-8IVA: ToolChoiceMode uses the declared text in text and JSON encodings.
	tests := []struct {
		mode ToolChoiceMode
		text string
	}{
		{mode: ToolChoiceAuto, text: "auto"},
		{mode: ToolChoiceNone, text: "none"},
		{mode: ToolChoiceRequired, text: "required"},
		{mode: ToolChoiceTool, text: "tool"},
	}

	for _, test := range tests {
		t.Run(test.text, func(t *testing.T) {
			text, err := test.mode.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() error = %v", err)
			}
			if got := string(text); got != test.text {
				t.Fatalf("MarshalText() = %q, want %q", got, test.text)
			}

			var decoded ToolChoiceMode
			if err := decoded.UnmarshalText(text); err != nil {
				t.Fatalf("UnmarshalText(%q) error = %v", text, err)
			}
			if decoded != test.mode {
				t.Fatalf("UnmarshalText(%q) = %d, want %d", text, decoded, test.mode)
			}

			encoded, err := json.Marshal(ToolChoice{Mode: test.mode})
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			var body map[string]any
			if err := json.Unmarshal(encoded, &body); err != nil {
				t.Fatalf("json.Unmarshal(%q) error = %v", encoded, err)
			}
			if got := body["Mode"]; got != test.text {
				t.Fatalf("JSON Mode = %#v, want %q", got, test.text)
			}
		})
	}

	mode := ToolChoiceRequired
	if err := mode.UnmarshalText([]byte("sometimes")); err == nil {
		t.Fatal("UnmarshalText accepted an unknown mode")
	}
	if mode != ToolChoiceRequired {
		t.Fatalf("failed UnmarshalText changed mode to %d", mode)
	}
	if _, err := ToolChoiceMode(99).MarshalText(); err == nil {
		t.Fatal("MarshalText accepted an unknown mode")
	}
}
