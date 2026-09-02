package agentkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

type openAIResponsesWire struct{ wireCodec }

func newOpenAIResponsesWire(classifier wireClassifier) WireFormat {
	wire := &openAIResponsesWire{}
	wire.wireCodec = wireCodec{
		encode:     wire.encodeRequest,
		decoder:    newOpenAIResponsesDecoder,
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
	Tools      json.RawMessage     `json:"tools,omitempty"`
}

func (w *openAIResponsesWire) encodeRequest(state RequestState) ([]byte, error) {
	input, err := buildOpenAIResponsesInput(state.History)
	if err != nil {
		return nil, err
	}
	request := buildOpenAIResponsesRequest(input, state.Settings)
	if len(state.Tools) > 0 {
		request.Tools, err = w.RenderTools(state.Tools)
		if err != nil {
			return nil, err
		}
	}
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
	var functionCalls []*openAIResponsesFunctionCall
	functionCallsByID := make(map[string]*openAIResponsesFunctionCall)
	functionCallsByIndex := make(map[int]*openAIResponsesFunctionCall)
	var normalizer usageNormalizer
	return func(frame []byte) (*Message, usageFragment, bool, error) {
		var event struct {
			Type        string `json:"type"`
			Delta       string `json:"delta"`
			ItemID      string `json:"item_id"`
			OutputIndex int    `json:"output_index"`
			Arguments   string `json:"arguments"`
			Item        struct {
				ID        string `json:"id"`
				Type      string `json:"type"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"item"`
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
		case "response.output_item.added":
			if event.Item.Type == "function_call" {
				functionCall := &openAIResponsesFunctionCall{
					itemID:      event.Item.ID,
					outputIndex: event.OutputIndex,
					callID:      event.Item.CallID,
					name:        event.Item.Name,
				}
				functionCalls = append(functionCalls, functionCall)
				functionCallsByID[functionCall.itemID] = functionCall
				functionCallsByIndex[functionCall.outputIndex] = functionCall
			}
		case "response.function_call_arguments.delta":
			if functionCall := openAIResponsesCallForEvent(functionCallsByID, functionCallsByIndex, event.ItemID, event.OutputIndex); functionCall != nil {
				functionCall.argumentDeltas.WriteString(event.Delta)
			}
		case "response.function_call_arguments.done":
			if functionCall := openAIResponsesCallForEvent(functionCallsByID, functionCallsByIndex, event.ItemID, event.OutputIndex); functionCall != nil {
				functionCall.finalArguments = event.Arguments
				functionCall.hasFinalArguments = true
			}
		case "response.output_item.done":
			if event.Item.Type == "function_call" {
				functionCall := openAIResponsesCallForEvent(functionCallsByID, functionCallsByIndex, event.Item.ID, event.OutputIndex)
				if functionCall != nil {
					functionCall.callID = event.Item.CallID
					functionCall.name = event.Item.Name
					functionCall.finalArguments = event.Item.Arguments
					functionCall.hasFinalArguments = true
				}
			}
		case "response.completed":
			usage := event.Response.Usage
			fragment := normalizer.update(usage.InputTokens, usage.InputDetails.CachedTokens, usage.OutputTokens, usage.OutputDetails.ReasoningTokens)
			blocks := make([]Block, 0, 1+len(functionCalls))
			if text != "" {
				blocks = append(blocks, Text{Text: text})
			}
			sort.SliceStable(functionCalls, func(left, right int) bool {
				return functionCalls[left].outputIndex < functionCalls[right].outputIndex
			})
			for _, functionCall := range functionCalls {
				toolUse, err := functionCall.toolUse()
				if err != nil {
					return nil, usageFragment{}, false, err
				}
				blocks = append(blocks, toolUse)
			}
			message := Message{Role: RoleAssistant, Blocks: blocks}
			return &message, fragment, true, nil
		}
		return nil, usageFragment{}, false, nil
	}
}

type openAIResponsesFunctionCall struct {
	itemID            string
	outputIndex       int
	callID            string
	name              string
	argumentDeltas    bytes.Buffer
	finalArguments    string
	hasFinalArguments bool
}

func openAIResponsesCallForEvent(byID map[string]*openAIResponsesFunctionCall, byIndex map[int]*openAIResponsesFunctionCall, itemID string, outputIndex int) *openAIResponsesFunctionCall {
	if itemID != "" {
		return byID[itemID]
	}
	return byIndex[outputIndex]
}

func (c *openAIResponsesFunctionCall) toolUse() (ToolUse, error) {
	arguments := c.argumentDeltas.Bytes()
	if c.hasFinalArguments {
		arguments = []byte(c.finalArguments)
	}
	arguments = bytes.TrimSpace(arguments)
	if len(arguments) == 0 || arguments[0] != '{' || !json.Valid(arguments) {
		return ToolUse{}, fmt.Errorf("agentkit: OpenAI Responses function call %q arguments are not a JSON object", c.itemID)
	}
	return ToolUse{ID: c.callID, Name: c.name, Input: append(json.RawMessage(nil), arguments...)}, nil
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

func (w *openAIResponsesWire) RenderTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderOpenAIResponsesTools(tools)
}

func (w *openAIResponsesWire) withClassifier(classifier wireClassifier) WireFormat {
	clone := &openAIResponsesWire{wireCodec: w.cloneWithClassifier(classifier)}
	clone.encode = clone.encodeRequest
	return clone
}
