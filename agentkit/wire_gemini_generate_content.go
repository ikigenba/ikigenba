package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type geminiGenerateContentWire struct {
	wireCodec
	cacheMark        int
	cacheName        string
	requestUsedCache bool
}

// geminiCacheErrorStatus is the error.status / error.message shape Gemini
// uses for both cache failure modes (D27), rather than the generateContent
// response grammar.
type geminiCacheErrorStatus struct {
	Error struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

// GeminiGenerateContentWire returns the built-in Gemini GenerateContent wire codec.
func GeminiGenerateContentWire() WireFormat { return newGeminiGenerateContentWire(nil) }

func newGeminiGenerateContentWire(classifier errorClassifier) wireFormat {
	wire := &geminiGenerateContentWire{}
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

type geminiSystemInstruction struct {
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
	SystemInstruction *geminiSystemInstruction `json:"systemInstruction,omitempty"`
	Contents          []geminiContent          `json:"contents"`
	GenerationConfig  *geminiGenerationConfig  `json:"generationConfig,omitempty"`
	ToolConfig        *geminiToolConfig        `json:"toolConfig,omitempty"`
	Tools             json.RawMessage          `json:"tools,omitempty"`
	CachedContent     string                   `json:"cachedContent,omitempty"`
}

func (w *geminiGenerateContentWire) encodeRequest(state requestState) ([]byte, error) {
	contents, systemParts, cacheLive, err := w.buildRequestContents(state)
	if err != nil {
		return nil, err
	}
	request := assembleGeminiRequest(state, contents, systemParts, cacheLive, w.cacheName)
	if err := w.addOutputSchema(&request, state.Output); err != nil {
		return nil, err
	}
	if err := w.addRequestTools(&request, state.Tools, cacheLive); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(request)
	return append(encoded, '\n'), err
}

func (w *geminiGenerateContentWire) buildRequestContents(state requestState) ([]geminiContent, []geminiPart, bool, error) {
	cacheLive := w.cacheName != "" && w.cacheMark == state.SavepointMark
	w.requestUsedCache = cacheLive
	history := state.History
	if cacheLive {
		history = history[state.SavepointMark:]
	}
	contents, systemParts, err := buildGeminiContents(history)
	return contents, systemParts, cacheLive, err
}

func assembleGeminiRequest(state requestState, contents []geminiContent, systemParts []geminiPart, cacheLive bool, cacheName string) geminiRequest {
	request := geminiRequest{
		Contents:         contents,
		GenerationConfig: applyGeminiSamplingOptions(buildGeminiThinkingConfig(settingsReasoning(state.Settings)), state.Settings.Options),
		CachedContent:    cacheName,
	}
	if !cacheLive {
		request.CachedContent = ""
		request.ToolConfig = buildGeminiToolConfig(state.Settings.ToolChoice)
		if len(systemParts) > 0 {
			request.SystemInstruction = &geminiSystemInstruction{Parts: systemParts}
		}
	}
	return request
}

func (w *geminiGenerateContentWire) addOutputSchema(request *geminiRequest, output *OutputContract) error {
	if output == nil {
		return nil
	}
	schema, err := w.renderOutputSchema(output.Schema)
	if err != nil {
		return fmt.Errorf("agentkit: render Gemini output schema: %w", err)
	}
	if request.GenerationConfig == nil {
		request.GenerationConfig = &geminiGenerationConfig{}
	}
	request.GenerationConfig.ResponseMIMEType = "application/json"
	request.GenerationConfig.ResponseJSONSchema = schema
	return nil
}

func (w *geminiGenerateContentWire) addRequestTools(request *geminiRequest, tools []Tool, cacheLive bool) error {
	if cacheLive || len(tools) == 0 {
		return nil
	}
	rendered, err := w.renderToolList(tools)
	if err != nil {
		return err
	}
	request.Tools = rendered
	return nil
}

type geminiCachedContentRequest struct {
	Model             string                   `json:"model"`
	Contents          []geminiContent          `json:"contents"`
	SystemInstruction *geminiSystemInstruction `json:"systemInstruction,omitempty"`
	Tools             json.RawMessage          `json:"tools,omitempty"`
	ToolConfig        *geminiToolConfig        `json:"toolConfig,omitempty"`
}

func (w *geminiGenerateContentWire) prepareCache(ctx context.Context, client *http.Client, endpoint Endpoint, state requestState) error {
	if state.SavepointMark <= 0 || (w.cacheName != "" && w.cacheMark == state.SavepointMark) {
		return nil
	}

	contents, systemParts, err := buildGeminiContents(state.History[:state.SavepointMark])
	if err != nil {
		return err
	}
	cacheRequest := geminiCachedContentRequest{
		Model:      state.Model,
		Contents:   contents,
		ToolConfig: buildGeminiToolConfig(state.Settings.ToolChoice),
	}
	if len(systemParts) > 0 {
		cacheRequest.SystemInstruction = &geminiSystemInstruction{Parts: systemParts}
	}
	if len(state.Tools) > 0 {
		rendered, renderErr := w.renderToolList(state.Tools)
		if renderErr != nil {
			return renderErr
		}
		cacheRequest.Tools = rendered
	}
	// Every member of cacheRequest is either a scalar, a typed JSON-safe
	// structure, or RawMessage emitted by renderToolList above.
	body, _ := json.Marshal(cacheRequest)

	cacheURL := *endpoint.config.baseURL
	cacheURL.Path = "/v1beta/cachedContents"
	cacheURL.RawPath = ""
	cacheURL.RawQuery = ""
	cacheURL.Fragment = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cacheURL.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if err := endpoint.config.auth.Authenticate(ctx, request, body); err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("agentkit: create Gemini cached content: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("agentkit: Gemini cached content response: %w", err)
	}
	if !isHTTPSuccess(response.StatusCode) {
		if isGeminiCacheTooSmallRejection(response.StatusCode, responseBody) {
			// R-NWI7-DP1G: a below-minimum prefix is not a consumer error.
			// Keep the cache empty so encodeRequest renders the turn uncached.
			return nil
		}
		return classifiedGeminiCacheError(response.StatusCode, response.Header, responseBody)
	}
	var created struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(responseBody, &created); err != nil {
		return fmt.Errorf("agentkit: decode Gemini cached content response: %w", err)
	}
	w.cacheName = created.Name
	w.cacheMark = state.SavepointMark
	return nil
}

func (w *geminiGenerateContentWire) releaseCache(ctx context.Context, client *http.Client, endpoint Endpoint) error {
	if w.cacheName == "" {
		return nil
	}

	deleteURL := *endpoint.config.baseURL
	deleteURL.Path = "/v1beta/" + w.cacheName
	deleteURL.RawPath = ""
	deleteURL.RawQuery = ""
	deleteURL.Fragment = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL.String(), nil)
	if err != nil {
		return err
	}
	if err := endpoint.config.auth.Authenticate(ctx, request, nil); err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("agentkit: delete Gemini cached content: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("agentkit: Gemini cached content delete response: %w", err)
	}
	if !isHTTPSuccess(response.StatusCode) {
		return classifiedGeminiCacheError(response.StatusCode, response.Header, responseBody)
	}
	w.cacheName = ""
	return nil
}

// isGeminiCacheTooSmallRejection reports whether body is the host's
// below-minimum-size cachedContents creation rejection (R-NWI7-DP1G).
func isGeminiCacheTooSmallRejection(status int, body []byte) bool {
	if status != http.StatusBadRequest {
		return false
	}
	var parsed geminiCacheErrorStatus
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	return parsed.Error.Status == "INVALID_ARGUMENT" && strings.HasPrefix(parsed.Error.Message, "Cached content is too small")
}

// classifiedGeminiCacheError gives cachedContents failures the same *Error
// shape as ordinary response classification, including auth for a 403.
func classifiedGeminiCacheError(status int, header http.Header, body []byte) error {
	return &Error{
		Category:   classifyStatus(status),
		Status:     status,
		Message:    string(body),
		RetryAfter: parseRetryAfter(header),
	}
}

// isCacheStaleRejection recognizes a missing cachedContents resource only
// when the rejected request actually referenced the cache (R-NXQ3-RGS5).
func (w *geminiGenerateContentWire) isCacheStaleRejection(status int, body []byte) bool {
	if status != http.StatusForbidden || !w.requestUsedCache {
		return false
	}
	var parsed geminiCacheErrorStatus
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	return parsed.Error.Status == "PERMISSION_DENIED"
}

// invalidateCache makes the next cache preparation recreate the resource.
func (w *geminiGenerateContentWire) invalidateCache() {
	w.cacheName = ""
}

func (w *geminiGenerateContentWire) renderToolList(tools []Tool) (json.RawMessage, error) {
	rendered, err := w.RenderTools(tools)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Tools json.RawMessage `json:"tools"`
	}
	// RenderTools currently returns marshaled JSON from a typed envelope. Keep
	// this boundary check in one place so callers do not duplicate that grammar
	// if the private renderer changes.
	if err := json.Unmarshal(rendered, &envelope); err != nil {
		return nil, err
	}
	return envelope.Tools, nil
}

func buildGeminiContents(history []Message) ([]geminiContent, []geminiPart, error) {
	contents := make([]geminiContent, 0, len(history))
	var systemParts []geminiPart
	callNames := make(map[string]string)
	for _, message := range history {
		if message.Role == RoleSystem {
			for _, block := range message.Blocks {
				if block, ok := block.(Text); ok {
					systemParts = append(systemParts, geminiPart{Text: block.Text})
				}
			}
			continue
		}
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
						return nil, nil, fmt.Errorf("agentkit: Gemini reasoning replay: %w", err)
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
						return nil, nil, fmt.Errorf("agentkit: Gemini tool-use replay: %w", err)
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
	return contents, systemParts, nil
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
					use, err := part.FunctionCall.asToolUse(part.ThoughtSignature)
					if err != nil {
						return nil, usageFragment{}, false, err
					}
					blocks = append(blocks, use)
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

func (c geminiFunctionCall) asToolUse(thoughtSignature string) (ToolUse, error) {
	input := bytes.TrimSpace(c.Args)
	if len(input) == 0 || input[0] != '{' || !json.Valid(input) {
		return ToolUse{}, fmt.Errorf("agentkit: Gemini function call %q input is not a JSON object", c.ID)
	}
	toolUse := ToolUse{ID: c.ID, Name: c.Name, Input: append(json.RawMessage(nil), input...)}
	if thoughtSignature != "" {
		// A struct containing only a string is unconditionally JSON-marshalable.
		provider, _ := json.Marshal(struct {
			ThoughtSignature string `json:"thoughtSignature"`
		}{ThoughtSignature: thoughtSignature})
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

func (w *geminiGenerateContentWire) RenderTools(tools []Tool) (json.RawMessage, error) {
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
