package agentkit

// catalog_table.go is the static model catalog: every chat model agentkit
// knows, each provider that serves it, and that offering's wire model name,
// context window, pricing, and reasoning vocabulary. The first offering of an
// entry is its default provider. Converted from the og-agentkit catalog data;
// z-ai native offerings and embedding entries are dropped (D0 day-one
// endpoints). Rates are nano-USD per token.

func tier(minInput, uncached, cacheRead, write5m, write1h, output int64) RateTier {
	return RateTier{
		MinInputTokens: minInput, InputUncached: uncached, CacheReadInput: cacheRead,
		CacheWrite5m: write5m, CacheWrite1h: write1h, Output: output,
	}
}

func rates(tiers ...RateTier) Pricing { return Pricing{Tiers: tiers} }

func effortSpec(levels []Effort, def Effort, canDisable bool) ReasoningSpec {
	return ReasoningSpec{
		Kind: ReasoningKindEffort, Levels: levels, CanDisable: canDisable,
		Default: ReasoningConfig{Mode: ReasoningEffort, Effort: def},
	}
}

func budgetSpec(minBudget, maxBudget int, canDisable bool, def ReasoningConfig) ReasoningSpec {
	return ReasoningSpec{
		Kind: ReasoningKindBudget, MinBudget: minBudget, MaxBudget: maxBudget,
		CanDisable: canDisable, Default: def,
	}
}

func toggleSpec(canEnable, canDisable bool, def ReasoningConfig) ReasoningSpec {
	return ReasoningSpec{Kind: ReasoningKindToggle, CanEnable: canEnable, CanDisable: canDisable, Default: def}
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

// offer builds one offering; openRouter builds the OpenRouter offering for a
// model whose router pricing and reasoning match its native offering.
func offer(provider Provider, wireModel string, context int64, pricing Pricing, reasoning ReasoningSpec) Offering {
	return Offering{Provider: provider, WireModel: wireModel, Context: context, Pricing: pricing, Reasoning: reasoning}
}

var catalogTable = []CatalogEntry{
	// ---- Anthropic ----------------------------------------------------------
	{Model: "claude-opus-5", Vendor: VendorAnthropic, Offerings: []Offering{
		offer(ProviderAnthropic, "claude-opus-5", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortMedium, true)),
		offer(ProviderOpenRouter, "anthropic/claude-opus-5", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortMedium, true)),
	}},
	{Model: "claude-opus-4-8", Vendor: VendorAnthropic, Offerings: []Offering{
		offer(ProviderAnthropic, "claude-opus-4-8", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortHigh, true)),
		offer(ProviderOpenRouter, "anthropic/claude-opus-4.8", 1_000_000,
			rates(tier(0, 5000, 500, 6250, 10000, 25000)), effortSpec(lowToMax, EffortHigh, true)),
	}},
	{Model: "claude-sonnet-4-6", Vendor: VendorAnthropic, Offerings: []Offering{
		offer(ProviderAnthropic, "claude-sonnet-4-6", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowHighMax, EffortHigh, true)),
		offer(ProviderOpenRouter, "anthropic/claude-sonnet-4.6", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowHighMax, EffortHigh, true)),
	}},
	{Model: "claude-haiku-4-5", Vendor: VendorAnthropic, Offerings: []Offering{
		offer(ProviderAnthropic, "claude-haiku-4-5", 200_000,
			rates(tier(0, 1000, 100, 1250, 2000, 5000)), budgetSpec(1024, 4096, true, reasoningOff)),
		offer(ProviderOpenRouter, "anthropic/claude-haiku-4.5", 200_000,
			rates(tier(0, 1000, 100, 1250, 2000, 5000)), budgetSpec(1024, 4096, true, reasoningOff)),
	}},
	{Model: "claude-fable-5", Vendor: VendorAnthropic, Offerings: []Offering{
		offer(ProviderAnthropic, "claude-fable-5", 1_000_000,
			rates(tier(0, 10000, 1000, 12500, 20000, 50000)), effortSpec(lowToMax, EffortMedium, false)),
		offer(ProviderOpenRouter, "anthropic/claude-fable-5", 1_000_000,
			rates(tier(0, 10000, 1000, 12500, 20000, 50000)), effortSpec(lowToMax, EffortMedium, false)),
	}},
	{Model: "claude-sonnet-5", Vendor: VendorAnthropic, Offerings: []Offering{
		offer(ProviderAnthropic, "claude-sonnet-5", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowToMax, EffortMedium, true)),
		offer(ProviderOpenRouter, "anthropic/claude-sonnet-5", 1_000_000,
			rates(tier(0, 3000, 300, 3750, 6000, 15000)), effortSpec(lowToMax, EffortMedium, true)),
	}},

	// ---- Google -------------------------------------------------------------
	{Model: "gemini-2.5-flash", Vendor: VendorGoogle, Offerings: []Offering{
		offer(ProviderGemini, "gemini-2.5-flash", 1_048_576,
			rates(tier(0, 300, 30, 0, 0, 2500)), budgetSpec(0, 24576, true, vendorDefault)),
		offer(ProviderOpenRouter, "google/gemini-2.5-flash", 1_048_576,
			rates(tier(0, 300, 30, 0, 0, 2500)), budgetSpec(0, 24576, true, vendorDefault)),
	}},
	{Model: "gemini-2.5-pro", Vendor: VendorGoogle, Offerings: []Offering{
		offer(ProviderGemini, "gemini-2.5-pro", 1_048_576,
			rates(tier(0, 1250, 125, 0, 0, 10000), tier(200_001, 2500, 250, 0, 0, 15000)),
			budgetSpec(128, 32768, false, vendorDefault)),
		offer(ProviderOpenRouter, "google/gemini-2.5-pro", 1_048_576,
			rates(tier(0, 1250, 125, 0, 0, 10000), tier(200_001, 2500, 250, 0, 0, 15000)),
			budgetSpec(128, 32768, false, vendorDefault)),
	}},
	{Model: "gemini-3.5-flash", Vendor: VendorGoogle, Offerings: []Offering{
		offer(ProviderGemini, "gemini-3.5-flash", 1_048_576,
			rates(tier(0, 1500, 150, 0, 0, 9000)), effortSpec(minToHigh, EffortMedium, false)),
		offer(ProviderOpenRouter, "google/gemini-3.5-flash", 1_048_576,
			rates(tier(0, 1500, 150, 0, 0, 9000)), effortSpec(minToHigh, EffortMedium, false)),
	}},
	{Model: "gemini-3.7-flash", Vendor: VendorGoogle, Offerings: []Offering{
		offer(ProviderGemini, "gemini-3.7-flash", 1_048_576,
			rates(tier(0, 750, 75, 0, 0, 3750)), effortSpec(lowToHigh, EffortMedium, false)),
		offer(ProviderOpenRouter, "google/gemini-3.7-flash", 1_048_576,
			rates(tier(0, 375, 38, 0, 0, 1875)), effortSpec(lowToHigh, EffortMedium, false)),
	}},
	{Model: "gemini-3.1-flash-lite", Vendor: VendorGoogle, Offerings: []Offering{
		offer(ProviderGemini, "gemini-3.1-flash-lite", 1_048_576,
			rates(tier(0, 250, 25, 0, 0, 1500)), effortSpec(minToHigh, EffortMedium, false)),
		offer(ProviderOpenRouter, "google/gemini-3.1-flash-lite", 1_048_576,
			rates(tier(0, 250, 25, 0, 0, 1500)), effortSpec(minToHigh, EffortMedium, true)),
	}},
	{Model: "gemini-3.1-pro-preview", Vendor: VendorGoogle, Offerings: []Offering{
		offer(ProviderGemini, "gemini-3.1-pro-preview", 1_048_576,
			rates(tier(0, 2000, 200, 0, 0, 12000), tier(200_001, 4000, 400, 0, 0, 18000)),
			effortSpec(lowToHigh, EffortHigh, false)),
		offer(ProviderOpenRouter, "google/gemini-3.1-pro-preview", 1_048_576,
			rates(tier(0, 2000, 200, 0, 0, 12000), tier(200_001, 4000, 400, 0, 0, 18000)),
			effortSpec(lowToHigh, EffortHigh, false)),
	}},

	// ---- OpenAI -------------------------------------------------------------
	{Model: "gpt-5.5-pro", Vendor: VendorOpenAI, Offerings: []Offering{
		offer(ProviderOpenAI, "gpt-5.5-pro", 1_050_000,
			rates(tier(0, 30000, 30000, 0, 0, 180000)), effortSpec(highXHigh, EffortHigh, false)),
		offer(ProviderOpenRouter, "openai/gpt-5.5-pro", 1_050_000,
			rates(tier(0, 30000, 30000, 0, 0, 180000)), effortSpec(highXHigh, EffortHigh, false)),
	}},
	{Model: "gpt-5.5", Vendor: VendorOpenAI, Offerings: []Offering{
		offer(ProviderOpenAI, "gpt-5.5", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000), tier(272_001, 10000, 1000, 0, 0, 45000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
		offer(ProviderOpenRouter, "openai/gpt-5.5", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000), tier(272_001, 10000, 1000, 0, 0, 45000)),
			effortSpec(noneToXHigh, EffortMedium, true)),
	}},
	{Model: "gpt-5.4", Vendor: VendorOpenAI, Offerings: []Offering{
		offer(ProviderOpenAI, "gpt-5.4", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000), tier(272_001, 5000, 500, 0, 0, 22500)),
			effortSpec(noneToXHigh, EffortNone, true)),
		offer(ProviderOpenRouter, "openai/gpt-5.4", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000), tier(272_001, 5000, 500, 0, 0, 22500)),
			effortSpec(noneToXHigh, EffortNone, true)),
	}},
	{Model: "gpt-5.4-mini", Vendor: VendorOpenAI, Offerings: []Offering{
		offer(ProviderOpenAI, "gpt-5.4-mini", 400_000,
			rates(tier(0, 750, 75, 0, 0, 4500)), effortSpec(noneToXHigh, EffortNone, true)),
		offer(ProviderOpenRouter, "openai/gpt-5.4-mini", 400_000,
			rates(tier(0, 750, 75, 0, 0, 4500)), effortSpec(noneToXHigh, EffortNone, true)),
	}},
	{Model: "gpt-5.4-nano", Vendor: VendorOpenAI, Offerings: []Offering{
		offer(ProviderOpenAI, "gpt-5.4-nano", 400_000,
			rates(tier(0, 200, 20, 0, 0, 1250)), effortSpec(noneToXHigh, EffortNone, true)),
		offer(ProviderOpenRouter, "openai/gpt-5.4-nano", 400_000,
			rates(tier(0, 200, 20, 0, 0, 1250)), effortSpec(noneToXHigh, EffortNone, true)),
	}},
	{Model: "gpt-5.6-sol", Vendor: VendorOpenAI, Offerings: []Offering{
		offer(ProviderOpenAI, "gpt-5.6-sol", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(ProviderOpenRouter, "openai/gpt-5.6-sol", 1_050_000,
			rates(tier(0, 5000, 500, 0, 0, 30000)), effortSpec(noneToXHigh, EffortMedium, true)),
	}},
	{Model: "gpt-5.6-terra", Vendor: VendorOpenAI, Offerings: []Offering{
		offer(ProviderOpenAI, "gpt-5.6-terra", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(ProviderOpenRouter, "openai/gpt-5.6-terra", 1_050_000,
			rates(tier(0, 2500, 250, 0, 0, 15000)), effortSpec(noneToXHigh, EffortMedium, true)),
	}},
	{Model: "gpt-5.6-luna", Vendor: VendorOpenAI, Offerings: []Offering{
		offer(ProviderOpenAI, "gpt-5.6-luna", 400_000,
			rates(tier(0, 1000, 100, 0, 0, 6000)), effortSpec(noneToXHigh, EffortMedium, true)),
		offer(ProviderOpenRouter, "openai/gpt-5.6-luna", 400_000,
			rates(tier(0, 1000, 100, 0, 0, 6000)), effortSpec(noneToXHigh, EffortMedium, true)),
	}},

	// ---- xAI ----------------------------------------------------------------
	{Model: "grok-4.5", Vendor: VendorXAI, Offerings: []Offering{
		offer(ProviderXAI, "grok-4.5", 500_000,
			rates(tier(0, 2000, 300, 0, 0, 6000), tier(200_001, 4000, 600, 0, 0, 12000)),
			effortSpec(lowToHigh, EffortHigh, false)),
		offer(ProviderOpenRouter, "x-ai/grok-4.5", 500_000,
			rates(tier(0, 2000, 300, 0, 0, 6000), tier(200_001, 4000, 600, 0, 0, 12000)),
			effortSpec(lowToHigh, EffortHigh, false)),
	}},
	{Model: "grok-4.6", Vendor: VendorXAI, Offerings: []Offering{
		offer(ProviderXAI, "grok-4.6", 500_000,
			rates(tier(0, 2000, 500, 0, 0, 6000), tier(200_001, 4000, 1000, 0, 0, 12000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(ProviderOpenRouter, "x-ai/grok-4.6", 500_000,
			rates(tier(0, 2000, 500, 0, 0, 6000), tier(200_001, 4000, 1000, 0, 0, 12000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
	}},
	{Model: "grok-4.3", Vendor: VendorXAI, Offerings: []Offering{
		offer(ProviderXAI, "grok-4.3", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToHigh, EffortLow, false)),
		offer(ProviderOpenRouter, "x-ai/grok-4.3", 256_000,
			rates(tier(0, 3000, 0, 0, 0, 15000)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "grok-4.20", Vendor: VendorXAI, Offerings: []Offering{
		offer(ProviderXAI, "grok-4.20", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			toggleSpec(true, false, reasoningOn)),
		offer(ProviderOpenRouter, "x-ai/grok-4.20", 2_000_000,
			rates(tier(0, 3000, 0, 0, 0, 15000), tier(200_001, 6000, 0, 0, 0, 30000)),
			toggleSpec(true, true, reasoningOff)),
	}},
	{Model: "grok-4.20-multi-agent", Vendor: VendorXAI, Offerings: []Offering{
		offer(ProviderXAI, "grok-4.20-multi-agent", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
		offer(ProviderOpenRouter, "x-ai/grok-4.20-multi-agent", 1_000_000,
			rates(tier(0, 1250, 200, 0, 0, 2500), tier(200_001, 2500, 400, 0, 0, 5000)),
			effortSpec(lowToXHigh, EffortHigh, false)),
	}},

	// ---- OpenRouter-only vendors -------------------------------------------
	{Model: "deepseek-v4-flash", Vendor: VendorDeepSeek, Offerings: []Offering{
		offer(ProviderOpenRouter, "deepseek/deepseek-v4-flash", 128_000,
			rates(tier(0, 300, 30, 0, 0, 1200)), toggleSpec(true, true, vendorDefault)),
	}},
	{Model: "deepseek-v4-pro", Vendor: VendorDeepSeek, Offerings: []Offering{
		offer(ProviderOpenRouter, "deepseek/deepseek-v4-pro", 128_000,
			rates(tier(0, 600, 60, 0, 0, 2400)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "nemotron-3.5-lightning", Vendor: VendorNVIDIA, Offerings: []Offering{
		offer(ProviderOpenRouter, "nvidia/nemotron-3.5-lightning", 1_000_000,
			rates(tier(0, 80, 40, 0, 0, 200)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "qwen3.8-max", Vendor: VendorQwen, Offerings: []Offering{
		offer(ProviderOpenRouter, "qwen/qwen3.8-max", 1_000_000,
			rates(tier(0, 2000, 250, 0, 0, 6000)), effortSpec(lowMedXHigh, EffortXHigh, false)),
	}},
	{Model: "qwen3.8-27b", Vendor: VendorQwen, Offerings: []Offering{
		offer(ProviderOpenRouter, "qwen/qwen3.8-27b", 262_144,
			rates(tier(0, 450, 0, 0, 0, 3200)), effortSpec(lowMedXHigh, EffortXHigh, true)),
	}},
	{Model: "kimi-k3", Vendor: VendorMoonshot, Offerings: []Offering{
		offer(ProviderOpenRouter, "moonshotai/kimi-k3", 256_000,
			rates(tier(0, 600, 60, 0, 0, 2500)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "kimi-k2.7-code", Vendor: VendorMoonshot, Offerings: []Offering{
		offer(ProviderOpenRouter, "moonshotai/kimi-k2.7-code", 256_000,
			rates(tier(0, 600, 60, 0, 0, 2500)), toggleSpec(true, false, reasoningOn)),
	}},
	{Model: "kimi-k2.6", Vendor: VendorMoonshot, Offerings: []Offering{
		offer(ProviderOpenRouter, "moonshotai/kimi-k2.6", 256_000,
			rates(tier(0, 600, 60, 0, 0, 2500)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "glm-5.2", Vendor: VendorZAI, Offerings: []Offering{
		offer(ProviderOpenRouter, "z-ai/glm-5.2", 202_752,
			rates(tier(0, 1400, 260, 0, 0, 4400)), effortSpec(highXHigh, EffortHigh, true)),
	}},
	{Model: "glm-5.1", Vendor: VendorZAI, Offerings: []Offering{
		offer(ProviderOpenRouter, "z-ai/glm-5.1", 202_752,
			rates(tier(0, 1400, 260, 0, 0, 4400)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "glm-4.7", Vendor: VendorZAI, Offerings: []Offering{
		offer(ProviderOpenRouter, "z-ai/glm-4.7", 202_752,
			rates(tier(0, 600, 110, 0, 0, 2200)), toggleSpec(true, true, reasoningOn)),
	}},
	{Model: "glm-4.6", Vendor: VendorZAI, Offerings: []Offering{
		offer(ProviderOpenRouter, "z-ai/glm-4.6", 202_752,
			rates(tier(0, 600, 110, 0, 0, 2200)), toggleSpec(true, true, reasoningOn)),
	}},
}
