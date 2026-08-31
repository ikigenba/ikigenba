# D15-event-log

A consumer that wants a durable trace of a turn supplies an `io.Writer` at
construction; agentkit writes **one JSON object per line, one line per protocol
event** of the turn — the same shape a `codex exec --json` stream has. The log is
**message-granular**: it mirrors the D13 event stream exactly and carries no token
deltas. It is a forensic sibling to `Stream`, not a second control path — the
consumer already drives the turn through `Stream`; the log is what is left on disk
afterward.

**Time is injected, monotonic sequence is per-turn.** Each record's `Time` comes
from an injected clock (the idgen-precedent pattern, D3), so a replayed turn logs
identically; `Seq` is a monotonic counter within a turn, reset at each
`turn_start`, so a reader can order records without trusting clock resolution.

```go
// RecordType is the closed set of event-log record kinds. There is deliberately
// no "warning" kind: fail-loud (D4) removed the concept — an unresolved cost is
// Cost.Known=false, an unexpressible request fails at Send.
type RecordType string

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
```

**`turn_start` carries the full `Identity` split (D1).** Because `Identity` keeps
endpoint, auth mode, and model as separate fields rather than one fused id, a
consumer post-processing a log file can filter "every OpenAI turn" and "every
subscription-paid turn" independently — the log never collapses that distinction
into a single provider string.

**The log is best-effort and never load-bearing.** A `nil` log is valid and
writes nothing, so the orchestrator carries no per-call-site nil check. A write
failure is retained on the log for inspection but **never aborts the turn and
never changes `Stream.Err()`** — a full disk must not fail a model call that
otherwise succeeded. The live `Stream` is the source of truth; the log is a
recording of it.

```go
// NewLog builds a log over w, timestamping with now. A nil w yields a nil-
// behaving log. now is injected for determinism (D3).
func NewLog(w io.Writer, now func() time.Time) *Log

// Close emits exactly one cumulative summary record — total Usage and total
// Cost across the conversation's turns — and marks the log closed. It is
// idempotent: a second Close writes nothing and returns nil. After Close, any
// Send on the owning Conversation returns ErrClosed (D4).
func (l *Log) Close() error
```

**Cost is present and always priced on the accounting records.** Every `usage`
and `summary` record carries a `Cost`, resolved through the three-deep path (D3);
its `Known` may be false, but the field is always there, so a log reader never has
to reprice. The cumulative `summary.Cost.Known` is false if any turn's cost was
unknown, mirroring the aggregation rule (D3) — a log total is honest about
under-counting for the same reason the in-memory total is.

## REQUIREMENTS

- R-5ELJ-F97A: When a log writer is supplied, agentkit MUST write exactly one JSON object per line, one line per protocol event of a turn, message-granular and carrying no token deltas, matching the D13 event stream.
- R-5FTF-T0XZ: Each `LogRecord` MUST timestamp from the injected clock and MUST carry a `Seq` that is monotonic within a turn and reset at each `turn_start`.
- R-5H1C-6SOO: The record kinds MUST be exactly turn_start, message, tool_use, tool_result, usage, error, retry, turn_end, summary — with no `warning` kind.
- R-5JH4-YC62: A `turn_start` record MUST carry the `Identity` with endpoint, auth mode, and model as separate fields so a consumer can filter on each independently.
- R-5KP1-C3WR: A `nil` log MUST write nothing and MUST require no per-call-site nil check; log payloads MUST reuse the canonical `Identity`/`Usage`/`Cost`/`Error`/`Block` types rather than log-only shadow structs.
- R-5LWX-PVNG: A log write failure MUST NOT abort the turn and MUST NOT change `Stream.Err()`; the failure MAY be retained on the log for inspection.
- R-5N4U-3NE5: `Close` MUST emit exactly one cumulative `summary` record and MUST be idempotent; a `Send` after `Close` MUST return `ErrClosed`.
- R-5OCQ-HF4U: Every `usage` and `summary` record MUST carry a `Cost` resolved through the D3 path, and the `summary` cost's `Known` MUST be false whenever any contributing turn's cost was unknown.
