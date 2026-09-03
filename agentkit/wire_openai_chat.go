package agentkit

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type openAIChatWire struct{ wireCodec }

func newOpenAIChatWire(classifier wireClassifier) WireFormat {
	wire := &openAIChatWire{}
	wire.wireCodec = wireCodec{
		encode:     wire.encodeRequest,
		decoder:    newOpenAIChatDecoder,
		reserved:   []string{"openai", "response_format"},
		classifier: classifier,
		capabilities: wireCapabilities{
			name:       "OpenAI Chat Completions",
			reasoning:  reasoningShapeOff | reasoningShapeEffort,
			toolChoice: toolChoiceShapeNone | toolChoiceShapeRequired | toolChoiceShapeTool,
		},
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

type chatNamedFunction struct {
	Name string `json:"name"`
}

type chatNamedTool struct {
	Type     string            `json:"type"`
	Function chatNamedFunction `json:"function"`
}

type openAIChatJSONSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type openAIChatResponseFormat struct {
	Type       string               `json:"type"`
	JSONSchema openAIChatJSONSchema `json:"json_schema"`
}

type openAIChatRequest struct {
	Model           string                    `json:"model"`
	Messages        []chatMessage             `json:"messages"`
	ReasoningEffort string                    `json:"reasoning_effort,omitempty"`
	ToolChoice      any                       `json:"tool_choice,omitempty"`
	Tools           json.RawMessage           `json:"tools,omitempty"`
	ResponseFormat  *openAIChatResponseFormat `json:"response_format,omitempty"`
}

func buildOpenAIChatMessages(history []Message) ([]chatMessage, error) {
	messages := make([]chatMessage, 0, len(history))
	for _, message := range history {
		encoded := chatMessage{Role: openAIRole(message.Role)}
		var toolResults []chatMessage
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
				toolResults = append(toolResults, chatMessage{
					Role:       openAIRole(RoleTool),
					Content:    block.Content,
					ToolCallID: block.ToolUseID,
				})
			}
		}
		if message.Role == RoleTool && len(toolResults) > 1 {
			messages = append(messages, toolResults...)
		} else {
			messages = append(messages, encoded)
		}
	}
	return messages, nil
}

func configureOpenAIChatRequest(request *openAIChatRequest, settings Settings) {
	switch settings.Reasoning.Mode {
	case ReasoningOff:
		request.ReasoningEffort = "none"
	case ReasoningEffort:
		request.ReasoningEffort = effortName(settings.Reasoning.Effort)
	}
	switch settings.ToolChoice.Mode {
	case ToolChoiceNone:
		request.ToolChoice = "none"
	case ToolChoiceRequired:
		request.ToolChoice = "required"
	case ToolChoiceTool:
		request.ToolChoice = chatNamedTool{Type: "function", Function: chatNamedFunction{Name: settings.ToolChoice.Name}}
	}
}

func (w *openAIChatWire) encodeRequest(state RequestState) ([]byte, error) {
	messages, err := buildOpenAIChatMessages(state.History)
	if err != nil {
		return nil, err
	}
	request := openAIChatRequest{Model: state.Model, Messages: messages}
	if state.Output != nil {
		schema, renderErr := w.renderOutputSchema(state.Output.Schema)
		if renderErr != nil {
			return nil, fmt.Errorf("agentkit: render OpenAI Chat output schema: %w", renderErr)
		}
		request.ResponseFormat = &openAIChatResponseFormat{
			Type: "json_schema",
			JSONSchema: openAIChatJSONSchema{
				Name: "agentkit_output", Strict: true, Schema: schema,
			},
		}
	}
	configureOpenAIChatRequest(&request, state.Settings)
	if len(state.Tools) > 0 {
		request.Tools, err = w.RenderTools(state.Tools)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(request)
	return append(encoded, '\n'), err
}

func newOpenAIChatDecoder() frameDecoder {
	var orderedBlocks []*openAIChatStreamBlock
	toolCallsByIndex := make(map[int]*openAIChatStreamToolCall)
	var normalizer usageNormalizer
	return func(frame []byte) (*Message, usageFragment, bool, error) {
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
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
			if choice.Delta.Content != "" {
				if len(orderedBlocks) == 0 || orderedBlocks[len(orderedBlocks)-1].toolCall != nil {
					orderedBlocks = append(orderedBlocks, &openAIChatStreamBlock{})
				}
				orderedBlocks[len(orderedBlocks)-1].text.WriteString(choice.Delta.Content)
			}
			for _, delta := range choice.Delta.ToolCalls {
				toolCall := toolCallsByIndex[delta.Index]
				if toolCall == nil {
					toolCall = &openAIChatStreamToolCall{}
					toolCallsByIndex[delta.Index] = toolCall
					orderedBlocks = append(orderedBlocks, &openAIChatStreamBlock{toolCall: toolCall})
				}
				if delta.ID != "" {
					toolCall.id = delta.ID
				}
				if delta.Function.Name != "" {
					toolCall.name = delta.Function.Name
				}
				toolCall.arguments.WriteString(delta.Function.Arguments)
			}
			if choice.FinishReason != nil {
				blocks := make([]Block, 0, len(orderedBlocks))
				for _, block := range orderedBlocks {
					if block.toolCall == nil {
						blocks = append(blocks, Text{Text: block.text.String()})
						continue
					}
					toolUse, err := block.toolCall.toolUse()
					if err != nil {
						return nil, usageFragment{}, false, err
					}
					blocks = append(blocks, toolUse)
				}
				message := Message{Role: RoleAssistant, Blocks: blocks}
				return &message, usageFragment{}, false, nil
			}
		}
		if chunk.Usage != nil {
			usage := chunk.Usage
			return nil, normalizer.update(usage.PromptTokens, usage.PromptDetails.CachedTokens, nil, nil, usage.CompletionTokens, usage.CompletionDetails.ReasoningTokens), true, nil
		}
		return nil, usageFragment{}, false, nil
	}
}

type openAIChatStreamBlock struct {
	text     bytes.Buffer
	toolCall *openAIChatStreamToolCall
}

type openAIChatStreamToolCall struct {
	id        string
	name      string
	arguments bytes.Buffer
}

func (c *openAIChatStreamToolCall) toolUse() (ToolUse, error) {
	input := bytes.TrimSpace(c.arguments.Bytes())
	if len(input) == 0 || input[0] != '{' || !json.Valid(input) {
		return ToolUse{}, fmt.Errorf("agentkit: OpenAI Chat tool call %q input is not a JSON object", c.id)
	}
	return ToolUse{ID: c.id, Name: c.name, Input: append(json.RawMessage(nil), input...)}, nil
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

func (w *openAIChatWire) RenderTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderOpenAIChatTools(tools)
}
