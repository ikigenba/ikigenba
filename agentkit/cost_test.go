package agentkit

import (
	"testing"
)

func TestPriceUsageArithmetic(t *testing.T) {
	got := priceUsage(
		Usage{InputTokens: 2, CachedTokens: 3, OutputTokens: 5, ReasoningTokens: 7},
		Pricing{InputPerToken: 11, CachedPerToken: 13, OutputPerToken: 17, ReasoningPerToken: 19},
	)
	const wantAmount = int64(2*11 + 3*13 + 5*17 + 7*19)
	if got != Cost(wantAmount) {
		t.Fatalf("priceUsage() = %d, want amount %d", got, wantAmount)
	}
}

func TestResolveCostUsesFirstAvailableRung(t *testing.T) {
	// R-2E2H-DO7H
	usage := Usage{InputTokens: 2}
	consumer := map[string]Pricing{
		"gpt-4.1-mini": {InputPerToken: 900},
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
		"custom-chat": {InputPerToken: 23},
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
