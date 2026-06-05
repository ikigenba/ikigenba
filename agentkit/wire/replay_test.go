package wire_test

import (
	"bytes"
	"strings"
	"testing"

	"agentkit/wire"
)

// R-ZUV1-9HJV: when ikigai-cli consumes a stdin user event, it emits
// an equivalent `user` event on stdout — same role, same text content
// blocks — before the model's response begins. Round-tripping a stdin
// line through ParseStdinUserEvent and wire.Encode must produce that
// canonical replay shape.
func TestR_ZUV1_9HJV_ReplayUserInput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single_text_block",
			in:   `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`,
			want: `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}` + "\n",
		},
		{
			name: "multiple_text_blocks",
			in:   `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}}`,
			want: `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}}` + "\n",
		},
		{
			name: "utf8_text",
			in:   `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"héllo — 世界"}]}}`,
			want: `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"héllo — 世界"}]}}` + "\n",
		},
		{
			name: "empty_content_array",
			in:   `{"type":"user","message":{"role":"user","content":[]}}`,
			want: `{"type":"user","message":{"role":"user","content":[]}}` + "\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := wire.ParseStdinUserEvent([]byte(tc.in))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			var buf bytes.Buffer
			if err := wire.Encode(&buf, ev); err != nil {
				t.Fatalf("encode: %v", err)
			}
			got := buf.String()
			if got != tc.want {
				t.Errorf("replay mismatch\n got: %s\nwant: %s", got, tc.want)
			}
			// The replay must not serialize content as `null` even
			// when empty — the documented shape uses `[]`.
			if strings.Contains(got, `"content":null`) {
				t.Errorf("content serialized as null: %s", got)
			}
		})
	}
}
