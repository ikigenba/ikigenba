# D6-rendering

One output style: agent-repl's decorated transcript, with every line
prefixed by the address of the agent that produced it, followed by a summary
block. All of it is written through one `Trace` value, which serializes its
writes, so the output stays whole whatever runs concurrently later.

```go
package render

// Trace writes the transcript. Conversation to stdout, errors to stderr.
// It satisfies agent.Trace.
type Trace struct{ /* unexported */ }
func NewTrace(stdout, stderr io.Writer) *Trace
func (t *Trace) Event(address string, ev agentkit.Event)
func (t *Trace) Error(address string, err error)
func (t *Trace) Record(rec agentkit.LogRecord) string
func (t *Trace) Summary(usage agentkit.Usage, cost agentkit.Cost, session string)

// OneLine collapses text to a single line: every line terminator becomes one space.
func OneLine(s string) string
```

**Lines.** Every rendered line has the shape `<address> <label> › <body>` and
is followed by one blank line. Labels and bodies are agent-repl's, unchanged:

- `assistant` — the text of a `MessageDone`'s `Text` blocks joined by
  newlines, printed as is; the model's own line breaks are kept and
  continuation lines carry no prefix. A message with no `Text` block renders
  nothing.
- `tool` — a `ToolCall`: the tool name, a space, and the input as compact
  JSON.
- `result` — a `ToolReturn`: the tool name recovered from the matching call
  (or the id when no call was seen), a space, `error: ` when the result is an
  error, and the content collapsed by `OneLine`. Never truncated.
- `error` — a terminal error, to stderr, collapsed by `OneLine`.

`OutputDone` renders nothing. The address of a `delegate` call's `result`
line is the parent's, so a reader sees the child's trace between the
parent's `tool › delegate` and `result › delegate` lines, indented by nothing
but its longer address.

**Records.** `Record` is the text a transcript entry gets in the store (D4).
It is the same vocabulary without the prefix and without the trailing blank
line, so a search hit reads like the trace: `assistant › ...`, `tool › ...`,
`result › ...`, and `user › ...` for the prompt message. A `message` record
whose role is the tool role renders `""`, since its results were already
rendered as `tool_result` records; every other record type (`turn_start`,
`usage`, `limit`, `error`, `retry`, `turn_end`, `summary`, `output`) renders
`""` too. They are stored for the record, not for search.

**Summary.** Read from the pass `Result`, written after the last event:

```
summary
· tokens   in=8120 cache(r=0 w=0) out=611 reasoning=0 total=8731
· cost     $0.031200 pass
· session  7f0c2e3a-1d5b-4c9e-9a2f-3b8d6e1f0a47
```

`in` is `InputTokens`, `cache(r=…)` is `CachedTokens`, `w=` the sum of the
two cache-write buckets, `out` is `OutputTokens`, `reasoning` is
`ReasoningTokens`, `total` the sum of all six, and the cost the nano-USD
`Cost` in dollars to six decimals. The session line is what a caller copies
into `-resume`.

## REQUIREMENTS

- R-JR6N-EU2E: Package `internal/render` MUST export `Trace` as an opaque struct type together with `NewTrace(stdout, stderr io.Writer) *Trace` and the methods `Event(address string, ev agentkit.Event)`, `Error(address string, err error)`, `Record(rec agentkit.LogRecord) string`, and `Summary(usage agentkit.Usage, cost agentkit.Cost, session string)`, and `*Trace` MUST satisfy `agent.Trace` by method set (verified without importing `internal/agent`).
- R-JSEJ-SLT3: Package `internal/render` MUST export `OneLine(s string) string`, which MUST replace every `\r\n` and every remaining `\n` with a single space and change nothing else.
- R-JTMG-6DJS: `Trace.Event` with a `MessageDone` whose message has at least one `Text` block MUST write `<address> assistant › `, the `Text` blocks' texts joined by `\n` and otherwise unchanged, a newline, and a blank line to stdout; with a message having no `Text` block it MUST write nothing.
- R-JUUC-K5AH: `Trace.Event` with a `ToolCall` MUST write `<address> tool › `, the tool name, a space, the input as compact JSON (`json.Compact`) or `OneLine` of the input bytes when they are not valid JSON, a newline, and a blank line to stdout.
- R-JW28-XX16: `Trace.Event` with a `ToolReturn` MUST write `<address> result › `, the name of the tool from the earlier `ToolCall` with the same address whose `Use.ID` equals the result's `ToolUseID` (or the `ToolUseID` itself when no such call was seen), a space, `error: ` iff `IsError`, `OneLine` of the content, a newline, and a blank line to stdout; with an `OutputDone` it MUST write nothing.
- R-JXA5-BORV: `Trace.Error` MUST write `<address> error › `, `OneLine` of `err.Error()`, a newline, and a blank line to stderr and nothing to stdout.
- R-JYI1-PGIK: `Trace.Record` MUST return, for a `message` record with role user or assistant, `<role> › ` followed by the `Text` blocks' texts joined by `\n`; for a `tool_use` record, `tool › `, the name, a space, and the compact-JSON input; for a `tool_result` record, `result › `, `error: ` iff `IsError`, and `OneLine` of the content; and `""` for a `message` record with the tool role and for every other record type.
- R-JZPY-3899: `Trace.Summary` MUST write to stdout exactly the four lines `summary`, `· tokens   in=<InputTokens> cache(r=<CachedTokens> w=<CacheWrite5mTokens+CacheWrite1hTokens>) out=<OutputTokens> reasoning=<ReasoningTokens> total=<sum of all six fields>`, `· cost     $<cost> pass` where `<cost>` is `Cost` divided by 1,000,000,000 formatted with exactly six decimals, and `· session  <session>`, each line newline-terminated.
- R-K0XU-GZZY: `Trace` MUST be safe for concurrent use: concurrent `Event` calls from several goroutines MUST produce output in which every rendered line and its blank line are contiguous and unbroken, verified with the race detector.
- R-K25Q-URQN: `Run` MUST pass a `render.Trace` over its stdout and stderr as `agent.Config.Trace`, so that a pass's events appear on stdout with their addresses and its errors on stderr, and MUST call `Trace.Summary` with the pass's `Usage`, `Cost`, and the session id after the pass.
