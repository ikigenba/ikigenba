package wire

// ThinkingBlock is an assistant content block carrying extended-thinking
// / reasoning text from the model.
//
// R-SA9P-R1H4: shape is {"type":"thinking","thinking":"<string>"}.
// NewThinkingBlock fixes Type to "thinking" so callers cannot forge a
// malformed block.
type ThinkingBlock struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

// NewThinkingBlock fixes Type to "thinking".
func NewThinkingBlock(thinking string) ThinkingBlock {
	return ThinkingBlock{Type: "thinking", Thinking: thinking}
}
