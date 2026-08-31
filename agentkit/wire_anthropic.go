package agentkit

import (
	"encoding/json"
	"fmt"
)

type anthropicWire struct{ wireCodec }

func newAnthropicWire(classifier wireClassifier) WireFormat {
	wire := &anthropicWire{}
	wire.wireCodec = wireCodec{
		encode:     encodeAnthropicRequest,
		decoder:    newAnthropicDecoder,
		render:     renderAnthropicTools,
		replay:     replayEncodingProviderBlock,
		reserved:   []string{"anthropic"},
		classifier: classifier,
	}
	return wire
}

type anthropicContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

func encodeAnthropicRequest(state RequestState) ([]byte, error) {
	messages := make([]anthropicMessage, 0, len(state.History))
	for _, message := range state.History {
		encoded := anthropicMessage{Role: anthropicRole(message.Role)}
		for _, block := range message.Blocks {
			switch block := block.(type) {
			case Text:
				encoded.Content = append(encoded.Content, anthropicContent{Type: "text", Text: block.Text})
			case Reasoning:
				if len(block.Provider) > 0 {
					var replay anthropicContent
					if err := json.Unmarshal(block.Provider, &replay); err != nil {
						return nil, fmt.Errorf("agentkit: invalid Anthropic reasoning replay: %w", err)
					}
					encoded.Content = append(encoded.Content, replay)
				} else {
					encoded.Content = append(encoded.Content, anthropicContent{Type: "thinking", Thinking: block.Text})
				}
			case ToolUse:
				encoded.Content = append(encoded.Content, anthropicContent{Type: "tool_use", ID: block.ID, Name: block.Name, Input: block.Input})
			case ToolResult:
				encoded.Content = append(encoded.Content, anthropicContent{Type: "tool_result", ToolUseID: block.ToolUseID, Content: block.Content, IsError: block.IsError})
			}
		}
		messages = append(messages, encoded)
	}
	encoded, err := json.Marshal(struct {
		Messages []anthropicMessage `json:"messages"`
	}{Messages: messages})
	return append(encoded, '\n'), err
}

func anthropicRole(role Role) string {
	if role == RoleAssistant {
		return "assistant"
	}
	return "user"
}

func newAnthropicDecoder() frameDecoder {
	var text string
	var usage usageNormalizer
	return func(frame []byte) (*Message, usageFragment, bool, error) {
		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Text  string `json:"text"`
				Usage struct {
					OutputTokens *int64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"delta"`
			Message struct {
				Usage struct {
					InputTokens  *int64 `json:"input_tokens"`
					CachedTokens *int64 `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(frame, &event); err != nil {
			return nil, usageFragment{}, false, err
		}
		switch event.Type {
		case "message_start":
			fragment := usage.update(event.Message.Usage.InputTokens, event.Message.Usage.CachedTokens, nil, nil)
			return nil, fragment, true, nil
		case "content_block_delta":
			text += event.Delta.Text
		case "message_delta":
			return nil, usage.update(nil, nil, event.Delta.Usage.OutputTokens, nil), true, nil
		case "message_stop":
			message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: text}}}
			return &message, usageFragment{}, false, nil
		}
		return nil, usageFragment{}, false, nil
	}
}

func renderAnthropicTools(tools []Tool) (json.RawMessage, error) {
	type declaration struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	declarations := make([]declaration, len(tools))
	for index, tool := range tools {
		declarations[index] = declaration{tool.Name(), tool.Description(), tool.Schema()}
	}
	return json.Marshal(declarations)
}
