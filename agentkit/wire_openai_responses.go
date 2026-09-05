package agentkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

type openAIResponsesWire struct{ wireCodec }

// OpenAIResponsesWire returns the built-in OpenAI Responses wire codec.
func OpenAIResponsesWire() WireFormat { return newOpenAIResponsesWire(nil) }

func newOpenAIResponsesWire(classifier errorClassifier) wireFormat {
	wire := &openAIResponsesWire{}
	wire.wireCodec = wireCodec{
		encode:      wire.encodeRequest,
		decoder:     newOpenAIResponsesDecoder,
		optionSpecs: wireOptionSpecsWithoutStop,
		classifier:  classifier,
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
	Effort    string `json:"effort,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
	MaxTokens *int   `json:"max_tokens,omitempty"`
}

type responsesNamedTool struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type openAIResponsesJSONSchemaFormat struct {
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type openAIResponsesText struct {
	Format openAIResponsesJSONSchemaFormat `json:"format"`
}

type openAIResponsesRequest struct {
	Model           string               `json:"model"`
	Input           []json.RawMessage    `json:"input"`
	Stream          bool                 `json:"stream"`
	Store           bool                 `json:"store"`
	Temperature     *float64             `json:"temperature,omitempty"`
	TopP            *float64             `json:"top_p,omitempty"`
	MaxOutputTokens *int                 `json:"max_output_tokens,omitempty"`
	Reasoning       *responsesReasoning  `json:"reasoning,omitempty"`
	ToolChoice      any                  `json:"tool_choice,omitempty"`
	Tools           json.RawMessage      `json:"tools,omitempty"`
	Text            *openAIResponsesText `json:"text,omitempty"`
}

func (w *openAIResponsesWire) encodeRequest(state requestState) ([]byte, error) {
	input, err := buildOpenAIResponsesInput(state.History)
	if err != nil {
		return nil, err
	}
	request := buildOpenAIResponsesRequest(input, state.Settings)
	request.Model = state.Model
	if state.Output != nil {
		schema, renderErr := w.renderOutputSchema(state.Output.Schema)
		if renderErr != nil {
			return nil, fmt.Errorf("agentkit: render OpenAI Responses output schema: %w", renderErr)
		}
		request.Text = &openAIResponsesText{
			Format: openAIResponsesJSONSchemaFormat{
				Type: "json_schema", Name: "agentkit_output", Strict: true, Schema: schema,
			},
		}
	}
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
	request := openAIResponsesRequest{Input: input, Stream: true, Store: false}
	if v, ok := settingsFloatOption(settings.Options, "temperature"); ok {
		request.Temperature = &v
	}
	if v, ok := settingsFloatOption(settings.Options, "top_p"); ok {
		request.TopP = &v
	}
	if v, ok := settingsMaxOutputTokens(settings.Options); ok {
		request.MaxOutputTokens = &v
	}
	reasoning := settingsReasoning(settings)
	switch reasoning.Mode {
	case ReasoningOff:
		request.Reasoning = &responsesReasoning{Effort: "none"}
	case ReasoningEffort:
		request.Reasoning = &responsesReasoning{Effort: effortName(reasoning.Effort)}
	case ReasoningOn:
		enabled := true
		request.Reasoning = &responsesReasoning{Enabled: &enabled}
	case ReasoningBudget:
		budget := reasoning.Budget
		request.Reasoning = &responsesReasoning{MaxTokens: &budget}
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
	blocksByIndex := make(map[int]*openAIResponsesStreamBlock)
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
			block := blocksByIndex[event.OutputIndex]
			if block == nil {
				block = &openAIResponsesStreamBlock{}
				blocksByIndex[event.OutputIndex] = block
			}
			block.text.WriteString(event.Delta)
		case "response.output_item.added":
			switch event.Item.Type {
			case "message":
				if blocksByIndex[event.OutputIndex] == nil {
					blocksByIndex[event.OutputIndex] = &openAIResponsesStreamBlock{}
				}
			case "function_call":
				functionCall := &openAIResponsesFunctionCall{
					itemID:      event.Item.ID,
					outputIndex: event.OutputIndex,
					callID:      event.Item.CallID,
					name:        event.Item.Name,
				}
				blocksByIndex[event.OutputIndex] = &openAIResponsesStreamBlock{functionCall: functionCall}
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
			fragment := normalizer.update(usage.InputTokens, usage.InputDetails.CachedTokens, nil, nil, usage.OutputTokens, usage.OutputDetails.ReasoningTokens)
			indices := make([]int, 0, len(blocksByIndex))
			for index := range blocksByIndex {
				indices = append(indices, index)
			}
			sort.Ints(indices)
			blocks := make([]Block, 0, len(indices))
			for _, index := range indices {
				block := blocksByIndex[index]
				if block.functionCall == nil {
					if block.text.Len() > 0 {
						blocks = append(blocks, Text{Text: block.text.String()})
					}
					continue
				}
				toolUse, err := block.functionCall.toolUse()
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

type openAIResponsesStreamBlock struct {
	text         bytes.Buffer
	functionCall *openAIResponsesFunctionCall
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
