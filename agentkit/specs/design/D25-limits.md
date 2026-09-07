# D25-limits

A turn runs until the model stops calling tools (D12). Nothing in agentkit
bounds how many tools it may call or how large its context may grow, so a
consumer running many agents — each a `Conversation` with a budget — has only
one lever: cancel the context and lose the reason. `Limits` gives the
conversation two bounds of its own, both scoped to its **current history**, both
enforced by the orchestrator, and both recorded in the log (D15) as the reason
the work stopped.

Current history, not lifetime: a `Restore` (D26) rewinds the transcript, and
both counters rewind with it, because both describe the conversation as it now
stands rather than the work it has ever done. Cumulative lifetime accounting
lives on the `Log`, whose `summary` record (D15) survives every `Restore`.
A conversation that restores repeatedly therefore has no lifetime bound on tool
calls at all; a `Limits` field scoped to the conversation's whole life would be
a separate, later addition.

```go
package agentkit

// Limits bounds a Conversation's current history. A zero field is no bound.
type Limits struct {
	MaxToolCalls     int   // tool calls dispatchable since the last Restore
	MaxContextTokens int64 // largest context one round-trip may consume
}

// LimitKind names which bound of Limits was crossed.
type LimitKind string

const (
	LimitToolCalls     LimitKind = "tool_calls"
	LimitContextTokens LimitKind = "context_tokens"
)

// LimitInfo is the payload of a limit log record (D15): the bound, its value,
// and the value that crossed it.
type LimitInfo struct {
	Kind   LimitKind `json:"kind"`
	Max    int64     `json:"max"`
	Actual int64     `json:"actual"`
}

// ErrLimitExceeded is the terminal error of a turn refused by Limits.
var ErrLimitExceeded = errors.New("agentkit: limit exceeded")
```

`Limits` is one more field of `Config` (D18), fixed at construction like
everything else there. Construction does not inspect it; a negative bound is
`ErrInvalidConfig` at `Send`, like every other configuration fault (D18).

**Two checkpoints, one rule.** The orchestrator consults `Limits` at two points:
at the start of a `Send`, before the first provider call, and after each
round-trip, before dispatching the tools that round-trip requested. At either
point the conversation is over budget when

- the **context** of the most recently completed round-trip — the sum of the six
  `Usage` buckets (D3) for that call, across any turn — exceeds
  `MaxContextTokens`, or
- (at the dispatch checkpoint only) dispatching this round-trip's tool calls
  would bring the number of tool calls dispatched since the most recent
  `Restore` (D26) past `MaxToolCalls`.

Over budget means the work is refused: no provider call is made, no tool is
dispatched, a `limit` record is written to the log naming the bound and the
offending value, and the turn ends with `ErrLimitExceeded` on `Stream.Err()`.
As for any terminal error, `History` is left unchanged (D12); the log already
holds the assistant message that asked for the refused tools, so nothing is
lost.

The checkpoints are where they are for two reasons. A round-trip's context is
only known once the round-trip has completed, so the check that reads it can
only run afterwards — and the model's final answer on a round-trip that
requested no tools is a completed turn that should commit, which is why an
over-limit context does not fail the turn it was measured on but the next
piece of work. The tool-call check runs before dispatch rather than after, so a
budget of N means at most N tool calls ever run: a round-trip requesting more
than the remainder dispatches none of them rather than a partial set the model
would have to reason about.

A limit refusal is not a provider failure: `ErrLimitExceeded` is a sentinel, not
an `*Error`, so `Retryable` reports false and no `error` record is written —
the `limit` record is the whole account.

## REQUIREMENTS

- R-TILY-AHD5: `agentkit` MUST export `type Limits struct { MaxToolCalls int; MaxContextTokens int64 }` with exactly those two fields.
- R-TJTU-O93U: `agentkit` MUST export `type LimitKind string` whose complete set of exported constants is exactly `LimitToolCalls = "tool_calls"` and `LimitContextTokens = "context_tokens"`.
- R-TL1R-20UJ: `agentkit` MUST export `type LimitInfo struct { Kind LimitKind; Max int64; Actual int64 }` with exactly those three fields and the JSON tags `kind`, `max`, and `actual`.
- R-TNHJ-TKBX: `agentkit` MUST export the sentinel error `ErrLimitExceeded`, created with `errors.New`, and a limit refusal's terminal error MUST satisfy `errors.Is(err, ErrLimitExceeded)`, MUST NOT be an `*Error`, and `Retryable` MUST return false for it.
- R-TOPG-7C2M: A `Conversation` whose `Config.Limits` is the zero value MUST never write a `limit` record and MUST never end a turn with `ErrLimitExceeded`.
- R-TPXC-L3TB: A `Send` on a `Conversation` whose `Config.Limits` has a negative `MaxToolCalls` or a negative `MaxContextTokens` MUST fail with `ErrInvalidConfig`, make no provider call, and leave `History` unchanged.
- R-8DNP-GLHN: When `MaxToolCalls` is positive and a round-trip requests tool calls whose count, added to the number of tool calls the conversation has dispatched since its most recent successful `Restore` (D26) — or since the conversation began, if it has had none — exceeds `MaxToolCalls`, the orchestrator MUST dispatch none of them, write one `limit` record with `Kind` `LimitToolCalls`, `Max` equal to `MaxToolCalls`, and `Actual` equal to that sum, and end the turn with `ErrLimitExceeded` on `Stream.Err()` and `History` unchanged.
- R-8EVL-UD8C: When `MaxContextTokens` is positive and the sum of the six `Usage` fields of the most recently completed provider round-trip, in any turn since the conversation's most recent successful `Restore` (D26), exceeds `MaxContextTokens`, the orchestrator MUST, at the next start of a `Send` or the next tool dispatch, whichever comes first, make no provider call and dispatch no tool, write one `limit` record with `Kind` `LimitContextTokens`, `Max` equal to `MaxContextTokens`, and `Actual` equal to that sum, and end the turn with `ErrLimitExceeded` on `Stream.Err()` and `History` unchanged.
- R-TTL1-QF1E: A round-trip that requests no tool calls MUST complete its turn normally regardless of its context total; an over-limit context MUST only refuse subsequent work.
- R-TUSY-46S3: A limit refusal MUST write exactly one `limit` record and no `error` record, and the `limit` record MUST precede the turn's `turn_end` record.
