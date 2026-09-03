package agentkit

import "testing"

func int64Pointer(value int64) *int64 {
	return &value
}

func TestMergeUsageLastPresentAbsoluteValueWins(t *testing.T) {
	// R-26R3-31RB
	got := mergeUsage(
		usageFragment{
			InputTokens:        int64Pointer(20),
			CachedTokens:       int64Pointer(5),
			CacheWrite5mTokens: int64Pointer(4),
			CacheWrite1hTokens: int64Pointer(2),
			OutputTokens:       int64Pointer(8),
			ReasoningTokens:    int64Pointer(3),
		},
		usageFragment{
			InputTokens:        int64Pointer(0),
			CacheWrite5mTokens: int64Pointer(7),
			CacheWrite1hTokens: int64Pointer(0),
			OutputTokens:       int64Pointer(13),
		},
	)
	want := Usage{InputTokens: 0, CachedTokens: 5, CacheWrite5mTokens: 7, CacheWrite1hTokens: 0, OutputTokens: 13, ReasoningTokens: 3}
	if got != want {
		t.Fatalf("mergeUsage() = %+v, want %+v", got, want)
	}
}

func TestMergeUsageRetainsDisjointInputAndOutputFragments(t *testing.T) {
	// R-296V-UL8P
	got := mergeUsage(
		usageFragment{
			InputTokens:        int64Pointer(42),
			CachedTokens:       int64Pointer(11),
			CacheWrite5mTokens: int64Pointer(8),
			CacheWrite1hTokens: int64Pointer(3),
		},
		usageFragment{OutputTokens: int64Pointer(17), ReasoningTokens: int64Pointer(6)},
	)
	want := Usage{InputTokens: 42, CachedTokens: 11, CacheWrite5mTokens: 8, CacheWrite1hTokens: 3, OutputTokens: 17, ReasoningTokens: 6}
	if got != want {
		t.Fatalf("mergeUsage() = %+v, want %+v", got, want)
	}
}

func TestMergeUsageDoesNotSumCumulativeSnapshots(t *testing.T) {
	// R-2AES-8CZE
	got := mergeUsage(
		usageFragment{
			InputTokens:        int64Pointer(30),
			CachedTokens:       int64Pointer(5),
			CacheWrite5mTokens: int64Pointer(6),
			CacheWrite1hTokens: int64Pointer(2),
			OutputTokens:       int64Pointer(4),
			ReasoningTokens:    int64Pointer(2),
		},
		usageFragment{
			InputTokens:        int64Pointer(30),
			CachedTokens:       int64Pointer(5),
			CacheWrite5mTokens: int64Pointer(9),
			CacheWrite1hTokens: int64Pointer(4),
			OutputTokens:       int64Pointer(12),
			ReasoningTokens:    int64Pointer(7),
		},
	)
	want := Usage{InputTokens: 30, CachedTokens: 5, CacheWrite5mTokens: 9, CacheWrite1hTokens: 4, OutputTokens: 12, ReasoningTokens: 7}
	if got != want {
		t.Fatalf("mergeUsage() = %+v, want final cumulative snapshot %+v", got, want)
	}
}

func TestUsageBucketsAreDisjointAtWireBoundary(t *testing.T) {
	// R-NFGP-0097
	vendorInputTotal := int64(100)
	vendorCachedReads := int64(25)
	vendorCacheWrite5m := int64(15)
	vendorCacheWrite1h := int64(10)
	vendorOutputTotal := int64(40)
	vendorReasoning := int64(12)
	freshInput := vendorInputTotal - vendorCachedReads - vendorCacheWrite5m - vendorCacheWrite1h
	generatedOutput := vendorOutputTotal - vendorReasoning

	got := mergeUsage(usageFragment{
		InputTokens:        &freshInput,
		CachedTokens:       &vendorCachedReads,
		CacheWrite5mTokens: &vendorCacheWrite5m,
		CacheWrite1hTokens: &vendorCacheWrite1h,
		OutputTokens:       &generatedOutput,
		ReasoningTokens:    &vendorReasoning,
	})
	want := Usage{
		InputTokens:        50,
		CachedTokens:       25,
		CacheWrite5mTokens: 15,
		CacheWrite1hTokens: 10,
		OutputTokens:       28,
		ReasoningTokens:    12,
	}
	if got != want {
		t.Fatalf("wire-boundary usage = %+v, want disjoint buckets %+v", got, want)
	}
	grandTotal := got.InputTokens + got.CachedTokens + got.CacheWrite5mTokens + got.CacheWrite1hTokens + got.OutputTokens + got.ReasoningTokens
	if grandTotal != vendorInputTotal+vendorOutputTotal {
		t.Fatalf("six-bucket grand total = %d, want vendor grand total %d", grandTotal, vendorInputTotal+vendorOutputTotal)
	}

	accumulated := addUsage(got, Usage{CacheWrite5mTokens: 3, CacheWrite1hTokens: 7})
	if accumulated.CacheWrite5mTokens != 18 || accumulated.CacheWrite1hTokens != 17 {
		t.Fatalf("addUsage() cache writes = %d/%d, want 18/17", accumulated.CacheWrite5mTokens, accumulated.CacheWrite1hTokens)
	}
}
