package wire

import "encoding/json"

// UsageTotals is the cumulative token summary for an iteration.
// R-Y5QZ-UNB2: the standard usage shape ralph-loops reads from result events.
type UsageTotals struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// ModelUsageEntry is one entry in the per-model breakdown map.
// R-Y5QZ-UNB2: keyed by full model ID in the result event's modelUsage field.
type ModelUsageEntry struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens,omitempty"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int     `json:"contextWindow,omitempty"`
	MaxOutputTokens          int     `json:"maxOutputTokens,omitempty"`
}

// IterationStats carries the accounting summary for a completed iteration.
// R-Y5QZ-UNB2: populated by agent.Run and forwarded to NewResultEventFull.
type IterationStats struct {
	NumTurns   int
	DurationMs int64
	Usage      UsageTotals
	ModelUsage map[string]ModelUsageEntry
}

// ResultEvent is the mandatory iteration terminator emitted on stdout.
//
// R-Y5QZ-UNB2: shape is
// {"type":"result","structured_output":<json-value>,"is_error":<bool>,
//  "num_turns":<int>,"duration_ms":<int>,"total_cost_usd":<number>,
//  "usage":{...},"modelUsage":{...}}.
// The accounting fields are populated by NewResultEventFull; they are
// omitted (not zero-valued) when NewResultEvent is used without stats.
// Replaces and retires R-13ZB-EZZK.
type ResultEvent struct {
	Type             string                     `json:"type"`
	StructuredOutput json.RawMessage            `json:"structured_output"`
	IsError          bool                       `json:"is_error"`
	NumTurns         int                        `json:"num_turns,omitempty"`
	DurationMs       int64                      `json:"duration_ms,omitempty"`
	TotalCostUSD     float64                    `json:"total_cost_usd,omitempty"`
	Usage            *UsageTotals               `json:"usage,omitempty"`
	ModelUsage       map[string]ModelUsageEntry `json:"modelUsage,omitempty"`
}

// NewResultEvent fixes Type to "result". The accounting fields
// (num_turns, duration_ms, total_cost_usd, usage, modelUsage) are not
// populated; use NewResultEventFull when iteration stats are available.
func NewResultEvent(structuredOutput any, isError bool) (ResultEvent, error) {
	raw, err := json.Marshal(structuredOutput)
	if err != nil {
		return ResultEvent{}, err
	}
	return ResultEvent{
		Type:             "result",
		StructuredOutput: raw,
		IsError:          isError,
	}, nil
}

// NewResultEventFull creates a result event with full iteration accounting.
// R-Y5QZ-UNB2: populates num_turns, duration_ms, total_cost_usd, usage,
// and modelUsage from stats.
func NewResultEventFull(structuredOutput any, isError bool, stats IterationStats) (ResultEvent, error) {
	ev, err := NewResultEvent(structuredOutput, isError)
	if err != nil {
		return ResultEvent{}, err
	}
	ev.NumTurns = stats.NumTurns
	ev.DurationMs = stats.DurationMs
	var totalCost float64
	for _, mu := range stats.ModelUsage {
		totalCost += mu.CostUSD
	}
	ev.TotalCostUSD = totalCost
	if stats.Usage.InputTokens > 0 || stats.Usage.OutputTokens > 0 {
		u := stats.Usage
		ev.Usage = &u
	}
	if len(stats.ModelUsage) > 0 {
		ev.ModelUsage = stats.ModelUsage
	}
	return ev, nil
}
