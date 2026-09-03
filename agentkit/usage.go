package agentkit

// Usage is the resolved token accounting for one turn. Every field is an
// absolute count for the whole turn, already merged across stream fragments.
// The buckets are non-overlapping by construction: the wire adapter subtracts
// any vendor subset (e.g. a cached count nested inside input, a reasoning count
// nested inside output) during decode. Consumers may sum the six fields to a
// grand total without fear of double counting. A wire that does not report a
// bucket leaves it zero.
type Usage struct {
	InputTokens        int64 // fresh (uncached) prompt tokens
	CachedTokens       int64 // prompt tokens served from cache
	CacheWrite5mTokens int64 // prompt tokens written to a 5-minute cache
	CacheWrite1hTokens int64 // prompt tokens written to a 1-hour cache
	OutputTokens       int64 // generated tokens excluding reasoning
	ReasoningTokens    int64 // reasoning/thinking tokens, when the wire separates them
}

// usageFragment is the per-chunk decode target: every field is optional so the
// merge can tell "reported as zero" from "not reported in this chunk". Wire
// adapters populate only the fields a given chunk carried.
type usageFragment struct {
	InputTokens        *int64
	CachedTokens       *int64
	CacheWrite5mTokens *int64
	CacheWrite1hTokens *int64
	OutputTokens       *int64
	ReasoningTokens    *int64
}

func mergeUsage(fragments ...usageFragment) Usage {
	var usage Usage
	for _, fragment := range fragments {
		if fragment.InputTokens != nil {
			usage.InputTokens = *fragment.InputTokens
		}
		if fragment.CachedTokens != nil {
			usage.CachedTokens = *fragment.CachedTokens
		}
		if fragment.CacheWrite5mTokens != nil {
			usage.CacheWrite5mTokens = *fragment.CacheWrite5mTokens
		}
		if fragment.CacheWrite1hTokens != nil {
			usage.CacheWrite1hTokens = *fragment.CacheWrite1hTokens
		}
		if fragment.OutputTokens != nil {
			usage.OutputTokens = *fragment.OutputTokens
		}
		if fragment.ReasoningTokens != nil {
			usage.ReasoningTokens = *fragment.ReasoningTokens
		}
	}
	return usage
}

func addUsage(left, right Usage) Usage {
	return Usage{
		InputTokens:        left.InputTokens + right.InputTokens,
		CachedTokens:       left.CachedTokens + right.CachedTokens,
		CacheWrite5mTokens: left.CacheWrite5mTokens + right.CacheWrite5mTokens,
		CacheWrite1hTokens: left.CacheWrite1hTokens + right.CacheWrite1hTokens,
		OutputTokens:       left.OutputTokens + right.OutputTokens,
		ReasoningTokens:    left.ReasoningTokens + right.ReasoningTokens,
	}
}
