package agentkit

// Usage is the resolved token accounting for one turn. Every field is an
// absolute count for the whole turn, already merged across stream fragments.
// The buckets are non-overlapping by construction: the wire adapter subtracts
// any vendor subset (e.g. a cached count nested inside input, a reasoning count
// nested inside output) during decode, so InputTokens excludes CachedTokens and
// OutputTokens excludes ReasoningTokens. Consumers may sum the four fields to a
// grand total without fear of double counting.
type Usage struct {
	InputTokens     int64 // fresh (uncached) prompt tokens
	CachedTokens    int64 // prompt tokens served from cache
	OutputTokens    int64 // generated tokens excluding reasoning
	ReasoningTokens int64 // reasoning/thinking tokens, when the wire separates them
}

// usageFragment is the per-chunk decode target: every field is optional so the
// merge can tell "reported as zero" from "not reported in this chunk". Wire
// adapters populate only the fields a given chunk carried.
type usageFragment struct {
	InputTokens     *int64
	CachedTokens    *int64
	OutputTokens    *int64
	ReasoningTokens *int64
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
		if fragment.OutputTokens != nil {
			usage.OutputTokens = *fragment.OutputTokens
		}
		if fragment.ReasoningTokens != nil {
			usage.ReasoningTokens = *fragment.ReasoningTokens
		}
	}
	return usage
}
