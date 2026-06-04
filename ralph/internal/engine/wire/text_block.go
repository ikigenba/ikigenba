package wire

// TextBlock is an assistant content block carrying model text output.
//
// R-WQOA-2LBZ: shape is {"type":"text","text":"<string>"}. NewTextBlock
// fixes Type to "text" so callers cannot forge a malformed block.
type TextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// NewTextBlock fixes Type to "text".
func NewTextBlock(text string) TextBlock {
	return TextBlock{Type: "text", Text: text}
}
