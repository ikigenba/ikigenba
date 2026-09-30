package agentkit

import (
	"testing"
)

func TestBlockVariantsAndDiscriminators(t *testing.T) {
	// R-20NL-671U
	variants := []struct {
		block Block
		want  string
	}{
		{block: Text{}, want: "text"},
		{block: Reasoning{}, want: "reasoning"},
		{block: ToolUse{}, want: "tool_use"},
		{block: ToolResult{}, want: "tool_result"},
	}
	for _, variant := range variants {
		if got := variant.block.BlockType(); got != variant.want {
			t.Errorf("%T.BlockType() = %q, want %q", variant.block, got, variant.want)
		}
	}
}
