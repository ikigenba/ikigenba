package agentkit

import (
	"encoding/json"
	"fmt"
)

type openAIChatWire struct{ wireCodec }

func newOpenAIChatWire(classifier wireClassifier) WireFormat {
	wire := &openAIChatWire{}
	wire.wireCodec = wireCodec{
		encode:     encodeOpenAIChatRequest,
		decoder:    newOpenAIChatDecoder,
		render:     renderOpenAIChatTools,
		reserved:   []string{"openai"},
		classifier: classifier,
	}
	return wire
}

type chatMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func encodeOpenAIChatRequest(state RequestState) ([]byte, error) {
	messages := make([]chatMessage, 0, len(state.History))
	for _, message := range state.History {
		encoded := chatMessage{Role: openAIRole(message.Role)}
		for _, block := range message.Blocks {
			switch block := block.(type) {
			case Text:
				encoded.Content += block.Text
			case Reasoning:
				encoded.ReasoningContent += block.Text
				if len(block.Provider) > 0 {
					var replay struct {
						ReasoningContent string `json:"reasoning_content"`
					}
					if err := json.Unmarshal(block.Provider, &replay); err != nil {
						return nil, fmt.Errorf("agentkit: invalid OpenAI Chat reasoning replay: %w", err)
					}
					if replay.ReasoningContent != "" {
						encoded.ReasoningContent = replay.ReasoningContent
					}
				}
			case ToolUse:
				encoded.ToolCalls = append(encoded.ToolCalls, chatToolCall{
					ID: block.ID, Type: "function",
					Function: chatToolFunction{Name: block.Name, Arguments: string(block.Input)},
				})
			case ToolResult:
				encoded.ToolCallID = block.ToolUseID
				encoded.Content += block.Content
			}
		}
		messages = append(messages, encoded)
	}
	encoded, err := json.Marshal(struct {
		Messages []chatMessage `json:"messages"`
	}{Messages: messages})
	return append(encoded, '\n'), err
}

func newOpenAIChatDecoder() frameDecoder {
	var text string
	var normalizer usageNormalizer
	return func(frame []byte) (*Message, usageFragment, bool, error) {
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     *int64 `json:"prompt_tokens"`
				CompletionTokens *int64 `json:"completion_tokens"`
				PromptDetails    struct {
					CachedTokens *int64 `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionDetails struct {
					ReasoningTokens *int64 `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(frame, &chunk); err != nil {
			return nil, usageFragment{}, false, err
		}
		for _, choice := range chunk.Choices {
			text += choice.Delta.Content
			if choice.FinishReason != nil {
				message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: text}}}
				return &message, usageFragment{}, false, nil
			}
		}
		if chunk.Usage != nil {
			usage := chunk.Usage
			return nil, normalizer.update(usage.PromptTokens, usage.PromptDetails.CachedTokens, usage.CompletionTokens, usage.CompletionDetails.ReasoningTokens), true, nil
		}
		return nil, usageFragment{}, false, nil
	}
}

func renderOpenAIChatTools(tools []Tool) (json.RawMessage, error) {
	type function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}
	type declaration struct {
		Type     string   `json:"type"`
		Function function `json:"function"`
	}
	declarations := make([]declaration, len(tools))
	for index, tool := range tools {
		declarations[index] = declaration{"function", function{tool.Name(), tool.Description(), tool.Schema()}}
	}
	return json.Marshal(declarations)
}
