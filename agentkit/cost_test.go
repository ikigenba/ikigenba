package agentkit

import (
	"testing"
)

// catalogResponsesOffering returns the first catalog offering on the
// openai-responses ID and its first rate tier, which must start at zero
// input so a small usage prices at it.
func catalogResponsesOffering(t *testing.T) (Offering, RateTier) {
	t.Helper()
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			if offering.ID != OfferingOpenAIResponses {
				continue
			}
			tier := offering.Pricing.Tiers[0]
			if tier.MinInputTokens != 0 || tier.InputUncached == 0 || tier.Output == 0 {
				t.Fatalf("offering %q first tier %+v cannot price a small usage", offering.WireModel, tier)
			}
			return offering, tier
		}
	}
	t.Fatalf("Catalog has no %q offering", OfferingOpenAIResponses)
	return Offering{}, RateTier{}
}

// catalogHasOffering reports whether any catalog offering has this ID and
// wire model.
func catalogHasOffering(id OfferingID, wireModel string) bool {
	for _, entry := range Catalog() {
		for _, offering := range entry.Offerings {
			if offering.ID == id && offering.WireModel == wireModel {
				return true
			}
		}
	}
	return false
}

func TestResolveCostMatchesOfferingIDAndWireModelExactly(t *testing.T) {
	// R-KLGQ-PSGW
	offering, tier := catalogResponsesOffering(t)
	usage := Usage{InputTokens: 2, OutputTokens: 3}
	want := Cost(2*tier.InputUncached + 3*tier.Output)
	if got := resolveCost(Identity{Endpoint: string(offering.ID), Model: offering.WireModel}, usage, nil); got != want {
		t.Fatalf("exact offering cost = %d, want %d", got, want)
	}
	for _, identity := range []Identity{
		{Endpoint: string(OfferingXAIResponses), Model: offering.WireModel},
		{Endpoint: string(offering.ID), Model: "openai/" + offering.WireModel},
		{Endpoint: string(OfferingOpenRouterChat), Model: offering.WireModel},
	} {
		if catalogHasOffering(OfferingID(identity.Endpoint), identity.Model) {
			t.Fatalf("identity %+v names a catalog offering; it cannot stand for a mismatched pair", identity)
		}
		if got := resolveCost(identity, usage, nil); got != 0 {
			t.Errorf("mismatched identity %+v cost = %d, want zero", identity, got)
		}
	}
}

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

func TestPredecodedWireCostPresenceControlsFallback(t *testing.T) {
	// R-2IY2-WR69
	const billedMilliUSD = int64(7)
	convertedNanoUSD := billedMilliUSD * 1_000_000
	offering, tier := catalogResponsesOffering(t)
	identity := Identity{Endpoint: string(offering.ID), Model: offering.WireModel}
	wantFallback := Cost(3 * tier.InputUncached)
	if wantFallback == Cost(convertedNanoUSD) {
		t.Fatalf("catalog-priced amount %d equals the wire amount; the fallback would be indistinguishable", wantFallback)
	}

	present := resolveCost(identity, Usage{InputTokens: 3}, &convertedNanoUSD)
	if present != Cost(7_000_000) {
		t.Fatalf("present predecoded wire cost = %d, want 7000000 nano-USD", present)
	}
	absent := resolveCost(identity, Usage{InputTokens: 3}, nil)
	if absent != wantFallback {
		t.Fatalf("absent wire cost fallback = %d, want catalog-priced amount %d", absent, wantFallback)
	}
}

func TestResolveCostUsesWireThenExactCatalogOffering(t *testing.T) {
	// R-NP7W-266R
	usage := Usage{InputTokens: 2, CachedTokens: 3, OutputTokens: 5, ReasoningTokens: 7}
	offering, tier := catalogResponsesOffering(t)
	identity := Identity{Endpoint: string(offering.ID), Model: offering.WireModel}
	wantCatalogCost := Cost(2*tier.InputUncached + 3*tier.CacheReadInput + (5+7)*tier.Output)
	wireAmount := int64(91)
	if wantCatalogCost == Cost(wireAmount) {
		t.Fatalf("catalog-priced amount %d equals the wire amount; the two rungs would be indistinguishable", wantCatalogCost)
	}
	if got := resolveCost(identity, usage, &wireAmount); got != Cost(91) {
		t.Fatalf("wire-priced cost = %d, want exact wire amount 91", got)
	}
	if got := resolveCost(identity, usage, nil); got != wantCatalogCost {
		t.Fatalf("catalog-priced cost = %d, want %d", got, wantCatalogCost)
	}
}

func TestResolveCostReturnsZeroForOffCatalogIdentity(t *testing.T) {
	// R-NRNO-TPO5
	identity := Identity{Endpoint: "custom-provider", Model: "released-today"}
	if got := resolveCost(identity, Usage{InputTokens: 10, OutputTokens: 20}, nil); got != 0 {
		t.Fatalf("off-catalog cost = %d, want zero", got)
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
