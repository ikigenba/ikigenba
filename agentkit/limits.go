package agentkit

import "errors"

// Limits bounds a Conversation for its whole life. A zero field is no bound.
type Limits struct {
	MaxToolCalls     int   // tool calls the conversation may dispatch, across all turns
	MaxContextTokens int64 // largest context one round-trip may consume
}

// LimitKind names which bound of Limits was crossed.
type LimitKind string

const (
	// LimitToolCalls identifies the conversation-wide tool call bound.
	LimitToolCalls LimitKind = "tool_calls"
	// LimitContextTokens identifies the per-round-trip context token bound.
	LimitContextTokens LimitKind = "context_tokens"
)

// LimitInfo is the payload of a limit log record: the bound, its value, and
// the value that crossed it.
type LimitInfo struct {
	Kind   LimitKind `json:"kind"`
	Max    int64     `json:"max"`
	Actual int64     `json:"actual"`
}

// ErrLimitExceeded is the terminal error of a turn refused by Limits.
var ErrLimitExceeded = errors.New("agentkit: limit exceeded")
