# D02-cli-grammar

What `cli.Run` does with the arguments and the `System`
`D01-layout-and-run-seam` hands it: how the top level reads an argument, how
`list` and `tree` read the arguments after them, when each command looks at
the machine and which harness package it asks, the diagnostics for a usage
error, for data that cannot be read, for a session that does not exist, and
for output that cannot be written, and which exit code each outcome returns.
It also defines the two escaped forms in `internal/quote`. The help texts and
the version string themselves, and when they are written, are
`D03-help-and-version`.

## The top level

Arguments are read strictly left to right, and the first argument that
decides the outcome wins; nothing after it is looked at by the top level.
Every argument at the top level decides, so the first argument alone
determines everything, unless it is a command, `list` or `tree`, which hands
every argument after it to that command. An argument that begins with `-` is
an option. It is a known option only when it is exactly one of `--help`,
`-h`, `--version`, or `-V`; any other option is unknown and is named back as
typed, in escaped form. There is no bundling of short options and no
`--name=value` form, and `--` has no end-of-options meaning, so `-hV`,
`--help=x`, `-`, and `--` are all unknown options. An argument that does not
begin with `-` is a command; the commands are `list` and `tree`, spelled
exactly so, and every other, the empty string, `List`, and `Tree` included,
is an unknown command. So `agent-monitor --help list` and
`agent-monitor --help tree` print the top-level help, and
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
harness.

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

`tree` reads its own arguments the way `list` does, with one more place.
If any of them is exactly `--help` or `-h`, wherever it stands, the help of
`tree` wins over everything else (`D03`). Otherwise they are read left to
right and the first error wins: an argument beginning with `-` is an unknown
option (`tree`, too, takes no option but help); the first argument that is
not an option is the harness, which must be exactly `claude`, `codex`, or
`grok`; the second is the session id, taken as given, whatever its bytes;
any later argument that is not an option is unexpected. Only when no
argument is in error does a missing place count: with nothing after `tree`
the harness is missing, and with only a known harness the session id is
missing. So `agent-monitor tree claud` reports the unknown harness, not the
missing session id, and `agent-monitor tree claude --bogus` reports the
unknown option. The only arguments that draw a tree are therefore exactly
`tree`, one known harness, and one session id that does not begin with `-`.

The arguments are all checked before the environment and before any session
is looked for, exactly as for `list`: an empty `Home` fails with exit 3 and
reads nothing. Otherwise `tree` calls the chosen harness package's `Tree`
(`claude.Tree`, `codex.Tree`, or `grok.Tree`, declared in `D06`–`D08`) with
`System.Root`, `System.Home`, and the session id, and nothing else of any
harness. `D09-tree` closes what `Tree` may return: a tree and no error, the
error `tree.ErrNotFound`, or a `*session.ReadError`, and the two errors are
told apart without knowing the harness. On success `tree` prints
`tree.Draw` of the tree as its one product. When the session does not exist
it prints nothing on standard output and says so on standard error in one
line, naming the harness and echoing the id as an argument is echoed, with
exit 4, the one outcome only `tree` has. When the harness's locating
directory cannot be read it fails exactly as `list` does, with exit 3.

## Diagnostics and escaped forms

Every usage-error diagnostic follows the repository's command-line
conventions: the first line begins `agent-monitor: ` and names the offending
argument in single quotes (the missing harness and the missing session id
have none to name), then exactly one empty line, then the unprefixed hint to
run `agent-monitor --help`. The two exit-3 diagnostics and the exit-4
diagnostic are one line each. The usage text is never written to standard
error, and a failure writes nothing to standard output.

An argument is echoed in the form `quote.Arg` gives it, so that no argument
can forge output: ordinary printable text, ASCII or not, passes through
unchanged, so every plain argument reads exactly as typed, while `\`, `'`,
control bytes, DEL, bytes that are not valid UTF-8, and non-printable Unicode
characters (line and paragraph separators, bidirectional overrides, C1
controls, non-ASCII spaces) are escaped Go-style. A newline therefore never
reaches standard error from an argument, an empty argument shows as `''`, and
a terminal escape sequence is shown rather than rendered. The session id in
the not-found diagnostic is such an argument. `quote.Field` is the same form
with one difference: `'` passes through as typed, because a table field, a
tree label, or a path is printed without surrounding quotes. `session.Table`
and `tree.Draw` print their text with `quote.Field`, and the cannot-read
diagnostic prints its path with it.

## Writing

Every product — the help texts, the version line, the session table, the
drawn tree — is handed to standard output in one write, and every diagnostic
to standard error in one write, so a diagnostic lands whole and a failed
product write is one well-defined event. When that write fails, agent-monitor
says so on standard error with the write error's own text as the reason,
writes nothing more to standard output, and exits 1. If standard error cannot
be written either, there is nowhere left to report to: the exit code stays
the one the outcome already chose — 1, 2, 3, or 4 — and nothing is retried.

## REQUIREMENTS

- R-PM1Z-M9F6: Every call to `Run` MUST return one of `ExitSuccess`, `ExitWriteFailed`, `ExitUsage`, `ExitDataUnreadable`, or `ExitSessionNotFound`, and no other value.
- R-PN9W-015V: `Run` MUST classify `args[0]` as a known option if and only if it is byte-for-byte equal to one of `--help`, `-h`, `--version`, or `-V`; as the command `list` if and only if it is byte-for-byte equal to `list`; as the command `tree` if and only if it is byte-for-byte equal to `tree`; as an unknown option if and only if it begins with `-` and is not a known option; and as an unknown command if and only if it does not begin with `-` and is neither `list` nor `tree`, the empty string, `List`, and `Tree` included.
- R-POHS-DSWK: When `args` is not empty and `args[0]` is neither `list` nor `tree`, `Run` MUST behave exactly as it does for `args[:1]` — the same bytes written to `stdout`, the same bytes written to `stderr`, and the same return value — whatever the later elements of `args` are, so that `["--help", "list"]` and `["--help", "tree"]` behave as `["--help"]` and `["bogus", "tree"]` as `["bogus"]`.
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
- R-PPPO-RKN9: When `args[0]` is `tree` and no element of `args[1:]` is `--help` or `-h`, `Run` MUST classify each element of `args[1:]` as follows: an element that begins with `-` is an unknown option, `--version`, `-V`, `-`, and `--` included; the first element that does not begin with `-` is the harness argument, a known harness if and only if it is byte-for-byte equal to one of `claude`, `codex`, or `grok`, and otherwise an unknown harness, the empty string and `Claude` included; the second element that does not begin with `-` is the session-id argument, whatever its bytes, the empty string included; and every later element that does not begin with `-` is an unexpected argument.
- R-PQXL-5CDY: When `args[0]` is `tree`, no element of `args[1:]` is `--help` or `-h`, and some element of `args[1:]` is an unknown option, an unknown harness, or an unexpected argument, the first such element in order MUST decide the outcome and the elements after it MUST NOT affect it, so that `["tree", "claud"]`, `["tree", "claud", "--bogus"]`, and `["tree", "claud", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra"]` report unknown harness `claud`, `["tree", "--bogus", "claud"]`, `["tree", "claude", "--bogus"]`, and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "--bogus", "extra"]` report unknown option `--bogus`, and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra", "--bogus"]` and `["tree", "claude", "7c2e9a41-3b0d-4f6e-9a57-2d8c1e0b5f93", "extra", "more"]` report unexpected argument `extra`.
- R-PS5H-J44N: When `args[0]` is `tree` and the element of `args[1:]` that decides the outcome is an unknown option `arg`, `Run` MUST write exactly the unknown-option diagnostic `"agent-monitor: unknown option '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-PULA-ANM1: When `args[0]` is `tree` and the element of `args[1:]` that decides the outcome is an unknown harness `arg`, `Run` MUST write exactly the unknown-harness diagnostic `"agent-monitor: unknown harness '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-PVT6-OFCQ: When `args[0]` is `tree` and the element of `args[1:]` that decides the outcome is an unexpected argument `arg`, `Run` MUST write exactly the unexpected-argument diagnostic `"agent-monitor: unexpected argument '" + quote.Arg(arg) + "'\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-PX13-273F: When `args` is exactly `["tree"]`, `Run` MUST write exactly the missing-harness diagnostic `"agent-monitor: missing harness\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-PY8Z-FYU4: When `args` is exactly `["tree", h]` with `h` one of `claude`, `codex`, or `grok`, `Run` MUST write exactly the missing-session-id diagnostic `"agent-monitor: missing session id\n\nsee 'agent-monitor --help' for usage\n"` to `stderr`, write nothing to `stdout`, and return `ExitUsage`.
- R-PZGV-TQKT: Whenever `args` is neither exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok` nor exactly `["tree", h, id]` with `h` one of `claude`, `codex`, or `grok` and `id` a string that does not begin with `-`, the bytes `Run` writes to each stream and the value it returns MUST be the same for every `sys`, so that every usage error, the three help texts, and the version are reported identically when `sys.Home` is empty.
- R-Q0OS-7IBI: `Run` MUST make no call to any method of `sys.Root` unless `sys.Home` is not the empty string and `args` is either exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok` or exactly `["tree", h, id]` with `h` one of `claude`, `codex`, or `grok` and `id` a string that does not begin with `-`.
- R-ELPH-J8HK: When `args` is exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok` and `sys.Home` is the empty string, `Run` MUST write exactly the home-directory diagnostic `"agent-monitor: cannot find the home directory: HOME is not set\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`.
- R-EMXD-X089: When `args` is exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok` and `sys.Home` is not the empty string, `Run` MUST call the `List` function of the package `internal/harness/<h>` — `claude.List` for `claude`, `codex.List` for `codex`, `grok.List` for `grok` — exactly once, passing `sys.Root` and `sys.Home`, and MUST call the `List` function of no other harness package.
- R-EO5A-ARYY: When the harness `List` call `Run` makes returns sessions `s` and a nil error, `Run` MUST write exactly `session.Table(s)` to `stdout`, and, unless that write returns an error, MUST write nothing to `stderr` and return `ExitSuccess`.
- R-EPD6-OJPN: When the harness `List` call `Run` makes returns a non-nil error `err` such that `errors.As(err, &e)` holds for a variable `e` of type `*session.ReadError`, `Run` MUST write exactly the cannot-read diagnostic `"agent-monitor: cannot read " + quote.Field(e.Path) + ": " + e.Err.Error() + "\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`, so that, for example, a `Path` of `/home/dev/.claude/sessions` with a cause whose text is `permission denied` writes `agent-monitor: cannot read /home/dev/.claude/sessions: permission denied`.
- R-Q1WO-LA27: When `args` is exactly `["tree", h, id]` with `h` one of `claude`, `codex`, or `grok` and `id` a string that does not begin with `-`, and `sys.Home` is the empty string, `Run` MUST write exactly the home-directory diagnostic `"agent-monitor: cannot find the home directory: HOME is not set\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`.
- R-Q34K-Z1SW: When `args` is exactly `["tree", h, id]` with `h` one of `claude`, `codex`, or `grok` and `id` a string that does not begin with `-`, and `sys.Home` is not the empty string, `Run` MUST call the `Tree` function of the package `internal/harness/<h>` — `claude.Tree` for `claude`, `codex.Tree` for `codex`, `grok.Tree` for `grok` — exactly once, passing `sys.Root`, `sys.Home`, and `id`, and MUST call the `Tree` function of no other harness package and the `List` function of no harness package.
- R-Q4CH-CTJL: When the harness `Tree` call `Run` makes returns a tree `t` and a nil error, `Run` MUST write exactly `tree.Draw(t)` to `stdout`, and, unless that write returns an error, MUST write nothing to `stderr` and return `ExitSuccess`.
- R-Q5KD-QLAA: When the harness `Tree` call `Run` makes returns a non-nil error `err` such that `errors.As(err, &e)` holds for a variable `e` of type `*session.ReadError`, `Run` MUST write exactly the cannot-read diagnostic `"agent-monitor: cannot read " + quote.Field(e.Path) + ": " + e.Err.Error() + "\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`, so that, for example, a `Path` of `/home/dev/.codex/sessions` with a cause whose text is `permission denied` writes `agent-monitor: cannot read /home/dev/.codex/sessions: permission denied`.
- R-Q6SA-4D0Z: When the harness `Tree` call `Run` makes for `args` `["tree", h, id]` returns a non-nil error `err` for which `errors.Is(err, tree.ErrNotFound)` holds, `Run` MUST write exactly the session-not-found diagnostic `"agent-monitor: no " + h + " session '" + quote.Arg(id) + "'\n"` to `stderr`, write nothing to `stdout`, and return `ExitSessionNotFound`, so that `["tree", "claude", "bogus"]` naming no Claude Code session writes exactly `agent-monitor: no claude session 'bogus'` and a newline.
- R-2WJ5-1JXU: `Run` MUST deliver each product it writes to `stdout` as exactly one call to `stdout.Write`, and each diagnostic it writes to `stderr` as exactly one call to `stderr.Write`.
- R-2XR1-FBOJ: When the call `Run` makes to `stdout.Write` returns a non-nil error `err`, whichever product was being written, `Run` MUST make no further call to `stdout.Write`, MUST write exactly `"agent-monitor: write error: " + err.Error() + "\n"` to `stderr`, and MUST return `ExitWriteFailed`.
- R-2YYX-T3F8: When `Run` is writing the write-error diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitWriteFailed`.
- R-Q806-I4RO: When `Run` is writing an unknown-option, unknown-command, unknown-harness, unexpected-argument, missing-harness, or missing-session-id diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitUsage`.
- R-ERSZ-G371: When `Run` is writing the home-directory diagnostic or a cannot-read diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitDataUnreadable`.
- R-Q982-VWID: When `Run` is writing the session-not-found diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitSessionNotFound`.
- R-31EQ-KMWM: When `Run` returns `ExitUsage` it MUST have made no call to `stdout.Write`, and when it returns `ExitWriteFailed` its only call to `stdout.Write` MUST be the one that returned the error.
- R-ET0V-TUXQ: When `Run` returns `ExitDataUnreadable` it MUST have made no call to `stdout.Write`.
- R-QAFZ-9O92: When `Run` returns `ExitSessionNotFound` it MUST have made no call to `stdout.Write`.
- R-QBNV-NFZR: When `Run` returns `ExitSuccess` it MUST have made no call to `stderr.Write`, and every call `Run` makes to `stderr.Write` MUST pass exactly an unknown-option, unknown-command, unknown-harness, unexpected-argument, missing-harness, missing-session-id, home-directory, cannot-read, session-not-found, or write-error diagnostic, so that `Run` never writes `Usage`, `ListUsage`, or `TreeUsage` to `stderr`.
