package wire

import "encoding/json"

// ToolResultBlock is a user content block carrying the result of a
// previously-issued tool_use.
//
// R-Z6H1-M2PZ: shape is
// {"type":"tool_result","tool_use_id":"<id>","is_error":<bool>,"content":<json-value>}.
// Content is a JSON value (string, structured object/array, or null);
// RawMessage keeps that union honest. NewToolResultBlock fixes Type to
// "tool_result" so callers cannot forge a malformed block.
type ToolResultBlock struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

// NewToolResultBlock fixes Type to "tool_result" and marshals content
// into a JSON value. A nil content becomes JSON null.
func NewToolResultBlock(toolUseID string, isError bool, content any) (ToolResultBlock, error) {
	raw, err := json.Marshal(content)
	if err != nil {
		return ToolResultBlock{}, err
	}
	return ToolResultBlock{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		IsError:   isError,
		Content:   raw,
	}, nil
}
