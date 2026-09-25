# D02-cli-grammar

What `cli.Run` does with the arguments and the `System`
`D01-layout-and-run-seam` hands it: how the top level reads an argument, how
`list`, `tree`, and `chat` read the arguments after them, when each command
looks at the machine and which harness package it asks, the diagnostics for
a usage error, for data that cannot be read, for a session or agent that
does not exist, and for output that cannot be written, and which exit code
each outcome returns.
It also defines the two escaped forms in `internal/quote`. The help texts and
the version string themselves, and when they are written, are
`D03-help-and-version`.

## The top level

Arguments are read strictly left to right, and the first argument that
decides the outcome wins; nothing after it is looked at by the top level.
Every argument at the top level decides, so the first argument alone
determines everything, unless it is a command, `list`, `tree`, or `chat`, which hands
every argument after it to that command. An argument that begins with `-` is
an option. It is a known option only when it is exactly one of `--help`,
`-h`, `--version`, or `-V`; any other option is unknown and is named back as
typed, in escaped form. There is no bundling of short options and no
`--name=value` form, and `--` has no end-of-options meaning, so `-hV`,
`--help=x`, `-`, and `--` are all unknown options. An argument that does not
begin with `-` is a command; the commands are `list`, `tree`, and `chat`,
spelled exactly so, and every other, the empty string, `List`, `Tree`, and
`Chat` included, is an unknown command. So `agent-monitor --help list` and
`agent-monitor --help chat` print the top-level help, and
`agent-monitor bogus tree` fails on `bogus`. With no arguments at all,
agent-monitor prints its help (`D03`).

## list

`list` reads its own arguments by its own rules. If any of them is exactly
`--help` or `-h`, wherever it stands, the help of `list` wins over everything
else (`D03`). Otherwise they are read left to right and the first error wins:
an argument beginning with `-` is an unknown option (`list` takes no option
but help, so the top-level `--version` is unknown here); the first argument
that is not an option is the harness, which must be exactly `claude`,
`codex`, or `grok`; any later argument that is not an option is unexpected.
With no argument after `list` at all, the harness is missing. The only
arguments that list sessions are therefore exactly `list` and one known
harness. The `--no-color` that `tree` takes is an unknown option here.

The arguments are checked before the environment: a usage error, the help,
and the version are the same whatever the `System` is, and none of them, nor
the missing-`HOME` failure, touches `System.Root`. Only then does `list` look
at `System.Home`. An empty `Home` — `HOME` unset or empty — fails with exit 3
and reads nothing; the home directory is never looked up elsewhere.
Otherwise `list` calls the chosen harness package's `List` (`claude.List`,
`codex.List`, or `grok.List`, declared in `D06`–`D08`) with `System.Root`
and `System.Home`. On success it prints `session.Table` of the sessions
(`D04`) — the header alone when there are none. When the harness's data
cannot be read, `List` returns a `*session.ReadError`, and `list` prints
nothing on standard output and names the path it tried and the cause on
standard error, with exit 3.

## tree

`tree` reads its own arguments the way `list` does, with one more place
and one option. If any of them is exactly `--help` or `-h`, wherever it
stands, the help of `tree` wins over everything else (`D03`). Otherwise they
are read left to right and the first error wins. An argument exactly
`--no-color` is the no-colour option: it may stand anywhere after `tree`, as
often as the developer likes, and it is never in error and never fills a
place, so it is skipped when the places are counted. Any other argument
beginning with `-` is an unknown option — `--no-color=x`, `--no-colour`, and
`--No-Color` included, since there is no `--name=value` form and spelling is
exact. The first argument that is not an option is the harness, which must
be exactly `claude`, `codex`, or `grok`; the second is the session id, taken
as given, whatever its bytes; any later argument that is not an option is
unexpected. Only when no argument is in error does a missing place count:
with nothing after `tree` but `--no-color`, if that, the harness is missing,
and with only a known harness the session id is missing. So
`agent-monitor tree claud` and `agent-monitor tree --no-color claud` report
the unknown harness, not the missing session id, and
`agent-monitor tree claude --bogus` reports the unknown option. The
arguments that draw a tree — the *drawing arguments* — are therefore `tree`,
one known harness, and one session id that does not begin with `-`, with
any number of `--no-color` among or after them. `--no-color` is an option of
`tree` only: before `tree` it is a top-level unknown option.

The arguments are all checked before the environment and before any session
is looked for, exactly as for `list`: an empty `Home` fails with exit 3 and
reads nothing. Otherwise `tree` calls the chosen harness package's `Tree`
(`claude.Tree`, `codex.Tree`, or `grok.Tree`, declared in `D06`–`D08`) with
`System.Root`, `System.Home`, and the session id, and nothing else of any
harness. `D09-tree` closes what `Tree` may return: a tree and no error, the
error `tree.ErrNotFound`, or a `*session.ReadError`, and the two errors are
told apart without knowing the harness. On success `tree` prints
`tree.Draw` of the tree as its one product, in colour or not (`D09` fixes
what each looks like). The choice is made here, from the arguments and the
`System` alone: colour is on only when `System.Terminal` says standard
output is a terminal, `System.NoColor` is empty, `System.Term` is not exactly
`dumb`, and no `--no-color` was given. A `NO_COLOR` set to the empty string
leaves colour on, and a `TERM` that is unset, or anything but `dumb` byte
for byte, does not by itself turn it off. When the session does not exist
it prints nothing on standard output and says so on standard error in one
line, naming the harness and echoing the id as an argument is echoed, with
exit 4, `ExitNotFound`. When the harness's locating
directory cannot be read it fails exactly as `list` does, with exit 3.

## chat

`chat` reads its own arguments the way `tree` does, with one more place and
no option but help. If any of them is exactly `--help` or `-h`, wherever it
stands, the help of `chat` wins over everything else (`D03`). Otherwise they
are read left to right and the first error wins. Every argument beginning
with `-` is an unknown option: `chat` never prints colour, so `--no-color`
is unknown here, and so are the top-level `--version` and `-V`. The first
argument that is not an option is the harness, which must be exactly
`claude`, `codex`, or `grok`; the second is the session id and the third the
agent id, each taken as given, whatever its bytes; any later argument that
is not an option is unexpected. Only when no argument is in error does a
missing place count: with nothing after `chat` the harness is missing, and
with only a known harness the session id is missing. The agent id is
optional. The arguments that print a chat — the *chat arguments* — are
therefore `chat`, one known harness, one session id, and at most one agent
id, none of them beginning with `-`.

The arguments are all checked before the environment and before any session
is looked for, exactly as for `tree`: an empty `Home` fails with exit 3 and
reads nothing. Otherwise `chat` calls the chosen harness package's `Chat`
(`claude.Chat`, `codex.Chat`, or `grok.Chat`, declared in `D10-chat`) once,
with `System.Root`, `System.Home`, the session id, and the agent id — or,
when no agent id was given, the session id again, since the root's id names
the root — and nothing else of any harness. `D10` closes what `Chat` may
return: a transcript that has made its first pass with that pass's entries,
or `tree.ErrNotFound`, `chat.ErrAgentNotFound`, or a `*session.ReadError`.
On success the one product is every entry in `chat.Format` form, in order,
followed by `chat.TotalsLine` of the transcript's usage and recorded counts;
`chat` makes no second pass, so what it prints is one snapshot. It never
colours, whatever the `System` says. A session that does not exist is
reported as for `tree`; an agent the session does not hold is reported in
one line naming the harness and echoing both ids as arguments are echoed;
both exit 4, `ExitNotFound`. Data that cannot be read — the locating
directory or the agent's transcript — fails as `list` does, with exit 3,
naming the path.

## Diagnostics and escaped forms

Every usage-error diagnostic follows the repository's command-line
conventions: the first line begins `agent-monitor: ` and names the offending
argument in single quotes (the missing harness and the missing session id
have none to name), then exactly one empty line, then the unprefixed hint to
run `agent-monitor --help`. The two exit-3 diagnostics and the two exit-4
diagnostics are one line each. The usage text is never written to standard
error, and a failure writes nothing to standard output.

An argument is echoed in the form `quote.Arg` gives it, so that no argument
can forge output: ordinary printable text, ASCII or not, passes through
unchanged, so every plain argument reads exactly as typed, while `\`, `'`,
control bytes, DEL, bytes that are not valid UTF-8, and non-printable Unicode
characters (line and paragraph separators, bidirectional overrides, C1
controls, non-ASCII spaces) are escaped Go-style. A newline therefore never
reaches standard error from an argument, an empty argument shows as `''`, and
a terminal escape sequence is shown rather than rendered. The session id in
the not-found diagnostics and the agent id in the agent-not-found
diagnostic are such arguments. `quote.Field` is the same form
with one difference: `'` passes through as typed, because a table field, a
tree label, or a path is printed without surrounding quotes. `session.Table`
and `tree.Draw` print their text with `quote.Field`, and the cannot-read
diagnostic prints its path with it.

## Writing

Every product — the help texts, the version line, the session table, the
drawn tree, the chat — is handed to standard output in one write, and every diagnostic
to standard error in one write, so a diagnostic lands whole and a failed
product write is one well-defined event. When that write fails, agent-monitor
says so on standard error with the write error's own text as the reason,
writes nothing more to standard output, and exits 1. If standard error cannot
be written either, there is nowhere left to report to: the exit code stays
the one the outcome already chose — 1, 2, 3, or 4 — and nothing is retried.

## REQUIREMENTS

- R-JHZ8-JMQA: Every call to `Run` MUST return one of `ExitSuccess`, `ExitWriteFailed`, `ExitUsage`, `ExitDataUnreadable`, or `ExitNotFound`, and no other value.
- R-JJ74-XEGZ: `Run` MUST classify `args[0]` as a known option if and only if it is byte-for-byte equal to one of `--help`, `-h`, `--version`, or `-V`; as the command `list` if and only if it is byte-for-byte equal to `list`; as the command `tree` if and only if it is byte-for-byte equal to `tree`; as the command `chat` if and only if it is byte-for-byte equal to `chat`; as an unknown option if and only if it begins with `-` and is not a known option; and as an unknown command if and only if it does not begin with `-` and is none of `list`, `tree`, and `chat`, the empty string, `List`, `Tree`, and `Chat` included.
- R-JKF1-B67O: When `args` is not empty and `args[0]` is none of `list`, `tree`, and `chat`, `Run` MUST behave exactly as it does for `args[:1]` — the same bytes written to `stdout`, the same bytes written to `stderr`, and the same return value — whatever the later elements of `args` are, so that `["--help", "list"]`, `["--help", "tree"]`, and `["--help", "chat"]` behave as `["--help"]` and `["bogus", "chat"]` as `["bogus"]`.
- R-2VB8-NS75: `Run` MUST treat each of `-`, `--`, `-hV`, `-Vh`, `--help=x`, `--version=x`, `--HELP`, and `-H` as an unknown option, and MUST give `--` no end-of-options meaning, so that `Run` with `args` `["--", "--help"]` reports unknown option `--`.
- R-E3EZ-SOD5: The `internal/quote` package MUST export `func Arg(s string) string`.
- R-E4MW-6G3U: The `internal/quote` package MUST export `func Field(s string) string`.
- R-XGG4-ZUCK: `quote.Arg(s)` MUST return the concatenation, reading `s` from its first byte to its last and taking at each position the element `utf8.DecodeRuneInString` decodes there, of: for a byte that does not begin a valid UTF-8 encoding (the decode yields `utf8.RuneError` with width 1), `\x` followed by the byte's value as two lowercase hexadecimal digits; for byte 0x5C (`\`), `\\`; for byte 0x27 (`'`), `\'`; for byte 0x0A, `\n`; for byte 0x09, `\t`; for byte 0x0D, `\r`; for any other byte 0x00–0x1F and for byte 0x7F, `\x` followed by the byte's value as two lowercase hexadecimal digits; for any other byte 0x20–0x7E, that byte unchanged; for a validly encoded rune `r` of U+0080 or above with `unicode.IsPrint(r)` true, its encoding unchanged; and for a validly encoded rune `r` of U+0080 or above with `unicode.IsPrint(r)` false, `\u` followed by `r` as four lowercase hexadecimal digits when `r` is at most U+FFFF, otherwise `\U` followed by `r` as eight lowercase hexadecimal digits — so that, for example, `bogus` maps to `bogus`, `a` TAB `b` to `a\tb`, `it's` to `it\'s`, `é` to `é`, the single byte 0xFF to `\xff`, ESC `[31m` to `\x1b[31m`, U+202E to `\u202e`, and U+E0001 to `\U000e0001`.
- R-XHO1-DM39: `quote.Field(s)` MUST return exactly the string `quote.Arg(s)` returns except that each byte 0x27 (`'`) of `s` is represented by the single byte `'` instead of by `\'`, so that, for example, `fix Bob's checkout` maps to `fix Bob's checkout`, `it's` TAB to `it's\t`, `a\b` to `a\\b`, and U+202E to `\u202e`.
- R-E8AL-BRBX: When `args[0]` is `list` and no element of `args[1:]` is `--help` or `-h`, `Run` MUST classify each element of `args[1:]` as follows: an element that begins with `-` is an unknown option, `--version`, `-V`, `-`, and `--` included; the first element that does not begin with `-` is the harness argument, a known harness if and only if it is byte-for-byte equal to one of `claude`, `codex`, or `grok`, and otherwise an unknown harness, the empty string and `Claude` included; and every later element that does not begin with `-` is an unexpected argument.
- R-E9IH-PJ2M: When `args[0]` is `list`, no element of `args[1:]` is `--help` or `-h`, and some element of `args[1:]` is an unknown option, an unknown harness, or an unexpected argument, the first such element in order MUST decide the outcome and the elements after it MUST NOT affect it, so that `["list", "claud", "--bogus"]` and `["list", "claud", "extra"]` report unknown harness `claud`, `["list", "--bogus", "claud"]` and `["list", "claude", "--bogus"]` report unknown option `--bogus`, and `["list", "claude", "extra", "more"]` reports unexpected argument `extra`.
- R-EBYA-H2K0: When `args[0]` is an unknown option `arg`, or `args[0]` is `list` and the element of `args[1:]` that decides the outcome is an unknown option `arg`, `Run` MUST write exactly the unknown-option diagnostic `"agent-monitor: unknown option '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-ED66-UUAP: When `args[0]` is an unknown command `arg`, `Run` MUST write exactly the unknown-command diagnostic `"agent-monitor: unknown command '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-EFLZ-MDS3: When `args[0]` is `list` and the element of `args[1:]` that decides the outcome is an unknown harness `arg`, `Run` MUST write exactly the unknown-harness diagnostic `"agent-monitor: unknown harness '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-EGTW-05IS: When `args[0]` is `list` and the element of `args[1:]` that decides the outcome is an unexpected argument `arg`, `Run` MUST write exactly the unexpected-argument diagnostic `"agent-monitor: unexpected argument '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-EI1S-DX9H: When `args` is exactly `["list"]`, `Run` MUST write exactly the missing-harness diagnostic `"agent-monitor: missing harness\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-69XU-63BD: `args` MUST be *drawing arguments*, with harness argument `h` and session-id argument `id`, if and only if `args[0]` is `tree` and the elements of `args[1:]` that are not byte-for-byte equal to `--no-color`, taken in order, are exactly `[h, id]` with `h` one of `claude`, `codex`, or `grok` and `id` a string that does not begin with `-`, so that `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, `["tree", "--no-color", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, `["tree", "claude", "--no-color", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "--no-color"]`, and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "--no-color"]` are drawing arguments with `h` `claude` and `id` `7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93`, while `["tree", "claude"]`, `["tree", "--no-color=x", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra"]` are not.
- R-6B5Q-JV22: When `args[0]` is `tree` and no element of `args[1:]` is `--help` or `-h`, `Run` MUST classify each element of `args[1:]` as follows: an element byte-for-byte equal to `--no-color` is the no-colour option, which is never an unknown option, a harness argument, a session-id argument, or an unexpected argument, however many times and wherever it appears; every other element that begins with `-` is an unknown option, `--no-color=x`, `--no-colour`, `--No-Color`, `--version`, `-V`, `-`, and `--` included; the first element that does not begin with `-` is the harness argument, a known harness if and only if it is byte-for-byte equal to one of `claude`, `codex`, or `grok`, and otherwise an unknown harness, the empty string and `Claude` included; the second element that does not begin with `-` is the session-id argument, whatever its bytes, the empty string included; and every later element that does not begin with `-` is an unexpected argument.
- R-PQXL-5CDY: When `args[0]` is `tree`, no element of `args[1:]` is `--help` or `-h`, and some element of `args[1:]` is an unknown option, an unknown harness, or an unexpected argument, the first such element in order MUST decide the outcome and the elements after it MUST NOT affect it, so that `["tree", "claud"]`, `["tree", "claud", "--bogus"]`, and `["tree", "claud", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra"]` report unknown harness `claud`, `["tree", "--bogus", "claud"]`, `["tree", "claude", "--bogus"]`, and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "--bogus", "extra"]` report unknown option `--bogus`, and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra", "--bogus"]` and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra", "more"]` report unexpected argument `extra`.
- R-PS5H-J44N: When `args[0]` is `tree` and the element of `args[1:]` that decides the outcome is an unknown option `arg`, `Run` MUST write exactly the unknown-option diagnostic `"agent-monitor: unknown option '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-PULA-ANM1: When `args[0]` is `tree` and the element of `args[1:]` that decides the outcome is an unknown harness `arg`, `Run` MUST write exactly the unknown-harness diagnostic `"agent-monitor: unknown harness '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-PVT6-OFCQ: When `args[0]` is `tree` and the element of `args[1:]` that decides the outcome is an unexpected argument `arg`, `Run` MUST write exactly the unexpected-argument diagnostic `"agent-monitor: unexpected argument '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-6CDM-XMSR: When `args[0]` is `tree` and every element of `args[1:]` is byte-for-byte equal to `--no-color`, there being zero or more of them, `Run` MUST write exactly the missing-harness diagnostic `"agent-monitor: missing harness\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`, so that `["tree"]`, `["tree", "--no-color"]`, and `["tree", "--no-color", "--no-color"]` each write it.
- R-6DLJ-BEJG: When `args[0]` is `tree` and the elements of `args[1:]` that are not byte-for-byte equal to `--no-color` are exactly `[h]` with `h` one of `claude`, `codex`, or `grok`, `Run` MUST write exactly the missing-session-id diagnostic `"agent-monitor: missing session id\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`, so that `["tree", "claude"]` and `["tree", "--no-color", "grok", "--no-color"]` each write it.
- R-JLMX-OXYD: Whenever `args` is neither exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok`, nor drawing arguments, nor chat arguments, the bytes `Run` writes to each stream and the value it returns MUST be the same for every `sys`, so that every usage error, the four help texts, and the version are reported identically when `sys.Home` is empty and whatever `sys.NoColor`, `sys.Term`, and `sys.Terminal` are.
- R-JO2Q-GHFR: `Run` MUST make no call to any method of `sys.Root` unless `sys.Home` is not the empty string and `args` is exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok`, drawing arguments, or chat arguments.
- R-ELPH-J8HK: When `args` is exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok` and `sys.Home` is the empty string, `Run` MUST write exactly the home-directory diagnostic `"agent-monitor: cannot find the home directory: HOME is not set\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`.
- R-EMXD-X089: When `args` is exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok` and `sys.Home` is not the empty string, `Run` MUST call the `List` function of the package `internal/harness/<h>` — `claude.List` for `claude`, `codex.List` for `codex`, `grok.List` for `grok` — exactly once, passing `sys.Root` and `sys.Home`, and MUST call the `List` function of no other harness package.
- R-EO5A-ARYY: When the harness `List` call `Run` makes returns sessions `s` and a nil error, `Run` MUST write exactly `session.Table(s)` to `stdout`, and, unless that write returns an error, MUST write nothing to `stderr` and return `ExitSuccess`.
- R-EPD6-OJPN: When the harness `List` call `Run` makes returns a non-nil error `err` such that `errors.As(err, &e)` holds for a variable `e` of type `*session.ReadError`, `Run` MUST write exactly the cannot-read diagnostic `"agent-monitor: cannot read " + quote.Field(e.Path) + ": " + e.Err.Error() + "\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`, so that, for example, a `Path` of `/home/dev/.claude/sessions` with a cause whose text is `permission denied` writes `agent-monitor: cannot read /home/dev/.claude/sessions: permission denied`.
- R-6H98-GPRJ: When `args` is drawing arguments and `sys.Home` is the empty string, `Run` MUST write exactly the home-directory diagnostic `"agent-monitor: cannot find the home directory: HOME is not set\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`.
- R-6IH4-UHI8: When `args` is drawing arguments with harness argument `h` and session-id argument `id`, and `sys.Home` is not the empty string, `Run` MUST call the `Tree` function of the package `internal/harness/<h>` — `claude.Tree` for `claude`, `codex.Tree` for `codex`, `grok.Tree` for `grok` — exactly once, passing `sys.Root`, `sys.Home`, and `id`, and MUST call the `Tree` function of no other harness package and the `List` function of no harness package.
- R-6JP1-898X: When the harness `Tree` call `Run` makes returns a tree `t` and a nil error, `Run` MUST write exactly `tree.Draw(t, c)` to `stdout`, where `c` is the colour decision for `args` and `sys`, and, unless that write returns an error, MUST write nothing to `stderr` and return `ExitSuccess`.
- R-6KWX-M0ZM: The colour decision for drawing arguments `args` and a `System` `sys` MUST be true if and only if `sys.Terminal` is true, `sys.NoColor` is the empty string, `sys.Term` is not byte-for-byte equal to `dumb`, and no element of `args[1:]` is byte-for-byte equal to `--no-color`, so that it is true for `Terminal` true, `NoColor` `""`, and each of `Term` `""`, `"xterm-256color"`, `"Dumb"`, and `"dumb "` with no `--no-color`, and false when, the rest being so, `Terminal` is false, or `NoColor` is `"1"` or `"0"`, or `Term` is `"dumb"`, or `args[1:]` holds `--no-color` once or more.
- R-Q5KD-QLAA: When the harness `Tree` call `Run` makes returns a non-nil error `err` such that `errors.As(err, &e)` holds for a variable `e` of type `*session.ReadError`, `Run` MUST write exactly the cannot-read diagnostic `"agent-monitor: cannot read " + quote.Field(e.Path) + ": " + e.Err.Error() + "\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`, so that, for example, a `Path` of `/home/dev/.codex/sessions` with a cause whose text is `permission denied` writes `agent-monitor: cannot read /home/dev/.codex/sessions: permission denied`.
- R-JPAM-U96G: When the harness `Tree` call `Run` makes for drawing arguments with harness argument `h` and session-id argument `id` returns a non-nil error `err` for which `errors.Is(err, tree.ErrNotFound)` holds, `Run` MUST write exactly the session-not-found diagnostic `"agent-monitor: no " + h + " session '" + quote.Arg(id) + "'\n"` to `stderr`, write nothing to `stdout`, and return `ExitNotFound`, so that `["tree", "claude", "bogus"]` and `["tree", "--no-color", "claude", "bogus"]` naming no Claude Code session each write exactly `agent-monitor: no claude session 'bogus'` and a newline.
- R-JQIJ-80X5: `args` MUST be *chat arguments*, with harness argument `h`, session-id argument `id`, and agent argument `a`, if and only if `args[0]` is `chat`, `args[1:]` has two or three elements, none of which begins with `-`, and `args[1]` is `h`, one of `claude`, `codex`, or `grok`; `id` is then `args[2]`, and `a` is `args[3]` when `args[1:]` has three elements and `id` when it has two; so that `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]` is chat arguments with `a` equal to `id`, `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a1c4e7f09b2d38561"]` is chat arguments with `a` `a1c4e7f09b2d38561`, `["chat", "codex", "", ""]` is chat arguments with `id` and `a` empty, and `["chat", "claude"]`, `["chat", "--no-color", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]`, and `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a1c4e7f09b2d38561", "extra"]` are not.
- R-JRQF-LSNU: When `args[0]` is `chat` and no element of `args[1:]` is `--help` or `-h`, `Run` MUST classify each element of `args[1:]` as follows: an element that begins with `-` is an unknown option, `--no-color`, `--version`, `-V`, `-`, and `--` included; the first element that does not begin with `-` is the harness argument, a known harness if and only if it is byte-for-byte equal to one of `claude`, `codex`, or `grok`, and otherwise an unknown harness, the empty string and `Claude` included; the second element that does not begin with `-` is the session-id argument and the third the agent argument, each whatever its bytes, the empty string included; and every later element that does not begin with `-` is an unexpected argument.
- R-JSYB-ZKEJ: When `args[0]` is `chat`, no element of `args[1:]` is `--help` or `-h`, and some element of `args[1:]` is an unknown option, an unknown harness, or an unexpected argument, the first such element in order MUST decide the outcome and the elements after it MUST NOT affect it, so that `["chat", "claud"]`, `["chat", "claud", "--bogus"]`, and `["chat", "claud", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]` report unknown harness `claud`, `["chat", "--bogus"]`, `["chat", "--bogus", "claud"]`, and `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "--bogus"]` report unknown option `--bogus`, `["chat", "--no-color", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93"]` reports unknown option `--no-color`, and `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a1c4e7f09b2d38561", "extra"]` and `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a1c4e7f09b2d38561", "extra", "more"]` report unexpected argument `extra`.
- R-JU68-DC58: When `args[0]` is `chat` and the element of `args[1:]` that decides the outcome is an unknown option `arg`, `Run` MUST write exactly the unknown-option diagnostic `"agent-monitor: unknown option '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-JVE4-R3VX: When `args[0]` is `chat` and the element of `args[1:]` that decides the outcome is an unknown harness `arg`, `Run` MUST write exactly the unknown-harness diagnostic `"agent-monitor: unknown harness '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-JWM1-4VMM: When `args[0]` is `chat` and the element of `args[1:]` that decides the outcome is an unexpected argument `arg`, `Run` MUST write exactly the unexpected-argument diagnostic `"agent-monitor: unexpected argument '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-JXTX-INDB: When `args` is exactly `["chat"]`, `Run` MUST write exactly the missing-harness diagnostic `"agent-monitor: missing harness\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-JZ1T-WF40: When `args` is exactly `["chat", h]` with `h` one of `claude`, `codex`, or `grok`, `Run` MUST write exactly the missing-session-id diagnostic `"agent-monitor: missing session id\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-K09Q-A6UP: When `args` is chat arguments and `sys.Home` is the empty string, `Run` MUST write exactly the home-directory diagnostic `"agent-monitor: cannot find the home directory: HOME is not set\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`.
- R-K1HM-NYLE: When `args` is chat arguments with harness argument `h`, session-id argument `id`, and agent argument `a`, and `sys.Home` is not the empty string, `Run` MUST call the `Chat` function of the package `internal/harness/<h>` — `claude.Chat` for `claude`, `codex.Chat` for `codex`, `grok.Chat` for `grok` — exactly once, passing `sys.Root`, `sys.Home`, `id`, and `a`, and MUST call the `Chat` function of no other harness package and the `List` and `Tree` functions of no harness package.
- R-K2PJ-1QC3: When the harness `Chat` call `Run` makes returns a transcript `t`, entries `e`, and a nil error, `Run` MUST write to `stdout` exactly the concatenation of `chat.Format(x)` for each element `x` of `e`, in order, followed by `chat.TotalsLine(t.Usage(), t.Recorded())`, MUST make no call to `t.Read`, and, unless that write returns an error, MUST write nothing to `stderr` and return `ExitSuccess`; so that entries `e` that are empty give exactly the totals line.
- R-K3XF-FI2S: When `args` is chat arguments, the bytes `Run` writes to each stream and the value it returns MUST be the same whatever `sys.NoColor`, `sys.Term`, and `sys.Terminal` are.
- R-K6D8-71K6: When the harness `Chat` call `Run` makes returns a non-nil error `err` such that `errors.As(err, &e)` holds for a variable `e` of type `*session.ReadError`, `Run` MUST write exactly the cannot-read diagnostic `"agent-monitor: cannot read " + quote.Field(e.Path) + ": " + e.Err.Error() + "\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`, so that, for example, a `Path` of `/home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a2d5f8e1c3b049672.jsonl` with a cause whose text is `permission denied` writes `agent-monitor: cannot read /home/dev/.claude/projects/-home-dev-src-shop/7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93/subagents/agent-a2d5f8e1c3b049672.jsonl: permission denied`.
- R-K7L4-KTAV: When the harness `Chat` call `Run` makes for chat arguments with harness argument `h` and session-id argument `id` returns a non-nil error `err` for which `errors.Is(err, tree.ErrNotFound)` holds, `Run` MUST write exactly the session-not-found diagnostic `"agent-monitor: no " + h + " session '" + quote.Arg(id) + "'\n"` to `stderr`, write nothing to `stdout`, and return `ExitNotFound`, so that `["chat", "claude", "bogus"]` and `["chat", "claude", "bogus", "a1c4e7f09b2d38561"]` naming no Claude Code session each write exactly `agent-monitor: no claude session 'bogus'` and a newline.
- R-K8T0-YL1K: When the harness `Chat` call `Run` makes for chat arguments with harness argument `h`, session-id argument `id`, and agent argument `a` returns a non-nil error `err` for which `errors.Is(err, chat.ErrAgentNotFound)` holds, `Run` MUST write exactly the agent-not-found diagnostic `"agent-monitor: no " + h + " agent '" + quote.Arg(a) + "' in session '" + quote.Arg(id) + "'\n"` to `stderr`, write nothing to `stdout`, and return `ExitNotFound`, so that `["chat", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "a0b1c2d3e4f506172"]` whose session holds no such agent writes exactly `agent-monitor: no claude agent 'a0b1c2d3e4f506172' in session '7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93'` and a newline.
- R-2WJ5-1JXU: `Run` MUST deliver each product it writes to `stdout` as exactly one call to `stdout.Write`, and each diagnostic it writes to `stderr` as exactly one call to `stderr.Write`.
- R-2XR1-FBOJ: When the call `Run` makes to `stdout.Write` returns a non-nil error `err`, whichever product was being written, `Run` MUST make no further call to `stdout.Write`, MUST write exactly `"agent-monitor: write error: " + err.Error() + "\n"` to `stderr`, and MUST return `ExitWriteFailed`.
- R-2YYX-T3F8: When `Run` is writing the write-error diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitWriteFailed`.
- R-Q806-I4RO: When `Run` is writing an unknown-option, unknown-command, unknown-harness, unexpected-argument, missing-harness, or missing-session-id diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitUsage`.
- R-ERSZ-G371: When `Run` is writing the home-directory diagnostic or a cannot-read diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitDataUnreadable`.
- R-KA0X-CCS9: When `Run` is writing the session-not-found or the agent-not-found diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitNotFound`.
- R-31EQ-KMWM: When `Run` returns `ExitUsage` it MUST have made no call to `stdout.Write`, and when it returns `ExitWriteFailed` its only call to `stdout.Write` MUST be the one that returned the error.
- R-ET0V-TUXQ: When `Run` returns `ExitDataUnreadable` it MUST have made no call to `stdout.Write`.
- R-KB8T-Q4IY: When `Run` returns `ExitNotFound` it MUST have made no call to `stdout.Write`.
- R-KCGQ-3W9N: When `Run` returns `ExitSuccess` it MUST have made no call to `stderr.Write`, and every call `Run` makes to `stderr.Write` MUST pass exactly an unknown-option, unknown-command, unknown-harness, unexpected-argument, missing-harness, missing-session-id, home-directory, cannot-read, session-not-found, agent-not-found, or write-error diagnostic, so that `Run` never writes `Usage`, `ListUsage`, `TreeUsage`, or `ChatUsage` to `stderr`.
