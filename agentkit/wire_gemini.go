package agentkit

import (
	"encoding/json"
	"fmt"
)

type geminiWire struct{ wireCodec }

func newGeminiWire(classifier wireClassifier) WireFormat {
	wire := &geminiWire{}
	wire.wireCodec = wireCodec{
		encode:     encodeGeminiRequest,
		decoder:    newGeminiDecoder,
		render:     renderGeminiTools,
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
}

func encodeGeminiRequest(state RequestState) ([]byte, error) {
	contents, err := buildGeminiContents(state.History)
	if err != nil {
		return nil, err
	}
	request := geminiRequest{
		Contents:         contents,
		GenerationConfig: buildGeminiThinkingConfig(state.Settings.Reasoning),
		ToolConfig:       buildGeminiToolConfig(state.Settings.ToolChoice),
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
			message := Message{Role: RoleAssistant, Blocks: []Block{Text{Text: text}}}
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
		declarations[index] = declaration{tool.Name(), tool.Description(), tool.Schema()}
	}
	return json.Marshal(struct {
		FunctionDeclarations []declaration `json:"functionDeclarations"`
	}{FunctionDeclarations: declarations})
}
