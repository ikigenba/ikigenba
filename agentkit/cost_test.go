package agentkit

import (
	"testing"
)

func TestPricingCostEmptyScheduleIsZero(t *testing.T) {
	// R-NLK6-WUYO
	usage := Usage{
		InputTokens: 2, CachedTokens: 3, CacheWrite5mTokens: 5,
		CacheWrite1hTokens: 7, OutputTokens: 11, ReasoningTokens: 13,
	}
	if got := (Pricing{}).Cost(usage); got != 0 {
		t.Fatalf("empty Pricing.Cost() = %d, want zero", got)
	}
	if got := (Pricing{Tiers: []RateTier{}}).Cost(usage); got != 0 {
		t.Fatalf("zero-length Pricing.Cost() = %d, want zero", got)
	}
}

func TestPricingCostUsesEveryUsageBucket(t *testing.T) {
	// R-NNZZ-OEG2
	usage := Usage{
		InputTokens: 2, CachedTokens: 3, CacheWrite5mTokens: 5,
		CacheWrite1hTokens: 7, OutputTokens: 11, ReasoningTokens: 13,
	}
	pricing := Pricing{Tiers: []RateTier{{
		InputUncached: 17, CacheReadInput: 19, CacheWrite5m: 23,
		CacheWrite1h: 29, Output: 31,
	}}}
	const wantAmount = int64(2*17 + 3*19 + 5*23 + 7*29 + (11+13)*31)
	if got := pricing.Cost(usage); got != Cost(wantAmount) {
		t.Fatalf("Pricing.Cost() = %d, want six-bucket amount %d", got, wantAmount)
	}
}

func TestPricingCostSelectsTierByTotalInput(t *testing.T) {
	// R-NMS3-AMPD
	pricing := Pricing{Tiers: []RateTier{
		{MinInputTokens: 0, InputUncached: 1},
		{MinInputTokens: 10, InputUncached: 10},
		{MinInputTokens: 20, InputUncached: 100},
	}}
	usage := Usage{
		InputTokens: 2, CachedTokens: 3, CacheWrite5mTokens: 4, CacheWrite1hTokens: 1,
	}
	if got := pricing.Cost(usage); got != Cost(20) {
		t.Fatalf("inclusive total-input tier cost = %d, want 20", got)
	}

	usage = Usage{InputTokens: 2, CachedTokens: 6, CacheWrite5mTokens: 7, CacheWrite1hTokens: 6}
	if got := pricing.Cost(usage); got != Cost(200) {
		t.Fatalf("last reached tier cost = %d, want 200", got)
	}

	pricing.Tiers[0].MinInputTokens = 5
	usage = Usage{InputTokens: 2}
	if got := pricing.Cost(usage); got != Cost(2) {
		t.Fatalf("first-tier fallback cost = %d, want 2", got)
	}
}

func TestResolveCostUsesFirstAvailableRung(t *testing.T) {
	// R-2E2H-DO7H
	usage := Usage{InputTokens: 2}
	consumer := map[string]Pricing{
		"gpt-4.1-mini": {Tiers: []RateTier{{InputUncached: 900}}},
	}
	wireAmount := int64(77)

	if got := resolveCost("gpt-4.1-mini", usage, &wireAmount, consumer); got != Cost(77) {
		t.Fatalf("wire precedence cost = %d, want amount 77", got)
	}
	if got := resolveCost("gpt-4.1-mini", usage, nil, consumer); got != Cost(1_800) {
		t.Fatalf("consumer precedence cost = %d, want amount 1800", got)
	}
	if got := resolveCost("gpt-4.1-mini", usage, nil, nil); got != Cost(800) {
		t.Fatalf("built-in fallback cost = %d, want amount 800", got)
	}
}

func TestBuiltInChatPricingResolvesLiteralModel(t *testing.T) {
	// R-2HQ6-IZFK
	usage := Usage{InputTokens: 2, CachedTokens: 3, OutputTokens: 5, ReasoningTokens: 7}
	got := resolveCost("gpt-4.1-mini", usage, nil, nil)
	const wantAmount = int64(2*400 + 3*100 + 5*1_600 + 7*1_600)
	if got != Cost(wantAmount) {
		t.Fatalf("built-in chat cost = %d, want amount %d", got, wantAmount)
	}
	if got := resolveCost("text-embedding-3-small", usage, nil, nil); got != 0 {
		t.Fatalf("off-catalog embedding cost = %d, want zero", got)
	}
}

func TestPredecodedWireCostPresenceControlsFallback(t *testing.T) {
	// R-2IY2-WR69
	const billedMilliUSD = int64(7)
	convertedNanoUSD := billedMilliUSD * 1_000_000
	consumer := map[string]Pricing{
		"custom-chat": {Tiers: []RateTier{{InputUncached: 23}}},
	}

	present := resolveCost("custom-chat", Usage{InputTokens: 3}, &convertedNanoUSD, consumer)
	if present != Cost(7_000_000) {
		t.Fatalf("present predecoded wire cost = %d, want 7000000 nano-USD", present)
	}
	absent := resolveCost("custom-chat", Usage{InputTokens: 3}, nil, consumer)
	if absent != Cost(69) {
		t.Fatalf("absent wire cost fallback = %d, want consumer-priced amount 69", absent)
	}
}

func TestAggregateCostsUsesOrdinaryIntegerAddition(t *testing.T) {
	// R-NSVL-7HEU
	if got := aggregateCosts(Cost(7), Cost(0), Cost(-3), Cost(11)); got != Cost(15) {
		t.Fatalf("aggregateCosts(7, 0, -3, 11) = %d, want 15", got)
	}
	if got := aggregateCosts(); got != Cost(0) {
		t.Fatalf("aggregateCosts() = %d, want zero", got)
	}
}
