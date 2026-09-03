package agentkit

// Cost is the price of one turn in nano-USD (1e-9 USD). It is a bare amount;
// aggregation is ordinary addition.
type Cost int64

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
	return Cost(usage.InputTokens*pricing.InputPerToken +
		usage.CachedTokens*pricing.CachedPerToken +
		usage.OutputTokens*pricing.OutputPerToken +
		usage.ReasoningTokens*pricing.ReasoningPerToken)
}

func resolveCost(model string, usage Usage, wireAmount *int64, consumerPricing map[string]Pricing) Cost {
	if wireAmount != nil {
		return Cost(*wireAmount)
	}
	if pricing, ok := consumerPricing[model]; ok {
		return priceUsage(usage, pricing)
	}
	if pricing, ok := builtInChatPricing[model]; ok {
		return priceUsage(usage, pricing)
	}
	return 0
}

func aggregateCosts(costs ...Cost) Cost {
	var total Cost
	for _, cost := range costs {
		total += cost
	}
	return total
}
