package agentkit

// Cost is the price of one turn in nano-USD (1e-9 USD). It is a bare amount;
// aggregation is ordinary addition.
type Cost int64

// RateTier is one row of a model's price schedule. MinInputTokens is the
// input-token floor at which the row starts applying; the first row's floor is
// always zero. Rates are nano-USD per token so the arithmetic stays in integers
// end to end. A rate the vendor does not charge is zero.
type RateTier struct {
	MinInputTokens int64 // tier floor on total input tokens (inclusive)
	InputUncached  int64 // nano-USD per fresh input token
	CacheReadInput int64 // nano-USD per cached input token
	CacheWrite5m   int64 // nano-USD per 5-minute cache-write token
	CacheWrite1h   int64 // nano-USD per 1-hour cache-write token
	Output         int64 // nano-USD per output token; reasoning bills here too
}

// Pricing is a model's full price schedule on one provider: one or more tiers
// ordered by ascending floor. The tier applied to a turn is the last one whose
// floor the turn's total input tokens reach.
type Pricing struct {
	Tiers []RateTier
}

// Cost prices a Usage against this schedule. An empty schedule prices to zero.
func (p Pricing) Cost(u Usage) Cost {
	if len(p.Tiers) == 0 {
		return 0
	}

	tier := p.Tiers[0]
	totalInput := u.InputTokens + u.CachedTokens + u.CacheWrite5mTokens + u.CacheWrite1hTokens
	for _, candidate := range p.Tiers[1:] {
		if candidate.MinInputTokens <= totalInput {
			tier = candidate
		}
	}

	return Cost(u.InputTokens*tier.InputUncached +
		u.CachedTokens*tier.CacheReadInput +
		u.CacheWrite5mTokens*tier.CacheWrite5m +
		u.CacheWrite1hTokens*tier.CacheWrite1h +
		(u.OutputTokens+u.ReasoningTokens)*tier.Output)
}

func resolveCost(conversationIdentity Identity, usage Usage, wireAmount *int64) Cost {
	if wireAmount != nil {
		return Cost(*wireAmount)
	}
	for _, entry := range catalogTable {
		for _, candidate := range entry.Offerings {
			if string(candidate.ID) == conversationIdentity.Endpoint && candidate.WireModel == conversationIdentity.Model {
				return candidate.Pricing.Cost(usage)
			}
		}
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
