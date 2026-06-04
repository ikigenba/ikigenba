package wire_test

import (
	"bytes"
	"strings"
	"testing"

	"ralph/internal/engine/wire"
)

const mib = 1 << 20

// R-SJGQ-N1DV: each output line is at most 16 MiB after encoding.
// The cap is ralph-loops' scanner buffer; tool results that would
// exceed it must be truncated by the emitting tool (per tools.md),
// not at the wire layer. This test pins both sides of that contract:
//
//  1. A tool_result carrying the maximum Bash output budgeted by
//     R-LXOL-5ACL (30000 bytes) encodes to far less than 16 MiB,
//     so well-behaved tools never approach the cap.
//  2. The wire layer does NOT enforce a cap of its own — handed an
//     oversize payload, Encode returns nil and writes every byte.
//     Enforcement lives in the tool, where truncation is visible to
//     the model; silent wire-layer trimming would violate that.
func TestR_SJGQ_N1DV_LineSizeBudget(t *testing.T) {
	// Bash's own truncation cap is the real-world ceiling for tool
	// output that reaches the wire layer.
	bashCap := strings.Repeat("x", 30000)
	event := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_01",
					"content":     bashCap,
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := wire.Encode(&buf, event); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if buf.Len() >= 16*mib {
		t.Errorf("Bash-cap tool_result encoded to %d bytes, want < 16 MiB (R-SJGQ-N1DV)", buf.Len())
	}
	if buf.Len() > 64*1024 {
		t.Errorf("Bash-cap tool_result encoded to %d bytes, want well under 64 KiB — tool budget headroom", buf.Len())
	}

	// Wire layer must not enforce its own cap. A payload above 16
	// MiB encodes without error and writes every byte; truncation
	// belongs to the tool. See tools.md R-LXOL-5ACL.
	oversize := strings.Repeat("y", 17*mib)
	event2 := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_02",
					"content":     oversize,
				},
			},
		},
	}

	var buf2 bytes.Buffer
	if err := wire.Encode(&buf2, event2); err != nil {
		t.Fatalf("Encode of oversize payload returned error %v; wire layer must not enforce a cap (R-SJGQ-N1DV)", err)
	}
	if buf2.Len() <= 16*mib {
		t.Errorf("Encode of oversize payload wrote %d bytes; wire layer appears to have truncated (R-SJGQ-N1DV)", buf2.Len())
	}
	if !bytes.Contains(buf2.Bytes(), []byte(oversize)) {
		t.Errorf("Encode of oversize payload did not preserve the full content; wire layer must not truncate (R-SJGQ-N1DV)")
	}
}
