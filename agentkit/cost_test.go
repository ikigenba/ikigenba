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
	if got != (Cost{Amount: wantAmount, Known: true}) {
		t.Fatalf("priceUsage() = %+v, want known amount %d", got, wantAmount)
	}
}

func TestResolveCostUsesFirstAvailableRung(t *testing.T) {
	// R-2E2H-DO7H
	usage := Usage{InputTokens: 2}
	consumer := map[string]Pricing{
		"gpt-4.1-mini": {InputPerToken: 900},
	}
	wireAmount := int64(77)

	if got := resolveCost("gpt-4.1-mini", usage, &wireAmount, consumer); got != (Cost{Amount: 77, Known: true}) {
		t.Fatalf("wire precedence cost = %+v, want amount 77", got)
	}
	if got := resolveCost("gpt-4.1-mini", usage, nil, consumer); got != (Cost{Amount: 1_800, Known: true}) {
		t.Fatalf("consumer precedence cost = %+v, want amount 1800", got)
	}
	if got := resolveCost("gpt-4.1-mini", usage, nil, nil); got != (Cost{Amount: 800, Known: true}) {
		t.Fatalf("built-in fallback cost = %+v, want amount 800", got)
	}
}

func TestBuiltInChatPricingResolvesLiteralModel(t *testing.T) {
	// R-2HQ6-IZFK
	usage := Usage{InputTokens: 2, CachedTokens: 3, OutputTokens: 5, ReasoningTokens: 7}
	got := resolveCost("gpt-4.1-mini", usage, nil, nil)
	const wantAmount = int64(2*400 + 3*100 + 5*1_600 + 7*1_600)
	if got != (Cost{Amount: wantAmount, Known: true}) {
		t.Fatalf("built-in chat cost = %+v, want known amount %d", got, wantAmount)
	}
	if unknown := resolveCost("text-embedding-3-small", usage, nil, nil); unknown.Known {
		t.Fatalf("embedding model unexpectedly resolved from chat-only table: %+v", unknown)
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
	if present != (Cost{Amount: 7_000_000, Known: true}) {
		t.Fatalf("present predecoded wire cost = %+v, want 7000000 nano-USD", present)
	}
	absent := resolveCost("custom-chat", Usage{InputTokens: 3}, nil, consumer)
	if absent != (Cost{Amount: 69, Known: true}) {
		t.Fatalf("absent wire cost fallback = %+v, want consumer-priced amount 69", absent)
	}
}
