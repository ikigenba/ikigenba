package agentkit

import "testing"

func int64Pointer(value int64) *int64 {
	return &value
}

func TestMergeUsageLastPresentAbsoluteValueWins(t *testing.T) {
	// R-26R3-31RB
	got := mergeUsage(
		usageFragment{
			InputTokens:     int64Pointer(20),
			CachedTokens:    int64Pointer(5),
			OutputTokens:    int64Pointer(8),
			ReasoningTokens: int64Pointer(3),
		},
		usageFragment{
			InputTokens:  int64Pointer(0),
			OutputTokens: int64Pointer(13),
		},
	)
	want := Usage{InputTokens: 0, CachedTokens: 5, OutputTokens: 13, ReasoningTokens: 3}
	if got != want {
		t.Fatalf("mergeUsage() = %+v, want %+v", got, want)
	}
}

func TestMergeUsageRetainsDisjointInputAndOutputFragments(t *testing.T) {
	// R-296V-UL8P
	got := mergeUsage(
		usageFragment{InputTokens: int64Pointer(42), CachedTokens: int64Pointer(11)},
		usageFragment{OutputTokens: int64Pointer(17), ReasoningTokens: int64Pointer(6)},
	)
	want := Usage{InputTokens: 42, CachedTokens: 11, OutputTokens: 17, ReasoningTokens: 6}
	if got != want {
		t.Fatalf("mergeUsage() = %+v, want %+v", got, want)
	}
}

func TestMergeUsageDoesNotSumCumulativeSnapshots(t *testing.T) {
	// R-2AES-8CZE
	got := mergeUsage(
		usageFragment{
			InputTokens:     int64Pointer(30),
			CachedTokens:    int64Pointer(5),
			OutputTokens:    int64Pointer(4),
			ReasoningTokens: int64Pointer(2),
		},
		usageFragment{
			InputTokens:     int64Pointer(30),
			CachedTokens:    int64Pointer(5),
			OutputTokens:    int64Pointer(12),
			ReasoningTokens: int64Pointer(7),
		},
	)
	want := Usage{InputTokens: 30, CachedTokens: 5, OutputTokens: 12, ReasoningTokens: 7}
	if got != want {
		t.Fatalf("mergeUsage() = %+v, want final cumulative snapshot %+v", got, want)
	}
}
