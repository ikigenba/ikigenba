package agentkit

import (
	"encoding/json"
	"fmt"
)

type anthropicWire struct{ wireCodec }

func newAnthropicWire(classifier wireClassifier) WireFormat {
	wire := &anthropicWire{}
	wire.wireCodec = wireCodec{
		encode:     wire.encodeRequest,
		decoder:    newAnthropicDecoder,
		reserved:   []string{"anthropic"},
		classifier: classifier,
		capabilities: wireCapabilities{
			name:       "Anthropic Messages",
			reasoning:  reasoningShapeOff | reasoningShapeBudget,
			toolChoice: toolChoiceShapeRequired | toolChoiceShapeTool,
		},
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

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicRequest struct {
	Messages   []anthropicMessage   `json:"messages"`
	Thinking   *anthropicThinking   `json:"thinking,omitempty"`
	ToolChoice *anthropicToolChoice `json:"tool_choice,omitempty"`
	Tools      json.RawMessage      `json:"tools,omitempty"`
}

func buildAnthropicMessages(history []Message) ([]anthropicMessage, error) {
	messages := make([]anthropicMessage, 0, len(history))
	for _, message := range history {
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
	return messages, nil
}

func configureAnthropicRequest(request *anthropicRequest, settings Settings) {
	switch settings.Reasoning.Mode {
	case ReasoningOff:
		request.Thinking = &anthropicThinking{Type: "disabled"}
	case ReasoningBudget:
		request.Thinking = &anthropicThinking{Type: "enabled", BudgetTokens: settings.Reasoning.Budget}
	}
	switch settings.ToolChoice.Mode {
	case ToolChoiceRequired:
		request.ToolChoice = &anthropicToolChoice{Type: "any"}
	case ToolChoiceTool:
		request.ToolChoice = &anthropicToolChoice{Type: "tool", Name: settings.ToolChoice.Name}
	}
}

func (w *anthropicWire) encodeRequest(state RequestState) ([]byte, error) {
	messages, err := buildAnthropicMessages(state.History)
	if err != nil {
		return nil, err
	}
	request := anthropicRequest{Messages: messages}
	configureAnthropicRequest(&request, state.Settings)
	if len(state.Tools) > 0 {
		request.Tools, err = w.RenderTools(state.Tools)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(request)
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

func (w *anthropicWire) RenderTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderAnthropicTools(tools)
}

func (w *anthropicWire) withClassifier(classifier wireClassifier) WireFormat {
	clone := &anthropicWire{wireCodec: w.cloneWithClassifier(classifier)}
	clone.encode = clone.encodeRequest
	return clone
}
