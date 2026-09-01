package agentkit

import (
	"encoding/json"
	"fmt"
)

type openAIResponsesWire struct{ wireCodec }

func newOpenAIResponsesWire(classifier wireClassifier) WireFormat {
	wire := &openAIResponsesWire{}
	wire.wireCodec = wireCodec{
		encode:     encodeOpenAIResponsesRequest,
		decoder:    newOpenAIResponsesDecoder,
		render:     renderOpenAIResponsesTools,
		reserved:   []string{"openai"},
		classifier: classifier,
		capabilities: wireCapabilities{
			name:       "OpenAI Responses",
			reasoning:  reasoningShapeOff | reasoningShapeEffort,
			toolChoice: toolChoiceShapeNone | toolChoiceShapeRequired | toolChoiceShapeTool,
		},
	}
	return wire
}

type responsesContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesInput struct {
	Role    string             `json:"role"`
	Content []responsesContent `json:"content"`
}

type responsesReasoning struct {
	Effort string `json:"effort"`
}

type responsesNamedTool struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type openAIResponsesRequest struct {
	Input      []json.RawMessage   `json:"input"`
	Reasoning  *responsesReasoning `json:"reasoning,omitempty"`
	ToolChoice any                 `json:"tool_choice,omitempty"`
}

func encodeOpenAIResponsesRequest(state RequestState) ([]byte, error) {
	input, err := buildOpenAIResponsesInput(state.History)
	if err != nil {
		return nil, err
	}
	request := buildOpenAIResponsesRequest(input, state.Settings)
	encoded, err := json.Marshal(request)
	return append(encoded, '\n'), err
}

func buildOpenAIResponsesInput(history []Message) ([]json.RawMessage, error) {
	input := make([]json.RawMessage, 0, len(history))
	for _, message := range history {
		item := responsesInput{Role: openAIRole(message.Role)}
		for _, block := range message.Blocks {
			switch block := block.(type) {
			case Text:
				kind := "input_text"
				if message.Role == RoleAssistant {
					kind = "output_text"
				}
				item.Content = append(item.Content, responsesContent{Type: kind, Text: block.Text})
			case Reasoning:
				if len(block.Provider) == 0 {
					return nil, fmt.Errorf("agentkit: OpenAI Responses reasoning replay requires provider bytes")
				}
				if len(item.Content) > 0 {
					encoded, err := json.Marshal(item)
					if err != nil {
						return nil, err
					}
					input = append(input, encoded)
					item.Content = nil
				}
				if !json.Valid(block.Provider) {
					return nil, fmt.Errorf("agentkit: invalid OpenAI Responses reasoning replay")
				}
				input = append(input, append(json.RawMessage(nil), block.Provider...))
			case ToolUse:
				encoded, err := json.Marshal(struct {
					Type      string `json:"type"`
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{"function_call", block.ID, block.Name, string(block.Input)})
				if err != nil {
					return nil, err
				}
				input = append(input, encoded)
			case ToolResult:
				encoded, err := json.Marshal(struct {
					Type   string `json:"type"`
					CallID string `json:"call_id"`
					Output string `json:"output"`
				}{"function_call_output", block.ToolUseID, block.Content})
				if err != nil {
					return nil, err
				}
				input = append(input, encoded)
			}
		}
		if len(item.Content) > 0 {
			encoded, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			input = append(input, encoded)
		}
	}
	return input, nil
}

func buildOpenAIResponsesRequest(input []json.RawMessage, settings Settings) openAIResponsesRequest {
	request := openAIResponsesRequest{Input: input}
	switch settings.Reasoning.Mode {
	case ReasoningOff:
		request.Reasoning = &responsesReasoning{Effort: "none"}
	case ReasoningEffort:
		request.Reasoning = &responsesReasoning{Effort: effortName(settings.Reasoning.Effort)}
	}
	switch settings.ToolChoice.Mode {
	case ToolChoiceNone:
		request.ToolChoice = "none"
	case ToolChoiceRequired:
		request.ToolChoice = "required"
	case ToolChoiceTool:
		request.ToolChoice = responsesNamedTool{Type: "function", Name: settings.ToolChoice.Name}
	}
	return request
}

func openAIRole(role Role) string {
	switch role {
	case RoleSystem:
		return "developer"
	case RoleAssistant:
		return "assistant"
	case RoleTool:
		return "tool"
	default:
		return "user"
	}
}

func newOpenAIResponsesDecoder() frameDecoder {
	var text string
	var normalizer usageNormalizer
	return func(frame []byte) (*Message, usageFragment, bool, error) {
		var event struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response struct {
				Usage struct {
					InputTokens  *int64 `json:"input_tokens"`
					OutputTokens *int64 `json:"output_tokens"`
					InputDetails struct {
						CachedTokens *int64 `json:"cached_tokens"`
					} `json:"input_tokens_details"`
					OutputDetails struct {
						ReasoningTokens *int64 `json:"reasoning_tokens"`
					} `json:"output_tokens_details"`
				} `json:"usage"`
			} `json:"response"`
		}
		if err := json.Unmarshal(frame, &event); err != nil {
			return nil, usageFragment{}, false, err
		}
		switch event.Type {
		case "response.output_text.delta":
			text += event.Delta
		case "response.completed":
			usage := event.Response.Usage
			fragment := normalizer.update(usage.InputTokens, usage.InputDetails.CachedTokens, usage.OutputTokens, usage.OutputDetails.ReasoningTokens)
			message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: text}}}
			return &message, fragment, true, nil
		}
		return nil, usageFragment{}, false, nil
	}
}

func renderOpenAIResponsesTools(tools []Tool) (json.RawMessage, error) {
	type declaration struct {
		Type        string          `json:"type"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}
	declarations := make([]declaration, len(tools))
	for index, tool := range tools {
		declarations[index] = declaration{"function", tool.Name(), tool.Description(), tool.Schema()}
	}
	return json.Marshal(declarations)
}
