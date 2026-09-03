package agentkit

import (
	"encoding/json"
	"fmt"
)

// History is an ordered transcript whose entries end at completed turn
// boundaries.
type History []Message

type jsonMessage struct {
	Role   Role              `json:"role"`
	Blocks []json.RawMessage `json:"blocks"`
}

type blockTag struct {
	Type string `json:"type"`
}

type jsonText struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Provider json.RawMessage `json:"provider"`
}

type jsonReasoning struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Redacted bool            `json:"redacted"`
	Provider json.RawMessage `json:"provider"`
}

type jsonToolUse struct {
	Type     string          `json:"type"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Provider json.RawMessage `json:"provider"`
}

type jsonToolResult struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	Content   string          `json:"content"`
	IsError   bool            `json:"is_error"`
	Provider  json.RawMessage `json:"provider"`
}

// MarshalJSON writes a stable, explicitly tagged representation of every
// block in the transcript.
func (h History) MarshalJSON() ([]byte, error) {
	messages := make([]jsonMessage, len(h))
	for messageIndex, message := range h {
		blocks := make([]json.RawMessage, len(message.Blocks))
		for blockIndex, block := range message.Blocks {
			encoded, err := marshalBlock(block)
			if err != nil {
				return nil, fmt.Errorf("agentkit: history message %d block %d: %w", messageIndex, blockIndex, err)
			}
			blocks[blockIndex] = encoded
		}
		messages[messageIndex] = jsonMessage{Role: message.Role, Blocks: blocks}
	}
	return json.Marshal(messages)
}

func marshalBlock(block Block) (json.RawMessage, error) {
	var value any
	switch block := block.(type) {
	case Text:
		value = jsonText{Type: block.BlockType(), Text: block.Text, Provider: block.Provider}
	case Reasoning:
		value = jsonReasoning{Type: block.BlockType(), Text: block.Text, Redacted: block.Redacted, Provider: block.Provider}
	case ToolUse:
		value = jsonToolUse{Type: block.BlockType(), ID: block.ID, Name: block.Name, Input: block.Input, Provider: block.Provider}
	case ToolResult:
		value = jsonToolResult{Type: block.BlockType(), ToolUseID: block.ToolUseID, Content: block.Content, IsError: block.IsError, Provider: block.Provider}
	default:
		return nil, fmt.Errorf("unsupported block type %T", block)
	}
	return json.Marshal(value)
}

// UnmarshalJSON reconstructs the concrete sealed block variants named by each
// serialization discriminator.
func (h *History) UnmarshalJSON(data []byte) error {
	var messages []jsonMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return err
	}

	history := make(History, len(messages))
	for messageIndex, message := range messages {
		blocks := make([]Block, len(message.Blocks))
		for blockIndex, block := range message.Blocks {
			decoded, err := unmarshalBlock(block)
			if err != nil {
				return fmt.Errorf("agentkit: history message %d block %d: %w", messageIndex, blockIndex, err)
			}
			blocks[blockIndex] = decoded
		}
		history[messageIndex] = Message{Role: message.Role, Blocks: blocks}
	}
	*h = history
	return nil
}

func unmarshalBlock(data []byte) (Block, error) {
	var tag blockTag
	if err := json.Unmarshal(data, &tag); err != nil {
		return nil, err
	}
	switch tag.Type {
	case Text{}.BlockType():
		var block jsonText
		if err := json.Unmarshal(data, &block); err != nil {
			return nil, err
		}
		return Text{Text: block.Text, Provider: block.Provider}, nil
	case Reasoning{}.BlockType():
		var block jsonReasoning
		if err := json.Unmarshal(data, &block); err != nil {
			return nil, err
		}
		return Reasoning{Text: block.Text, Redacted: block.Redacted, Provider: block.Provider}, nil
	case ToolUse{}.BlockType():
		var block jsonToolUse
		if err := json.Unmarshal(data, &block); err != nil {
			return nil, err
		}
		return ToolUse{ID: block.ID, Name: block.Name, Input: block.Input, Provider: block.Provider}, nil
	case ToolResult{}.BlockType():
		var block jsonToolResult
		if err := json.Unmarshal(data, &block); err != nil {
			return nil, err
		}
		return ToolResult{ToolUseID: block.ToolUseID, Content: block.Content, IsError: block.IsError, Provider: block.Provider}, nil
	default:
		return nil, fmt.Errorf("unknown block type %q", tag.Type)
	}
}
