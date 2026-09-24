# D02-cli-grammar

What `cli.Run` does with the arguments and the `System`
`D01-layout-and-run-seam` hands it: how the top level reads an argument, how
`list` reads the arguments after it, when `list` looks at the machine and
which harness package it asks, the diagnostics for a usage error, for data
that cannot be read, and for output that cannot be written, and which exit
code each outcome returns. It also defines the two escaped forms in
`internal/quote`. The help texts and the version string themselves, and when
they are written, are `D03-help-and-version`.

## The top level

Arguments are read strictly left to right, and the first argument that
decides the outcome wins; nothing after it is looked at by the top level.
Every argument at the top level decides, so the first argument alone
determines everything, unless it is the command `list`, which hands every
argument after it to `list`. An argument that begins with `-` is an option.
It is a known option only when it is exactly one of `--help`, `-h`,
`--version`, or `-V`; any other option is unknown and is named back as typed,
in escaped form. There is no bundling of short options and no
`--name=value` form, and `--` has no end-of-options meaning, so `-hV`,
`--help=x`, `-`, and `--` are all unknown options. An argument that does not
begin with `-` is a command; the only command is `list`, spelled exactly so,
and every other, the empty string included, is an unknown command. So
`agent-monitor --help list` prints the top-level help, and
`agent-monitor bogus list` fails on `bogus`. With no arguments at all,
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

## Diagnostics and escaped forms

Every usage-error diagnostic follows the repository's command-line
conventions: the first line begins `agent-monitor: ` and names the offending
argument in single quotes (the missing harness has none to name), then
exactly one empty line, then the unprefixed hint to run
`agent-monitor --help`. The two exit-3 diagnostics are one line each. The
usage text is never written to standard error, and a failure writes nothing
to standard output.

An argument is echoed in the form `quote.Arg` gives it, so that no argument
can forge output: ordinary printable text, ASCII or not, passes through
unchanged, so every plain argument reads exactly as typed, while `\`, `'`,
control bytes, DEL, bytes that are not valid UTF-8, and non-printable Unicode
characters (line and paragraph separators, bidirectional overrides, C1
controls, non-ASCII spaces) are escaped Go-style. A newline therefore never
reaches standard error from an argument, an empty argument shows as `''`, and
a terminal escape sequence is shown rather than rendered. `quote.Field` is the
same form with one difference: `'` passes through as typed, because a table
field or a path is printed without surrounding quotes. `session.Table` prints
its text fields with `quote.Field`, and the cannot-read diagnostic prints its
path with it.

## Writing

Every product — the help texts, the version line, the session table — is
handed to standard output in one write, and every diagnostic to standard
error in one write, so a diagnostic lands whole and a failed product write is
one well-defined event. When that write fails, agent-monitor says so on
standard error with the write error's own text as the reason, writes nothing
more to standard output, and exits 1. If standard error cannot be written
either, there is nowhere left to report to: the exit code stays the one the
outcome already chose — 1, 2, or 3 — and nothing is retried.

## REQUIREMENTS

- R-DZRA-ND52: Every call to `Run` MUST return one of `ExitSuccess`, `ExitWriteFailed`, `ExitUsage`, or `ExitDataUnreadable`, and no other value.
- R-E0Z7-14VR: `Run` MUST classify `args[0]` as a known option if and only if it is byte-for-byte equal to one of `--help`, `-h`, `--version`, or `-V`; as the command `list` if and only if it is byte-for-byte equal to `list`; as an unknown option if and only if it begins with `-` and is not a known option; and as an unknown command if and only if it does not begin with `-` and is not `list`, the empty string and `List` included.
- R-E273-EWMG: When `args` is not empty and `args[0]` is not `list`, `Run` MUST behave exactly as it does for `args[:1]` — the same bytes written to `stdout`, the same bytes written to `stderr`, and the same return value — whatever the later elements of `args` are, so that `["--help", "list"]` behaves as `["--help"]` and `["bogus", "list"]` as `["bogus"]`.
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
- R-EJ9O-RP06: Whenever `args` is not exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok`, the bytes `Run` writes to each stream and the value it returns MUST be the same for every `sys`, so that every usage error, both help texts, and the version are reported identically when `sys.Home` is empty.
- R-EKHL-5GQV: `Run` MUST make no call to any method of `sys.Root` unless `args` is exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok` and `sys.Home` is not the empty string.
- R-ELPH-J8HK: When `args` is exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok` and `sys.Home` is the empty string, `Run` MUST write exactly the home-directory diagnostic `"agent-monitor: cannot find the home directory: HOME is not set\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`.
- R-EMXD-X089: When `args` is exactly `["list", h]` with `h` one of `claude`, `codex`, or `grok` and `sys.Home` is not the empty string, `Run` MUST call the `List` function of the package `internal/harness/<h>` — `claude.List` for `claude`, `codex.List` for `codex`, `grok.List` for `grok` — exactly once, passing `sys.Root` and `sys.Home`, and MUST call the `List` function of no other harness package.
- R-EO5A-ARYY: When the harness `List` call `Run` makes returns sessions `s` and a nil error, `Run` MUST write exactly `session.Table(s)` to `stdout`, and, unless that write returns an error, MUST write nothing to `stderr` and return `ExitSuccess`.
- R-EPD6-OJPN: When the harness `List` call `Run` makes returns a non-nil error `err` such that `errors.As(err, &e)` holds for a variable `e` of type `*session.ReadError`, `Run` MUST write exactly the cannot-read diagnostic `"agent-monitor: cannot read " + quote.Field(e.Path) + ": " + e.Err.Error() + "\n"` to `stderr`, write nothing to `stdout`, and return `ExitDataUnreadable`, so that, for example, a `Path` of `/home/dev/.claude/sessions` with a cause whose text is `permission denied` writes `agent-monitor: cannot read /home/dev/.claude/sessions: permission denied`.
- R-2WJ5-1JXU: `Run` MUST deliver each product it writes to `stdout` as exactly one call to `stdout.Write`, and each diagnostic it writes to `stderr` as exactly one call to `stderr.Write`.
- R-2XR1-FBOJ: When the call `Run` makes to `stdout.Write` returns a non-nil error `err`, whichever product was being written, `Run` MUST make no further call to `stdout.Write`, MUST write exactly `"agent-monitor: write error: " + err.Error() + "\n"` to `stderr`, and MUST return `ExitWriteFailed`.
- R-2YYX-T3F8: When `Run` is writing the write-error diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitWriteFailed`.
- R-EQL3-2BGC: When `Run` is writing an unknown-option, unknown-command, unknown-harness, unexpected-argument, or missing-harness diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitUsage`.
- R-ERSZ-G371: When `Run` is writing the home-directory diagnostic or a cannot-read diagnostic and that call to `stderr.Write` returns a non-nil error, `Run` MUST make no further write to either stream and MUST return `ExitDataUnreadable`.
- R-31EQ-KMWM: When `Run` returns `ExitUsage` it MUST have made no call to `stdout.Write`, and when it returns `ExitWriteFailed` its only call to `stdout.Write` MUST be the one that returned the error.
- R-ET0V-TUXQ: When `Run` returns `ExitDataUnreadable` it MUST have made no call to `stdout.Write`.
- R-EU8S-7MOF: When `Run` returns `ExitSuccess` it MUST have made no call to `stderr.Write`, and every call `Run` makes to `stderr.Write` MUST pass exactly an unknown-option, unknown-command, unknown-harness, unexpected-argument, missing-harness, home-directory, cannot-read, or write-error diagnostic, so that `Run` never writes `Usage` or `ListUsage` to `stderr`.
