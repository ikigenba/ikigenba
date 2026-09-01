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

func TestResolvedAndUnresolvedCostKnownSemantics(t *testing.T) {
	// R-0THQ-QIYI
	usage := Usage{InputTokens: 2}
	wireAmount := int64(37)
	consumerPricing := map[string]Pricing{
		"consumer-model":      {InputPerToken: 23},
		"consumer-free-model": {},
	}

	resolved := map[string]Cost{
		"wire":     resolveCost("unpriced-model", usage, &wireAmount, nil),
		"consumer": resolveCost("consumer-model", usage, nil, consumerPricing),
		"built-in": resolveCost("gpt-4.1-mini", usage, nil, nil),
	}
	wantAmounts := map[string]int64{
		"wire":     37,
		"consumer": 46,
		"built-in": 800,
	}
	for path, cost := range resolved {
		if !cost.Known || cost.Amount != wantAmounts[path] {
			t.Errorf("%s resolved cost = %+v, want known amount %d", path, cost, wantAmounts[path])
		}
	}

	resolvedZero := resolveCost("consumer-free-model", usage, nil, consumerPricing)
	if resolvedZero != (Cost{Amount: 0, Known: true}) {
		t.Fatalf("resolved zero cost = %+v, want a genuine known zero", resolvedZero)
	}
	unresolved := resolveCost("absent-from-all-pricing", usage, nil, consumerPricing)
	if unresolved != (Cost{Amount: 0, Known: false}) {
		t.Fatalf("unresolved cost = %+v, want exactly zero amount with Known false", unresolved)
	}

	isFreeTurn := func(cost Cost) bool {
		return cost.Known && cost.Amount == 0
	}
	if !isFreeTurn(resolvedZero) {
		t.Fatal("known resolved zero was not interpreted as a real free turn")
	}
	if isFreeTurn(unresolved) {
		t.Fatal("unknown zero was incorrectly interpreted as a real free turn")
	}
}

func TestResolveCostUnknownModelCompletesUnresolved(t *testing.T) {
	// R-2FAD-RFY6
	got := resolveCost("unknown-chat-model", Usage{InputTokens: 100}, nil, nil)
	if got != (Cost{Amount: 0, Known: false}) {
		t.Fatalf("resolveCost() = %+v, want zero unknown cost", got)
	}
}

func TestAggregateCostsPropagatesUnknown(t *testing.T) {
	// R-2GIA-57OV
	got := aggregateCosts(
		Cost{Amount: 100, Known: true},
		Cost{Amount: 40, Known: false},
		Cost{Amount: 20, Known: true},
	)
	if got != (Cost{Amount: 160, Known: false}) {
		t.Fatalf("aggregateCosts() = %+v, want amount retained with unknown status", got)
	}
	if allKnown := aggregateCosts(Cost{Amount: 8, Known: true}, Cost{Amount: 13, Known: true}); allKnown != (Cost{Amount: 21, Known: true}) {
		t.Fatalf("aggregateCosts(all known) = %+v, want known amount 21", allKnown)
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
