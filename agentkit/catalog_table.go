package agentkit

import "net/url"

// catalog_table.go is the static model catalog: every chat model agentkit
// knows, each host that serves it over which wire format, and that offering's
// wire model name, context window, pricing, and reasoning vocabulary. The
// first offering of an entry is its default host: the model's own vendor when
// the catalog has that host, else OpenRouter. Converted from the og-agentkit catalog data;
// z-ai native offerings and embedding entries are dropped (D0 day-one
// endpoints). Rates are nano-USD per token.
//
// Refreshed 2026-10-18: deprecated rows removed, released models added, and
// every rate as published on that date, OpenRouter's from the models API
// snapshot of 2026-10-18 17:42 GMT. The probes behind the new rows are in
// docs/probes/catalog-2026-10.md.

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
	noneToMax   = []Effort{EffortNone, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	lowToHigh   = []Effort{EffortLow, EffortMedium, EffortHigh}
	lowToXHigh  = []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh}
	minToHigh   = []Effort{EffortMinimal, EffortLow, EffortMedium, EffortHigh}
	highXHigh   = []Effort{EffortHigh, EffortXHigh}
	lowMedXHigh = []Effort{EffortLow, EffortMedium, EffortXHigh}
	minToMax    = []Effort{EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
)

// offeringTransport is the per-id transport an offering carries: the host and
// wire name a user types, the wire codec, the api_key endpoint URL (Gemini's
// names the model in its path), and — where the host honors an OAuth bearer —
// the oauth endpoint spec (D21). alwaysOAuth lists that spec on every
// offering of the id (xAI); otherwise it is listed only where codex() opts a
// model in, because the Codex backend serves a subset of OpenAI's models.
type offeringTransport struct {
	host        Host
	wireName    WireName
	wire        WireFormat
	apiKeyURL   func(wireModel string) string
	oauth       EndpointSpec
	alwaysOAuth bool
}

func fixedURL(raw string) func(string) string { return func(string) string { return raw } }

func geminiURL(wireModel string) string {
	return "https://generativelanguage.googleapis.com/v1beta/models/" + url.PathEscape(wireModel) + ":streamGenerateContent?alt=sse"
}

const (
	openAIRefreshEndpoint = "https://auth.openai.com/oauth/token"
	openAIClientID        = "app_EMoamEEZ73f0CkXaXp7hrann"
	xaiRefreshEndpoint    = "https://auth.x.ai/oauth2/token"
	xaiClientID           = "b1a00492-073a-47ea-816f-4c329264a828"
)

var (
	// Public PKCE clients: the same refresh endpoints and client ids the oauth
	// CLI's --help examples log in with. Anthropic's terms do not permit OAuth.
	openAIRotation = Rotation{RefreshURL: openAIRefreshEndpoint, ClientID: openAIClientID}
	xaiRotation    = Rotation{RefreshURL: xaiRefreshEndpoint, ClientID: xaiClientID}

	// A ChatGPT OAuth token is honored only by the Codex backend, never by
	// api.openai.com, and only on the responses protocol (proven live, D21).
	codexOAuth = EndpointSpec{AuthMode: AuthModeOAuth, BaseURL: "https://chatgpt.com/backend-api/codex/responses", Rotation: openAIRotation}
	// api.x.ai accepts an xAI OAuth bearer on both protocols at the same URLs.
	xaiResponsesOAuth = EndpointSpec{AuthMode: AuthModeOAuth, BaseURL: "https://api.x.ai/v1/responses", Rotation: xaiRotation}
	xaiChatOAuth      = EndpointSpec{AuthMode: AuthModeOAuth, BaseURL: "https://api.x.ai/v1/chat/completions", Rotation: xaiRotation}
	noOAuth           = EndpointSpec{}
)

var offeringTransports = map[OfferingID]offeringTransport{
	OfferingAnthropicMessages:     {HostAnthropic, WireMessages, AnthropicMessagesWire(), fixedURL("https://api.anthropic.com/v1/messages"), noOAuth, false},
	OfferingOpenAIResponses:       {HostOpenAI, WireResponses, OpenAIResponsesWire(), fixedURL("https://api.openai.com/v1/responses"), codexOAuth, false},
	OfferingOpenAIChat:            {HostOpenAI, WireChat, OpenAIChatWire(), fixedURL("https://api.openai.com/v1/chat/completions"), noOAuth, false},
	OfferingGeminiGenerateContent: {HostGemini, WireGenerateContent, GeminiGenerateContentWire(), geminiURL, noOAuth, false},
	OfferingXAIResponses:          {HostXAI, WireResponses, XAIResponsesWire(), fixedURL("https://api.x.ai/v1/responses"), xaiResponsesOAuth, true},
	OfferingXAIChat:               {HostXAI, WireChat, XAIChatWire(), fixedURL("https://api.x.ai/v1/chat/completions"), xaiChatOAuth, true},
	OfferingOpenRouterChat:        {HostOpenRouter, WireChat, ChatWire(), fixedURL("https://openrouter.ai/api/v1/chat/completions"), noOAuth, false},
	OfferingOpenRouterResponses:   {HostOpenRouter, WireResponses, ResponsesWire(), fixedURL("https://openrouter.ai/api/v1/responses"), noOAuth, false},
}

// offer builds one offering, filling its transport from offeringTransports:
// the api_key endpoint spec first, then the oauth spec when the id always
// lists one.
func offer(id OfferingID, wireModel string, context int64, pricing Pricing, reasoning ReasoningSpec) Offering {
	transport := offeringTransports[id]
	endpoints := []EndpointSpec{{AuthMode: AuthModeAPIKey, BaseURL: transport.apiKeyURL(wireModel)}}
	if transport.alwaysOAuth {
		endpoints = append(endpoints, transport.oauth)
	}
	return Offering{
		ID: id, Host: transport.host, WireName: transport.wireName, WireFormat: transport.wire,
		Endpoints: endpoints, WireModel: wireModel, Context: context, Pricing: pricing, Reasoning: reasoning,
	}
}

// codex opts an openai-responses offering into the Codex oauth endpoint: the
// model is one the Codex backend serves under a ChatGPT account.
func codex(o Offering) Offering {
	o.Endpoints = append(o.Endpoints, offeringTransports[o.ID].oauth)
	return o
}

// maxOutput records the vendor's output cap, read from the vendor's own
// rejection of an oversize max_tokens (D21). Only Anthropic's are proven.
func maxOutput(o Offering, limit int64) Offering {
	o.MaxOutputTokens = limit
	return o
}

var catalogTable = []CatalogEntry{
	// ---- Anthropic ----------------------------------------------------------
	{Model: "claude-opus-5", Offerings: []Offering{
		maxOutput(offer(OfferingAnthropicMessages, "claude-opus-5", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortMedium, true)), 128000),
		offer(OfferingOpenRouterChat, "anthropic/claude-opus-5", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-opus-5", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortMedium, true)),
	}},
	{Model: "claude-opus-4-8", Offerings: []Offering{
		maxOutput(offer(OfferingAnthropicMessages, "claude-opus-4-8", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortHigh, true)), 128000),
		offer(OfferingOpenRouterChat, "anthropic/claude-opus-4.8", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortHigh, true)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-opus-4.8", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortHigh, true)),
	}},
	{Model: "claude-sonnet-4-6", Offerings: []Offering{
		maxOutput(offer(OfferingAnthropicMessages, "claude-sonnet-4-6", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowHighMax, EffortHigh, true)), 128000),
		offer(OfferingOpenRouterChat, "anthropic/claude-sonnet-4.6", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowHighMax, EffortHigh, true)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-sonnet-4.6", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowHighMax, EffortHigh, true)),
	}},
	{Model: "claude-haiku-4-5", Offerings: []Offering{
		maxOutput(offer(OfferingAnthropicMessages, "claude-haiku-4-5", 200_000,
			rates(tier(0, 1000, 100, 1250, 2000, 5000)), budgetSpec(1024, 4096, true, reasoningOff)), 64000),
		offer(OfferingOpenRouterChat, "anthropic/claude-haiku-4.5", 200_000,
			rates(tier(0, 1000, 100, 1250, 2000, 5000)), budgetSpec(1024, 4096, true, reasoningOff)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-haiku-4.5", 200_000,
			rates(tier(0, 1000, 100, 1250, 2000, 5000)), budgetSpec(1024, 4096, true, reasoningOff)),
	}},
	{Model: "claude-fable-5", Offerings: []Offering{
		maxOutput(offer(OfferingAnthropicMessages, "claude-fable-5", 1_000_000,
			rates(tier(0, 10000, 1000, 12500, 20000, 50000)), effortSpec(lowToMax, EffortMedium, false)), 128000),
		offer(OfferingOpenRouterChat, "anthropic/claude-fable-5", 1_000_000,
			rates(tier(0, 10000, 1000, 12500, 20000, 50000)), effortSpec(lowToMax, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-fable-5", 1_000_000,
			rates(tier(0, 10000, 1000, 12500, 20000, 50000)), effortSpec(lowToMax, EffortMedium, false)),
	}},
	{Model: "claude-fable-5-1", Offerings: []Offering{
		maxOutput(offer(OfferingAnthropicMessages, "claude-fable-5-1", 1_000_000,
			rates(tier(0, 10000, 250, 12500, 20000, 50000)), effortSpec(lowToMax, EffortHigh, false)), 128000),
		offer(OfferingOpenRouterChat, "anthropic/claude-fable-5.1", 1_000_000,
			rates(tier(0, 10000, 250, 12500, 20000, 50000)), effortSpec(lowToMax, EffortHigh, false)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-fable-5.1", 1_000_000,
			rates(tier(0, 10000, 250, 12500, 20000, 50000)), effortSpec(lowToMax, EffortHigh, false)),
	}},
	{Model: "claude-sonnet-5", Offerings: []Offering{
		maxOutput(offer(OfferingAnthropicMessages, "claude-sonnet-5", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowToMax, EffortMedium, true)), 128000),
		offer(OfferingOpenRouterChat, "anthropic/claude-sonnet-5", 1_000_000,
			rates(tier(0, 2000, 200, 2500, 4000, 10000)), effortSpec(lowToMax, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-sonnet-5", 1_000_000,
			rates(tier(0, 2000, 200, 2500, 4000, 10000)), effortSpec(lowToMax, EffortMedium, true)),
	}},
	{Model: "claude-opus-5-5", Offerings: []Offering{
		maxOutput(offer(OfferingAnthropicMessages, "claude-opus-5-5", 1_000_000,
			rates(tier(0, 4000, 200, 5000, 8000, 20000)), effortSpec(lowToMax, EffortMedium, false)), 128000),
		offer(OfferingOpenRouterChat, "anthropic/claude-opus-5.5", 1_000_000,
			rates(tier(0, 4000, 200, 5000, 8000, 20000)), effortSpec(lowToMax, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-opus-5.5", 1_000_000,
			rates(tier(0, 4000, 200, 5000, 8000, 20000)), effortSpec(lowToMax, EffortMedium, false)),
	}},
	// Sonnet 5.5 rejects thinking "disabled"; its off form is "between_tools",
	// which the Anthropic wire does not send, so the row cannot disable.
	{Model: "claude-sonnet-5-5", Offerings: []Offering{
		maxOutput(offer(OfferingAnthropicMessages, "claude-sonnet-5-5", 1_000_000,
			rates(tier(0, 2000, 100, 2500, 4000, 10000)), effortSpec(lowToMax, EffortHigh, false)), 128000),
		offer(OfferingOpenRouterChat, "anthropic/claude-sonnet-5.5", 1_000_000,
			rates(tier(0, 2000, 100, 2500, 4000, 10000)), effortSpec(lowToMax, EffortHigh, false)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-sonnet-5.5", 1_000_000,
			rates(tier(0, 2000, 100, 2500, 4000, 10000)), effortSpec(lowToMax, EffortHigh, false)),
	}},
	// Haiku 5.5 is priced by prompt length: a prompt over 100,000 tokens pays
	// the second tier. It disables only at effort high or below.
	{Model: "claude-haiku-5-5", Offerings: []Offering{
		maxOutput(offer(OfferingAnthropicMessages, "claude-haiku-5-5", 1_000_000,
			rates(tier(0, 100, 10, 125, 200, 500), tier(100_001, 500, 50, 625, 1000, 2500)),
			effortSpec(lowToMax, EffortMedium, true)), 128000),
		offer(OfferingOpenRouterChat, "anthropic/claude-haiku-5.5", 1_000_000,
			rates(tier(0, 100, 10, 125, 200, 500), tier(100_001, 500, 50, 625, 1000, 2500)),
			effortSpec(lowToMax, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "anthropic/claude-haiku-5.5", 1_000_000,
			rates(tier(0, 100, 10, 125, 200, 500), tier(100_001, 500, 50, 625, 1000, 2500)),
			effortSpec(lowToMax, EffortMedium, true)),
	}},

	// ---- Google -------------------------------------------------------------
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

	// gemini-3.8-flash disables natively (thinkingBudget 0 answers with no
	// thought tokens) but OpenRouter rejects reasoning "none" for it (D21).
	// Its rates are introductory, through 2026-12-31.
	{Model: "gemini-3.8-flash", Offerings: []Offering{
		offer(OfferingGeminiGenerateContent, "gemini-3.8-flash", 1_048_576,
			rates(tier(0, 750, 75, 0, 0, 3750)), levelSpec(lowToHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "google/gemini-3.8-flash", 1_048_576,
			rates(tier(0, 825, 83, 0, 0, 4125)), levelSpec(lowToHigh, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "google/gemini-3.8-flash", 1_048_576,
			rates(tier(0, 825, 83, 0, 0, 4125)), levelSpec(lowToHigh, EffortMedium, false)),
	}},
	{Model: "gemini-3.5-flash-lite", Offerings: []Offering{
		offer(OfferingGeminiGenerateContent, "gemini-3.5-flash-lite", 1_048_576,
			rates(tier(0, 300, 30, 0, 0, 2500)), levelSpec(minToHigh, EffortMinimal, false)),
		offer(OfferingOpenRouterChat, "google/gemini-3.5-flash-lite", 1_048_576,
			rates(tier(0, 300, 30, 0, 0, 2500)), levelSpec(minToHigh, EffortMinimal, false)),
		offer(OfferingOpenRouterResponses, "google/gemini-3.5-flash-lite", 1_048_576,
			rates(tier(0, 300, 30, 0, 0, 2500)), levelSpec(minToHigh, EffortMinimal, false)),
	}},
	// gemini-4-argon and gemini-3.6-flash, like gemini-3.8-flash, accept
	// thinkingBudget 0 natively while OpenRouter rejects disabling them. Their
	// rates are introductory: Argon through 2026-11-30, 3.6 Flash through
	// 2026-12-31.
	{Model: "gemini-4-argon", Offerings: []Offering{
		offer(OfferingGeminiGenerateContent, "gemini-4-argon", 1_048_576,
			rates(tier(0, 2000, 100, 0, 0, 10000)), levelSpec(lowToHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "google/gemini-4-argon", 1_048_576,
			rates(tier(0, 2000, 100, 0, 0, 10000)), levelSpec(lowToHigh, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "google/gemini-4-argon", 1_048_576,
			rates(tier(0, 2000, 100, 0, 0, 10000)), levelSpec(lowToHigh, EffortMedium, false)),
	}},
	{Model: "gemini-3.6-flash", Offerings: []Offering{
		offer(OfferingGeminiGenerateContent, "gemini-3.6-flash", 1_048_576,
			rates(tier(0, 750, 75, 0, 0, 3750)), levelSpec(minToHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "google/gemini-3.6-flash", 1_048_576,
			rates(tier(0, 750, 75, 0, 0, 3750)), levelSpec(minToHigh, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "google/gemini-3.6-flash", 1_048_576,
			rates(tier(0, 750, 75, 0, 0, 3750)), levelSpec(minToHigh, EffortMedium, false)),
	}},

	// ---- OpenAI -------------------------------------------------------------
	// OpenRouter lists no cache-read price for gpt-5.5-pro; a read costs the
	// uncached rate.
	{Model: "gpt-5.5-pro", Offerings: []Offering{
		offer(OfferingOpenAIResponses, "gpt-5.5-pro", 1_050_000,
			rates(tier(0, 30000, 30000, 0, 0, 180000)), effortSpec(highXHigh, EffortHigh, false)),
		offer(OfferingOpenAIChat, "gpt-5.5-pro", 1_050_000,
			rates(tier(0, 30000, 30000, 0, 0, 180000)), effortSpec(highXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.5-pro", 1_050_000,
			rates(tier(0, 30000, 30000, 0, 0, 180000), tier(272_001, 60000, 60000, 0, 0, 270000)),
			effortSpec(highXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.5-pro", 1_050_000,
			rates(tier(0, 30000, 30000, 0, 0, 180000), tier(272_001, 60000, 60000, 0, 0, 270000)),
			effortSpec(highXHigh, EffortHigh, false)),
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
	{Model: "gpt-5.6-sol", Offerings: []Offering{
		codex(offer(OfferingOpenAIResponses, "gpt-5.6-sol", 1_050_000,
			rates(tier(0, 4000, 400, 0, 0, 20000), tier(272_001, 8000, 800, 0, 0, 30000)),
			effortSpec(noneToXHigh, EffortMedium, true))),
		offer(OfferingOpenAIChat, "gpt-5.6-sol", 1_050_000,
			rates(tier(0, 4000, 400, 0, 0, 20000), tier(272_001, 8000, 800, 0, 0, 30000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.6-sol", 1_050_000,
			rates(tier(0, 4000, 400, 0, 0, 20000), tier(272_001, 8000, 800, 0, 0, 30000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.6-sol", 1_050_000,
			rates(tier(0, 4000, 400, 0, 0, 20000), tier(272_001, 8000, 800, 0, 0, 30000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
	}},
	{Model: "gpt-5.6-terra", Offerings: []Offering{
		codex(offer(OfferingOpenAIResponses, "gpt-5.6-terra", 1_050_000,
			rates(tier(0, 2000, 200, 0, 0, 12000), tier(272_001, 4000, 400, 0, 0, 18000)),
			effortSpec(noneToXHigh, EffortMedium, true))),
		offer(OfferingOpenAIChat, "gpt-5.6-terra", 1_050_000,
			rates(tier(0, 2000, 200, 0, 0, 12000), tier(272_001, 4000, 400, 0, 0, 18000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.6-terra", 1_050_000,
			rates(tier(0, 2000, 200, 0, 0, 12000), tier(272_001, 4000, 400, 0, 0, 18000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.6-terra", 1_050_000,
			rates(tier(0, 2000, 200, 0, 0, 12000), tier(272_001, 4000, 400, 0, 0, 18000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
	}},
	{Model: "gpt-5.6-luna", Offerings: []Offering{
		codex(offer(OfferingOpenAIResponses, "gpt-5.6-luna", 1_050_000,
			rates(tier(0, 200, 20, 0, 0, 1200), tier(272_001, 400, 40, 0, 0, 1800)),
			effortSpec(noneToXHigh, EffortMedium, true))),
		offer(OfferingOpenAIChat, "gpt-5.6-luna", 1_050_000,
			rates(tier(0, 200, 20, 0, 0, 1200), tier(272_001, 400, 40, 0, 0, 1800)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-5.6-luna", 1_050_000,
			rates(tier(0, 200, 20, 0, 0, 1200), tier(272_001, 400, 40, 0, 0, 1800)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-5.6-luna", 1_050_000,
			rates(tier(0, 200, 20, 0, 0, 1200), tier(272_001, 400, 40, 0, 0, 1800)),
			effortSpec(noneToXHigh, EffortMedium, true)),
	}},
	{Model: "gpt-6-astra", Offerings: []Offering{
		codex(offer(OfferingOpenAIResponses, "gpt-6-astra", 1_050_000,
			rates(tier(0, 10000, 1000, 0, 0, 50000), tier(272_001, 20000, 2000, 0, 0, 75000)),
			effortSpec(lowToMax, EffortMedium, false))),
		offer(OfferingOpenAIChat, "gpt-6-astra", 1_050_000,
			rates(tier(0, 10000, 1000, 0, 0, 50000), tier(272_001, 20000, 2000, 0, 0, 75000)),
			effortSpec(lowToMax, EffortMedium, false)),
		offer(OfferingOpenRouterChat, "openai/gpt-6-astra", 1_050_000,
			rates(tier(0, 10000, 1000, 0, 0, 50000), tier(272_001, 20000, 2000, 0, 0, 75000)),
			effortSpec(lowToMax, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "openai/gpt-6-astra", 1_050_000,
			rates(tier(0, 10000, 1000, 0, 0, 50000), tier(272_001, 20000, 2000, 0, 0, 75000)),
			effortSpec(lowToMax, EffortMedium, false)),
	}},
	// The GPT-6 rows: OpenAI's Responses wire accepts effort "max" but its Chat
	// wire rejects it, so the paired OpenAI offerings stop at "xhigh"; OpenRouter
	// takes "max" on both of its wires. On Chat Completions, gpt-6-luna and
	// gpt-6-sol call functions only at effort "none", and gpt-6.1-sol not at all.
	{Model: "gpt-6-luna", Offerings: []Offering{
		codex(offer(OfferingOpenAIResponses, "gpt-6-luna", 1_050_000,
			rates(tier(0, 100, 10, 0, 0, 500), tier(272_001, 200, 20, 0, 0, 750)),
			effortSpec(noneToXHigh, EffortMedium, true))),
		offer(OfferingOpenAIChat, "gpt-6-luna", 1_050_000,
			rates(tier(0, 100, 10, 0, 0, 500), tier(272_001, 200, 20, 0, 0, 750)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-6-luna", 1_050_000,
			rates(tier(0, 100, 10, 0, 0, 500), tier(272_001, 200, 20, 0, 0, 750)),
			effortSpec(noneToMax, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-6-luna", 1_050_000,
			rates(tier(0, 100, 10, 0, 0, 500), tier(272_001, 200, 20, 0, 0, 750)),
			effortSpec(noneToMax, EffortMedium, true)),
	}},
	{Model: "gpt-6-sol", Offerings: []Offering{
		codex(offer(OfferingOpenAIResponses, "gpt-6-sol", 1_050_000,
			rates(tier(0, 2000, 200, 0, 0, 10000), tier(272_001, 4000, 400, 0, 0, 15000)),
			effortSpec(noneToXHigh, EffortMedium, true))),
		offer(OfferingOpenAIChat, "gpt-6-sol", 1_050_000,
			rates(tier(0, 2000, 200, 0, 0, 10000), tier(272_001, 4000, 400, 0, 0, 15000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(OfferingOpenRouterChat, "openai/gpt-6-sol", 1_050_000,
			rates(tier(0, 2000, 200, 0, 0, 10000), tier(272_001, 4000, 400, 0, 0, 15000)),
			effortSpec(noneToMax, EffortMedium, true)),
		offer(OfferingOpenRouterResponses, "openai/gpt-6-sol", 1_050_000,
			rates(tier(0, 2000, 200, 0, 0, 10000), tier(272_001, 4000, 400, 0, 0, 15000)),
			effortSpec(noneToMax, EffortMedium, true)),
	}},
	{Model: "gpt-6.1-sol", Offerings: []Offering{
		codex(offer(OfferingOpenAIResponses, "gpt-6.1-sol", 1_050_000,
			rates(tier(0, 2000, 100, 0, 0, 10000), tier(272_001, 4000, 200, 0, 0, 15000)),
			effortSpec(lowToXHigh, EffortMedium, false))),
		offer(OfferingOpenAIChat, "gpt-6.1-sol", 1_050_000,
			rates(tier(0, 2000, 100, 0, 0, 10000), tier(272_001, 4000, 200, 0, 0, 15000)),
			effortSpec(lowToXHigh, EffortMedium, false)),
		offer(OfferingOpenRouterChat, "openai/gpt-6.1-sol", 1_050_000,
			rates(tier(0, 2000, 100, 0, 0, 10000), tier(272_001, 4000, 200, 0, 0, 15000)),
			effortSpec(lowToMax, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "openai/gpt-6.1-sol", 1_050_000,
			rates(tier(0, 2000, 100, 0, 0, 10000), tier(272_001, 4000, 200, 0, 0, 15000)),
			effortSpec(lowToMax, EffortMedium, false)),
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
	{Model: "grok-4.7", Offerings: []Offering{
		offer(OfferingXAIResponses, "grok-4.7", 500_000,
			rates(tier(0, 2000, 500, 0, 0, 6000), tier(200_000, 4000, 1000, 0, 0, 12000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingXAIChat, "grok-4.7", 500_000,
			rates(tier(0, 2000, 500, 0, 0, 6000), tier(200_000, 4000, 1000, 0, 0, 12000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterChat, "x-ai/grok-4.7", 500_000,
			rates(tier(0, 2000, 500, 0, 0, 6000), tier(200_000, 4000, 1000, 0, 0, 12000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterResponses, "x-ai/grok-4.7", 500_000,
			rates(tier(0, 2000, 500, 0, 0, 6000), tier(200_000, 4000, 1000, 0, 0, 12000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
	}},
	{Model: "grok-4.3", Offerings: []Offering{
		offer(OfferingXAIResponses, "grok-4.3", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToHigh, EffortLow, false)),
		offer(OfferingXAIChat, "grok-4.3", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToHigh, EffortLow, false)),
		offer(OfferingOpenRouterChat, "x-ai/grok-4.3", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "x-ai/grok-4.3", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "grok-4.20", Offerings: []Offering{
		offer(OfferingXAIResponses, "grok-4.20", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			toggleSpec(true, false, reasoningOn)),
		offer(OfferingXAIChat, "grok-4.20", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			toggleSpec(true, false, reasoningOn)),
		offer(OfferingOpenRouterChat, "x-ai/grok-4.20", 2_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			toggleSpec(true, true, reasoningOff)),
		offer(OfferingOpenRouterResponses, "x-ai/grok-4.20", 2_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			toggleSpec(true, true, reasoningOff)),
	}},
	{Model: "grok-4.20-multi-agent", Offerings: []Offering{
		offer(OfferingXAIResponses, "grok-4.20-multi-agent", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingXAIChat, "grok-4.20-multi-agent", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterChat, "x-ai/grok-4.20-multi-agent", 2_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(OfferingOpenRouterResponses, "x-ai/grok-4.20-multi-agent", 2_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
	}},
	// grok-build-0.1 always reasons and rejects every reasoning_effort value;
	// it is also served under the alias grok-code-fast-1.
	{Model: "grok-build-0.1", Offerings: []Offering{
		offer(OfferingXAIResponses, "grok-build-0.1", 256_000,
			rates(tier(0, 1000, 200, 0, 0, 2000), tier(200_001, 2000, 400, 0, 0, 4000)),
			toggleSpec(true, false, reasoningOn)),
		offer(OfferingXAIChat, "grok-build-0.1", 256_000,
			rates(tier(0, 1000, 200, 0, 0, 2000), tier(200_001, 2000, 400, 0, 0, 4000)),
			toggleSpec(true, false, reasoningOn)),
		offer(OfferingOpenRouterChat, "x-ai/grok-build-0.1", 256_000,
			rates(tier(0, 1000, 200, 0, 0, 2000), tier(200_001, 2000, 400, 0, 0, 4000)),
			toggleSpec(true, false, reasoningOn)),
		offer(OfferingOpenRouterResponses, "x-ai/grok-build-0.1", 256_000,
			rates(tier(0, 1000, 200, 0, 0, 2000), tier(200_001, 2000, 400, 0, 0, 4000)),
			toggleSpec(true, false, reasoningOn)),
	}},

	// ---- OpenRouter-only vendors -------------------------------------------
	{Model: "deepseek-v4-flash", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "deepseek/deepseek-v4-flash", 1_048_576,
			rates(tier(0, 91, 18, 0, 0, 182)), toggleSpec(true, true, vendorDefault)),
		offer(OfferingOpenRouterResponses, "deepseek/deepseek-v4-flash", 1_048_576,
			rates(tier(0, 91, 18, 0, 0, 182)), toggleSpec(true, true, vendorDefault)),
	}},
	{Model: "deepseek-v4.1-flash", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "deepseek/deepseek-v4.1-flash", 1_048_576,
			rates(tier(0, 40, 2, 0, 0, 144)), effortSpec([]Effort{EffortLow, EffortHigh, EffortMax}, EffortHigh, true)),
		offer(OfferingOpenRouterResponses, "deepseek/deepseek-v4.1-flash", 1_048_576,
			rates(tier(0, 40, 2, 0, 0, 144)), effortSpec([]Effort{EffortLow, EffortHigh, EffortMax}, EffortHigh, true)),
	}},
	{Model: "deepseek-v4-pro", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "deepseek/deepseek-v4-pro", 1_048_576,
			rates(tier(0, 209, 17, 0, 0, 418)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "deepseek/deepseek-v4-pro", 1_048_576,
			rates(tier(0, 209, 17, 0, 0, 418)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "nemotron-3.5-lightning", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "nvidia/nemotron-3.5-lightning", 262_144,
			rates(tier(0, 70, 40, 0, 0, 200)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "nvidia/nemotron-3.5-lightning", 262_144,
			rates(tier(0, 70, 40, 0, 0, 200)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "nemotron-3.5-super-vl", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "nvidia/nemotron-3.5-super-vl", 1_048_576,
			rates(tier(0, 100, 80, 0, 0, 400)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "nvidia/nemotron-3.5-super-vl", 1_048_576,
			rates(tier(0, 100, 80, 0, 0, 400)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "qwen3.8-max-prime", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "qwen/qwen3.8-max-prime", 1_000_000,
			rates(tier(0, 4000, 500, 0, 0, 12000)), effortSpec(lowMedXHigh, EffortXHigh, false)),
		offer(OfferingOpenRouterResponses, "qwen/qwen3.8-max-prime", 1_000_000,
			rates(tier(0, 4000, 500, 0, 0, 12000)), effortSpec(lowMedXHigh, EffortXHigh, false)),
	}},
	{Model: "qwen3.8-2.4t-a95b", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "qwen/qwen3.8-2.4t-a95b", 1_048_576,
			rates(tier(0, 2000, 250, 0, 0, 6000)), effortSpec(lowMedXHigh, EffortXHigh, false)),
		offer(OfferingOpenRouterResponses, "qwen/qwen3.8-2.4t-a95b", 1_048_576,
			rates(tier(0, 2000, 250, 0, 0, 6000)), effortSpec(lowMedXHigh, EffortXHigh, false)),
	}},
	{Model: "qwen3.8-27b", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "qwen/qwen3.8-27b", 1_000_000,
			rates(tier(0, 240, 50, 0, 0, 2200)), effortSpec(lowMedXHigh, EffortXHigh, true)),
		offer(OfferingOpenRouterResponses, "qwen/qwen3.8-27b", 1_000_000,
			rates(tier(0, 240, 50, 0, 0, 2200)), effortSpec(lowMedXHigh, EffortXHigh, true)),
	}},
	{Model: "kimi-k3", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "moonshotai/kimi-k3", 1_048_576,
			rates(tier(0, 304, 210, 0, 0, 11936)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "moonshotai/kimi-k3", 1_048_576,
			rates(tier(0, 304, 210, 0, 0, 11936)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "kimi-k2.7-code", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "moonshotai/kimi-k2.7-code", 262_144,
			rates(tier(0, 671, 180, 0, 0, 3350)), toggleSpec(true, false, reasoningOn)),
		offer(OfferingOpenRouterResponses, "moonshotai/kimi-k2.7-code", 262_144,
			rates(tier(0, 671, 180, 0, 0, 3350)), toggleSpec(true, false, reasoningOn)),
	}},
	{Model: "kimi-k2.6", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "moonshotai/kimi-k2.6", 262_144,
			rates(tier(0, 650, 150, 0, 0, 3410)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "moonshotai/kimi-k2.6", 262_144,
			rates(tier(0, 650, 150, 0, 0, 3410)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "glm-5.2", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-5.2", 1_048_576,
			rates(tier(0, 620, 140, 0, 0, 2420)), effortSpec(highXHigh, EffortHigh, true)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-5.2", 1_048_576,
			rates(tier(0, 620, 140, 0, 0, 2420)), effortSpec(highXHigh, EffortHigh, true)),
	}},
	{Model: "glm-5.1", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-5.1", 204_800,
			rates(tier(0, 966, 179, 0, 0, 3036)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-5.1", 204_800,
			rates(tier(0, 966, 179, 0, 0, 3036)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "glm-4.7", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-4.7", 204_800,
			rates(tier(0, 540, 99, 0, 0, 1980)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-4.7", 204_800,
			rates(tier(0, 540, 99, 0, 0, 1980)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "glm-4.6", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-4.6", 204_800,
			rates(tier(0, 500, 100, 0, 0, 2000)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-4.6", 204_800,
			rates(tier(0, 500, 100, 0, 0, 2000)), toggleSpec(true, true, reasoningOn)),
	}},
	// OpenRouter completes "medium" for GLM 5.3 although Z.ai documents only
	// low/high/max; "none" is rejected (D21, September 2026 additions).
	{Model: "glm-5.3", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-5.3", 1_048_576,
			rates(tier(0, 79, 78, 0, 0, 1400)), effortSpec(lowHighMax, EffortMax, false)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-5.3", 1_048_576,
			rates(tier(0, 79, 78, 0, 0, 1400)), effortSpec(lowHighMax, EffortMax, false)),
	}},
	{Model: "glm-5.3-flash", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-5.3-flash", 1_048_576,
			rates(tier(0, 48, 6, 0, 0, 200)), effortSpec(lowHighMax, EffortMax, false)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-5.3-flash", 1_048_576,
			rates(tier(0, 48, 6, 0, 0, 200)), effortSpec(lowHighMax, EffortMax, false)),
	}},
	{Model: "glm-5.3-flashx", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-5.3-flashx", 1_048_576,
			rates(tier(0, 370, 90, 0, 0, 1250)), effortSpec([]Effort{EffortLow, EffortHigh, EffortMax}, EffortMax, false)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-5.3-flashx", 1_048_576,
			rates(tier(0, 370, 90, 0, 0, 1250)), effortSpec([]Effort{EffortLow, EffortHigh, EffortMax}, EffortMax, false)),
	}},
	{Model: "glm-5.3-prime", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "z-ai/glm-5.3-prime", 1_000_000,
			rates(tier(0, 2800, 560, 0, 0, 8800)), effortSpec(lowHighMax, EffortMax, false)),
		offer(OfferingOpenRouterResponses, "z-ai/glm-5.3-prime", 1_000_000,
			rates(tier(0, 2800, 560, 0, 0, 8800)), effortSpec(lowHighMax, EffortMax, false)),
	}},
	{Model: "qwen3.8-flash", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "qwen/qwen3.8-flash", 1_000_000,
			rates(tier(0, 150, 16, 0, 0, 470)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "qwen/qwen3.8-flash", 1_000_000,
			rates(tier(0, 150, 16, 0, 0, 470)), toggleSpec(true, true, reasoningOn)),
	}},
	// Hunyuan carries OpenRouter's top-level rate; its time-of-day rate for
	// 00:00-16:00 UTC is not modeled, since the catalog has no clock.
	{Model: "hunyuan-4-preview", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "tencent/hy4-preview", 1_048_576,
			rates(tier(0, 751, 38, 0, 0, 2251)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "tencent/hy4-preview", 1_048_576,
			rates(tier(0, 751, 38, 0, 0, 2251)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "minimax-m3", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "minimax/minimax-m3", 1_048_576,
			rates(tier(0, 300, 60, 0, 0, 1200)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "minimax/minimax-m3", 1_048_576,
			rates(tier(0, 300, 60, 0, 0, 1200)), toggleSpec(true, true, reasoningOn)),
	}},
	// NVIDIA publishes no per-token price for Nemotron 3 Ultra; the row carries OpenRouter's.
	{Model: "nemotron-3-ultra", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "nvidia/nemotron-3-ultra-550b-a55b", 262_144,
			rates(tier(0, 600, 120, 0, 0, 2400)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "nvidia/nemotron-3-ultra-550b-a55b", 262_144,
			rates(tier(0, 600, 120, 0, 0, 2400)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "muse-spark-1.3", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "meta/muse-spark-1.3", 1_048_576,
			rates(tier(0, 1250, 150, 0, 0, 4250)), effortSpec(minToMax, EffortMedium, false)),
		offer(OfferingOpenRouterResponses, "meta/muse-spark-1.3", 1_048_576,
			rates(tier(0, 1250, 150, 0, 0, 4250)), effortSpec(minToMax, EffortMedium, false)),
	}},
	{Model: "mimo-v2.6-pro", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "xiaomi/mimo-v2.6-pro", 1_050_000,
			rates(tier(0, 435, 4, 0, 0, 870)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "xiaomi/mimo-v2.6-pro", 1_050_000,
			rates(tier(0, 435, 4, 0, 0, 870)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "mimo-v2.6-flash", Offerings: []Offering{
		offer(OfferingOpenRouterChat, "xiaomi/mimo-v2.6-flash", 1_050_000,
			rates(tier(0, 140, 3, 0, 0, 280)), toggleSpec(true, true, reasoningOn)),
		offer(OfferingOpenRouterResponses, "xiaomi/mimo-v2.6-flash", 1_050_000,
			rates(tier(0, 140, 3, 0, 0, 280)), toggleSpec(true, true, reasoningOn)),
	}},
}
