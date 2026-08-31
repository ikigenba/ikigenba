package agentkit

import "encoding/json"

// Role names who authored a Message. It is a closed enumeration; a wire that
// carries one role out-of-band renders it from the corresponding Message.
type Role int

const (
	// RoleSystem identifies system or developer context.
	RoleSystem Role = iota
	// RoleUser identifies consumer input.
	RoleUser
	// RoleAssistant identifies model output.
	RoleAssistant
	// RoleTool identifies tool results returned to the model.
	RoleTool
)

// Message is one entry in a History: a role and its blocks, in order.
type Message struct {
	Role   Role
	Blocks []Block
}

// Block is one atom of message content. Its unexported marker seals the set of
// variants while BlockType supplies the History serialization discriminator.
type Block interface {
	BlockType() string
	isBlock()
}

// Text is spoken content: a user prompt or a model's visible reply.
type Text struct {
	Text     string
	Provider json.RawMessage
}

// BlockType returns Text's stable serialization discriminator.
func (Text) BlockType() string { return "text" }
func (Text) isBlock()          {}

// Reasoning is a span of model thinking selected by the provider wire parser.
type Reasoning struct {
	Text     string
	Redacted bool
	Provider json.RawMessage
}

// BlockType returns Reasoning's stable serialization discriminator.
func (Reasoning) BlockType() string { return "reasoning" }
func (Reasoning) isBlock()          {}

// ToolUse is the model's request to call a tool.
type ToolUse struct {
	ID       string
	Name     string
	Input    json.RawMessage
	Provider json.RawMessage
}

// BlockType returns ToolUse's stable serialization discriminator.
func (ToolUse) BlockType() string { return "tool_use" }
func (ToolUse) isBlock()          {}

// ToolResult is the outcome of running a tool, fed back to the model.
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
	Provider  json.RawMessage
}

// BlockType returns ToolResult's stable serialization discriminator.
func (ToolResult) BlockType() string { return "tool_result" }
func (ToolResult) isBlock()          {}
