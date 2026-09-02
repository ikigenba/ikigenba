package agentkit

import (
	"encoding/json"
	"fmt"
)

type geminiWire struct{ wireCodec }

func newGeminiWire(classifier wireClassifier) WireFormat {
	wire := &geminiWire{}
	wire.wireCodec = wireCodec{
		encode:     wire.encodeRequest,
		decoder:    newGeminiDecoder,
		reserved:   []string{"gemini"},
		classifier: classifier,
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
	ThinkingConfig *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
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

func (w *geminiWire) encodeRequest(state RequestState) ([]byte, error) {
	contents, err := buildGeminiContents(state.History)
	if err != nil {
		return nil, err
	}
	request := geminiRequest{
		Contents:         contents,
		GenerationConfig: buildGeminiThinkingConfig(state.Settings.Reasoning),
		ToolConfig:       buildGeminiToolConfig(state.Settings.ToolChoice),
	}
	if len(state.Tools) > 0 {
		rendered, renderErr := w.RenderTools(state.Tools)
		if renderErr != nil {
			return nil, renderErr
		}
		var declaration struct {
			Tools json.RawMessage `json:"tools"`
		}
		// RenderTools currently returns marshaled JSON; keep the boundary check so
		// encodeRequest remains defensive if that private implementation changes.
		if err := json.Unmarshal(rendered, &declaration); err != nil {
			return nil, err
		}
		request.Tools = declaration.Tools
	}
	encoded, err := json.Marshal(request)
	return append(encoded, '\n'), err
}

func buildGeminiContents(history []Message) ([]geminiContent, error) {
	contents := make([]geminiContent, 0, len(history))
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
				content.Parts = append(content.Parts, geminiPart{FunctionCall: &geminiFunctionCall{Name: block.Name, Args: block.Input}})
			case ToolResult:
				response := &geminiFunctionResponse{Name: block.ToolUseID}
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
	if thinking == nil {
		return nil
	}
	return &geminiGenerationConfig{ThinkingConfig: thinking}
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
	if calling == nil {
		return nil
	}
	return &geminiToolConfig{FunctionCallingConfig: calling}
}

func newGeminiDecoder() frameDecoder {
	var text string
	var functionCalls []geminiFunctionCall
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
				text += part.Text
				if part.FunctionCall != nil {
					functionCalls = append(functionCalls, *part.FunctionCall)
				}
			}
			finished = finished || candidate.FinishReason != ""
		}
		fragment := usageFragment{}
		hasUsage := response.Usage != nil
		if hasUsage {
			usage := response.Usage
			fragment = normalizer.update(usage.PromptTokens, usage.CachedTokens, usage.CandidateTokens, usage.ThoughtsTokens)
		}
		if finished {
			blocks := make([]Block, 0, 1+len(functionCalls))
			if text != "" {
				blocks = append(blocks, Text{Text: text})
			}
			for _, call := range functionCalls {
				blocks = append(blocks, ToolUse{ID: call.ID, Name: call.Name, Input: call.Args})
			}
			message := Message{Role: RoleAssistant, Blocks: blocks}
			return &message, fragment, hasUsage, nil
		}
		return nil, fragment, hasUsage, nil
	}
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

func (w *geminiWire) withClassifier(classifier wireClassifier) WireFormat {
	clone := &geminiWire{wireCodec: w.cloneWithClassifier(classifier)}
	clone.encode = clone.encodeRequest
	return clone
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
