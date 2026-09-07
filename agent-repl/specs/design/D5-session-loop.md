# D5-session-loop

`cli.Run` sequences the whole program: parse, short-circuit help and version,
validate, create the log file, open the session, then loop over prompts until
input ends. Order matters because each step's failure has a different exit
code (D2) and because help and version must cost nothing — no file, no
network, no stdin.

```
ParseFlags ──ErrHelp──▶ usage → stdout, exit 0
    │ error ──────────▶ error + usage → stderr, exit 2
    ▼
Version? ──yes────────▶ version → stdout, exit 0
    ▼
Validate ──error─────▶ error + usage → stderr, exit 2
    ▼
create log file ──error─▶ error → stderr, exit 1
    ▼
session.Open ──error─▶ error → stderr, exit 1
    ▼
loop: prompt, read line, send, render ... until EOF or interrupt at prompt
    ▼
Log.Close, summary, exit 0
```

**The log file.** Every session writes agentkit's event log to
`<Home>/.agent-repl/logs/<stamp>.jsonl`, where `<stamp>` is `Deps.Now()` in
UTC formatted `20060102T150405Z`. The directory is created if missing (mode
`0700`) and the file is created new (mode `0600`). The log is agentkit's own
`Log` over a sink that forwards each record line to the file and, in raw
mode, to stdout (D6), built with `Deps.Now` as its clock and `Deps.LogID` as
its identity, so every record carries the session's UUID (D1). The file is
the whole transcript: agentkit records the user's prompt, any system file,
and each message the model and the tools produce, with one `usage` record per
provider round-trip and the turn's total on `turn_end`. The summary block a
person sees at the end is read off that same stream: the `summary` record
agentkit writes on `Close` carries the cumulative usage and cost, so
agent-repl never keeps its own totals.

**The loop.** Each iteration prints the prompt (decorated mode only), reads
one line of stdin, and either ends the session, skips the line, or runs a turn:

- End of input ends the session. A final line without a trailing newline is
  still a line.
- A line that is empty after stripping its terminator (`\n` or `\r\n`) is
  skipped: no turn, no blank line, just the next prompt.
- Any other line is sent as one turn. The stream is consumed to completion,
  each event handed to the renderer as it arrives, and the stream's terminal
  error, if any, rendered after the events. Then the next prompt.

**Interrupts.** `Deps.Interrupts` delivers one receive per `SIGINT` (`main`
wires `signal.Notify`). A receive while a turn is in flight cancels that
turn's context: the stream ends, its error is rendered, and the loop continues
to the next prompt. A receive while waiting at the prompt ends the session
exactly as end of input does. Cancelling the `ctx` passed to `Run` ends the
session at the next opportunity regardless of state. A cancelled turn leaves
agentkit's history unchanged (its own guarantee), so the next prompt continues
the same conversation.

**Ending.** However the session ends — end of input, interrupt at the prompt,
or `ctx` done — `Run` closes the log (which writes the `summary` record),
renders the summary in decorated mode, and returns 0. A turn that failed does
not change the exit code: the user saw the error and chose to continue or
stop.

## REQUIREMENTS

- R-VXK1-CMU3: `Run` MUST sequence its steps as `ParseFlags`, then help and version short-circuits, then `Validate`, then log-file creation, then `session.Open`, then the input loop, such that a usage error creates no log file and reads no stdin, and a log-file or `Open` failure reads no stdin.
- R-VYRX-QEKS: `Run` MUST create the directory `<Deps.Home>/.agent-repl/logs` with mode `0700` when it does not exist and create the file `<Deps.Home>/.agent-repl/logs/<stamp>.jsonl` with mode `0600`, where `<stamp>` is `Deps.Now()` in UTC formatted `20060102T150405Z`, before the first prompt.
- R-VZZU-46BH: When the log directory or file cannot be created, `Run` MUST write `error: ` followed by the cause to stderr, write nothing to stdout, and exit 1.
- R-W17Q-HY26: When `session.Open` fails, `Run` MUST write `error: ` followed by the cause to stderr, exit 1, and leave the created log file containing no records.
- R-P31N-2V1H: `Run` MUST build the session's `agentkit.Log` with `agentkit.NewLog` over a writer that appends every record line to the log file, timestamped by `Deps.Now` and identified by `Deps.LogID`, such that after a session the file holds exactly the records agentkit wrote, one JSON object per line, each carrying `Deps.LogID` in its `id` field, ending with a `summary` record.
- R-NIWO-J7VP: `Run` MUST build `session.Config` from `Options` and `Deps` field for field — `Provider`, `Model`, `Wire`, `Auth`, `AuthFile`, `BaseURL`, `SystemFile`, and `Settings` from `Options`; `Home`, `Getenv`, and `Root` from `Deps` — and pass the log it created.
- R-W4VF-N9A9: For each line of stdin that is non-empty after stripping a trailing `\n` or `\r\n`, `Run` MUST call `Session.Send` exactly once with that line and consume the returned stream to completion before reading the next line; a line that is empty after stripping MUST cause no `Send`.
- R-W7B8-ESRN: A final line of stdin that ends without a newline MUST be sent as a turn.
- R-W8J4-SKIC: An error from a turn's stream — including `agentkit.ErrInvalidConfig` from an unknown option and a provider error — MUST be rendered and MUST NOT end the session: `Run` MUST continue to read and send subsequent lines and MUST exit 0.
- R-W9R1-6C91: A receive on `Deps.Interrupts` while a turn is in flight MUST cancel the context passed to `Session.Send` for that turn, and `Run` MUST then continue to read and send subsequent lines.
- R-WAYX-K3ZQ: A receive on `Deps.Interrupts` while `Run` is waiting for a line of stdin MUST end the session exactly as end of input does, exiting 0 even when stdin has not reached end of input.
- R-WC6T-XVQF: When the `ctx` passed to `Run` is cancelled, `Run` MUST return 0 within a bounded time without waiting for further stdin.
- R-WDEQ-BNH4: At end of input and on an interrupt at the prompt, `Run` MUST close the log exactly once before returning, so the log file ends with the `summary` record, and MUST exit 0.
