package agentkit

import (
	"reflect"
	"testing"
)

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

func TestUsageBucketsAreDisjointAtWireBoundary(t *testing.T) {
	// R-2BMO-M4Q3
	const (
		wireInputTotal      = int64(120)
		wireCachedSubset    = int64(40)
		wireOutputTotal     = int64(75)
		wireReasoningSubset = int64(25)
	)

	// A future wire adapter performs its vendor-specific subset normalization
	// before producing these shared fragments.
	freshInput := wireInputTotal - wireCachedSubset
	plainOutput := wireOutputTotal - wireReasoningSubset
	got := mergeUsage(usageFragment{
		InputTokens:     &freshInput,
		CachedTokens:    int64Pointer(wireCachedSubset),
		OutputTokens:    &plainOutput,
		ReasoningTokens: int64Pointer(wireReasoningSubset),
	})
	want := Usage{InputTokens: 80, CachedTokens: 40, OutputTokens: 50, ReasoningTokens: 25}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized Usage = %+v, want %+v", got, want)
	}
	if total := got.InputTokens + got.CachedTokens + got.OutputTokens + got.ReasoningTokens; total != 195 {
		t.Fatalf("disjoint bucket total = %d, want 195", total)
	}
}
