# D6-rendering

Two output styles, chosen by `-raw`. The **decorated** style is a transcript a
person reads; the **raw** style is agentkit's log-record stream a script
reads. Both are driven from the same two sources: the turn's `agentkit.Event`
stream, and the JSONL records agentkit writes to the log.

```go
package render

// Decorated writes the transcript. Conversation to stdout, errors to stderr.
type Decorated struct{ /* unexported */ }
func NewDecorated(stdout, stderr io.Writer) *Decorated
func (d *Decorated) Prompt()                                   // "you › "
func (d *Decorated) Begin()                                    // the blank line after the user's line
func (d *Decorated) Event(ev agentkit.Event)                   // one line plus a blank line
func (d *Decorated) Error(err error)                           // "error › ..." plus a blank line, to stderr
func (d *Decorated) End()                                      // ends the prompt line at end of input
func (d *Decorated) Summary(usage agentkit.Usage, cost agentkit.Cost)

// OneLine collapses text to a single line: every line terminator becomes one space.
func OneLine(s string) string

// LogSink is the io.Writer handed to agentkit.NewLog. It forwards every record
// line to each destination and remembers the last summary record.
type LogSink struct{ /* unexported */ }
func NewLogSink(dst ...io.Writer) *LogSink
func (s *LogSink) Write(p []byte) (int, error)
func (s *LogSink) Summary() (agentkit.Usage, agentkit.Cost, bool)
```

**Decorated.** A session reads like this — the user's line is echoed by the
terminal, not by agent-repl:

```
you › Hi, I'm Mike.

assistant › Hi Mike! Nice to meet you. How can I help?

you › what is in go.mod?

tool › Read {"file_path":"go.mod"}

result › Read      1	module github.com/ikigenba/ikigenba/agent-repl      2	      3	go 1.26

assistant › It declares the module path and Go 1.26, with no requirements.

you › 
summary
· tokens  in=1204 cache(r=0 w=0) out=63 reasoning=0 total=1267
· cost     $0.004115 session
```

Every rendered line has the shape `<label> › <body>` and is followed by one
blank line, so turns are visually separated whatever mix of text, tool calls,
and results they contain. Labels: `assistant` for a model message (the text
of its `Text` blocks, joined by newlines, printed as is — the model's own line
breaks are kept); `tool` for a tool call (the tool name, a space, and the
input as compact JSON); `result` for a tool result (the tool name recovered
from the matching call, a space, `error: ` when the result is an error, and
the content collapsed by `OneLine`); `error` for a turn's terminal error (its
text collapsed by `OneLine`, so a provider message can never split the line).
Tool calls and results are never truncated: they are the record of what the
model did, and the log has the same bytes. A model message with no `Text`
block renders nothing. `OutputDone` renders nothing.

The summary is read from the `summary` record via `LogSink`: `in` is
`InputTokens`, `cache(r=…)` is `CachedTokens`, `w=` is the sum of the two
cache-write buckets, `out` is `OutputTokens`, `reasoning` is
`ReasoningTokens`, `total` is the sum of all six buckets, and the cost is the
nano-USD `Cost` shown in dollars to six decimals.

**Raw.** With `-raw`, stdout carries exactly the record lines agentkit writes,
as they are written — `LogSink` fans out to the file and stdout — and nothing
else: no prompt, no transcript, no summary block (the `summary` record is the
last line). Turn errors still go to stderr in the decorated `error ›` form so
a script watching stdout sees only records.

## REQUIREMENTS

- R-WEMM-PF7T: Package `internal/render` MUST export `Decorated` as an opaque struct type together with `NewDecorated(stdout, stderr io.Writer) *Decorated` and the methods `Prompt()`, `Begin()`, `Event(ev agentkit.Event)`, `Error(err error)`, `End()`, and `Summary(usage agentkit.Usage, cost agentkit.Cost)`.
- R-WFUJ-36YI: Package `internal/render` MUST export `OneLine(s string) string`, which MUST replace every `\r\n` and every remaining `\n` with a single space and change nothing else.
- R-WH2F-GYP7: Package `internal/render` MUST export `LogSink` as an opaque struct type together with `NewLogSink(dst ...io.Writer) *LogSink`, the method `Write(p []byte) (int, error)` satisfying `io.Writer`, and the method `Summary() (agentkit.Usage, agentkit.Cost, bool)`.
- R-WIAB-UQFW: `LogSink.Write` MUST forward every byte to every destination in order and, after a line that decodes as an `agentkit.LogRecord` of type `summary` has been written, `Summary` MUST return that record's `Usage` and `Cost` with `true`; before any such line it MUST return zero values with `false`.
- R-WJI8-8I6L: `Decorated.Prompt` MUST write exactly `you › ` (with a trailing space and no newline) to stdout; `Begin` MUST write exactly one newline to stdout; `End` MUST write exactly one newline to stdout.
- R-WKQ4-M9XA: `Decorated.Event` with a `MessageDone` whose message has at least one `Text` block MUST write `assistant › `, the `Text` blocks' texts joined by `\n` and otherwise unchanged, a newline, and a blank line to stdout; with a message having no `Text` block it MUST write nothing.
- R-WLY1-01NZ: `Decorated.Event` with a `ToolCall` MUST write `tool › `, the tool name, a space, the input as compact JSON (`json.Compact`) or `OneLine` of the input bytes when they are not valid JSON, a newline, and a blank line to stdout.
- R-WN5X-DTEO: `Decorated.Event` with a `ToolReturn` MUST write `result › `, the name of the tool from the earlier `ToolCall` whose `Use.ID` equals the result's `ToolUseID` (or the `ToolUseID` itself when no such call was seen), a space, `error: ` iff `IsError`, `OneLine` of the content, a newline, and a blank line to stdout.
- R-WODT-RL5D: `Decorated.Event` with an `OutputDone` MUST write nothing.
- R-CNZG-JOTG: `Decorated.Error` MUST write `error › `, `OneLine` of `err.Error()`, a newline, and a blank line to stderr and nothing to stdout.
- R-WS1I-WWDG: `Decorated.Summary` MUST write to stdout exactly the three lines `summary`, `· tokens  in=<InputTokens> cache(r=<CachedTokens> w=<CacheWrite5mTokens+CacheWrite1hTokens>) out=<OutputTokens> reasoning=<ReasoningTokens> total=<sum of all six fields>`, and `· cost     $<cost> session` where `<cost>` is `Cost` divided by 1,000,000,000 formatted with exactly six decimals, each line newline-terminated.
- R-WT9F-AO45: In decorated mode, `Run` MUST call `Prompt` before each read of stdin, `Begin` once for each line that is sent, `Event` for every event of the turn's stream in order, `Error` for the stream's terminal error when non-nil, `End` once when the session ends, and `Summary` with the values from `LogSink.Summary` after closing the log.
- R-WUHB-OFUU: In raw mode, `Run` MUST include stdout among the `LogSink` destinations so that stdout receives exactly the record lines agentkit writes and nothing else, MUST write no prompt, transcript, or summary block to stdout, and MUST still write turn errors to stderr through `Decorated.Error`.
