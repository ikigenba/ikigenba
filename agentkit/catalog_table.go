package agentkit

import "net/url"

// catalog_table.go is the static model catalog: every chat model agentkit
// knows, each host that serves it over which wire format, and that offering's
// wire model name, context window, pricing, and reasoning vocabulary. The
// first offering of an entry is its default host: the model's own vendor when
// the catalog has that host, else OpenRouter. Converted from the og-agentkit catalog data;
// z-ai native offerings and embedding entries are dropped (D0 day-one
// endpoints). Rates are nano-USD per token.

func tier(minInput, uncached, cacheRead, write5m, write1h, output int64) RateTier {
	return RateTier{
		MinInputTokens: minInput, InputUncached: uncached, CacheReadInput: cacheRead,
		CacheWrite5m: write5m, CacheWrite1h: write1h, Output: output,
	}
}

func rates(tiers ...RateTier) Pricing { return Pricing{Tiers: tiers} }

// The Term on each spec is the vendor's own word for the knob (D21): "effort"
// for an effort level, "thinking_level" for Gemini 3.x's level, "thinking_budget"
// for an integer budget, and "thinking" for a bare toggle. It is a model datum,
// so it is the same on every host that serves the model.

func effortSpec(levels []Effort, def Effort, canDisable bool) ReasoningSpec {
	return ReasoningSpec{
		Kind: ReasoningKindEffort, Term: "effort", Levels: levels, CanDisable: canDisable,
		Default: ReasoningConfig{Mode: ReasoningEffort, Effort: def},
	}
}

// levelSpec is effortSpec under Gemini 3.x's name for the same control.
func levelSpec(levels []Effort, def Effort, canDisable bool) ReasoningSpec {
	spec := effortSpec(levels, def, canDisable)
	spec.Term = "thinking_level"
	return spec
}

func budgetSpec(minBudget, maxBudget int, canDisable bool, def ReasoningConfig) ReasoningSpec {
	return ReasoningSpec{
		Kind: ReasoningKindBudget, Term: "thinking_budget", MinBudget: minBudget, MaxBudget: maxBudget,
		CanDisable: canDisable, Default: def,
	}
}

func toggleSpec(canEnable, canDisable bool, def ReasoningConfig) ReasoningSpec {
	return ReasoningSpec{Kind: ReasoningKindToggle, Term: "thinking", CanEnable: canEnable, CanDisable: canDisable, Default: def}
}

var (
	vendorDefault = ReasoningConfig{Mode: ReasoningDefault}
	reasoningOff  = ReasoningConfig{Mode: ReasoningOff}
	reasoningOn   = ReasoningConfig{Mode: ReasoningOn}

	lowToMax    = []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	lowHighMax  = []Effort{EffortLow, EffortMedium, EffortHigh, EffortMax}
	noneToXHigh = []Effort{EffortNone, EffortLow, EffortMedium, EffortHigh, EffortXHigh}
	lowToHigh   = []Effort{EffortLow, EffortMedium, EffortHigh}
	lowToXHigh  = []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh}
	minToHigh   = []Effort{EffortMinimal, EffortLow, EffortMedium, EffortHigh}
	highXHigh   = []Effort{EffortHigh, EffortXHigh}
	lowMedXHigh = []Effort{EffortLow, EffortMedium, EffortXHigh}
)

// offeringTransport is the per-id transport an offering carries: the host and
// wire name a user types, the wire codec, the default base URL (Gemini's
// names the model in its path), the credential modes the host accepts, and —
// for the OAuth hosts — the token endpoint and public client id a refresh
// needs (D22).
type offeringTransport struct {
	host      Host
	wireName  WireName
	wire      WireFormat
	baseURL   func(wireModel string) string
	authModes []AuthMode
	oauth     OAuthClient
}

func fixedURL(raw string) func(string) string { return func(string) string { return raw } }

func geminiURL(wireModel string) string {
	return "https://generativelanguage.googleapis.com/v1beta/models/" + url.PathEscape(wireModel) + ":streamGenerateContent?alt=sse"
}

var (
	keyOrOAuth = []AuthMode{AuthModeAPIKey, AuthModeOAuth}
	keyOnly    = []AuthMode{AuthModeAPIKey}
	noOAuth    = OAuthClient{}

	// Public PKCE clients: the same token endpoints and client ids the oauth
	// CLI's --help examples log in with. Anthropic's terms do not permit OAuth.
	openAIOAuth = OAuthClient{TokenURL: openAIRefreshEndpoint, ClientID: openAIClientID}
	xaiOAuth    = OAuthClient{TokenURL: xaiRefreshEndpoint, ClientID: xaiClientID}
)

const (
	openAIRefreshEndpoint = "https://auth.openai.com/oauth/token"
	openAIClientID        = "app_EMoamEEZ73f0CkXaXp7hrann"
	xaiRefreshEndpoint    = "https://auth.x.ai/oauth2/token"
	xaiClientID           = "b1a00492-073a-47ea-816f-4c329264a828"
)

var offeringTransports = map[OfferingID]offeringTransport{
	OfferingAnthropicMessages:     {HostAnthropic, WireMessages, AnthropicMessagesWire(), fixedURL("https://api.anthropic.com/v1/messages"), keyOnly, noOAuth},
	OfferingOpenAIResponses:       {HostOpenAI, WireResponses, OpenAIResponsesWire(), fixedURL("https://api.openai.com/v1/responses"), keyOrOAuth, openAIOAuth},
	OfferingOpenAIChat:            {HostOpenAI, WireChat, OpenAIChatWire(), fixedURL("https://api.openai.com/v1/chat/completions"), keyOrOAuth, openAIOAuth},
	OfferingGeminiGenerateContent: {HostGemini, WireGenerateContent, GeminiGenerateContentWire(), geminiURL, keyOnly, noOAuth},
	OfferingXAIResponses:          {HostXAI, WireResponses, ResponsesWire(), fixedURL("https://api.x.ai/v1/responses"), keyOrOAuth, xaiOAuth},
	OfferingXAIChat:               {HostXAI, WireChat, ChatWire(), fixedURL("https://api.x.ai/v1/chat/completions"), keyOrOAuth, xaiOAuth},
	OfferingOpenRouterChat:        {HostOpenRouter, WireChat, ChatWire(), fixedURL("https://openrouter.ai/api/v1/chat/completions"), keyOnly, noOAuth},
	OfferingOpenRouterResponses:   {HostOpenRouter, WireResponses, ResponsesWire(), fixedURL("https://openrouter.ai/api/v1/responses"), keyOnly, noOAuth},
}

// offer builds one offering, filling its transport from offeringTransports.
func offer(id OfferingID, wireModel string, context int64, pricing Pricing, reasoning ReasoningSpec) Offering {
	transport := offeringTransports[id]
	return Offering{
		ID: id, Host: transport.host, WireName: transport.wireName, WireFormat: transport.wire,
		BaseURL: transport.baseURL(wireModel), AuthModes: append([]AuthMode(nil), transport.authModes...),
		OAuth: transport.oauth, WireModel: wireModel, Context: context, Pricing: pricing, Reasoning: reasoning,
	}
}

var catalogTable = []CatalogEntry{
	// ---- Anthropic ----------------------------------------------------------
	{Model: "claude-opus-5", Offerings: []Offering{
		offer(OfferingAnthropicMessages, "claude-opus-5", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "anthropic/claude-opus-5", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-opus-5", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortMedium, true)),
	}},
	{Model: "claude-opus-4-8", Offerings: []Offering{
		offer(OfferingAnthropicMessages, "claude-opus-4-8", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortHigh, true)),
		offer(OfferingOpenRouterChat, "anthropic/claude-opus-4.8", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortHigh, true)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-opus-4.8", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortHigh, true)),
	}},
	{Model: "claude-sonnet-4-6", Offerings: []Offering{
		offer(OfferingAnthropicMessages, "claude-sonnet-4-6", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowHighMax, EffortHigh, true)),
		offer(OfferingOpenRouterChat, "anthropic/claude-sonnet-4.6", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowHighMax, EffortHigh, true)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-sonnet-4.6", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowHighMax, EffortHigh, true)),
	}},
	{Model: "claude-haiku-4-5", Offerings: []Offering{
		offer(OfferingAnthropicMessages, "claude-haiku-4-5", 200_000,
			rates(tier(0, 1000, 100, 1250, 2000, 5000)), budgetSpec(1024, 4096, true, reasoningOff)),
		offer(OfferingOpenRouterChat, "anthropic/claude-haiku-4.5", 200_000,
			rates(tier(0, 1000, 100, 1250, 2000, 5000)), budgetSpec(1024, 4096, true, reasoningOff)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-haiku-4.5", 200_000,
			rates(tier(0, 1000, 100, 1250, 2000, 5000)), budgetSpec(1024, 4096, true, reasoningOff)),
	}},
	{Model: "claude-fable-5", Offerings: []Offering{
		offer(OfferingAnthropicMessages, "claude-fable-5", 1_000_000,
			rates(tier(0, 10000, 1000, 12500, 20000, 50000)), effortSpec(lowToMax, EffortMedium, false)),
		offer(OfferingOpenRouterChat, "anthropic/claude-fable-5", 1_000_000,
			rates(tier(0, 10000, 1000, 12500, 20000, 50000)), effortSpec(lowToMax, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-fable-5", 1_000_000,
			rates(tier(0, 10000, 1000, 12500, 20000, 50000)), effortSpec(lowToMax, EffortMedium, false)),
	}},
	{Model: "claude-sonnet-5", Offerings: []Offering{
		offer(OfferingAnthropicMessages, "claude-sonnet-5", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowToMax, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "anthropic/claude-sonnet-5", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowToMax, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-sonnet-5", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowToMax, EffortMedium, true)),
	}},

	// ---- Google -------------------------------------------------------------
	{Model: "gemini-2.5-flash", Offerings: []Offering{
		offer(OfferingGeminiGenerateContent, "gemini-2.5-flash", 1_048_576,
			rates(tier(0, 300, 30, 0, 0, 2500)), budgetSpec(0, 24576, true, vendorDefault)),
		offer(OfferingOpenRouterChat, "google/gemini-2.5-flash", 1_048_576,
			rates(tier(0, 300, 30, 0, 0, 2500)), budgetSpec(0, 24576, true, vendorDefault)),
		offer(OfferingOpenRouterResponses, "google/gemini-2.5-flash", 1_048_576,
			rates(tier(0, 300, 30, 0, 0, 2500)), budgetSpec(0, 24576, true, vendorDefault)),
	}},
	{Model: "gemini-2.5-pro", Offerings: []Offering{
		offer(OfferingGeminiGenerateContent, "gemini-2.5-pro", 1_048_576,
			rates(tier(0, 1250, 125, 0, 0, 10000), tier(200_001, 2500, 250, 0, 0, 15000)),
			budgetSpec(128, 32768, false, vendorDefault)),
		offer(OfferingOpenRouterChat, "google/gemini-2.5-pro", 1_048_576,
			rates(tier(0, 1250, 125, 0, 0, 10000), tier(200_001, 2500, 250, 0, 0, 15000)),
			budgetSpec(128, 32768, false, vendorDefault)),
		offer(OfferingOpenRouterResponses, "google/gemini-2.5-pro", 1_048_576,
			rates(tier(0, 1250, 125, 0, 0, 10000), tier(200_001, 2500, 250, 0, 0, 15000)),
			budgetSpec(128, 32768, false, vendorDefault)),
	}},
	{Model: "gemini-3.5-flash", Offerings: []Offering{
		offer(OfferingGeminiGenerateContent, "gemini-3.5-flash", 1_048_576,
			rates(tier(0, 1500, 150, 0, 0, 9000)), levelSpec(minToHigh, EffortMedium, false)),
		offer(OfferingOpenRouterChat, "google/gemini-3.5-flash", 1_048_576,
			rates(tier(0, 1500, 150, 0, 0, 9000)), levelSpec(minToHigh, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "google/gemini-3.5-flash", 1_048_576,
			rates(tier(0, 1500, 150, 0, 0, 9000)), levelSpec(minToHigh, EffortMedium, false)),
	}},
	{Model: "gemini-3.7-flash", Offerings: []Offering{
		offer(OfferingGeminiGenerateContent, "gemini-3.7-flash", 1_048_576,
			rates(tier(0, 750, 75, 0, 0, 3750)), levelSpec(lowToHigh, EffortMedium, false)),
		offer(OfferingOpenRouterChat, "google/gemini-3.7-flash", 1_048_576,
			rates(tier(0, 375, 38, 0, 0, 1875)), levelSpec(lowToHigh, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "google/gemini-3.7-flash", 1_048_576,
			rates(tier(0, 375, 38, 0, 0, 1875)), levelSpec(lowToHigh, EffortMedium, false)),
	}},
	{Model: "gemini-3.1-flash-lite", Offerings: []Offering{
		offer(OfferingGeminiGenerateContent, "gemini-3.1-flash-lite", 1_048_576,
			rates(tier(0, 250, 25, 0, 0, 1500)), levelSpec(minToHigh, EffortMedium, false)),
		offer(OfferingOpenRouterChat, "google/gemini-3.1-flash-lite", 1_048_576,
			rates(tier(0, 250, 25, 0, 0, 1500)), levelSpec(minToHigh, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "google/gemini-3.1-flash-lite", 1_048_576,
			rates(tier(0, 250, 25, 0, 0, 1500)), levelSpec(minToHigh, EffortMedium, true)),
	}},
	{Model: "gemini-3.1-pro-preview", Offerings: []Offering{
		offer(OfferingGeminiGenerateContent, "gemini-3.1-pro-preview", 1_048_576,
			rates(tier(0, 2000, 200, 0, 0, 12000), tier(200_001, 4000, 400, 0, 0, 18000)),
			levelSpec(lowToHigh, EffortHigh, false)),
		offer(OfferingOpenRouterChat, "google/gemini-3.1-pro-preview", 1_048_576,
			rates(tier(0, 2000, 200, 0, 0, 12000), tier(200_001, 4000, 400, 0, 0, 18000)),
			levelSpec(lowToHigh, EffortHigh, false)),
		offer(OfferingOpenRouterResponses, "google/gemini-3.1-pro-preview", 1_048_576,
			rates(tier(0, 2000, 200, 0, 0, 12000), tier(200_001, 4000, 400, 0, 0, 18000)),
			levelSpec(lowToHigh, EffortHigh, false)),
	}},

	// ---- OpenAI -------------------------------------------------------------
	{Model: "gpt-5.5-pro", Offerings: []Offering{
		offer(OfferingOpenAIResponses, "gpt-5.5-pro", 1_050_000,
			rates(tier(0, 30000, 30000, 0, 0, 180000)), effortSpec(highXHigh, EffortHigh, false)),
		offer(OfferingOpenAIChat, "gpt-5.5-pro", 1_050_000,
			rates(tier(0, 30000, 30000, 0, 0, 180000)), effortSpec(highXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.5-pro", 1_050_000,
			rates(tier(0, 30000, 30000, 0, 0, 180000)), effortSpec(highXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.5-pro", 1_050_000,
			rates(tier(0, 30000, 30000, 0, 0, 180000)), effortSpec(highXHigh, EffortHigh, false)),
	}},
	{Model: "gpt-5.5", Offerings: []Offering{
		offer(OfferingOpenAIResponses, "gpt-5.5", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000), tier(272_001, 10000, 1000, 0, 0, 45000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenAIChat, "gpt-5.5", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000), tier(272_001, 10000, 1000, 0, 0, 45000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.5", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000), tier(272_001, 10000, 1000, 0, 0, 45000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.5", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000), tier(272_001, 10000, 1000, 0, 0, 45000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
	}},
	{Model: "gpt-5.4", Offerings: []Offering{
		offer(OfferingOpenAIResponses, "gpt-5.4", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000), tier(272_001, 5000, 500, 0, 0, 22500)),
			effortSpec(noneToXHigh, EffortNone, true)),
		offer(OfferingOpenAIChat, "gpt-5.4", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000), tier(272_001, 5000, 500, 0, 0, 22500)),
			effortSpec(noneToXHigh, EffortNone, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.4", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000), tier(272_001, 5000, 500, 0, 0, 22500)),
			effortSpec(noneToXHigh, EffortNone, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.4", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000), tier(272_001, 5000, 500, 0, 0, 22500)),
			effortSpec(noneToXHigh, EffortNone, true)),
	}},
	{Model: "gpt-5.4-mini", Offerings: []Offering{
		offer(OfferingOpenAIResponses, "gpt-5.4-mini", 400_000,
			rates(tier(0, 750, 75, 0, 0, 4500)), effortSpec(noneToXHigh, EffortNone, true)),
		offer(OfferingOpenAIChat, "gpt-5.4-mini", 400_000,
			rates(tier(0, 750, 75, 0, 0, 4500)), effortSpec(noneToXHigh, EffortNone, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.4-mini", 400_000,
			rates(tier(0, 750, 75, 0, 0, 4500)), effortSpec(noneToXHigh, EffortNone, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.4-mini", 400_000,
			rates(tier(0, 750, 75, 0, 0, 4500)), effortSpec(noneToXHigh, EffortNone, true)),
	}},
	{Model: "gpt-5.4-nano", Offerings: []Offering{
		offer(OfferingOpenAIResponses, "gpt-5.4-nano", 400_000,
			rates(tier(0, 200, 20, 0, 0, 1250)), effortSpec(noneToXHigh, EffortNone, true)),
		offer(OfferingOpenAIChat, "gpt-5.4-nano", 400_000,
			rates(tier(0, 200, 20, 0, 0, 1250)), effortSpec(noneToXHigh, EffortNone, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.4-nano", 400_000,
			rates(tier(0, 200, 20, 0, 0, 1250)), effortSpec(noneToXHigh, EffortNone, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.4-nano", 400_000,
			rates(tier(0, 200, 20, 0, 0, 1250)), effortSpec(noneToXHigh, EffortNone, true)),
	}},
	{Model: "gpt-5.6-sol", Offerings: []Offering{
		offer(OfferingOpenAIResponses, "gpt-5.6-sol", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenAIChat, "gpt-5.6-sol", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.6-sol", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.6-sol", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000)), effortSpec(noneToXHigh, EffortMedium, true)),
	}},
	{Model: "gpt-5.6-terra", Offerings: []Offering{
		offer(OfferingOpenAIResponses, "gpt-5.6-terra", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenAIChat, "gpt-5.6-terra", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.6-terra", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.6-terra", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000)), effortSpec(noneToXHigh, EffortMedium, true)),
	}},
	{Model: "gpt-5.6-luna", Offerings: []Offering{
		offer(OfferingOpenAIResponses, "gpt-5.6-luna", 400_000,
			rates(tier(0, 1000, 100, 0, 0, 6000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenAIChat, "gpt-5.6-luna", 400_000,
			rates(tier(0, 1000, 100, 0, 0, 6000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.6-luna", 400_000,
			rates(tier(0, 1000, 100, 0, 0, 6000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.6-luna", 400_000,
			rates(tier(0, 1000, 100, 0, 0, 6000)), effortSpec(noneToXHigh, EffortMedium, true)),
	}},

	// ---- xAI ----------------------------------------------------------------
	{Model: "grok-4.5", Offerings: []Offering{
		offer(OfferingXAIResponses, "grok-4.5", 500_000,
			rates(tier(0, 2000, 300, 0, 0, 6000), tier(200_001, 4000, 600, 0, 0, 12000)),
			effortSpec(lowToHigh, EffortHigh, false)),
		offer(OfferingXAIChat, "grok-4.5", 500_000,
			rates(tier(0, 2000, 300, 0, 0, 6000), tier(200_001, 4000, 600, 0, 0, 12000)),
			effortSpec(lowToHigh, EffortHigh, false)),
		offer(OfferingOpenRouterChat, "x-ai/grok-4.5", 500_000,
			rates(tier(0, 2000, 300, 0, 0, 6000), tier(200_001, 4000, 600, 0, 0, 12000)),
			effortSpec(lowToHigh, EffortHigh, false)),
		offer(OfferingOpenRouterResponses, "x-ai/grok-4.5", 500_000,
			rates(tier(0, 2000, 300, 0, 0, 6000), tier(200_001, 4000, 600, 0, 0, 12000)),
			effortSpec(lowToHigh, EffortHigh, false)),
	}},
	{Model: "grok-4.6", Offerings: []Offering{
		offer(OfferingXAIResponses, "grok-4.6", 500_000,
			rates(tier(0, 2000, 500, 0, 0, 6000), tier(200_001, 4000, 1000, 0, 0, 12000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingXAIChat, "grok-4.6", 500_000,
			rates(tier(0, 2000, 500, 0, 0, 6000), tier(200_001, 4000, 1000, 0, 0, 12000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterChat, "x-ai/grok-4.6", 500_000,
			rates(tier(0, 2000, 500, 0, 0, 6000), tier(200_001, 4000, 1000, 0, 0, 12000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterResponses, "x-ai/grok-4.6", 500_000,
			rates(tier(0, 2000, 500, 0, 0, 6000), tier(200_001, 4000, 1000, 0, 0, 12000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
	}},
	{Model: "grok-4.3", Offerings: []Offering{
		offer(OfferingXAIResponses, "grok-4.3", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToHigh, EffortLow, false)),
		offer(OfferingXAIChat, "grok-4.3", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToHigh, EffortLow, false)),
		offer(OfferingOpenRouterChat, "x-ai/grok-4.3", 256_000,
			rates(tier(0, 3000, 0, 0, 0, 15000)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "x-ai/grok-4.3", 256_000,
			rates(tier(0, 3000, 0, 0, 0, 15000)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "grok-4.20", Offerings: []Offering{
		offer(OfferingXAIResponses, "grok-4.20", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			toggleSpec(true, false, reasoningOn)),
		offer(OfferingXAIChat, "grok-4.20", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			toggleSpec(true, false, reasoningOn)),
		offer(OfferingOpenRouterChat, "x-ai/grok-4.20", 2_000_000,
			rates(tier(0, 3000, 0, 0, 0, 15000), tier(200_001, 6000, 0, 0, 0, 30000)),
			toggleSpec(true, true, reasoningOff)),
		offer(OfferingOpenRouterResponses, "x-ai/grok-4.20", 2_000_000,
			rates(tier(0, 3000, 0, 0, 0, 15000), tier(200_001, 6000, 0, 0, 0, 30000)),
			toggleSpec(true, true, reasoningOff)),
	}},
	{Model: "grok-4.20-multi-agent", Offerings: []Offering{
		offer(OfferingXAIResponses, "grok-4.20-multi-agent", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingXAIChat, "grok-4.20-multi-agent", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterChat, "x-ai/grok-4.20-multi-agent", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterResponses, "x-ai/grok-4.20-multi-agent", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
	}},

	// ---- OpenRouter-only vendors -------------------------------------------
	{Model: "deepseek-v4-flash", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "deepseek/deepseek-v4-flash", 128_000,
			rates(tier(0, 300, 30, 0, 0, 1200)), toggleSpec(true, true, vendorDefault)),
		offer(OfferingOpenRouterResponses, "deepseek/deepseek-v4-flash", 128_000,
			rates(tier(0, 300, 30, 0, 0, 1200)), toggleSpec(true, true, vendorDefault)),
	}},
	{Model: "deepseek-v4-pro", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "deepseek/deepseek-v4-pro", 128_000,
			rates(tier(0, 600, 60, 0, 0, 2400)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "deepseek/deepseek-v4-pro", 128_000,
			rates(tier(0, 600, 60, 0, 0, 2400)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "nemotron-3.5-lightning", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "nvidia/nemotron-3.5-lightning", 1_000_000,
			rates(tier(0, 80, 40, 0, 0, 200)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "nvidia/nemotron-3.5-lightning", 1_000_000,
			rates(tier(0, 80, 40, 0, 0, 200)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "qwen3.8-max", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "qwen/qwen3.8-max", 1_000_000,
			rates(tier(0, 2000, 250, 0, 0, 6000)), effortSpec(lowMedXHigh, EffortXHigh, false)),
		offer(OfferingOpenRouterResponses, "qwen/qwen3.8-max", 1_000_000,
			rates(tier(0, 2000, 250, 0, 0, 6000)), effortSpec(lowMedXHigh, EffortXHigh, false)),
	}},
	{Model: "qwen3.8-27b", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "qwen/qwen3.8-27b", 262_144,
			rates(tier(0, 450, 0, 0, 0, 3200)), effortSpec(lowMedXHigh, EffortXHigh, true)),
		offer(OfferingOpenRouterResponses, "qwen/qwen3.8-27b", 262_144,
			rates(tier(0, 450, 0, 0, 0, 3200)), effortSpec(lowMedXHigh, EffortXHigh, true)),
	}},
	{Model: "kimi-k3", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "moonshotai/kimi-k3", 256_000,
			rates(tier(0, 600, 60, 0, 0, 2500)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "moonshotai/kimi-k3", 256_000,
			rates(tier(0, 600, 60, 0, 0, 2500)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "kimi-k2.7-code", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "moonshotai/kimi-k2.7-code", 256_000,
			rates(tier(0, 600, 60, 0, 0, 2500)), toggleSpec(true, false, reasoningOn)),
		offer(OfferingOpenRouterResponses, "moonshotai/kimi-k2.7-code", 256_000,
			rates(tier(0, 600, 60, 0, 0, 2500)), toggleSpec(true, false, reasoningOn)),
	}},
	{Model: "kimi-k2.6", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "moonshotai/kimi-k2.6", 256_000,
			rates(tier(0, 600, 60, 0, 0, 2500)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "moonshotai/kimi-k2.6", 256_000,
			rates(tier(0, 600, 60, 0, 0, 2500)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "glm-5.2", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-5.2", 202_752,
			rates(tier(0, 1400, 260, 0, 0, 4400)), effortSpec(highXHigh, EffortHigh, true)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-5.2", 202_752,
			rates(tier(0, 1400, 260, 0, 0, 4400)), effortSpec(highXHigh, EffortHigh, true)),
	}},
	{Model: "glm-5.1", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-5.1", 202_752,
			rates(tier(0, 1400, 260, 0, 0, 4400)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-5.1", 202_752,
			rates(tier(0, 1400, 260, 0, 0, 4400)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "glm-4.7", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-4.7", 202_752,
			rates(tier(0, 600, 110, 0, 0, 2200)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-4.7", 202_752,
			rates(tier(0, 600, 110, 0, 0, 2200)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "glm-4.6", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-4.6", 202_752,
			rates(tier(0, 600, 110, 0, 0, 2200)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-4.6", 202_752,
			rates(tier(0, 600, 110, 0, 0, 2200)), toggleSpec(true, true, reasoningOn)),
	}},
}
