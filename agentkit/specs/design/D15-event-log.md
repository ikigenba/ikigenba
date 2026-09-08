# D15-event-log

A consumer that wants a durable record of a conversation supplies an
`io.Writer` at construction; agentkit writes **one JSON object per line** for
everything that happens to that conversation. The log is **the transcript**:
every message that enters the conversation — the consumer's system and user
messages as much as the model's replies and the tool results — is a record,
in the order it entered, together with the protocol events around it. `History`
(D2) is a projection of the log: the committed messages of the successful turns.
An application that keeps only the log has everything it needs to show what an
agent was asked, what it said, what it ran, what it cost, and why it stopped.

The log is **message-granular**: it mirrors the D13 event stream and carries no
token deltas. Every event the stream yields has a record in the log; the log
also carries records the stream never yields — the consumer's own input, the
per-round-trip accounting, the turn brackets, and the reason a turn was refused.
It is a forensic sibling to `Stream`, not a second control path — the consumer
drives the turn through `Stream`; the log is what is left on disk afterward.

**A log has an identity.** `NewLog` takes an `id` the consumer chooses — in
practice a UUID or an agent address — and writes it on every record. A
multi-agent application that merges many conversations' logs into one store can
then filter a single agent's lifecycle, or a subtree of agents by id prefix,
with no wrapper of its own around the writer.

**The log opens with the conversation, and it says when the conversation
closed.** The first record `New` (D18) writes is a `conversation` record: the
identity, the settings, the tools and deferred groups advertised, the output
contract, the limits, and the log format version — everything fixed for the
conversation's life, written once at the front instead of repeated on every
turn. Codex's `session_meta` is the precedent. The last thing the conversation
writes is a `closed` record from `Close` (D26); only the log's own `summary`
may follow it. Together they make the log sufficient to reconstruct the
conversation's state: a reader who replays the records is holding the same
`History`, the same live savepoint, the same limit counters, and the same
open-or-closed state the conversation had when it wrote them. The envelope
already carries the log id, the time, and the sequence, so the `conversation`
payload does not repeat them.

**Time is injected, sequence is per log.** Each record's `Time` comes from an
injected clock (the idgen-precedent pattern, D3), so a replayed conversation
logs identically; `Seq` is a monotonic counter over the whole log, starting at
zero and never reset, so a reader orders records — including the system
messages that sit between turns — without trusting clock resolution.

```go
// RecordType is the closed set of event-log record kinds. There is deliberately
// no "warning" kind: fail-loud (D4) removed the concept — an unresolved cost is
// zero, an unexpressible request fails at Send.
type RecordType string

const (
	RecordConversation RecordType = "conversation" // written once by New: what is fixed for the conversation's life
	RecordTurnStart  RecordType = "turn_start"
	RecordMessage    RecordType = "message"     // every Message that enters the conversation
	RecordToolUse    RecordType = "tool_use"
	RecordToolResult RecordType = "tool_result"
	RecordOutput     RecordType = "output"      // validated structured result (D20)
	RecordUsage      RecordType = "usage"       // one provider round-trip's accounting
	RecordLimit      RecordType = "limit"       // a turn refused by a Limits bound (D25)
	RecordError      RecordType = "error"
	RecordRetry      RecordType = "retry"
	RecordTurnEnd    RecordType = "turn_end"    // carries the turn's total Usage and Cost
	RecordSummary    RecordType = "summary"     // carries the conversation's total Usage and Cost
	RecordClosed     RecordType = "closed"      // written once by Conversation.Close (D26)
)

// LogFormatVersion is the version of the record vocabulary this package
// writes; the conversation record carries it so a reader knows which codec
// produced the file.
const LogFormatVersion = 1

// LogRecord is one line of the log. Type selects which payload pointer is set;
// the rest are nil and omitted. ID is the log's identity, the same on every
// record. Time is the injected clock's reading; Seq is monotonic over the log.
// The payloads reuse the canonical types verbatim — no log-only shadow structs
// — so the log and the live stream never drift.
type LogRecord struct {
	Type RecordType `json:"type"`
	ID   string     `json:"id,omitempty"`
	Time time.Time  `json:"time"`
	Seq  int        `json:"seq"`

	Conversation *ConversationInfo `json:"conversation,omitempty"` // conversation
	Message    *Message        `json:"message,omitempty"`     // message (one Message, D2)
	ToolUse    *ToolUse        `json:"tool_use,omitempty"`    // tool_use
	ToolResult *ToolResult     `json:"tool_result,omitempty"` // tool_result
	Output     json.RawMessage `json:"output,omitempty"`      // output (OutputDone.Value, D20)
	Usage      *Usage          `json:"usage,omitempty"`       // usage, turn_end, summary
	Cost       *Cost           `json:"cost,omitempty"`        // usage, turn_end, summary
	Limit      *LimitInfo      `json:"limit,omitempty"`       // limit (D25)
	Err        *Error          `json:"error,omitempty"`       // error
	Retry      *RetryInfo      `json:"retry,omitempty"`       // retry
}

// RetryInfo records one backoff wait emitted by the retry driver (D14).
type RetryInfo struct {
	Attempt int           `json:"attempt"`
	Delay   time.Duration `json:"delay"`
	Reason  string        `json:"reason"` // the retried error's Error() text
}

// ConversationInfo is the payload of the conversation record: the construction
// facts that never change afterward. Identity, Settings, Output and Limits are
// the canonical types verbatim; Tools and Deferred are the advertised surface
// rendered as data, since a Tool is code and cannot be logged.
type ConversationInfo struct {
	Format   int             `json:"format"`             // LogFormatVersion
	Identity Identity        `json:"identity"`
	Settings Settings        `json:"settings"`
	Tools    []ToolInfo      `json:"tools,omitempty"`    // Config.Tools, in order
	Deferred []DeferredInfo  `json:"deferred,omitempty"` // Config.Deferred, in order
	Output   *OutputContract `json:"output,omitempty"`   // Config.Output
	Limits   Limits          `json:"limits"`
}

// ToolInfo is one advertised tool as data.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// DeferredInfo is one deferred group (D16) as data.
type DeferredInfo struct {
	Name  string     `json:"name"`
	Blurb string     `json:"blurb"`
	Tools []ToolInfo `json:"tools"`
}
```

`turn_start` carries nothing: the identity it used to repeat on every turn
lives in the `conversation` record now, and a reader filtering by endpoint or
model reads it once at the front.

**Every message is a record.** A `message` record is written for each `Message`
as it enters the conversation, whoever authored it:

- `AddSystem` (D24) writes the `RoleSystem` message it appends. It sits between
  turns, outside any `turn_start`/`turn_end` pair.
- `Send` writes the `RoleUser` message built from the caller's blocks
  immediately after `turn_start`, before any provider call — so a turn that
  fails, or is refused by a limit, still shows what was asked.
- Each completed `RoleAssistant` message is written as the stream yields it,
  followed by a `tool_use` record per `ToolUse` block it carries.
- Each dispatched tool writes a `tool_result` record as it returns — in
  completion order, since calls may run concurrently (D28) — and once the
  round's tools have all returned the `RoleTool` message that carries those
  results back to the model is written as one `message` record, its blocks in
  the order the model requested the calls.
- The corrective `RoleUser` message of a structured-output retry (D20) is
  written as the stream yields it.

The consequence is the projection rule: for a turn that commits, the `message`
records between its `turn_start` and `turn_end` are exactly the messages the
turn spliced onto `History`, in order. For a turn that ends in a terminal error
or a limit, they are exactly the messages the turn produced up to the failure —
messages `History` never received, and which only the log preserves. The
`tool_use` and `tool_result` records duplicate blocks that also appear inside
messages; they are kept because they are the log's mirror of the live `ToolCall`
and `ToolReturn` events, and because a search over "what did this agent run"
should not have to open messages to find out.

**Accounting is per round-trip, totalled per turn and per log.** A `usage`
record follows every completed provider round-trip with that round-trip's
`Usage` and `Cost` — the input side of that record is the context the model held
on that call, which is what a context limit (D25) reads. `turn_end` carries the
turn's total, the sum of its `usage` records, so a reader who wants the old
per-turn figure has it without adding. `summary`, written once on `Close`,
carries the conversation's total. Every `Cost` is resolved through the D3 path
(wire figure, catalog offering, else zero), so a log reader never reprices.

**Refusals are records.** When a `Limits` bound (D25) stops a turn the log gets
a `limit` record naming the bound, its value, and the value that crossed it. That
is the searchable answer to "why did this agent stop", distinct from an `error`
record, which is a provider or transport failure.

**The log is best-effort and never load-bearing.** A `nil` log is valid and
writes nothing, so the orchestrator carries no per-call-site nil check. A write
failure is retained on the log for inspection but **never aborts the turn and
never changes `Stream.Err()`** — a full disk must not fail a model call that
otherwise succeeded. The live `Stream` is the source of truth for the turn in
flight; the log is the record of it.

```go
// NewLog builds a log over w, timestamping with now and stamping id on every
// record. A nil w yields a nil-behaving log. now is injected for determinism
// (D3). id is the consumer's identity for this conversation, written verbatim;
// an empty id omits the field.
func NewLog(w io.Writer, now func() time.Time, id string) *Log

// Close emits exactly one cumulative summary record — total Usage and total
// Cost across the conversation's turns — and marks the log closed. It is
// idempotent: a second Close writes nothing and returns nil. After Close, any
// Send on the owning Conversation returns ErrClosed (D4).
func (l *Log) Close() error
```

## REQUIREMENTS

- R-5W0X-N9YY: `agentkit` MUST export `type RecordType string` whose complete set of exported constants is exactly `RecordConversation = "conversation"`, `RecordTurnStart = "turn_start"`, `RecordMessage = "message"`, `RecordToolUse = "tool_use"`, `RecordToolResult = "tool_result"`, `RecordOutput = "output"`, `RecordUsage = "usage"`, `RecordLimit = "limit"`, `RecordError = "error"`, `RecordRetry = "retry"`, `RecordTurnEnd = "turn_end"`, `RecordSummary = "summary"`, `RecordSavepoint = "savepoint"`, `RecordRestore = "restore"`, `RecordRelease = "release"` (D26), `RecordClosed = "closed"`, with no other member.
- R-5YGQ-ETGC: `agentkit` MUST export `type LogRecord struct { Type RecordType; ID string; Time time.Time; Seq int; Conversation *ConversationInfo; Message *Message; ToolUse *ToolUse; ToolResult *ToolResult; Output json.RawMessage; Usage *Usage; Cost *Cost; Limit *LimitInfo; Err *Error; Retry *RetryInfo }` with exactly those fields and the JSON tags `type`, `id` (omitempty), `time`, `seq`, and the `omitempty` fields `conversation`/`message`/`tool_use`/`tool_result`/`output`/`usage`/`cost`/`limit`/`error`/`retry`.
- R-5ZOM-SL71: `agentkit` MUST export `type ConversationInfo struct { Format int; Identity Identity; Settings Settings; Tools []ToolInfo; Deferred []DeferredInfo; Output *OutputContract; Limits Limits }` with exactly those fields and the JSON tags `format`, `identity`, `settings`, `tools` (omitempty), `deferred` (omitempty), `output` (omitempty), `limits`.
- R-60WJ-6CXQ: `agentkit` MUST export `type ToolInfo struct { Name string; Description string; Schema json.RawMessage }` with exactly those fields and the JSON tags `name`, `description`, `schema`.
- R-624F-K4OF: `agentkit` MUST export `type DeferredInfo struct { Name string; Blurb string; Tools []ToolInfo }` with exactly those fields and the JSON tags `name`, `blurb`, `tools`.
- R-63CB-XWF4: `agentkit` MUST export the untyped integer constant `LogFormatVersion = 1`.
- R-64K8-BO5T: A successful `New` (D18) given a non-nil `Config.Log` MUST write exactly one `conversation` record to that log before returning, and it MUST precede every other record the conversation writes; a `New` that returns an error MUST write none.
- R-65S4-PFWI: The `conversation` record's `ConversationInfo` MUST carry `Format` equal to `LogFormatVersion`, `Identity` equal to the conversation's `Identity`, `Settings` and `Limits` equal to the `Config` values as copied at construction, `Output` equal to `Config.Output` (nil when none), `Tools` holding one `ToolInfo` per `Config.Tools` entry in order with that tool's `Name()`, `Description()`, and `Schema()`, and `Deferred` holding one `DeferredInfo` per `Config.Deferred` group in order with its `Name`, `Blurb`, and its tools rendered the same way.
- R-687X-GZDW: A log MUST contain at most one `closed` record, and no record of any type other than `summary` may follow it.
- R-T7MU-UJOW: `agentkit` MUST export `Log` as an opaque type together with `func NewLog(w io.Writer, now func() time.Time, id string) *Log` and the method `func (*Log) Close() error`.
- R-T8UR-8BFL: Every record a `Log` writes MUST carry the `id` given to `NewLog` verbatim in its `ID` field, and the field MUST be omitted from the JSON line when that `id` is empty.
- R-TA2N-M36A: Each `LogRecord` MUST timestamp from the injected clock, and `Seq` MUST be `0` on the first record a `Log` writes and increase by exactly one on every subsequent record for the life of the log, never reset.
- R-TBAJ-ZUWZ: `Send` MUST write a `message` record carrying the `RoleUser` message built from the caller's blocks immediately after the turn's `turn_start` record and before any provider call, including on a turn that ends in a terminal error or is refused by a limit (D25).
- R-DN5C-9BTK: After the `tool_result` records of a round-trip, the orchestrator MUST write one `message` record carrying the `RoleTool` message whose blocks are that round-trip's `ToolResult` blocks in the order of the `ToolUse` blocks of the assistant message that requested them, whatever order the calls returned in.
- R-TDQC-REED: For a turn that commits, the `message` records written between its `turn_start` and `turn_end`, in order, MUST equal the sequence of `Message` values the turn appended to `History`; for a turn that ends in a terminal error or a limit refusal, they MUST equal the messages the turn produced up to that point, in order.
- R-TEY9-5652: After every completed provider round-trip the orchestrator MUST write one `usage` record carrying that round-trip's merged `Usage` and its `Cost` resolved through the D3 path, and MUST NOT write a `usage` record that aggregates more than one round-trip.
- R-TG65-IXVR: Every `turn_end` record MUST carry `Usage` and `Cost` equal to the field-wise integer sums of the `usage` records written since that turn's `turn_start`, and zero values when the turn completed no round-trip.
- R-THE1-WPMG: The `summary` record MUST carry `Usage` and `Cost` equal to the field-wise integer sums of every `turn_end` record the log has written.
- R-6701-37N7: A `turn_start` record MUST carry no payload field: every `omitempty` field of `LogRecord` MUST be absent from its JSON line.
- R-5KP1-C3WR: A `nil` log MUST write nothing and MUST require no per-call-site nil check; log payloads MUST reuse the canonical `Identity`/`Usage`/`Cost`/`Error`/`Block` types rather than log-only shadow structs.
- R-5LWX-PVNG: A log write failure MUST NOT abort the turn and MUST NOT change `Stream.Err()`; the failure MAY be retained on the log for inspection.
- R-5N4U-3NE5: `Close` MUST emit exactly one cumulative `summary` record and MUST be idempotent; a `Send` after `Close` MUST return `ErrClosed`.
- R-0NE8-TO91: `agentkit` MUST export `type RetryInfo struct { Attempt int; Delay time.Duration; Reason string }` with exactly those three fields.
