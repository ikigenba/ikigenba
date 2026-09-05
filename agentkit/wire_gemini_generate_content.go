package agentkit

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type geminiWire struct{ wireCodec }

// GeminiGenerateContentWire returns the built-in Gemini GenerateContent wire codec.
func GeminiGenerateContentWire() WireFormat { return newGeminiGenerateContentWire(nil) }

func newGeminiGenerateContentWire(classifier errorClassifier) wireFormat {
	wire := &geminiWire{}
	wire.wireCodec = wireCodec{
		encode:      wire.encodeRequest,
		decoder:     newGeminiDecoder,
		optionSpecs: wireOptionSpecsWithStop,
		classifier:  classifier,
		capabilities: wireCapabilities{
			name:       "Gemini GenerateContent",
			reasoning:  reasoningShapeOff | reasoningShapeOn | reasoningShapeEffort | reasoningShapeBudget,
			toolChoice: toolChoiceShapeNone | toolChoiceShapeRequired | toolChoiceShapeTool,
		},
	}
	return wire
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	Thought          bool                    `json:"thought,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type geminiFunctionResponse struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Response struct {
		Output  string `json:"output"`
		IsError bool   `json:"isError"`
	} `json:"response"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiThinkingConfig struct {
	ThinkingBudget *int   `json:"thinkingBudget,omitempty"`
	ThinkingLevel  string `json:"thinkingLevel,omitempty"`
}

type geminiGenerationConfig struct {
	Temperature        *float64              `json:"temperature,omitempty"`
	TopP               *float64              `json:"topP,omitempty"`
	MaxOutputTokens    *int                  `json:"maxOutputTokens,omitempty"`
	StopSequences      []string              `json:"stopSequences,omitempty"`
	ThinkingConfig     *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
	ResponseMIMEType   string                `json:"responseMimeType,omitempty"`
	ResponseJSONSchema json.RawMessage       `json:"responseJsonSchema,omitempty"`
}

type geminiFunctionCallingConfig struct {
	Mode                 string   `json:"mode"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type geminiToolConfig struct {
	FunctionCallingConfig *geminiFunctionCallingConfig `json:"functionCallingConfig"`
}

type geminiRequest struct {
	Contents         []geminiContent         `json:"contents"`
	GenerationConfig *geminiGenerationConfig `json:"generationConfig,omitempty"`
	ToolConfig       *geminiToolConfig       `json:"toolConfig,omitempty"`
	Tools            json.RawMessage         `json:"tools,omitempty"`
}

func (w *geminiWire) encodeRequest(state requestState) ([]byte, error) {
	contents, err := buildGeminiContents(state.History)
	if err != nil {
		return nil, err
	}
	request := geminiRequest{
		Contents:         contents,
		GenerationConfig: applyGeminiSamplingOptions(buildGeminiThinkingConfig(settingsReasoning(state.Settings)), state.Settings.Options),
		ToolConfig:       buildGeminiToolConfig(state.Settings.ToolChoice),
	}
	if state.Output != nil {
		schema, renderErr := w.renderOutputSchema(state.Output.Schema)
		if renderErr != nil {
			return nil, fmt.Errorf("agentkit: render Gemini output schema: %w", renderErr)
		}
		if request.GenerationConfig == nil {
			request.GenerationConfig = &geminiGenerationConfig{}
		}
		request.GenerationConfig.ResponseMIMEType = "application/json"
		request.GenerationConfig.ResponseJSONSchema = schema
	}
	if len(state.Tools) > 0 {
		rendered, renderErr := w.RenderTools(state.Tools)
		if renderErr != nil {
			return nil, renderErr
		}
		var toolEnvelope struct {
			Tools json.RawMessage `json:"tools"`
		}
		// RenderTools currently returns marshaled JSON; keep the boundary check so
		// encodeRequest remains defensive if that private implementation changes.
		if err := json.Unmarshal(rendered, &toolEnvelope); err != nil {
			return nil, err
		}
		request.Tools = toolEnvelope.Tools
	}
	encoded, err := json.Marshal(request)
	return append(encoded, '\n'), err
}

func buildGeminiContents(history []Message) ([]geminiContent, error) {
	contents := make([]geminiContent, 0, len(history))
	callNames := make(map[string]string)
	for _, message := range history {
		role := "user"
		if message.Role == RoleAssistant {
			role = "model"
		}
		content := geminiContent{Role: role}
		for _, block := range message.Blocks {
			switch block := block.(type) {
			case Text:
				content.Parts = append(content.Parts, geminiPart{Text: block.Text})
			case Reasoning:
				part := geminiPart{Text: block.Text, Thought: true}
				if len(block.Provider) > 0 {
					if err := json.Unmarshal(block.Provider, &part); err != nil {
						return nil, fmt.Errorf("agentkit: invalid Gemini reasoning replay: %w", err)
					}
				}
				content.Parts = append(content.Parts, part)
			case ToolUse:
				callNames[block.ID] = block.Name
				part := geminiPart{FunctionCall: &geminiFunctionCall{ID: block.ID, Name: block.Name, Args: block.Input}}
				if len(block.Provider) > 0 {
					var provider struct {
						ThoughtSignature string `json:"thoughtSignature"`
					}
					if err := json.Unmarshal(block.Provider, &provider); err != nil {
						return nil, fmt.Errorf("agentkit: invalid Gemini tool-use replay: %w", err)
					}
					part.ThoughtSignature = provider.ThoughtSignature
				}
				content.Parts = append(content.Parts, part)
			case ToolResult:
				name := callNames[block.ToolUseID]
				if name == "" {
					name = block.ToolUseID
				}
				response := &geminiFunctionResponse{ID: block.ToolUseID, Name: name}
				response.Response.Output = block.Content
				response.Response.IsError = block.IsError
				content.Parts = append(content.Parts, geminiPart{FunctionResponse: response})
			}
		}
		contents = append(contents, content)
	}
	return contents, nil
}

func buildGeminiThinkingConfig(reasoning ReasoningConfig) *geminiGenerationConfig {
	var thinking *geminiThinkingConfig
	switch reasoning.Mode {
	case ReasoningOff:
		budget := 0
		thinking = &geminiThinkingConfig{ThinkingBudget: &budget}
	case ReasoningOn:
		budget := -1
		thinking = &geminiThinkingConfig{ThinkingBudget: &budget}
	case ReasoningEffort:
		thinking = &geminiThinkingConfig{ThinkingLevel: effortName(reasoning.Effort)}
	case ReasoningBudget:
		budget := reasoning.Budget
		thinking = &geminiThinkingConfig{ThinkingBudget: &budget}
	}
	// Keep unknown or future reasoning modes absent instead of emitting a partial config.
	if thinking == nil {
		return nil
	}
	return &geminiGenerationConfig{ThinkingConfig: thinking}
}

// applyGeminiSamplingOptions folds temperature, top_p, max_output_tokens,
// and stop from options into config, allocating config if it is nil and at
// least one of the four is present (R-OLRY-B787). It leaves config
// (including a nil config) untouched when none of the four is present, so
// a Settings with no sampling option and no reasoning option still encodes
// with GenerationConfig absent (R-OPFN-GIGA).
func applyGeminiSamplingOptions(config *geminiGenerationConfig, options Options) *geminiGenerationConfig {
	temperature, hasTemperature := settingsFloatOption(options, "temperature")
	topP, hasTopP := settingsFloatOption(options, "top_p")
	maxOutputTokens, hasMaxOutputTokens := settingsMaxOutputTokens(options)
	stop, hasStop := settingsStopSequences(options)
	if !hasTemperature && !hasTopP && !hasMaxOutputTokens && !hasStop {
		return config
	}
	if config == nil {
		config = &geminiGenerationConfig{}
	}
	if hasTemperature {
		config.Temperature = &temperature
	}
	if hasTopP {
		config.TopP = &topP
	}
	if hasMaxOutputTokens {
		config.MaxOutputTokens = &maxOutputTokens
	}
	if hasStop {
		config.StopSequences = stop
	}
	return config
}

func buildGeminiToolConfig(choice ToolChoice) *geminiToolConfig {
	var calling *geminiFunctionCallingConfig
	switch choice.Mode {
	case ToolChoiceNone:
		calling = &geminiFunctionCallingConfig{Mode: "NONE"}
	case ToolChoiceRequired:
		calling = &geminiFunctionCallingConfig{Mode: "ANY"}
	case ToolChoiceTool:
		calling = &geminiFunctionCallingConfig{Mode: "ANY", AllowedFunctionNames: []string{choice.Name}}
	}
	// Keep unknown or future tool-choice modes absent instead of emitting a partial config.
	if calling == nil {
		return nil
	}
	return &geminiToolConfig{FunctionCallingConfig: calling}
}

func newGeminiDecoder() frameDecoder {
	var blocks []Block
	var normalizer usageNormalizer
	return func(frame []byte) (*Message, usageFragment, bool, error) {
		var response struct {
			Candidates []struct {
				Content struct {
					Parts []geminiPart `json:"parts"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
			Usage *struct {
				PromptTokens    *int64 `json:"promptTokenCount"`
				CachedTokens    *int64 `json:"cachedContentTokenCount"`
				CandidateTokens *int64 `json:"candidatesTokenCount"`
				ThoughtsTokens  *int64 `json:"thoughtsTokenCount"`
			} `json:"usageMetadata"`
		}
		if err := json.Unmarshal(frame, &response); err != nil {
			return nil, usageFragment{}, false, err
		}
		finished := false
		for _, candidate := range response.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					if len(blocks) > 0 {
						if text, ok := blocks[len(blocks)-1].(Text); ok {
							text.Text += part.Text
							blocks[len(blocks)-1] = text
							continue
						}
					}
					blocks = append(blocks, Text{Text: part.Text})
				}
				if part.FunctionCall != nil {
					toolUse, err := part.FunctionCall.toolUse(part.ThoughtSignature)
					if err != nil {
						return nil, usageFragment{}, false, err
					}
					blocks = append(blocks, toolUse)
				}
			}
			finished = finished || candidate.FinishReason != ""
		}
		fragment := usageFragment{}
		hasUsage := response.Usage != nil
		if hasUsage {
			usage := response.Usage
			fragment = normalizer.update(usage.PromptTokens, usage.CachedTokens, nil, nil, usage.CandidateTokens, usage.ThoughtsTokens)
		}
		if finished {
			message := Message{Role: RoleAssistant, Blocks: blocks}
			return &message, fragment, hasUsage, nil
		}
		return nil, fragment, hasUsage, nil
	}
}

func (c geminiFunctionCall) toolUse(thoughtSignature string) (ToolUse, error) {
	input := bytes.TrimSpace(c.Args)
	if len(input) == 0 || input[0] != '{' || !json.Valid(input) {
		return ToolUse{}, fmt.Errorf("agentkit: Gemini function call %q input is not a JSON object", c.ID)
	}
	toolUse := ToolUse{ID: c.ID, Name: c.Name, Input: append(json.RawMessage(nil), input...)}
	if thoughtSignature != "" {
		provider, err := json.Marshal(struct {
			ThoughtSignature string `json:"thoughtSignature"`
		}{ThoughtSignature: thoughtSignature})
		if err != nil {
			return ToolUse{}, err
		}
		toolUse.Provider = provider
	}
	return toolUse, nil
}

func renderGeminiTools(tools []Tool) (json.RawMessage, error) {
	type declaration struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}
	declarations := make([]declaration, len(tools))
	for index, tool := range tools {
		schema, err := narrowGeminiSchema(tool.Schema())
		if err != nil {
			return nil, err
		}
		declarations[index] = declaration{tool.Name(), tool.Description(), schema}
	}
	return json.Marshal(struct {
		Tools []struct {
			FunctionDeclarations []declaration `json:"functionDeclarations"`
		} `json:"tools"`
	}{Tools: []struct {
		FunctionDeclarations []declaration `json:"functionDeclarations"`
	}{{FunctionDeclarations: declarations}}})
}

func (w *geminiWire) RenderTools(tools []Tool) (json.RawMessage, error) {
	if err := validateCanonicalTools(tools); err != nil {
		return nil, err
	}
	return renderGeminiTools(tools)
}

func narrowGeminiSchema(schema json.RawMessage) (json.RawMessage, error) {
	var root any
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil, err
	}
	narrowGeminiSchemaNode(root)
	return json.Marshal(root)
}

func narrowGeminiSchemaNode(node any) {
	object, ok := node.(map[string]any)
	if !ok {
		return
	}
	if branches, ok := object["oneOf"].([]any); ok {
		for _, branch := range branches {
			candidate, branchOK := branch.(map[string]any)
			if !branchOK || candidate["type"] == "null" {
				continue
			}
			for key := range object {
				delete(object, key)
			}
			for key, value := range candidate {
				object[key] = value
			}
			break
		}
	}
	for _, keyword := range []string{"exclusiveMinimum", "exclusiveMaximum", "multipleOf", "uniqueItems"} {
		delete(object, keyword)
	}
	if properties, ok := object["properties"].(map[string]any); ok {
		for _, property := range properties {
			narrowGeminiSchemaNode(property)
		}
	}
	if items, present := object["items"]; present {
		narrowGeminiSchemaNode(items)
	}
	for _, keyword := range []string{"anyOf"} {
		branches, ok := object[keyword].([]any)
		if !ok {
			continue
		}
		for _, branch := range branches {
			narrowGeminiSchemaNode(branch)
		}
	}
}
