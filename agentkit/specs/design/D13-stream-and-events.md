# D13-stream-and-events

`Send` returns a `*Stream`: the consumer's live view of one turn as it runs. A
`Stream` yields **`Event`** values in the order they occur across the turn's
round-trips, and after iteration ends it reports the turn's terminal error, if
any. Events are **message-granular** — one event per completed protocol message,
never a token delta. Streaming decode (SSE or any other framing) terminates inside
the wire codec (D5); no chunk, delta, or framing artifact escapes into the event
stream. This is the same granularity the event log records (D15): the log is the
durable transcript of exactly these events.

`Event` is a **sealed union** of exactly three variants, closed by an unexported
marker so a consumer switches it exhaustively. The variants wrap the corresponding
`Block` types (D2); they are named for what happened rather than reusing the block
type names, so the event union and the block union stay distinct types in one
package.

```go
package agentkit

// Event is one thing that happened during a turn, at message granularity. It is
// a sealed union — the unexported isEvent marker keeps the set closed to
// MessageDone, ToolCall, and ToolReturn so a consumer can switch it exhaustively.
type Event interface {
	isEvent()
}

// MessageDone reports that the model finished one assistant message. Message is
// the completed message (its Block slice from D2), carrying any text and
// reasoning blocks in order.
type MessageDone struct {
	Message Message
}

// ToolCall reports that the model requested a tool. Use is the ToolUse block
// (D2) with the vendor's verbatim call id; the orchestrator will dispatch it and
// emit the matching ToolReturn.
type ToolCall struct {
	Use ToolUse
}

// ToolReturn reports that the orchestrator ran a tool and is feeding the result
// back to the model. Result is the ToolResult block (D2); Result.IsError marks an
// in-band tool failure (D12), which is a normal event, not a stream error.
type ToolReturn struct {
	Result ToolResult
}
```

A `Stream` is iterated with a range-over-func sequence and then checked for a
terminal error. Iteration is driven by consumption: ranging the sequence advances
the turn, so a consumer sees each round-trip's events as that round-trip
completes, not buffered to the end. When the sequence stops, `Err` reports why —
`nil` on a clean turn, otherwise the terminal error (D4). A tool that returns an
error does **not** appear on `Err`: it is an in-band `ToolReturn` with `IsError`
set (D12), which the model may recover from. Only a turn-ending failure — a
transport error, a classified vendor error, an unrecoverable decode — stops the
turn and lands on `Err`.

```go
package agentkit

// Stream is the live view of one turn's events. Events yields the turn's Event
// values in occurrence order as round-trips complete; after Events stops, Err
// reports the turn's terminal error (nil on success). A Stream is single-use and
// belongs to the Send that returned it.
type Stream struct {
	// unexported
}

// Events returns the turn's events in order. Ranging it drives the turn; the
// range ends when the turn ends.
func (s *Stream) Events() iter.Seq[Event]

// Err reports the terminal error that ended the turn, or nil. It is meaningful
// only after Events has stopped. An in-band tool error (ToolReturn.IsError) is
// never reported here.
func (s *Stream) Err() error
```

On a terminal error the turn appends nothing to `History` (D12); the consumer has
still observed, through the events already delivered, every round-trip that
completed before the failure. A consumer that ignores the `Stream` and only reads
`History` after `Send` returns still gets a correct transcript, because `History`
is spliced once on success — the `Stream` is the *live* view, `History` the
*settled* one.

## REQUIREMENTS

- R-4YQU-G8K9: `Event` MUST be a sealed union of exactly `MessageDone`, `ToolCall`, and `ToolReturn` — an interface with an unexported marker — so a consumer switches it exhaustively.
- R-4ZYQ-U0AY: A `Stream` MUST yield events at message granularity, one per completed protocol message, and MUST NOT expose token deltas or any framing artifact.
- R-516N-7S1N: A `Stream` MUST yield events in the order they occur across the turn's round-trips, delivering each round-trip's events as that round-trip completes rather than only at turn end.
- R-52EJ-LJSC: A tool returning an error MUST surface as a `ToolReturn` with `IsError` set and MUST NOT be reported by `Stream.Err()`.
- R-53MF-ZBJ1: `Stream.Err()` MUST report the turn's terminal error (transport, classified vendor error, or unrecoverable decode) once iteration has stopped, and `nil` for a turn that completed cleanly.
- R-54UC-D39Q: The events a `Stream` yields MUST match, one for one and in order, the message-granular records written to the event log (D15) for the same turn.
- R-0B78-ZYU3: `agentkit` MUST export `type MessageDone struct { Message Message }`, and `MessageDone` MUST implement `Event`.
- R-0CF5-DQKS: `agentkit` MUST export `type ToolCall struct { Use ToolUse }`, and `ToolCall` MUST implement `Event`.
- R-0DN1-RIBH: `agentkit` MUST export `type ToolReturn struct { Result ToolResult }`, and `ToolReturn` MUST implement `Event`.
- R-0G2U-J1SV: `agentkit` MUST export `Stream` as an opaque struct type with no exported fields, exposing the methods `Events() iter.Seq[Event]` and `Err() error`.
