package agentkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)

type anthropicMessagesWire struct{ wireCodec }

var anthropicOfferingMaxOutputTokens func(Identity) (int64, bool)

func init() {
	// Initialize after package variables so constructing catalog wire formats
	// does not create an initialization cycle back through catalogTable.
	anthropicOfferingMaxOutputTokens = offeringMaxOutputTokens
}

// AnthropicMessagesWire returns the built-in Anthropic Messages wire codec.
func AnthropicMessagesWire() WireFormat { return newAnthropicMessagesWire(nil) }

func newAnthropicMessagesWire(classifier errorClassifier) wireFormat {
	wire := &anthropicMessagesWire{}
	wire.wireCodec = wireCodec{
		encode:      wire.encodeRequest,
		decoder:     newAnthropicDecoder,
		optionSpecs: wireOptionSpecsWithStop,
		classifier:  classifier,
		capabilities: wireCapabilities{
			name:       "Anthropic Messages",
			reasoning:  reasoningShapeOff | reasoningShapeEffort | reasoningShapeBudget,
			toolChoice: toolChoiceShapeRequired | toolChoiceShapeTool,
		},
	}
	return wire
}

func (w *anthropicMessagesWire) setProtocolHeaders(req *http.Request) {
	req.Header.Set("anthropic-version", "2023-06-01")
}

type anthropicContent struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	Thinking     string                 `json:"thinking,omitempty"`
	Signature    string                 `json:"signature,omitempty"`
	ID           string                 `json:"id,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Input        json.RawMessage        `json:"input,omitempty"`
	ToolUseID    string                 `json:"tool_use_id,omitempty"`
	Content      string                 `json:"content,omitempty"`
	IsError      bool                   `json:"is_error,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicOutputFormat struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

type anthropicOutputConfig struct {
	Effort string                 `json:"effort,omitempty"`
	Format *anthropicOutputFormat `json:"format,omitempty"`
}

type anthropicRequest struct {
	Model         string                 `json:"model"`
	System        []anthropicContent     `json:"system,omitempty"`
	Messages      []anthropicMessage     `json:"messages"`
	Stream        bool                   `json:"stream"`
	Temperature   *float64               `json:"temperature,omitempty"`
	TopP          *float64               `json:"top_p,omitempty"`
	MaxTokens     *int                   `json:"max_tokens,omitempty"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	OutputConfig  *anthropicOutputConfig `json:"output_config,omitempty"`
	Thinking      *anthropicThinking     `json:"thinking,omitempty"`
	ToolChoice    *anthropicToolChoice   `json:"tool_choice,omitempty"`
	Tools         json.RawMessage        `json:"tools,omitempty"`
}

func buildAnthropicMessages(history []Message, mark int) ([]anthropicMessage, []anthropicContent, *anthropicContent, error) {
	encodeContent := func(message Message) ([]anthropicContent, error) {
		var content []anthropicContent
		for _, block := range message.Blocks {
			switch block := block.(type) {
			case Text:
				content = append(content, anthropicContent{Type: "text", Text: block.Text})
			case Reasoning:
				if len(block.Provider) > 0 {
					var replay anthropicContent
					if err := json.Unmarshal(block.Provider, &replay); err != nil {
						return nil, fmt.Errorf("agentkit: invalid Anthropic reasoning replay: %w", err)
					}
					content = append(content, replay)
				} else {
					content = append(content, anthropicContent{Type: "thinking", Thinking: block.Text})
				}
			case ToolUse:
				content = append(content, anthropicContent{Type: "tool_use", ID: block.ID, Name: block.Name, Input: block.Input})
			case ToolResult:
				content = append(content, anthropicContent{Type: "tool_result", ToolUseID: block.ToolUseID, Content: block.Content, IsError: block.IsError})
			}
		}
		return content, nil
	}

	boundary := 0
	var system []anthropicContent
	for boundary < len(history) && history[boundary].Role == RoleSystem {
		content, err := encodeContent(history[boundary])
		if err != nil {
			return nil, nil, nil, err
		}
		system = append(system, content...)
		boundary++
	}

	// Record the cache boundary by index so the final pointer is taken only
	// after all appends that could reallocate the slices are complete.
	markSystem := mark > 0 && mark <= boundary
	markMsgIndex, markBlockIndex := -1, -1
	pendingMarkBlockIndex := -1

	messages := make([]anthropicMessage, 0, len(history))
	var pendingSystem []anthropicContent
	for i, message := range history[boundary:] {
		index := boundary + i
		content, err := encodeContent(message)
		if err != nil {
			return nil, nil, nil, err
		}
		if message.Role == RoleSystem {
			pendingSystem = append(pendingSystem, content...)
			if mark == index+1 && len(content) > 0 {
				pendingMarkBlockIndex = len(pendingSystem) - 1
			}
			continue
		}
		messages = append(messages, anthropicMessage{Role: anthropicRole(message.Role), Content: content})
		if mark == index+1 && len(content) > 0 {
			markMsgIndex, markBlockIndex = len(messages)-1, len(content)-1
		}
		if message.Role == RoleUser && len(pendingSystem) > 0 {
			messages = append(messages, anthropicMessage{Role: "system", Content: pendingSystem})
			if pendingMarkBlockIndex >= 0 {
				markMsgIndex, markBlockIndex = len(messages)-1, pendingMarkBlockIndex
				pendingMarkBlockIndex = -1
			}
			pendingSystem = nil
		}
	}

	switch {
	case markSystem && len(system) > 0:
		return messages, system, &system[len(system)-1], nil
	case markMsgIndex >= 0:
		return messages, system, &messages[markMsgIndex].Content[markBlockIndex], nil
	default:
		return messages, system, nil, nil
	}
}

func configureAnthropicRequest(request *anthropicRequest, settings Settings, identity Identity) {
	reasoning := settingsReasoning(settings)
	switch reasoning.Mode {
	case ReasoningOff:
		request.Thinking = &anthropicThinking{Type: "disabled"}
	case ReasoningEffort:
		if request.OutputConfig == nil {
			request.OutputConfig = &anthropicOutputConfig{}
		}
		request.OutputConfig.Effort = effortName(reasoning.Effort)
	case ReasoningBudget:
		request.Thinking = &anthropicThinking{Type: "enabled", BudgetTokens: reasoning.Budget}
	}
	if v, ok := settingsFloatOption(settings.Options, "temperature"); ok {
		request.Temperature = &v
	}
	if v, ok := settingsFloatOption(settings.Options, "top_p"); ok {
		request.TopP = &v
	}
	if v, ok := settingsMaxOutputTokens(settings.Options); ok {
		request.MaxTokens = &v
	} else if limit, found := anthropicOfferingMaxOutputTokens(identity); found && limit != 0 {
		fallback := int(limit)
		request.MaxTokens = &fallback
	}
	if v, ok := settingsStopSequences(settings.Options); ok {
		request.StopSequences = v
	}
	switch settings.ToolChoice.Mode {
	case ToolChoiceRequired:
		request.ToolChoice = &anthropicToolChoice{Type: "any"}
	case ToolChoiceTool:
		request.ToolChoice = &anthropicToolChoice{Type: "tool", Name: settings.ToolChoice.Name}
	}
}

func (w *anthropicMessagesWire) encodeRequest(state requestState) ([]byte, error) {
	messages, system, cacheBlock, err := buildAnthropicMessages(state.History, state.SavepointMark)
	if err != nil {
		return nil, err
	}
	if cacheBlock != nil {
		cacheBlock.CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}
	request := anthropicRequest{Model: state.Model, System: system, Messages: messages, Stream: true}
	if state.Output != nil {
		schema, renderErr := w.renderOutputSchema(state.Output.Schema)
		if renderErr != nil {
			return nil, fmt.Errorf("agentkit: render Anthropic output schema: %w", renderErr)
		}
		request.OutputConfig = &anthropicOutputConfig{
			Format: &anthropicOutputFormat{Type: "json_schema", Schema: schema},
		}
	}
	configureAnthropicRequest(&request, state.Settings, state.Identity)
	if len(state.Tools) > 0 {
		request.Tools, err = w.RenderTools(state.Tools)
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(request)
	return append(encoded, '\n'), err
}

// validateMaxTokens enforces R-KCW0-HE9L: Send must have a max_tokens value
// to send, from either the max_output_tokens option or a catalog match.
func (w *anthropicMessagesWire) validateMaxTokens(identity Identity, settings Settings) error {
	if _, ok := settingsMaxOutputTokens(settings.Options); ok {
		return nil
	}
	if limit, found := offeringMaxOutputTokens(identity); found && limit != 0 {
		return nil
	}
	return fmt.Errorf("%w: Anthropic Messages: max_tokens requires the max_output_tokens option or a catalog offering with a known MaxOutputTokens for endpoint %q model %q", ErrInvalidConfig, identity.Endpoint, identity.Model)
}

func anthropicRole(role Role) string {
	if role == RoleAssistant {
		return "assistant"
	}
	return "user"
}

func newAnthropicDecoder() frameDecoder {
	blocksByIndex := make(map[int]*anthropicStreamBlock)
	toolUsesByIndex := make(map[int]*anthropicStreamToolUse)
	var usage usageNormalizer
	return func(frame []byte) (*Message, usageFragment, bool, error) {
		var event struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			Usage struct {
				OutputTokens *int64 `json:"output_tokens"`
			} `json:"usage"`
			Message struct {
				Usage struct {
					InputTokens   *int64 `json:"input_tokens"`
					CachedTokens  *int64 `json:"cache_read_input_tokens"`
					CacheCreation struct {
						Ephemeral5mTokens *int64 `json:"ephemeral_5m_input_tokens"`
						Ephemeral1hTokens *int64 `json:"ephemeral_1h_input_tokens"`
					} `json:"cache_creation"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(frame, &event); err != nil {
			return nil, usageFragment{}, false, err
		}
		switch event.Type {
		case "message_start":
			messageUsage := event.Message.Usage
			fragment := usage.update(
				messageUsage.InputTokens,
				messageUsage.CachedTokens,
				messageUsage.CacheCreation.Ephemeral5mTokens,
				messageUsage.CacheCreation.Ephemeral1hTokens,
				nil,
				nil,
			)
			return nil, fragment, true, nil
		case "content_block_start":
			switch event.ContentBlock.Type {
			case "text":
				blocksByIndex[event.Index] = &anthropicStreamBlock{}
			case "tool_use":
				toolUse := &anthropicStreamToolUse{
					id:         event.ContentBlock.ID,
					name:       event.ContentBlock.Name,
					startInput: append(json.RawMessage(nil), event.ContentBlock.Input...),
				}
				blocksByIndex[event.Index] = &anthropicStreamBlock{toolUse: toolUse}
				toolUsesByIndex[event.Index] = toolUse
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				block := blocksByIndex[event.Index]
				if block == nil {
					block = &anthropicStreamBlock{}
					blocksByIndex[event.Index] = block
				}
				block.text.WriteString(event.Delta.Text)
			case "input_json_delta":
				if toolUse := toolUsesByIndex[event.Index]; toolUse != nil {
					toolUse.input.WriteString(event.Delta.PartialJSON)
				}
			}
		case "content_block_stop":
			if toolUse := toolUsesByIndex[event.Index]; toolUse != nil {
				if err := toolUse.finish(); err != nil {
					return nil, usageFragment{}, false, err
				}
				delete(toolUsesByIndex, event.Index)
			}
		case "message_delta":
			return nil, usage.update(nil, nil, nil, nil, event.Usage.OutputTokens, nil), true, nil
		case "message_stop":
			indices := make([]int, 0, len(blocksByIndex))
			for index := range blocksByIndex {
				indices = append(indices, index)
			}
			sort.Ints(indices)
			blocks := make([]Block, 0, len(indices))
			for _, index := range indices {
				block := blocksByIndex[index]
				if block.toolUse == nil {
					if block.text.Len() > 0 {
						blocks = append(blocks, Text{Text: block.text.String()})
					}
					continue
				}
				if block.toolUse.output == nil {
					if err := block.toolUse.finish(); err != nil {
						return nil, usageFragment{}, false, err
					}
				}
				blocks = append(blocks, *block.toolUse.output)
			}
			message := Message{Role: RoleAssistant, Blocks: blocks}
			return &message, usageFragment{}, false, nil
		}
		return nil, usageFragment{}, false, nil
	}
}

type anthropicStreamBlock struct {
	text    bytes.Buffer
	toolUse *anthropicStreamToolUse
}

type anthropicStreamToolUse struct {
	id         string
	name       string
	startInput json.RawMessage
	input      bytes.Buffer
	output     *ToolUse
}

func (t *anthropicStreamToolUse) finish() error {
	input := t.startInput
	if t.input.Len() > 0 {
		input = t.input.Bytes()
	}
	input = bytes.TrimSpace(input)
	if len(input) == 0 || input[0] != '{' || !json.Valid(input) {
		return fmt.Errorf("agentkit: Anthropic tool use %q input is not a JSON object", t.id)
	}
	decoded := ToolUse{ID: t.id, Name: t.name, Input: append(json.RawMessage(nil), input...)}
	t.output = &decoded
	return nil
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

func (w *anthropicMessagesWire) RenderTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderAnthropicTools(tools)
}
