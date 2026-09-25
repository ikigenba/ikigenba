# D10-chat

`internal/chat` is the vocabulary the three harness packages and the command
share when `agent-monitor chat <harness> <session-id> [<agent-id>]` runs, as
`internal/session` (`D04-sessions-and-table`) is for `list` and
`internal/tree` (`D09-tree`) is for `tree`. It owns what a chat is made of
(entries and token usage), how it is printed, and the reader that turns one
agent's transcript into entries and running totals. It imports, of this
module, `internal/session` alone, as `D01-layout-and-run-seam` states: the
reader follows its file with a `session.Log` and reports a file it cannot
read as a `*session.ReadError`. It reaches nothing on the machine except
through the `fs.FS` it is handed, and never writes.

## Entries and their printed form

An entry is one thing said or done: the moment it was recorded, its kind, the
tool's name for a tool call, and its text. The kinds are a closed set of
seven words — `user`, `assistant`, `reasoning`, `agent`, `tool`, `result ok`,
`result error` — so they are a named string type with one constant each.
The text of a `user`, `assistant`, `reasoning`, or `agent` entry is its body;
the text of a `tool` entry is the tool's arguments as the harness recorded
them, as JSON text; a `result` entry has no body, because what a tool
returned is never printed. As in `D04`, a moment that is not known is said
by a separate flag, never by the zero time, and prints as `-`.

`Format` prints one entry: a header line, the body, and one empty line. The
header is the moment in UTC to the second, fractions dropped exactly as
`list` prints LAST ACTIVE, one space, and the kind — for a tool call, `tool`,
a space, and the tool's name. A tool name is printed with every control
character, newline and tab included, as `\xNN`, so a header is always one
line. A body keeps newline and tab and prints every other control character
as `\xNN`, with lowercase hex; nothing else in it is changed, so a body
reads exactly as recorded. A tool call's arguments are printed as one line
of JSON: valid JSON is compacted (whitespace outside strings removed, so a
newline inside an argument stays the two characters `\n`), and anything that
is not valid JSON is printed as a JSON string. The line is cut at 200
characters, counted as runes, and a cut line ends in `…`.

`TotalsLine` prints the totals line. Token usage has six counts: `in` (input
neither read from nor written to a cache), `cache-write`, `cache-read`,
`out` (every generated token, reasoning included), `reasoning` (the part of
`out` spent reasoning), and `calls` (model calls). Each harness records some
of these and not others; which ones is a fact about the harness, not about
one agent, so it travels beside the counts as a set of flags, and a count
the harness does not record prints as `-` even when it is zero. An agent
that has made no model call prints `0` for every count its harness records.

## Reading a transcript

A `Transcript` follows one agent's transcript file. It owns a `session.Log`
over that file, the harness's `Decoder`, and the running `Usage`. Each
`Read` is one pass of the `Log`: it takes in only the complete lines the
file gained since the last pass, hands every line that is a record (the
record of `D04`) to the decoder once, in file order, and returns the entries
the decoder produced for this pass; the usage the decoder reports is added
to the running totals. Nothing is read twice and nothing is stored anywhere
but in memory, so the command's snapshot is the first pass, and a later
`watch` keeps the same `Transcript` and calls `Read` again to get only what
was appended. A line that is not a record is skipped; a half-written last
line is never returned by the `Log`, so it is neither shown nor counted
until it is complete. When the `Log` resets (the file was replaced), the
`Transcript` starts over with a fresh decoder and zero totals and says so.

Decoding is the harness's business: a harness hands `NewTranscript` a
function that makes a fresh `Decoder`, and the decoder carries whatever
state it needs from record to record — a reply split over several records
counted as one call, a copied history skipped until the agent's own first
record. Each `Decode` is handed the filesystem the pass reads through,
read-only, because one harness cannot tell an agent's own usage from its
transcript alone: a Grok root's per-turn usage includes the subagents that
finished during that turn, so when the decoder meets the record that ends
the turn it reads the usage those subagents recorded in their own logs, at
that moment, through a `session.Log` it keeps for each such log, so each
later read takes in only what that log gained since. The agent's own
transcript is still read only by the passes, once; the decoder never reads
it. So a later `watch` that calls `Read` again also subtracts a subagent
that finished after the first pass. It is not exact in every case: if a
subagent's log grows after a turn has already subtracted it, a watch and a
fresh `chat` of the same moment can show different root totals. That has
never been observed and is accepted. A negative count a decoder returns
counts as zero, so the totals never go below zero.

The `Transcript` applies two rules on the harness's behalf so they
hold alike on every harness: an entry of a kind outside the seven is
dropped, and a text entry with empty text (an empty thinking block, say) is
dropped.

A transcript that does not exist yet is a transcript with no records. A
harness that finds no file for an agent gives the `Transcript` an empty
path, and its passes read nothing; a file that is named but missing reads
as empty too. Any other failure is a `*session.ReadError` naming the
transcript's path.

## The harness side

Each harness package exports `Chat`, which the command calls with the
machine's filesystem, the home directory, the session id, and the agent id
— the session id itself when the developer named no agent, since the root's
id names the root. `Chat` finds the session exactly as `Tree` does, so the
not-found and cannot-read outcomes of `D09-tree` carry over unchanged; finds
the agent among the session's agents, the ids `tree` draws; builds the
agent's `Transcript`; makes its first pass; and returns the transcript with
that pass's entries. It reads the agent's transcript only through that pass,
so a harness that needs a fact from the transcript itself (whether a Codex
rollout is a root) takes it from its own decoder after the pass rather than
reading the file again. Every other log it reads to find the agent is read
in one pass of a fresh `session.Log`, as `Tree` reads logs. The totals are
the agent's own. Where each harness finds a transcript, which records become
which entries, what counts as a model call, and which counts the harness
records are `D06-claude`, `D07-codex`, and `D08-grok`.

## REQUIREMENTS

- R-KHCB-MZ8F: The `internal/chat` package (import path `github.com/ikigenba/ikigenba/agent-monitor/internal/chat`, package name `chat`) MUST export the named type `type Kind string`.
- R-KIK8-0QZ4: The `internal/chat` package MUST export the constants `KindUser Kind = "user"`, `KindAssistant Kind = "assistant"`, `KindReasoning Kind = "reasoning"`, `KindAgent Kind = "agent"`, `KindTool Kind = "tool"`, `KindResultOK Kind = "result ok"`, and `KindResultError Kind = "result error"`, each declared with the type `Kind`.
- R-KJS4-EIPT: The `internal/chat` package MUST export the struct type `type Entry struct { Time time.Time; HasTime bool; Kind Kind; Tool string; Text string }`, with exactly these fields in this order.
- R-KL00-SAGI: The `internal/chat` package MUST export the struct type `type Usage struct { In int64; CacheWrite int64; CacheRead int64; Out int64; Reasoning int64; Calls int64 }`, with exactly these fields in this order.
- R-KNFT-JTXW: The `internal/chat` package MUST export the struct type `type Recorded struct { In bool; CacheWrite bool; CacheRead bool; Out bool; Reasoning bool; Calls bool }`, with exactly these fields in this order.
- R-6S43-37D6: The `internal/chat` package MUST export the interface type `type Decoder interface { Decode(fsys fs.FS, record []byte) ([]Entry, Usage) }`, with exactly that one method, where `fs` is the standard library's `io/fs`.
- R-KPVM-BDFA: The `internal/chat` package MUST export the struct type `Transcript`, with no exported field.
- R-KR3I-P55Z: The `internal/chat` package MUST export `func NewTranscript(path string, recorded Recorded, newDecoder func() Decoder) *Transcript`.
- R-KSBF-2WWO: The `internal/chat` package MUST export the method `func (t *Transcript) Read(fsys fs.FS) (entries []Entry, reset bool, err error)`, where `fs` is the standard library's `io/fs`; each call of `Read` is a *pass* of `t`.
- R-KTJB-GOND: The `internal/chat` package MUST export the method `func (t *Transcript) Usage() Usage`.
- R-KUR7-UGE2: The `internal/chat` package MUST export the method `func (t *Transcript) Recorded() Recorded`.
- R-KVZ4-884R: The `internal/chat` package MUST export the method `func (t *Transcript) Path() string`.
- R-KX70-LZVG: The `internal/chat` package MUST export the variable `ErrAgentNotFound` of type `error`, and its value MUST be non-nil.
- R-KYEW-ZRM5: The `internal/chat` package MUST export `func Format(e Entry) string`.
- R-KZMT-DJCU: The `internal/chat` package MUST export `func TotalsLine(u Usage, r Recorded) string`.
- R-L0UP-RB3J: Each of the packages `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok` MUST export the function `Chat(root fs.FS, home, sessionID, agentID string) (*chat.Transcript, []chat.Entry, error)`, where `fs` is the standard library's `io/fs` and `chat` is `internal/chat`.
- R-L22M-52U8: The *text escape* of a string `s` MUST be the concatenation, reading `s` from its first byte to its last and taking at each position the element `utf8.DecodeRuneInString` decodes there, of: for a byte that does not begin a valid UTF-8 encoding (the decode yields `utf8.RuneError` with width 1), `\x` followed by the byte's value as two lowercase hexadecimal digits; for byte 0x0A and for byte 0x09, that byte unchanged; for any other validly encoded rune `r` for which `unicode.IsControl(r)` is true, `\x` followed by `r` as two lowercase hexadecimal digits; and for every other validly encoded rune, its encoding unchanged — so that `a` LF TAB `b` maps to itself, `a` CR `b` to `a\x0db`, ESC `[31m` to `\x1b[31m`, DEL to `\x7f`, U+009B to `\x9b`, `C:\dir` and `it's` to themselves, `é` to `é`, and the single byte 0xFF to `\xff`.
- R-L3AI-IUKX: The *name escape* of a string `s` MUST be exactly its text escape except that byte 0x0A is represented by `\x0a` and byte 0x09 by `\x09` instead of being kept, so that `Bash` maps to `Bash` and `a` LF `b` to `a\x0ab`, and the name escape of any string contains no byte 0x0A.
- R-L5QB-AE2B: The *argument line* of a string `s` MUST be, when `json.Valid([]byte(s))` is true, the bytes `json.Compact` writes for `s`, and otherwise the JSON string encoding of `s` as a `json.Encoder` with `SetEscapeHTML(false)` writes it, without the trailing newline `Encode` appends; so that `{ "cmd": "ls",` LF `  "n": 1 }` gives `{"cmd":"ls","n":1}`, `{"content":"a\nb"}` (with the two characters `\` and `n`) gives itself, `{"q":"<a&b>"}` gives itself, and `not` LF `json` gives `"not\njson"`, a quotation mark, `not`, the two characters `\` and `n`, `json`, and a quotation mark.
- R-L6Y7-O5T0: The *cut argument line* of a string `s` MUST be its argument line `a` when `utf8.RuneCountInString(a)` is at most 200, and otherwise the first 200 runes of `a` followed by `…` (U+2026), so that an argument line of exactly 200 runes is not cut and one of 201 runes is its first 200 runes and `…`, whatever the byte length of those runes.
- R-L864-1XJP: The *header* of an `Entry` `e` whose `Kind` is one of the seven `Kind` constants MUST be its time part, one U+0020, and its kind part, where the time part is `e.Time.UTC().Format("2006-01-02T15:04:05Z")` when `e.HasTime` is true and `-` when it is false (so that fractional seconds are dropped, never rounded, and a `Time` of 2026-09-24T14:58:03.9-05:00 gives `2026-09-24T19:58:03Z`), and the kind part is `"tool " + ` the name escape of `e.Tool` when `e.Kind` is `KindTool` and `string(e.Kind)` otherwise; `e.Time` MUST NOT affect the header when `e.HasTime` is false.
- R-L9E0-FPAE: The *body* of an `Entry` `e` MUST be the empty string when `e.Kind` is `KindResultOK` or `KindResultError`, whatever `e.Text` is, or when `e.Text` is empty; otherwise it MUST be the text escape of `e.Text` followed by one `"\n"` when `e.Kind` is `KindUser`, `KindAssistant`, `KindReasoning`, or `KindAgent`, and the text escape of the cut argument line of `e.Text` followed by one `"\n"` when `e.Kind` is `KindTool`.
- R-LALW-TH13: `Format(e)` MUST return exactly the header of `e`, `"\n"`, the body of `e`, and `"\n"` when `e.Kind` is one of the seven `Kind` constants, and the empty string otherwise; so that an entry with `HasTime` true at 2026-09-24T19:58:03.412Z, `Kind` `KindUser`, and `Text` `commit all files` formats as `"2026-09-24T19:58:03Z user\ncommit all files\n\n"`, one with `Kind` `KindTool`, `Tool` `Bash`, and `Text` `{"command":"git status --short"}` at 2026-09-24T19:58:06.204Z as `"2026-09-24T19:58:06Z tool Bash\n{\"command\":\"git status --short\"}\n\n"`, and one with `Kind` `KindResultOK` at 2026-09-24T19:58:07.650Z as `"2026-09-24T19:58:07Z result ok\n\n"`.
- R-LBTT-78RS: `TotalsLine(u, r)` MUST return exactly `"tokens: in " + v(u.In, r.In) + "  cache-write " + v(u.CacheWrite, r.CacheWrite) + "  cache-read " + v(u.CacheRead, r.CacheRead) + "  out " + v(u.Out, r.Out) + "  reasoning " + v(u.Reasoning, r.Reasoning) + "  calls " + v(u.Calls, r.Calls) + "\n"`, where `v(n, rec)` is `strconv.FormatInt(n, 10)` when `rec` is true and `-` when it is false; so that with every flag true and counts 5, 5391, 46131, 201, 30, and 3 it returns `"tokens: in 5  cache-write 5391  cache-read 46131  out 201  reasoning 30  calls 3\n"`, with every flag true and a zero `Usage` `"tokens: in 0  cache-write 0  cache-read 0  out 0  reasoning 0  calls 0\n"`, and with only `Reasoning` false and a zero `Usage` `"tokens: in 0  cache-write 0  cache-read 0  out 0  reasoning -  calls 0\n"`.
- R-LD1P-L0IH: `t.Path()` MUST return the `path` and `t.Recorded()` the `recorded` that `NewTranscript` was called with to make `t`, whatever passes `t` has made.
- R-LE9L-YS96: A pass `t.Read(fsys)` of a `Transcript` `t` whose `Path()` is the empty string MUST call no method of `fsys`, call no `Decode`, and return nil entries, `reset` false, and a nil error.
- R-GUKJ-NF66: A pass `t.Read(fsys)` of a `Transcript` `t` whose `Path()` `p` is not empty MUST make exactly one pass `l.Read(fsys, strings.TrimPrefix(p, "/"))` of a `session.Log` `l` that belongs to `t` alone and is a zero-value `Log` when `NewTranscript` returns `t`, the same `l` for every pass of `t`; every call of a method of `fsys`, or of a file opened through it, that the pass makes other than by making that `Log` pass MUST be one made by a `Decode` call of the pass, within the limits `R-GTCN-9NFH` sets, so that the content of the file at `p` is read only by `ReadAt` within the ranges `D04-sessions-and-table` gives each `Log` pass, and no byte of it is read by two passes of `t` unless the `Log` resets.
- R-6TBZ-GZ3V: The *records* of a pass MUST be the lines the `Log` pass returns, in their order, for which `json.Valid` reports true and whose first byte that is not JSON whitespace is `{`; a pass `t.Read(fsys)` MUST call `Decode` of its decoder exactly once for each of its records, in their order, passing `fsys` itself and the record's bytes unchanged, and MUST NOT pass any other line to any `Decode`, so that a line that is not a record is skipped and the records after it are decoded as usual.
- R-LHXB-43H9: `NewTranscript(path, recorded, newDecoder)` MUST call `newDecoder` exactly once when `path` is not empty and not at all when it is empty, and the decoder it returns MUST be the decoder of every pass of the new `Transcript` until a pass resets; a pass that resets MUST call `newDecoder` exactly once before any `Decode` call of that pass, and the decoder it returns MUST be the decoder of that pass and of every later pass until the next pass that resets; `Read` MUST make no other call to `newDecoder`.
- R-LJ57-HV7Y: A pass that returns a nil error MUST return as `entries` the entries the pass's `Decode` calls returned, in the order of the calls and, within one call, in the order returned, omitting exactly each entry whose `Kind` is not one of the seven `Kind` constants and each entry whose `Kind` is `KindUser`, `KindAssistant`, `KindReasoning`, or `KindAgent` and whose `Text` is empty, every entry kept being unchanged; a pass whose records' `Decode` calls return no entry that is kept MUST return a nil or empty `entries`.
- R-76XN-M9BN: `t.Usage()` MUST return, for each of the six fields, the sum over every `Decode` call of `t`'s passes made after `NewTranscript` returned `t` or after the latest of its passes that reset, whichever is later, of that call's value of the field, each call's negative value counting as 0 on its own before the sum; so that it is the zero `Usage` before any `Decode` call, no field of it is ever negative, a pass that resets counts only its own records, and two `Decode` calls returning `In` 5 and then `In` -3 give an `In` of 5, not 2.
- R-LMSW-N6G1: `Read` MUST return `reset` true if and only if the `session.Log` pass it made returned `reset` true and a nil error.
- R-LO0T-0Y6Q: When the `session.Log` pass returns a non-nil error `err` for which `errors.Is(err, fs.ErrNotExist)` holds, `Read` MUST return nil entries, `reset` false, and a nil error, and MUST call no `Decode` and no `newDecoder`, so that a transcript file that does not exist reads as a transcript with no records and leaves `Usage()` as it was.
- R-LP8P-EPXF: When the `session.Log` pass returns a non-nil error `err` for which `errors.Is(err, fs.ErrNotExist)` does not hold, `Read` MUST return nil entries, `reset` false, and a `*session.ReadError` whose `Path` is `t.Path()` and whose `Err` is `pe.Err` when `errors.As(err, &pe)` holds for a variable `pe` of type `*fs.PathError` and `err` otherwise, and MUST call no `Decode` and no `newDecoder`, so that `Usage()` and the decoder of later passes are as they were before the call.
- R-LQGL-SHO4: The `Chat` function of each of `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok` MUST return either a non-nil `*chat.Transcript`, its entries, and a nil error, or a nil `*chat.Transcript`, nil entries, and an error that is exactly the value `tree.ErrNotFound`, exactly the value `chat.ErrAgentNotFound`, or of dynamic type `*session.ReadError`.
- R-LROI-69ET: `Chat(root, home, sessionID, agentID)` of a harness package MUST return `tree.ErrNotFound` if and only if the same package's `Tree(root, home, sessionID)` over the same filesystem content returns `tree.ErrNotFound`, and MUST return a `*session.ReadError` whose `Path` is that harness's locating directory (`D09-tree`) if and only if that `Tree` call returns one, whatever `agentID` is.
- R-LSWE-K15I: When the harness's `Tree(root, home, sessionID)` over the same filesystem content returns a `tree.Tree` `t` and a nil error, `Chat(root, home, sessionID, agentID)` of that harness MUST return `chat.ErrAgentNotFound` if and only if `agentID` is not byte-for-byte equal to `sessionID` and is not byte-for-byte equal to the `ID` of any drawn subagent of `t` (`D09-tree`); so that the session id names the root, and the empty string, a proper prefix of a drawn subagent's id, and the id of a subagent of another session each name no agent.
- R-LU4A-XSW7: When the harness's `Tree(root, home, sessionID)` over the same filesystem content returns a `tree.Tree` `t` and a nil error and `agentID` is byte-for-byte equal to `sessionID` or to the `ID` of a drawn subagent of `t`, `Chat(root, home, sessionID, agentID)` MUST make, within the call, exactly one pass of a `*chat.Transcript` `tr` built by `chat.NewTranscript` with the named agent's transcript path as its harness design defines it, or the empty path when that design finds no transcript file for the agent; when that pass returns entries `e` and a nil error, `Chat` MUST return `tr`, `e`, and a nil error, and when it returns a non-nil error, `Chat` MUST return a nil transcript, nil entries, and that error.
- R-5HSP-OSAY: Within one call, the `Chat` function of `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok` MUST take every line it derives from a log file (a JSONL file their designs name as a log) from passes of `session.Log` values only — for the named agent's transcript, the one pass of the `Transcript` it builds; for a subagent usage log its `Decoder` reads, the passes of the `Log` that decoder retains for that file, as `R-GTCN-9NFH` states; for any other log file, one pass of a zero-value `session.Log` made for that file within the call — and MUST NOT read a log file's content in any other way, not through `fs.ReadFile` or a `ReadFile` method, not through an opened file's `Read` method, and not through any further pass, so that the content of each log file is obtained within the call only through `ReadAt` calls on files opened by those passes and no byte of any log is read twice; a `Stat` of a log file, through `fs.Stat` or otherwise, reads no content and is not restricted by this requirement.
- R-PO1U-SH9N: The `Path()` of every `*chat.Transcript` returned by a harness `Chat`, when not empty, and the `Path` of every `*session.ReadError` it returns MUST begin with `/`, equal `path.Clean` of itself, and not end in `/`; every `*session.ReadError` it returns MUST have a non-nil `Err` that is not a `*fs.PathError`, and when the failure is a `*fs.PathError` `pe` from the `fs.FS`, `Err` MUST be `pe.Err`.
- R-LXS0-344A: The `Recorded()` of every `*chat.Transcript` returned by one harness package's `Chat` MUST be the same value, the one that harness's design declares, whatever the session, the agent, and the filesystem content.
- R-PP9R-690C: The `Usage()` of a `*chat.Transcript` returned by a harness `Chat` MUST count only the model calls the named agent itself made, never a call made by a subagent of the named agent, at any depth, and, for a subagent, never a call recorded in history its transcript copies from its parent or from a session it resumes; where the agent's own transcript records usage that includes its subagents', the harness's design MUST say how the agent's own share is derived, and MAY derive it from the usage those subagents record in their own files.
- R-GTCN-9NFH: A `Decode(fsys, record)` call of a `Decoder` that a harness `Chat` hands to `chat.NewTranscript` MUST make calls through `fsys` only while decoding a record its harness design names as one that needs them, and then only to list or stat directories its harness design names for locating a subagent's usage files and to read files its harness design names as recording a subagent's usage; it MUST NOT open, stat, or read the file at the path of the `Transcript` it decodes for; for each file it reads that its harness design names as a log, the decoder MUST keep one `session.Log`, a zero-value `Log` when first used, for as long as the decoder itself is in use (so a reset, which replaces the decoder, discards it), and every read of that file by any `Decode` call of the decoder MUST be one pass of that retained `Log` and no other read, so that each read takes in only the bytes appended since that `Log`'s previous pass and no byte of the file is read twice unless the `Log` resets; and it MUST NOT write, create, or remove anything.
