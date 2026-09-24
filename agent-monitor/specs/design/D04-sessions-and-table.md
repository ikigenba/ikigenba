# D04-sessions-and-table

`internal/session` is the vocabulary the three harness packages and the
command share when `agent-monitor list <harness>` runs. A harness package
turns what its harness left on disk into a list of `Session` values; the
command hands that list to `Table`, and what `Table` returns is the whole of
standard output. When the place a harness registers its live sessions
cannot be read, the harness package returns a `*ReadError` naming the path
it tried and the bare cause, and the command turns it into the
`cannot read` diagnostic. The package reaches nothing on the machine: it
formats and it splits bytes, and nothing else.

A session is its id, its status, the time it was last active, the directory
it works in, and its title. Status is one of three words — `working`,
`idle`, or `unknown` — so it is a named string type with one constant per
word, and what the table prints for it is the word itself. The time a
session was last active may not be known yet; that is said by a separate
flag rather than by the zero time, so that no time value is ever mistaken
for "absent".

The table is plain, aligned text. The header names five columns; every
column is as wide as its widest cell, header included, counted in
characters (runes) of the text as printed; two spaces separate columns; and
no line ends in a space, because a line stops right after its last
non-empty cell. LAST ACTIVE is always non-empty — a time or `-` — so a line
never stops before it. A row with no title therefore ends after its CWD, and
a row with neither CWD nor title ends after its LAST ACTIVE. The id, the
directory, and the title come from files other programs wrote, so they are
printed in the escaped form `quote.Field` produces (declared in
`D02-cli-grammar`): every row is exactly one line, and no cell can move the
terminal's cursor or forge another row. The time is printed in UTC to the
second, its fraction dropped rather than rounded, while ordering uses the
full recorded time, so two sessions a few milliseconds apart still sort
newest first even though they print the same second. Sessions whose time is
unknown sort after all the others. Ties sort by id, byte by byte, and rows
that tie on both keep the order they were handed in. With no sessions at
all, the table is the header alone, its columns as narrow as their names.

`ReadError` carries the absolute path a harness tried and the cause, as the
system described it, without the path repeated inside it; its message reads
`cannot read <path>: <cause>`. When a registry file holds something that is
not JSON, the cause is `ErrNotJSON`, whose text is `not valid JSON`.

Harness logs are JSON Lines files that another program may be appending to
while agent-monitor reads them, so the last line may be only half written.
`Lines` returns the complete lines of a log — each ended by a newline — and
leaves out a final fragment with no newline; a half-written last line is
therefore simply not seen, and the fields come from the last complete
record. Three terms the harness designs (`D06`, `D07`, `D08`) rely on are
defined here once, as requirements every harness `List` obeys: a *record*
of a log is a complete line that is a JSON object; a *timestamp* is a named
top-level string member that parses as RFC 3339 with optional fraction,
and one that does not parse counts as absent; and a log's *latest
timestamp* is the greatest of its records' timestamps — the maximum, not
the one written last, so a log whose lines are out of order still reports
its newest moment.

## REQUIREMENTS

- R-GNAW-66A5: The `internal/session` package MUST export the named type `type Status string`.
- R-GOIS-JY0U: The `internal/session` package MUST export the constants `StatusWorking Status = "working"`, `StatusIdle Status = "idle"`, and `StatusUnknown Status = "unknown"`, each declared with the type `Status`.
- R-GPQO-XPRJ: The `internal/session` package MUST export the struct type `type Session struct { ID string; Status Status; LastActive time.Time; HasLastActive bool; CWD string; Title string }`, with exactly these fields in this order.
- R-GQYL-BHI8: The `internal/session` package MUST export `func Table(sessions []Session) string`.
- R-GS6H-P98X: The `internal/session` package MUST export the struct type `type ReadError struct { Path string; Err error }`, with exactly these fields in this order.
- R-GTEE-30ZM: The `internal/session` package MUST export the method `func (e *ReadError) Error() string`, so that `*ReadError` implements `error` and `ReadError` (the non-pointer type) does not.
- R-W4QW-UJZU: The `internal/session` package MUST export the variable `ErrNotJSON` of type `error`, and its value MUST be non-nil.
- R-GVU6-UKH0: The `internal/session` package MUST export `func Lines(data []byte) [][]byte`.
- R-GX23-8C7P: For a `*ReadError` `e` whose `Err` is non-nil, `e.Error()` MUST return exactly `"cannot read " + e.Path + ": " + e.Err.Error()`, so that `(&ReadError{Path: "/home/dev/.claude/sessions", Err: fs.ErrPermission}).Error()` is `cannot read /home/dev/.claude/sessions: permission denied`.
- R-GZHV-ZVP3: `ErrNotJSON.Error()` MUST return exactly `not valid JSON`.
- R-H0PS-DNFS: Every `*ReadError` returned by the `List` function of `internal/harness/claude`, `internal/harness/codex`, or `internal/harness/grok` MUST have a `Path` that begins with `/`, equals `path.Clean` of itself, and does not end in `/`, and a non-nil `Err` that is not a `*fs.PathError`; when the failure is a `*fs.PathError` `pe` from the `fs.FS`, `Err` MUST be `pe.Err`.
- R-H1XO-RF6H: `Lines(data)` MUST return, in their order in `data`, the complete lines of `data` — each maximal run of bytes that contains no byte 0x0A and is immediately followed by a byte 0x0A — each without its terminating 0x0A and otherwise byte-for-byte unchanged (a 0x0D before the 0x0A is kept), empty lines included; the bytes after the last 0x0A in `data`, or all of `data` when it contains no 0x0A, MUST NOT be returned — so that `Lines([]byte("a\n\nb\nc"))` returns exactly `a`, the empty line, and `b`, and `Lines` of empty `data` or of `{"x":1` returns zero lines.
- R-H35L-56X6: `Table(sessions)` MUST return the header line followed by exactly one line per element of `sessions`, and nothing else, where every line, the last included, ends in exactly one `"\n"` and contains no other byte 0x0A.
- R-H4DH-IYNV: The header line of `Table` MUST have the cells `SESSION`, `STATUS`, `LAST ACTIVE`, `CWD`, and `TITLE`, in this order.
- R-H5LD-WQEK: The line `Table` writes for a `Session` `s` MUST have the cells, in this order, `quote.Field(s.ID)`, `string(s.Status)`, the LAST ACTIVE cell of `s`, `quote.Field(s.CWD)`, and `quote.Field(s.Title)`.
- R-H6TA-AI59: The LAST ACTIVE cell of a `Session` `s` MUST be `-` when `s.HasLastActive` is false, and otherwise `s.LastActive.UTC().Format("2006-01-02T15:04:05Z")`, so that fractional seconds are dropped, never rounded, whatever the location of `s.LastActive`: a `LastActive` of 2026-09-23T18:52:10.912-05:00 gives `2026-09-23T23:52:10Z`, and one of 2026-09-23T23:52:10.999999999Z gives `2026-09-23T23:52:10Z`.
- R-H816-O9VY: The width of each of the five columns of a `Table` output MUST be the largest `utf8.RuneCountInString` of that column's cells over the header line and every session line; each line MUST consist of, for every cell before its last non-empty cell, the cell followed by spaces up to its column's width and then exactly two spaces, followed by its last non-empty cell and then `"\n"`, so that no line contains a byte after its last non-empty cell other than the `"\n"` and no line ends in a space before the `"\n"`.
- R-H993-21MN: `Table` MUST order the session lines so that every session with `HasLastActive` true comes before every session with `HasLastActive` false, and sessions that both have `HasLastActive` true come in descending order of `LastActive` as compared by `time.Time.Compare`, the full time including fractional seconds, before truncation.
- R-HAGZ-FTDC: `Table` MUST order two sessions that tie — both with `HasLastActive` true and `LastActive` values for which `time.Time.Compare` returns 0, or both with `HasLastActive` false — by `ID` ascending in bytewise order of the raw `ID` strings (not of their escaped form), and sessions that also have equal `ID` MUST keep their relative order in `sessions`.
- R-HBOV-TL41: When `sessions` is empty or nil, `Table(sessions)` MUST return exactly `"SESSION  STATUS  LAST ACTIVE  CWD  TITLE\n"`.
- R-HCWS-7CUQ: `Table` MUST NOT modify `sessions` or any of its elements: the slice's length and every element are the same after the call as before it.
- R-HE4O-L4LF: A *record* of a log file MUST be an element of `Lines` applied to the file's whole content for which `json.Valid` reports true and whose first byte that is not JSON whitespace is `{`; the `List` function of `internal/harness/claude`, `internal/harness/codex`, and `internal/harness/grok` MUST take every value it derives from a log file (a JSONL file their designs name as a log) from the log's records only, ignoring every line that is not a record and the final fragment `Lines` does not return, so that a log whose last line is half written yields the same values as the same log without that fragment.
- R-HFCK-YWC4: A *timestamp* of a record under a member name `k` named by a harness design MUST be the time `t` for which `time.Parse(time.RFC3339Nano, s)` returns `t` and a nil error, where `s` is the JSON string value of the record's top-level member `k`; a record whose top-level member `k` is absent, is not a JSON string, or holds a string for which `time.Parse(time.RFC3339Nano, s)` returns an error MUST be treated by the harness `List` functions as having no timestamp under `k`, exactly as a record without that member.
- R-HHSD-QFTI: The *latest timestamp* of a log file under a member name `k` MUST be the greatest, as compared by `time.Time.Compare`, of the timestamps under `k` of the log's records, whatever their order in the file, and a log none of whose records has a timestamp under `k` has no latest timestamp; wherever a harness design sets a `Session`'s `LastActive` from a log's latest timestamp, the harness `List` MUST set `LastActive` to a time for which `time.Time.Compare` with the latest timestamp returns 0 and `HasLastActive` to true, and when the log has no latest timestamp MUST set `HasLastActive` to false.
