package wire_test

import (
	"strings"
	"testing"

	"agentkit/wire"
)

// R-UDBB-ANFD: ikigai-cli reads exactly one event type from stdin in v1:
// user events with shape
// {"type":"user","message":{"role":"user","content":[{"type":"text","text":"<prompt>"}]}}.
func TestR_UDBB_ANFD_ParseStdinUserEvent(t *testing.T) {
	t.Run("single_text_block", func(t *testing.T) {
		line := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`)
		ev, err := wire.ParseStdinUserEvent(line)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if ev.Type != "user" {
			t.Errorf("Type = %q, want \"user\"", ev.Type)
		}
		if ev.Message.Role != "user" {
			t.Errorf("Message.Role = %q, want \"user\"", ev.Message.Role)
		}
		if len(ev.Message.Content) != 1 {
			t.Fatalf("len(Content) = %d, want 1", len(ev.Message.Content))
		}
		blk, ok := ev.Message.Content[0].(wire.TextBlock)
		if !ok {
			t.Fatalf("content[0] = %T, want wire.TextBlock", ev.Message.Content[0])
		}
		if blk.Type != "text" || blk.Text != "hello" {
			t.Errorf("block = %+v, want type=text text=hello", blk)
		}
	})

	t.Run("multiple_text_blocks", func(t *testing.T) {
		line := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}}`)
		ev, err := wire.ParseStdinUserEvent(line)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(ev.Message.Content) != 2 {
			t.Fatalf("len(Content) = %d, want 2", len(ev.Message.Content))
		}
		want := []string{"a", "b"}
		for i, w := range want {
			blk := ev.Message.Content[i].(wire.TextBlock)
			if blk.Text != w {
				t.Errorf("content[%d].text = %q, want %q", i, blk.Text, w)
			}
		}
	})

	t.Run("utf8_text", func(t *testing.T) {
		line := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"héllo — 世界"}]}}`)
		ev, err := wire.ParseStdinUserEvent(line)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		blk := ev.Message.Content[0].(wire.TextBlock)
		if blk.Text != "héllo — 世界" {
			t.Errorf("Text = %q", blk.Text)
		}
	})

	t.Run("empty_content_array", func(t *testing.T) {
		line := []byte(`{"type":"user","message":{"role":"user","content":[]}}`)
		ev, err := wire.ParseStdinUserEvent(line)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if ev.Message.Content == nil {
			t.Errorf("Content = nil, want empty slice")
		}
		if len(ev.Message.Content) != 0 {
			t.Errorf("len(Content) = %d, want 0", len(ev.Message.Content))
		}
	})

	rejectCases := []struct {
		name    string
		line    string
		wantSub string
	}{
		{
			name:    "wrong_type_assistant",
			line:    `{"type":"assistant","message":{"role":"assistant","content":[]}}`,
			wantSub: `type = "assistant"`,
		},
		{
			name:    "wrong_type_result",
			line:    `{"type":"result","structured_output":{"status":"DONE"},"is_error":false}`,
			wantSub: `type = "result"`,
		},
		{
			name:    "wrong_role",
			line:    `{"type":"user","message":{"role":"assistant","content":[]}}`,
			wantSub: `role = "assistant"`,
		},
		{
			name:    "wrong_block_type",
			line:    `{"type":"user","message":{"role":"user","content":[{"type":"image","text":"x"}]}}`,
			wantSub: `content[0].type = "image"`,
		},
		{
			name:    "invalid_json",
			line:    `{not json`,
			wantSub: "invalid JSON",
		},
	}
	for _, tc := range rejectCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := wire.ParseStdinUserEvent([]byte(tc.line))
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}
