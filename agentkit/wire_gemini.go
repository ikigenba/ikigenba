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

func encodeGeminiRequest(state RequestState) ([]byte, error) {
	contents := make([]geminiContent, 0, len(state.History))
	for _, message := range state.History {
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
	encoded, err := json.Marshal(struct {
		Contents []geminiContent `json:"contents"`
	}{Contents: contents})
	return append(encoded, '\n'), err
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
