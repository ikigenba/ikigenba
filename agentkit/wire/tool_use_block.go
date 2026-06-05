package wire

import "encoding/json"

// ToolUseBlock is an assistant content block representing a tool
// invocation.
//
// R-XCMG-YGOH: shape is
// {"type":"tool_use","id":"<unique-id>","name":"<tool-name>","input":<json-value>}.
// Input is a JSON value (object, string, or null); RawMessage keeps
// that union honest. NewToolUseBlock fixes Type to "tool_use" so
// callers cannot forge a malformed block.
type ToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// NewToolUseBlock fixes Type to "tool_use" and marshals input into a
// JSON value. A nil input becomes JSON null.
func NewToolUseBlock(id, name string, input any) (ToolUseBlock, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return ToolUseBlock{}, err
	}
	return ToolUseBlock{Type: "tool_use", ID: id, Name: name, Input: raw}, nil
}
