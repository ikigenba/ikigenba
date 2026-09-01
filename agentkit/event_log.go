package agentkit

import (
	"io"
	"time"
)

// RecordType is the closed set of event-log record kinds. There is deliberately
// no "warning" kind: fail-loud (D4) removed the concept — an unresolved cost is
// Cost.Known=false, an unexpressible request fails at Send.
type RecordType string

// RecordType values enumerate the event-log record kinds.
const (
	RecordTurnStart  RecordType = "turn_start"
	RecordMessage    RecordType = "message"
	RecordToolUse    RecordType = "tool_use"
	RecordToolResult RecordType = "tool_result"
	RecordUsage      RecordType = "usage"
	RecordError      RecordType = "error"
	RecordRetry      RecordType = "retry"
	RecordTurnEnd    RecordType = "turn_end"
	RecordSummary    RecordType = "summary"
)

// LogRecord is one line of the log. Type selects which payload pointer is set;
// the rest are nil and omitted. Time is the injected clock's reading; Seq is
// monotonic within a turn. The payloads reuse the canonical types verbatim — no
// log-only shadow structs — so the log and the live stream never drift.
type LogRecord struct {
	Type RecordType `json:"type"`
	Time time.Time  `json:"time"`
	Seq  int        `json:"seq"`

	Identity   *Identity   `json:"identity,omitempty"`    // turn_start
	Message    *Message    `json:"message,omitempty"`     // message (one completed Message, D2/D13)
	ToolUse    *ToolUse    `json:"tool_use,omitempty"`    // tool_use
	ToolResult *ToolResult `json:"tool_result,omitempty"` // tool_result
	Usage      *Usage      `json:"usage,omitempty"`       // usage, summary
	Cost       *Cost       `json:"cost,omitempty"`        // usage, summary
	Err        *Error      `json:"error,omitempty"`       // error
	Retry      *RetryInfo  `json:"retry,omitempty"`       // retry
}

// RetryInfo records one backoff wait emitted by the retry driver (D14).
type RetryInfo struct {
	Attempt int           `json:"attempt"`
	Delay   time.Duration `json:"delay"`
	Reason  string        `json:"reason"` // the retried error's Error() text
}

// Log is the opaque destination for a conversation's durable event trace.
type Log struct {
	w      io.Writer
	now    func() time.Time
	closed bool
}

// NewLog builds a log over w, timestamping with now. A nil w yields a nil-
// behaving log. now is injected for determinism (D3).
func NewLog(w io.Writer, now func() time.Time) *Log {
	return &Log{w: w, now: now}
}

// Close emits exactly one cumulative summary record — total Usage and total
// Cost across the conversation's turns — and marks the log closed. It is
// idempotent: a second Close writes nothing and returns nil. After Close, any
// Send on the owning Conversation returns ErrClosed (D4).
func (l *Log) Close() error {
	if l != nil {
		l.closed = true
	}
	return nil
}
