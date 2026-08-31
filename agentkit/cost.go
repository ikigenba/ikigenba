package agentkit

// Cost is the price of one turn in nano-USD (1e-9 USD). Known reports whether
// Amount reflects a real figure; a false Known means the amount is unresolved
// (typically zero) and must not be treated as "this turn was free". Aggregation
// propagates Known: summing any Cost whose Known is false yields a sum whose
// Known is false, so a cumulative total can never silently under-count.
type Cost struct {
	Amount int64 // nano-USD
	Known  bool
}

// Pricing is a consumer-supplied per-model rate, consulted when a wire reports
// no cost of its own. Rates are nano-USD per token so the arithmetic stays in
// integers end to end.
type Pricing struct {
	InputPerToken     int64 // nano-USD per fresh input token
	CachedPerToken    int64 // nano-USD per cached input token
	OutputPerToken    int64 // nano-USD per output token
	ReasoningPerToken int64 // nano-USD per reasoning token
}

var builtInChatPricing = map[string]Pricing{
	"gpt-4.1-mini": {
		InputPerToken:     400,
		CachedPerToken:    100,
		OutputPerToken:    1_600,
		ReasoningPerToken: 1_600,
	},
}

func priceUsage(usage Usage, pricing Pricing) Cost {
	return Cost{
		Amount: usage.InputTokens*pricing.InputPerToken +
			usage.CachedTokens*pricing.CachedPerToken +
			usage.OutputTokens*pricing.OutputPerToken +
			usage.ReasoningTokens*pricing.ReasoningPerToken,
		Known: true,
	}
}

func resolveCost(model string, usage Usage, wireAmount *int64, consumerPricing map[string]Pricing) Cost {
	if wireAmount != nil {
		return Cost{Amount: *wireAmount, Known: true}
	}
	if pricing, ok := consumerPricing[model]; ok {
		return priceUsage(usage, pricing)
	}
	if pricing, ok := builtInChatPricing[model]; ok {
		return priceUsage(usage, pricing)
	}
	return Cost{}
}

func aggregateCosts(costs ...Cost) Cost {
	total := Cost{Known: true}
	for _, cost := range costs {
		total.Amount += cost.Amount
		total.Known = total.Known && cost.Known
	}
	return total
}
